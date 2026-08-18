#!/usr/bin/env bash
# ============================================================================
# Robin Trading Platform — Full System Benchmark Suite
# scripts/benchmark.sh
# ============================================================================
# Runs latency/throughput benchmarks for all hot-path components and
# emits a JSON report to benchmark_results/latest.json
#
# Requirements:
#   - Linux (uses perf, taskset, numactl)
#   - All services built (make build)
#   - wrk or vegeta for HTTP benchmarks
#
# Usage:
#   ./scripts/benchmark.sh [--quick] [--no-cpp] [--no-rust] [--no-go]
#
# Output:
#   benchmark_results/YYYYMMDD_HHMMSS.json
#   benchmark_results/latest.json  (symlink)
# ============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="${SCRIPT_DIR}/.."
RESULTS_DIR="${ROOT}/benchmark_results"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
OUTFILE="${RESULTS_DIR}/${TIMESTAMP}.json"
QUICK=${QUICK:-false}

# Parse flags
NO_CPP=false
NO_RUST=false
NO_GO=false
for arg in "$@"; do
    case "$arg" in
        --quick)   QUICK=true ;;
        --no-cpp)  NO_CPP=true ;;
        --no-rust) NO_RUST=true ;;
        --no-go)   NO_GO=true ;;
    esac
done

mkdir -p "${RESULTS_DIR}"

# Colors
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'

