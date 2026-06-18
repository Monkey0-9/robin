#!/bin/bash
set -euo pipefail

LOG_DIR="./logs"
PID_DIR="./pids"
CONFIG_DIR="./etc"
mkdir -p "${LOG_DIR}" "${PID_DIR}" "${CONFIG_DIR}"

log() { echo "[$(date '+%Y-%m-%dT%H:%M:%S%z')] $*" | tee -a "${LOG_DIR}/startup.log"; }

cleanup() {
    log "=== Shutting down Robin Trading Platform ==="
    for pid_file in "${PID_DIR}"/*.pid; do
        if [ -f "${pid_file}" ]; then
            local pid
            pid=$(cat "${pid_file}")
            local name
            name=$(basename "${pid_file}" .pid)
            log "Stopping ${name} (PID: ${pid})..."
            kill "${pid}" 2>/dev/null || true
            rm -f "${pid_file}"
        fi
    done
    log "=== All services stopped ==="
}

trap cleanup EXIT SIGINT SIGTERM

start_service() {
    local name=$1
    local binary=$2
    local args=${3:-}
    local pid_file="${PID_DIR}/${name}.pid"

    if [ -f "${pid_file}" ]; then
        local old_pid
        old_pid=$(cat "${pid_file}")
        if kill -0 "${old_pid}" 2>/dev/null; then
            log "[WARN] ${name} already running (PID: ${old_pid})"
            return 0
        fi
        rm -f "${pid_file}"
    fi

    log "Starting ${name}..."
    if [ -x "${binary}" ]; then
        if [ -n "${args}" ]; then
            nohup "${binary}" ${args} \
                > "${LOG_DIR}/${name}.log" 2>&1 &
        else
            nohup "${binary}" \
                > "${LOG_DIR}/${name}.log" 2>&1 &
        fi
        local pid=$!
        echo "${pid}" > "${pid_file}"
        log "[OK] ${name} started (PID: ${pid})"
    else
        log "[WARN] ${name} binary not found: ${binary}"
    fi
}

set_cpu_affinity() {
    local name=$1
    local pid_file="${PID_DIR}/${name}.pid"
    local cpus=$2

    if [ -f "${pid_file}" ]; then
        local pid
        pid=$(cat "${pid_file}")
        if taskset -cp "${cpus}" "${pid}" 2>/dev/null; then
            log "[OK] ${name} pinned to CPU(s) ${cpus}"
        else
            log "[WARN] Could not set CPU affinity for ${name}"
        fi
    fi
}

log "============================================"
log "  Robin Trading Platform - Startup"
log "  $(date)"
log "============================================"

log "Checking prerequisites..."
for cmd in taskset pgrep; do
    if ! command -v "${cmd}" &>/dev/null; then
        log "[FATAL] ${cmd} not found"
        exit 1
    fi
done

echo 0 > /proc/sys/kernel/perf_event_paranoid 2>/dev/null || true
echo -1 > /proc/sys/kernel/perf_event_mlock_kb 2>/dev/null || true

log "=== Starting Hot-Path Services ==="

start_service "matching-engine" \
    "./services/execution-core/build/matching_engine"

start_service "network-bridge" \
    "./services/network-bridge/build/kernel_bypass_ingest"

start_service "ingestion" \
    "./services/ingestion/build/cpp_ingestion"

log "=== Starting Risk Analytics ==="

start_service "risk-gate" \
    "./services/risk-analytics/target/release/robin_risk" \
    "--shm-path /dev/shm/robin_risk"

log "=== Starting Infrastructure ==="

start_service "orchestrator" \
    "./services/gateway/bin/orchestrator"

log "=== Setting CPU Affinity ==="

set_cpu_affinity "matching-engine" "2-3"
set_cpu_affinity "network-bridge" "4-5"
set_cpu_affinity "ingestion" "6-7"
set_cpu_affinity "risk-gate" "8-9"

log "============================================"
log "  Startup Complete"
log "  Services running. PID files in ${PID_DIR}"
log "  Logs in ${LOG_DIR}"
log "============================================"

log "Press Ctrl+C to stop all services."
wait
