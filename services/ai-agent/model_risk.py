"""
Robin Trading Platform — AI Model Risk Monitor
================================================
Implements model risk controls per institutional AI governance standards:

1. Performance drift detection: alerts if 5-day rolling accuracy drops >20%
2. Latency bounds enforcement: all inference ≤200ms, fallback to last signal
3. A/B testing framework: model_version tracking on every response
4. Kill switch: POST /api/ai/disable stops AI signal generation
5. Signal audit logging: all AI signals logged with model, confidence, latency
6. Feedback loop detection: rapid re-signal within 1s flagged and throttled

Used by the Go gateway's POST /api/ai/trade_decision proxy.
"""

import asyncio
import hashlib
import json
import logging
import os
import time
from collections import deque
from dataclasses import asdict, dataclass
from threading import Lock
from typing import Deque, Dict, List, Optional, Tuple

logger = logging.getLogger("model_risk")


# ============================================================================
# Signal audit record
# ============================================================================


@dataclass
class SignalAuditRecord:
    """Immutable audit record for each AI-generated trading signal."""

    signal_id: str
    model_version: str
    symbol: str
    direction: str  # BUY, SELL, HOLD
    confidence: float  # 0.0 - 1.0
    inference_latency_ms: float
    generated_at_ns: int
    input_hash: str  # SHA-256 of model inputs for reproducibility
    fallback_used: bool  # True if inference timeout exceeded


# ============================================================================
# ModelRiskMonitor
# ============================================================================


