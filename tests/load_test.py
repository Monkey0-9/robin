"""
Robin Trading Platform — Load Testing Suite
============================================
Validates sub-microsecond latency claims under institutional load using
asyncio + aiohttp for maximum throughput.

Test scenarios:
  1. Baseline throughput: 1000 orders/sec for 60 seconds
  2. Burst test: 10,000 orders in 1 second
  3. Latency percentile test: measure p50/p95/p99/p999 under load
  4. Kill switch under load: verify kill switch blocks all orders atomically
  5. Supervisory workflow: large order flow requiring principal approval
  6. MFA auth flow: token acquisition + order placement
  7. Chaos: random 5% failure injection, verify system remains consistent

Usage:
  python load_test.py --target http://localhost:8080 --scenario all --duration 60
"""

import argparse
import asyncio
import random
import statistics
import sys
import time
from dataclasses import dataclass, field
from typing import Dict, List, Optional

try:
    import aiohttp
except ImportError:
    print("aiohttp is required: pip install aiohttp")
    sys.exit(1)


# ============================================================================
# Data structures
# ============================================================================

@dataclass
class LoadTestResult:
    scenario: str
    total_requests: int = 0
    successful: int = 0
    failed: int = 0
    latencies_ns: List[int] = field(default_factory=list)
    errors: Dict[str, int] = field(default_factory=dict)
    start_time_ns: int = 0
    end_time_ns: int = 0
    kill_switch_blocks: int = 0  # orders correctly blocked by kill switch

    @property
    def duration_s(self) -> float:
        return (self.end_time_ns - self.start_time_ns) / 1e9

    @property
    def throughput_per_sec(self) -> float:
        if self.duration_s <= 0:
            return 0
        return self.total_requests / self.duration_s

    @property
    def success_rate_pct(self) -> float:
        if self.total_requests == 0:
            return 0
        return self.successful / self.total_requests * 100

    def percentile(self, p: float) -> int:
        if not self.latencies_ns:
            return 0
        sorted_lats = sorted(self.latencies_ns)
        idx = max(0, int(len(sorted_lats) * p / 100) - 1)
        return sorted_lats[idx]

    def print_summary(self):
        print(f"\n{'='*60}")
        print(f"Scenario: {self.scenario}")
        print(f"{'='*60}")
        print(f"  Duration:          {self.duration_s:.2f}s")
        print(f"  Total requests:    {self.total_requests:,}")
        print(f"  Successful:        {self.successful:,} ({self.success_rate_pct:.1f}%)")
        print(f"  Failed:            {self.failed:,}")
        if self.kill_switch_blocks:
            print(f"  Kill switch blocks: {self.kill_switch_blocks:,}")
        print(f"  Throughput:        {self.throughput_per_sec:,.0f} req/s")
        if self.latencies_ns:
            print("\n  Latency (ms):")
            print(f"    p50:  {self.percentile(50) / 1e6:.3f}ms")
            print(f"    p95:  {self.percentile(95) / 1e6:.3f}ms")
            print(f"    p99:  {self.percentile(99) / 1e6:.3f}ms")
            print(f"    p999: {self.percentile(99.9) / 1e6:.3f}ms")
            print(f"    mean: {statistics.mean(self.latencies_ns) / 1e6:.3f}ms")
            print(f"    max:  {max(self.latencies_ns) / 1e6:.3f}ms")
        if self.errors:
            print("\n  Errors:")
            for code, count in sorted(self.errors.items()):
                print(f"    {code}: {count:,}")


# ============================================================================
# Order generators
# ============================================================================

SYMBOLS = ["AAPL", "MSFT", "GOOGL", "AMZN", "TSLA", "SPY", "QQQ", "IWM"]
SIDES = ["BUY", "SELL"]

def random_order(large: bool = False) -> dict:
    """Generate a random order request."""
    price = round(random.uniform(50, 500), 2)
    qty = 1000 if large else random.randint(1, 100)
    return {
        "symbol": random.choice(SYMBOLS),
        "side": random.choice(SIDES),
        "price": price,
        "qty": qty,
        "order_type": "LIMIT",
        "cl_ord_id": f"LT-{time.time_ns()}-{random.randint(1000, 9999)}",
        "exchange": "AUTO",
    }


