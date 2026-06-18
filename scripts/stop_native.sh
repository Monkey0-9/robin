#!/bin/bash
set -euo pipefail

LOG_DIR="/var/log/robin"
PID_DIR="/var/run/robin"

log() { echo "[$(date '+%Y-%m-%dT%H:%M:%S%z')] $*" | tee -a "${LOG_DIR}/shutdown.log"; }

log "============================================"
log "  Robin Trading Platform - Shutdown"
log "  $(date)"
log "============================================"

SERVICES=(
    "matching-engine"
    "network-bridge"
    "ingestion"
    "risk-gate"
    "orchestrator"
    "fpga-emulator"
)

for svc in "${SERVICES[@]}"; do
    pid_file="${PID_DIR}/${svc}.pid"
    if [ -f "${pid_file}" ]; then
        pid=$(cat "${pid_file}")
        log "Stopping ${svc} (PID: ${pid})..."

        kill -TERM "${pid}" 2>/dev/null || true

        for i in $(seq 1 10); do
            if ! kill -0 "${pid}" 2>/dev/null; then
                break
            fi
            sleep 0.1
        done

        if kill -0 "${pid}" 2>/dev/null; then
            log "[WARN] ${svc} still running, sending SIGKILL..."
            kill -KILL "${pid}" 2>/dev/null || true
        fi

        rm -f "${pid_file}"
        log "[OK] ${svc} stopped"
    else
        log "[INFO] ${svc} not running"
    fi
done

rm -f /dev/shm/robin_risk 2>/dev/null || true

log "=== Shutdown complete ==="