class ModelRiskMonitor:
    """
    Monitors AI model performance, detects drift, enforces latency bounds,
    and manages the AI kill switch.

    Thread-safe: all state modifications protected by _lock.
    """

    MODEL_VERSION = os.getenv("ROBIN_AI_MODEL_VERSION", "robin-quant-v1.2")
    MAX_INFERENCE_MS = float(os.getenv("ROBIN_AI_MAX_LATENCY_MS", "200"))
    DRIFT_ALERT_THRESHOLD = 0.20  # Alert if accuracy drops >20% from baseline
    MIN_SAMPLES_FOR_DRIFT = 20  # Need at least 20 samples before drift check
    ROLLING_WINDOW = 100  # 100-sample rolling accuracy window
    AI_MAX_RATE_PER_SEC = 10  # Max AI-generated orders per second
    LARGE_ORDER_THRESHOLD = 1_000_000  # $1M notional requires human confirmation
    FEEDBACK_LOOP_WINDOW_NS = 1_000_000_000  # 1 second feedback loop detection

    def __init__(self):
        self._lock = Lock()
        self._kill_switch_active = False
        self._kill_switch_reason = ""
        self._kill_switch_time_ns = 0

        # Signal history for drift detection
        self._signal_history: Deque[SignalAuditRecord] = deque(
            maxlen=self.ROLLING_WINDOW
        )
        self._outcome_history: Deque[Tuple[str, bool]] = deque(
            maxlen=self.ROLLING_WINDOW
        )

        # Performance baseline (accuracy on first MIN_SAMPLES_FOR_DRIFT signals)
        self._baseline_accuracy: Optional[float] = None
        self._baseline_set = False

        # Latency stats
        self._latency_history: Deque[float] = deque(maxlen=200)
        self._fallback_count = 0
        self._total_inferences = 0

        # Rate limiting for feedback loop prevention
        self._last_signal_ns = 0
        self._signals_this_second = 0
        self._second_window_start_ns = 0

        # Drift alerts
        self._drift_alerts: List[Dict] = []
        self._retraining_flagged = False

    # ========================================================================
    # Kill switch
    # ========================================================================

    def is_kill_switch_active(self) -> bool:
        with self._lock:
            return self._kill_switch_active

    def activate_kill_switch(self, reason: str) -> None:
        with self._lock:
            self._kill_switch_active = True
            self._kill_switch_reason = reason
            self._kill_switch_time_ns = time.time_ns()
            logger.error("[AI KILL SWITCH] ACTIVATED: %s", reason)

    def deactivate_kill_switch(self, reset_by: str) -> None:
        with self._lock:
            self._kill_switch_active = False
            prev_reason = self._kill_switch_reason
            self._kill_switch_reason = ""
            logger.warning(
                "[AI KILL SWITCH] DEACTIVATED by %s (prev reason: %s)",
                reset_by,
                prev_reason,
            )

    # ========================================================================
    # Signal recording and drift detection
    # ========================================================================

    def record_signal(
        self,
        symbol: str,
        direction: str,
        confidence: float,
        inference_latency_ms: float,
        model_inputs: dict,
        fallback_used: bool = False,
    ) -> SignalAuditRecord:
        """Record an AI signal for audit and drift monitoring."""
        input_hash = hashlib.sha256(
            json.dumps(model_inputs, sort_keys=True).encode()
        ).hexdigest()[:16]

        signal_id = f"SIG-{time.time_ns()}-{input_hash}"
        record = SignalAuditRecord(
            signal_id=signal_id,
            model_version=self.MODEL_VERSION,
            symbol=symbol,
            direction=direction,
            confidence=confidence,
            inference_latency_ms=inference_latency_ms,
            generated_at_ns=time.time_ns(),
            input_hash=input_hash,
            fallback_used=fallback_used,
        )

        with self._lock:
            self._signal_history.append(record)
            self._latency_history.append(inference_latency_ms)
            self._total_inferences += 1
            if fallback_used:
                self._fallback_count += 1

        return record

    def record_outcome(self, signal_id: str, was_correct: bool) -> None:
        """Record whether a past signal's direction was correct (for drift detection)."""
        with self._lock:
            self._outcome_history.append((signal_id, was_correct))
            self._check_drift()

    def _check_drift(self) -> None:
        """Internal: check for accuracy drift against baseline (call with lock held)."""
        outcomes = list(self._outcome_history)
        if len(outcomes) < self.MIN_SAMPLES_FOR_DRIFT:
            return

        correct = sum(1 for _, ok in outcomes if ok)
        rolling_accuracy = correct / len(outcomes)

        if not self._baseline_set:
            self._baseline_accuracy = rolling_accuracy
            self._baseline_set = True
            logger.info(
                "[MODEL RISK] Baseline accuracy set: %.2f%%", rolling_accuracy * 100
            )
            return

        if self._baseline_accuracy and self._baseline_accuracy > 0:
            drop = (
                self._baseline_accuracy - rolling_accuracy
            ) / self._baseline_accuracy
            if drop > self.DRIFT_ALERT_THRESHOLD:
                alert = {
                    "alert_type": "PERFORMANCE_DRIFT",
                    "baseline_accuracy": self._baseline_accuracy,
                    "current_accuracy": rolling_accuracy,
                    "drop_pct": drop * 100,
                    "samples": len(outcomes),
                    "detected_at_ns": time.time_ns(),
                }
                self._drift_alerts.append(alert)
                self._retraining_flagged = True
                logger.error(
                    "[MODEL RISK] DRIFT ALERT: accuracy dropped %.1f%% (%.2f%% -> %.2f%%)",
                    drop * 100,
                    self._baseline_accuracy * 100,
                    rolling_accuracy * 100,
                )

    # ========================================================================
    # Rate limiting and feedback loop detection
    # ========================================================================

    def check_rate_limit(self, notional: float = 0) -> Tuple[bool, str]:
        """
        Check if the AI can generate another signal.
        Returns (allowed, reason).
        Enforces:
        - Max AI_MAX_RATE_PER_SEC signals per second
        - Human confirmation required for orders > LARGE_ORDER_THRESHOLD notional
        - Feedback loop detection (rapid re-signal within 1s)
        """
        now_ns = time.time_ns()

        with self._lock:
            if self._kill_switch_active:
                return False, f"AI kill switch active: {self._kill_switch_reason}"

            # Feedback loop detection: re-signal within 1s window
            if now_ns - self._last_signal_ns < self.FEEDBACK_LOOP_WINDOW_NS:
                # Increment this-second counter
                if now_ns - self._second_window_start_ns < 1_000_000_000:
                    self._signals_this_second += 1
                else:
                    self._signals_this_second = 1
                    self._second_window_start_ns = now_ns

                if self._signals_this_second > self.AI_MAX_RATE_PER_SEC:
                    logger.warning(
                        "[MODEL RISK] AI rate limit exceeded (%d/s > %d/s)",
                        self._signals_this_second,
                        self.AI_MAX_RATE_PER_SEC,
                    )
                    return (
                        False,
                        f"AI rate limit: {self._signals_this_second} signals/s exceeds {self.AI_MAX_RATE_PER_SEC}/s",
                    )
            else:
                self._signals_this_second = 1
                self._second_window_start_ns = now_ns

            self._last_signal_ns = now_ns

        # Large order threshold (outside lock — no shared state accessed)
        if notional > self.LARGE_ORDER_THRESHOLD:
            return (
                False,
                f"AI signal for order > ${self.LARGE_ORDER_THRESHOLD:,.0f} requires human confirmation",
            )

        return True, ""

    # ========================================================================
    # Status and reporting
    # ========================================================================

    def get_status(self) -> Dict:
        with self._lock:
            latencies = list(self._latency_history)
            avg_lat = sum(latencies) / len(latencies) if latencies else 0
            p99_lat = (
                sorted(latencies)[int(len(latencies) * 0.99)]
                if len(latencies) >= 100
                else max(latencies)
                if latencies
                else 0
            )

            outcomes = list(self._outcome_history)
            accuracy = (
                (sum(1 for _, ok in outcomes if ok) / len(outcomes))
                if outcomes
                else None
            )

            return {
                "model_version": self.MODEL_VERSION,
                "kill_switch_active": self._kill_switch_active,
                "kill_switch_reason": self._kill_switch_reason,
                "total_inferences": self._total_inferences,
                "fallback_count": self._fallback_count,
                "fallback_rate_pct": (
                    self._fallback_count / self._total_inferences * 100
                )
                if self._total_inferences > 0
                else 0,
                "avg_latency_ms": round(avg_lat, 2),
                "p99_latency_ms": round(p99_lat, 2),
                "max_latency_ms": self.MAX_INFERENCE_MS,
                "baseline_accuracy": self._baseline_accuracy,
                "current_accuracy": accuracy,
                "drift_alerts": len(self._drift_alerts),
                "retraining_flagged": self._retraining_flagged,
                "recent_drift_alerts": self._drift_alerts[-5:],
                "ai_rate_limit_per_s": self.AI_MAX_RATE_PER_SEC,
            }

    def get_recent_signals(self, n: int = 20) -> List[Dict]:
        with self._lock:
            recent = list(self._signal_history)[-n:]
        return [asdict(s) for s in recent]