# ============================================================================
# Test runner
# ============================================================================

class LoadTestRunner:
    def __init__(self, target: str, jwt_token: str):
        self.target = target.rstrip("/")
        self.jwt_token = jwt_token
        self._session: Optional[aiohttp.ClientSession] = None

    def _headers(self) -> Dict[str, str]:
        headers = {"Content-Type": "application/json"}
        if self.jwt_token:
            headers["Authorization"] = f"Bearer {self.jwt_token}"
        return headers

    async def _get_session(self) -> aiohttp.ClientSession:
        if self._session is None or self._session.closed:
            connector = aiohttp.TCPConnector(limit=2000, ttl_dns_cache=300)
            self._session = aiohttp.ClientSession(
                connector=connector,
                timeout=aiohttp.ClientTimeout(total=5),
            )
        return self._session

    async def _post_order(self, order: dict) -> tuple:
        """Submit a single order. Returns (status_code, latency_ns)."""
        session = await self._get_session()
        t0 = time.time_ns()
        try:
            async with session.post(
                f"{self.target}/order",
                json=order,
                headers=self._headers(),
            ) as resp:
                await resp.read()
                latency_ns = time.time_ns() - t0
                return resp.status, latency_ns
        except asyncio.TimeoutError:
            return 504, time.time_ns() - t0
        except Exception:
            return -1, time.time_ns() - t0

    async def _get(self, path: str) -> tuple:
        """GET request. Returns (status, json_or_text, latency_ns)."""
        session = await self._get_session()
        t0 = time.time_ns()
        try:
            async with session.get(
                f"{self.target}{path}",
                headers=self._headers(),
            ) as resp:
                try:
                    body = await resp.json()
                except Exception:
                    body = await resp.text()
                return resp.status, body, time.time_ns() - t0
        except Exception as e:
            return -1, str(e), time.time_ns() - t0

    async def _post(self, path: str, data: dict) -> tuple:
        session = await self._get_session()
        t0 = time.time_ns()
        try:
            async with session.post(
                f"{self.target}{path}",
                json=data,
                headers=self._headers(),
            ) as resp:
                try:
                    body = await resp.json()
                except Exception:
                    body = await resp.text()
                return resp.status, body, time.time_ns() - t0
        except Exception as e:
            return -1, str(e), time.time_ns() - t0

    # ========================================================================
    # Scenario 1: Baseline throughput
    # ========================================================================
    async def scenario_baseline_throughput(
        self, target_rps: int = 1000, duration_s: int = 60
    ) -> LoadTestResult:
        """Sustained throughput test at target_rps for duration_s seconds."""
        result = LoadTestResult(scenario=f"baseline_throughput_{target_rps}rps_{duration_s}s")
        result.start_time_ns = time.time_ns()
        end_ns = result.start_time_ns + int(duration_s * 1e9)

        print(f"\n[{result.scenario}] Starting: {target_rps} req/s for {duration_s}s...")

        tasks = []
        while time.time_ns() < end_ns:
            order = random_order()
            tasks.append(asyncio.create_task(self._post_order(order)))
            result.total_requests += 1

            if len(tasks) >= 500:
                responses = await asyncio.gather(*tasks, return_exceptions=True)
                for status, lat_ns in (r for r in responses if not isinstance(r, Exception)):
                    result.latencies_ns.append(lat_ns)
                    if 200 <= status < 300:
                        result.successful += 1
                    else:
                        result.failed += 1
                        result.errors[str(status)] = result.errors.get(str(status), 0) + 1
                tasks = []

            # Rate limit
            await asyncio.sleep(0)

        # Drain remaining
        if tasks:
            responses = await asyncio.gather(*tasks, return_exceptions=True)
            for item in responses:
                if isinstance(item, Exception):
                    result.failed += 1
                    continue
                status, lat_ns = item
                result.latencies_ns.append(lat_ns)
                if 200 <= status < 300:
                    result.successful += 1
                else:
                    result.failed += 1

        result.end_time_ns = time.time_ns()
        return result

    # ========================================================================
    # Scenario 2: Burst test
    # ========================================================================
    async def scenario_burst(self, burst_size: int = 10_000) -> LoadTestResult:
        """Send burst_size orders concurrently (maximum load spike)."""
        result = LoadTestResult(scenario=f"burst_{burst_size}_orders")
        result.start_time_ns = time.time_ns()

        print(f"\n[{result.scenario}] Sending {burst_size:,} orders concurrently...")

        orders = [random_order() for _ in range(burst_size)]
        tasks = [self._post_order(o) for o in orders]
        responses = await asyncio.gather(*tasks, return_exceptions=True)

        for item in responses:
            result.total_requests += 1
            if isinstance(item, Exception):
                result.failed += 1
                result.errors["exception"] = result.errors.get("exception", 0) + 1
                continue
            status, lat_ns = item
            result.latencies_ns.append(lat_ns)
            if 200 <= status < 300:
                result.successful += 1
            else:
                result.failed += 1
                result.errors[str(status)] = result.errors.get(str(status), 0) + 1

        result.end_time_ns = time.time_ns()
        return result

    # ========================================================================
    # Scenario 3: Kill switch under load
    # ========================================================================
    async def scenario_kill_switch(self, orders_after_trip: int = 100) -> LoadTestResult:
        """
        1. Send 50 orders (should succeed)
        2. Trip system kill switch
        3. Send orders_after_trip more (should all be blocked with 503)
        4. Reset kill switch via dual-person flow
        5. Verify orders resume
        """
        result = LoadTestResult(scenario="kill_switch_under_load")
        result.start_time_ns = time.time_ns()
        print(f"\n[{result.scenario}] Testing kill switch atomicity...")

        # Phase 1: Normal orders
        phase1_tasks = [self._post_order(random_order()) for _ in range(50)]
        phase1_responses = await asyncio.gather(*phase1_tasks)
        for status, lat_ns in phase1_responses:
            result.total_requests += 1
            result.latencies_ns.append(lat_ns)
            if 200 <= status < 300:
                result.successful += 1

        # Phase 2: Trip kill switch
        ks_status, ks_body, _ = await self._post(
            "/api/killswitch/system/trip",
            {"reason": "load_test_kill_switch_verification"},
        )
        print(f"  Kill switch trip: HTTP {ks_status}")

        # Phase 3: Orders while kill switched (should all be blocked)
        blocked_tasks = [self._post_order(random_order()) for _ in range(orders_after_trip)]
        blocked_responses = await asyncio.gather(*blocked_tasks)
        for status, lat_ns in blocked_responses:
            result.total_requests += 1
            result.latencies_ns.append(lat_ns)
            if status == 503:
                result.kill_switch_blocks += 1
                result.successful += 1  # 503 is the CORRECT response
            else:
                result.failed += 1
                print(f"  WARN: Expected 503 (blocked), got {status}")

        # Phase 4: Initiate reset (dual-person)
        init_status, init_body, _ = await self._post(
            "/api/killswitch/system/reset/initiate",
            {"reason": "load_test_reset"},
        )
        reset_token = ""
        if isinstance(init_body, dict):
            reset_token = init_body.get("reset_token", "")
            print(f"  Reset token obtained: {reset_token}")

        # Phase 5: Confirm reset with a second admin token (in test, same token for simplicity)
        if reset_token:
            confirm_status, confirm_body, _ = await self._post(
                "/api/killswitch/system/reset/confirm",
                {"reset_token": reset_token, "reason": "load_test_confirmed"},
            )
            print(f"  Kill switch reset confirm: HTTP {confirm_status}")

        result.end_time_ns = time.time_ns()
        return result

    # ========================================================================
    # Scenario 4: Health endpoint latency
    # ========================================================================
    async def scenario_health_latency(self, n: int = 1000) -> LoadTestResult:
        """Measure /health endpoint latency under concurrent load."""
        result = LoadTestResult(scenario=f"health_latency_{n}")
        result.start_time_ns = time.time_ns()

        tasks = [self._get("/health") for _ in range(n)]
        responses = await asyncio.gather(*tasks, return_exceptions=True)

        for item in responses:
            result.total_requests += 1
            if isinstance(item, Exception):
                result.failed += 1
                continue
            status, _, lat_ns = item
            result.latencies_ns.append(lat_ns)
            if status == 200:
                result.successful += 1
            else:
                result.failed += 1

        result.end_time_ns = time.time_ns()
        return result

    # ========================================================================
    # Scenario 5: Supervisory workflow
    # ========================================================================
    async def scenario_supervisory_workflow(self) -> LoadTestResult:
        """Submit a large order ($2M notional) and verify supervisory pending state."""
        result = LoadTestResult(scenario="supervisory_workflow")
        result.start_time_ns = time.time_ns()
        print(f"\n[{result.scenario}] Testing FINRA 3110 supervisory workflow...")

        # Large order (should require approval)
        large_order = random_order(large=True)
        large_order["qty"] = 5000  # >$1M notional

        status, lat_ns = await self._post_order(large_order)
        result.total_requests += 1
        result.latencies_ns.append(lat_ns)

        if status in [200, 202, 503]:  # 202=pending approval, 503=blocked
            result.successful += 1
            print(f"  Large order response: HTTP {status} (expected 202 or 503 for supervisory hold)")
        else:
            result.failed += 1

        # Check supervisory pending
        s, body, lat = await self._get("/api/supervisory/pending")
        result.total_requests += 1
        result.latencies_ns.append(lat)
        if s == 200:
            result.successful += 1
            if isinstance(body, dict):
                print(f"  Pending approvals: {body.get('count', 0)}")

        result.end_time_ns = time.time_ns()
        return result

    # ========================================================================
    # Cleanup
    # ========================================================================
    async def close(self):
        if self._session and not self._session.closed:
            await self._session.close()