log_info()  { echo -e "${GREEN}[BENCH]${NC} $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }

# JSON accumulator
JSON_RESULTS="{"

add_result() {
    local component="$1"
    local metric="$2"
    local value="$3"
    local unit="$4"
    local pass="$5"   # "true" or "false"
    JSON_RESULTS+="\"${component}_${metric}\":{\"value\":${value},\"unit\":\"${unit}\",\"pass\":${pass}},"
    if [[ "$pass" == "true" ]]; then
        log_info "  ✅ ${component} ${metric}: ${value} ${unit}"
    else
        log_error "  ❌ ${component} ${metric}: ${value} ${unit} (REGRESSION)"
    fi
}

# ── 1. C++ Matching Engine Benchmarks ────────────────────────────────────────
if [[ "$NO_CPP" != "true" ]]; then
    log_info "=== C++ Matching Engine Benchmarks ==="
    ENGINE_BENCH="${ROOT}/services/execution-core/build/order_book_benchmark"

    if [[ -x "$ENGINE_BENCH" ]]; then
        # Run benchmark and parse throughput/latency
        BENCH_OUT=$(taskset -c 0 "${ENGINE_BENCH}" 2>&1)

        # Extract throughput (orders/sec)
        THROUGHPUT=$(echo "$BENCH_OUT" | grep -oP 'throughput: \K[\d]+' | head -1 || echo 0)
        # Extract p99 latency (nanoseconds)
        P99_NS=$(echo "$BENCH_OUT" | grep -oP 'p99: \K[\d]+' | head -1 || echo 999999)

        THROUGHPUT_PASS=$(( THROUGHPUT >= 1000000 )) && echo true || echo false
        P99_PASS=$(( P99_NS <= 500 )) && echo true || echo false

        add_result "matching_engine" "throughput_orders_per_sec" "${THROUGHPUT:-0}" "orders/s" "${THROUGHPUT_PASS:-false}"
        add_result "matching_engine" "p99_latency_ns"            "${P99_NS:-0}"     "ns"       "${P99_PASS:-false}"
    else
        log_warn "matching_engine benchmark not built. Run: cd services/execution-core && cmake --build build"
        add_result "matching_engine" "throughput_orders_per_sec" 0 "orders/s" false
        add_result "matching_engine" "p99_latency_ns"            0 "ns"       false
    fi
fi

# ── 2. Rust Risk Gate Benchmarks ─────────────────────────────────────────────
if [[ "$NO_RUST" != "true" ]]; then
    log_info "=== Rust Risk Gate Benchmarks (criterion) ==="
    RISK_DIR="${ROOT}/services/risk-analytics"

    if [[ -f "${RISK_DIR}/Cargo.toml" ]]; then
        BENCH_FILTER="check_order"
        if [[ "$QUICK" == "true" ]]; then
            BENCH_FILTER="check_order_warm"
        fi

        BENCH_OUT=$(cd "${RISK_DIR}" && \
            cargo bench --bench gate_bench \
                -- --output-format bencher \
                "${BENCH_FILTER}" 2>&1 || true)

        # Extract mean latency in ns from criterion output
        MEAN_NS=$(echo "$BENCH_OUT" | grep -oP 'time:\s+\[\S+ \K[\d.]+(?= ns)' | head -1 || echo 999999)
        MEAN_NS_INT=${MEAN_NS%.*}

        PASS=$(( ${MEAN_NS_INT:-999999} <= 500 )) && echo true || echo false
        add_result "risk_gate" "mean_latency_ns" "${MEAN_NS_INT:-0}" "ns" "${PASS:-false}"

        # Throughput: run concurrent check test
        TPUT_OUT=$(cd "${RISK_DIR}" && \
            cargo test --release -- concurrent_throughput --nocapture 2>&1 || true)
        TPUT=$(echo "$TPUT_OUT" | grep -oP 'throughput: \K[\d]+' | head -1 || echo 0)
        TPUT_PASS=$(( ${TPUT:-0} >= 10000000 )) && echo true || echo false
        add_result "risk_gate" "throughput_orders_per_sec" "${TPUT:-0}" "orders/s" "${TPUT_PASS:-false}"
    else
        log_warn "Risk gate Cargo.toml not found"
        add_result "risk_gate" "mean_latency_ns" 0 "ns" false
        add_result "risk_gate" "throughput_orders_per_sec" 0 "orders/s" false
    fi
fi

# ── 3. Go Gateway HTTP Benchmarks ────────────────────────────────────────────
if [[ "$NO_GO" != "true" ]]; then
    log_info "=== Go Gateway HTTP Benchmarks ==="

    # Check if gateway is running
    if curl -sf http://localhost:8080/health > /dev/null 2>&1; then
        if command -v vegeta &>/dev/null; then
            DURATION="30s"
            [[ "$QUICK" == "true" ]] && DURATION="10s"

            VEGETA_OUT=$(echo "GET http://localhost:8080/health" | \
                vegeta attack -rate=10000 -duration="${DURATION}" | \
                vegeta report -type=json 2>&1)

            P50=$(echo "$VEGETA_OUT" | python3 -c "import json,sys; d=json.load(sys.stdin); print(int(d['latencies']['50th']/1000))" 2>/dev/null || echo 999999)
            P99=$(echo "$VEGETA_OUT" | python3 -c "import json,sys; d=json.load(sys.stdin); print(int(d['latencies']['99th']/1000))" 2>/dev/null || echo 999999)
            TPUT=$(echo "$VEGETA_OUT" | python3 -c "import json,sys; d=json.load(sys.stdin); print(int(d['throughput']))" 2>/dev/null || echo 0)
            ERR=$(echo "$VEGETA_OUT" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d['success'])" 2>/dev/null || echo 0)

            P50_PASS=$(( ${P50:-999999} <= 1000 )) && echo true || echo false  # p50 < 1ms
            P99_PASS=$(( ${P99:-999999} <= 5000 )) && echo true || echo false  # p99 < 5ms

            add_result "gateway" "p50_latency_us"          "${P50:-0}"   "μs"    "${P50_PASS:-false}"
            add_result "gateway" "p99_latency_us"          "${P99:-0}"   "μs"    "${P99_PASS:-false}"
            add_result "gateway" "throughput_requests_sec"  "${TPUT:-0}" "req/s" true
        elif command -v wrk &>/dev/null; then
            WRK_OUT=$(wrk -t 4 -c 100 -d 30s http://localhost:8080/health 2>&1)
            RPS=$(echo "$WRK_OUT" | grep -oP 'Requests/sec:\s+\K[\d.]+' | head -1 || echo 0)
            add_result "gateway" "throughput_requests_sec" "${RPS%.*}" "req/s" true
        else
            log_warn "Install vegeta or wrk for HTTP benchmarks: go install github.com/tsenart/vegeta@latest"
            add_result "gateway" "p50_latency_us" 0 "μs" false
            add_result "gateway" "p99_latency_us" 0 "μs" false
        fi
    else
        log_warn "Gateway not running on localhost:8080 — skipping HTTP benchmarks"
        add_result "gateway" "p50_latency_us" 0 "μs" false
        add_result "gateway" "p99_latency_us" 0 "μs" false
    fi
fi

# ── 4. Memory Allocator Micro-benchmark ──────────────────────────────────────
log_info "=== Memory Pool Benchmark ==="
POOL_BENCH="${ROOT}/services/execution-core/build/order_book_benchmark"
if [[ -x "$POOL_BENCH" ]]; then
    POOL_OUT=$(taskset -c 0 "${POOL_BENCH}" --bench-pool 2>&1 || true)
    ALLOC_NS=$(echo "$POOL_OUT" | grep -oP 'pool alloc: \K[\d]+' | head -1 || echo 999)
    ALLOC_PASS=$(( ${ALLOC_NS:-999} <= 50 )) && echo true || echo false
    add_result "memory_pool" "alloc_latency_ns" "${ALLOC_NS:-0}" "ns" "${ALLOC_PASS:-false}"
else
    add_result "memory_pool" "alloc_latency_ns" 0 "ns" false
fi

# ── 5. Compliance Throughput ──────────────────────────────────────────────────
log_info "=== Compliance Benchmark ==="
if curl -sf http://localhost:9095/metrics > /dev/null 2>&1; then
    EVENTS=$(curl -sf http://localhost:9095/metrics | grep '^robin_compliance_events_total ' | awk '{print $2}' || echo 0)
    add_result "compliance" "events_processed" "${EVENTS:-0}" "events" true
else
    log_warn "Compliance daemon not running on :9095"
    add_result "compliance" "events_processed" 0 "events" false
fi

# ── Finalize JSON ──────────────────────────────────────────────────────────────
JSON_RESULTS="${JSON_RESULTS%,}}"  # remove trailing comma, close object
FINAL_JSON=$(cat <<EOF
{
  "timestamp": "${TIMESTAMP}",
  "system": "$(uname -srm)",
  "cpu": "$(grep 'model name' /proc/cpuinfo 2>/dev/null | head -1 | cut -d: -f2 | xargs || echo 'unknown')",
  "results": ${JSON_RESULTS}
}
EOF
)

echo "$FINAL_JSON" > "${OUTFILE}"
ln -sf "${OUTFILE}" "${RESULTS_DIR}/latest.json"

log_info ""
log_info "=== Benchmark Complete ==="
log_info "Results: ${OUTFILE}"
log_info "Latest:  ${RESULTS_DIR}/latest.json"
echo ""

# Check for regressions
REGRESSIONS=$(echo "$FINAL_JSON" | python3 -c "
import json, sys
d = json.load(sys.stdin)
failures = [k for k, v in d['results'].items() if not v['pass']]
if failures:
    print('REGRESSIONS: ' + ', '.join(failures))
    sys.exit(1)
print('ALL PASS')
" 2>&1 || true)

echo "$REGRESSIONS"
if echo "$REGRESSIONS" | grep -q "REGRESSIONS"; then
    exit 1
fi
