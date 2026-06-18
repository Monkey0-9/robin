#!/bin/bash
set -euo pipefail

LATENCY_LOGS="./logs/latency"
RESULTS_FILE="${LATENCY_LOGS}/benchmark_results.json"
ITERATIONS=${ITERATIONS:-1000000}
WARMUP=${WARMUP:-100000}
BATCH_SIZE=${BATCH_SIZE:-64}

log() { echo "[$(date '+%Y-%m-%dT%H:%M:%S%z')] $*"; }

mkdir -p "${LATENCY_LOGS}"

check_prereqs() {
    log "Checking prerequisites..."
    command -v perf >/dev/null 2>&1 || { log "WARNING: perf not found"; }
    command -v taskset >/dev/null 2>&1 || { log "FATAL: taskset required"; exit 1; }
    command -v numactl >/dev/null 2>&1 || { log "WARNING: numactl not found"; }

    if [ -f /proc/sys/kernel/perf_event_paranoid ]; then
        local paranoid
        paranoid=$(cat /proc/sys/kernel/perf_event_paranoid)
        log "perf_event_paranoid = ${paranoid}"
    fi
}

setup_isolation() {
    log "Setting up CPU isolation for benchmark..."
    for cpu in 0 1 2 3; do
        if [ -f "/sys/devices/system/cpu/cpu${cpu}/online" ]; then
            echo 1 > "/sys/devices/system/cpu/cpu${cpu}/online" 2>/dev/null || true
        fi
    done
}

run_matching_bench() {
    log "=== Order Matching Latency Benchmark ==="
    log "Iterations: ${ITERATIONS} | Warmup: ${WARMUP} | Batch: ${BATCH_SIZE}"

    local results

    if [ -f services/execution-core/build/matching_engine ]; then
        results=$(taskset -c 2-3 \
            services/execution-core/build/matching_engine 2>&1) || true
        echo "${results}" >> "${LATENCY_LOGS}/matching_bench.log"
        log "Matching engine benchmark complete"
    else
        log "WARNING: matching_engine binary not found"
    fi
}

run_perf_analysis() {
    log "=== Performance Counter Analysis ==="
    local output_file="${LATENCY_LOGS}/perf_analysis.log"

    if command -v perf >/dev/null 2>&1; then
        if [ -f services/execution-core/build/matching_engine ]; then
            perf stat -e cycles,instructions,cache-references,cache-misses,\
                branch-misses,branch-instructions,context-switches,cpu-migrations,\
                page-faults,L1-dcache-load-misses,L1-icache-load-misses,\
                LLC-load-misses,dTLB-load-misses,iTLB-load-misses \
                -p "$$" -o "${output_file}" sleep 0.1 2>/dev/null || true

            log "Performance counters saved to ${output_file}"
        fi
    fi
}

run_network_bench() {
    log "=== Network Latency Benchmark ==="
    if [ -f services/network-bridge/build/kernel_bypass_ingest ]; then
        taskset -c 4-5 \
            services/network-bridge/build/kernel_bypass_ingest \
            2>&1 | tee -a "${LATENCY_LOGS}/network_bench.log"
        log "Network benchmark complete"
    fi
}

run_ingestion_bench() {
    log "=== Ingestion Latency Benchmark ==="
    if [ -f services/ingestion/build/cpp_ingestion ]; then
        taskset -c 6-7 \
            services/ingestion/build/cpp_ingestion \
            > "${LATENCY_LOGS}/ingestion_bench.log" 2>&1 || true
        log "Ingestion benchmark complete"
    fi
}

generate_report() {
    log "=== Generating Latency Report ==="

    cat > "${RESULTS_FILE}" << EOF
{
  "timestamp": "$(date -u '+%Y-%m-%dT%H:%M:%SZ')",
  "benchmark": {
    "iterations": ${ITERATIONS},
    "warmup": ${WARMUP},
    "batch_size": ${BATCH_SIZE}
  },
  "system": {
    "hostname": "$(hostname)",
    "kernel": "$(uname -r)",
    "cpu": "$(grep 'model name' /proc/cpuinfo | head -1 | sed 's/.*: //')",
    "cores": $(nproc),
    "memory_gb": $(free -g | awk '/^Mem:/{print $2}')
  }
}
EOF

    log "Results saved to ${RESULTS_FILE}"
    cat "${RESULTS_FILE}"
}

cleanup() {
    log "Cleaning up..."
}

trap cleanup EXIT

log "=== Robin Trading Latency Benchmark Suite ==="
log "Starting at $(date)"

check_prereqs
setup_isolation

if [ "${1:-}" = "quick" ]; then
    log "Quick mode: single iteration"
    ITERATIONS=10000
    WARMUP=1000
fi

run_matching_bench
run_perf_analysis
run_network_bench
run_ingestion_bench
generate_report

log "=== Benchmark Suite Complete ==="
log "Results: ${RESULTS_FILE}"