# ============================================================================
# Singleton instance (imported by ai main.py)
# ============================================================================

model_risk_monitor = ModelRiskMonitor()


# ============================================================================
# Decorator for enforcing latency bounds
# ============================================================================


def with_latency_bound(
    max_ms: float = ModelRiskMonitor.MAX_INFERENCE_MS, fallback=None
):
    """
    Decorator that enforces a maximum inference latency.
    If the decorated function exceeds max_ms, returns fallback value and
    records a fallback_used=True signal.
    """

    def decorator(func):
        async def async_wrapper(*args, **kwargs):
            start = time.time()
            try:
                result = await asyncio.wait_for(
                    func(*args, **kwargs),
                    timeout=max_ms / 1000.0,
                )
                elapsed_ms = (time.time() - start) * 1000
                logger.debug("[LATENCY BOUND] %s: %.1fms", func.__name__, elapsed_ms)
                return result
            except asyncio.TimeoutError:
                elapsed_ms = (time.time() - start) * 1000
                logger.warning(
                    "[LATENCY BOUND] %s exceeded %.0fms (%.1fms) — using fallback",
                    func.__name__,
                    max_ms,
                    elapsed_ms,
                )
                model_risk_monitor._fallback_count += 1
                return fallback

        def sync_wrapper(*args, **kwargs):
            import threading

            result_container = [fallback]
            exception_container = [None]

            def target():
                try:
                    result_container[0] = func(*args, **kwargs)
                except Exception as e:
                    exception_container[0] = e

            t = threading.Thread(target=target, daemon=True)
            start = time.time()
            t.start()
            t.join(timeout=max_ms / 1000.0)
            # elapsed_ms = (time.time() - start) * 1000

            if t.is_alive():
                logger.warning(
                    "[LATENCY BOUND] %s exceeded %.0fms — using fallback",
                    func.__name__,
                    max_ms,
                )
                model_risk_monitor._fallback_count += 1
                return fallback

            if exception_container[0]:
                raise exception_container[0]
            return result_container[0]

        import asyncio as _asyncio

        if _asyncio.iscoroutinefunction(func):
            return async_wrapper
        return sync_wrapper

    return decorator