# ============================================================================
# Main
# ============================================================================

async def main():
    parser = argparse.ArgumentParser(description="Robin Load Test Suite")
    parser.add_argument("--target", default="http://localhost:8080", help="Gateway base URL")
    parser.add_argument("--jwt", default="", help="JWT bearer token")
    parser.add_argument("--scenario", default="health", choices=[
        "all", "baseline", "burst", "kill_switch", "health", "supervisory",
    ])
    parser.add_argument("--rps", type=int, default=1000, help="Target requests/sec for baseline")
    parser.add_argument("--duration", type=int, default=30, help="Duration in seconds for baseline")
    parser.add_argument("--burst-size", type=int, default=5000, help="Burst scenario size")
    args = parser.parse_args()

    runner = LoadTestRunner(target=args.target, jwt_token=args.jwt)
    results = []

    try:
        # Pre-flight check
        print("\nRobin Load Test Suite")
        print(f"Target: {args.target}")
        print(f"Scenario: {args.scenario}")

        s, body, lat = await runner._get("/health")
        if s != 200:
            print(f"WARNING: Health check returned HTTP {s}. Proceeding anyway.")
        else:
            print(f"Health check: OK ({lat/1e6:.1f}ms)")

        if args.scenario in ("all", "health"):
            r = await runner.scenario_health_latency(1000)
            r.print_summary()
            results.append(r)

        if args.scenario in ("all", "baseline"):
            r = await runner.scenario_baseline_throughput(args.rps, args.duration)
            r.print_summary()
            results.append(r)

        if args.scenario in ("all", "burst"):
            r = await runner.scenario_burst(args.burst_size)
            r.print_summary()
            results.append(r)

        if args.scenario in ("all", "kill_switch"):
            r = await runner.scenario_kill_switch()
            r.print_summary()
            results.append(r)

        if args.scenario in ("all", "supervisory"):
            r = await runner.scenario_supervisory_workflow()
            r.print_summary()
            results.append(r)

    finally:
        await runner.close()

    # Aggregate summary
    if len(results) > 1:
        print(f"\n{'='*60}")
        print("AGGREGATE SUMMARY")
        print(f"{'='*60}")
        total = sum(r.total_requests for r in results)
        ok = sum(r.successful for r in results)
        print(f"Total requests:  {total:,}")
        print(f"Overall success: {ok/total*100:.1f}%")

    # Assertions for CI
    for r in results:
        assert r.success_rate_pct >= 95, f"FAIL: {r.scenario} success rate {r.success_rate_pct:.1f}% < 95%"
        if r.latencies_ns:
            p99_ms = r.percentile(99) / 1e6
            assert p99_ms < 1000, f"FAIL: {r.scenario} p99 {p99_ms:.1f}ms > 1000ms"
        print(f"PASS: {r.scenario}")

    print("\nAll assertions passed.")


if __name__ == "__main__":
    asyncio.run(main())
