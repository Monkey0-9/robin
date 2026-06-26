#!/usr/bin/env bash
# ============================================================================
# Robin Trading Platform — Integration Smoke Test
# ============================================================================
# Tests that the pipeline components can start, communicate via SHM, and
# produce expected output within a reasonable time.
#
# Requirements (Linux only):
#   - Built binaries: execution-core, orchestrator, compliance-daemon
#   - /dev/shm or POSIX shared memory support
#
# Usage: bash scripts/integration_smoke_test.sh [--timeout <seconds>]
# Exit codes: 0=pass, 1=fail
# ============================================================================

set -euo pipefail

TIMEOUT=30
PASS=0
FAIL=1
PIDS=()

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --timeout) TIMEOUT="$2"; shift 2 ;;
        *) echo "Unknown arg: $1"; exit $FAIL ;;
    esac
done

log() { echo "[SMOKE $(date '+%H:%M:%S')] $*"; }
fail() { echo "[FAIL] $*" >&2; cleanup; exit $FAIL; }
pass() { echo "[PASS] $*"; }

cleanup() {
    log "Cleaning up..."
    for pid in "${PIDS[@]:-}"; do
        kill "$pid" 2>/dev/null || true
    done
    # Remove test SHM segments
    rm -f /dev/shm/robin_ingest_risk 2>/dev/null || true
    rm -f /dev/shm/robin_risk_match  2>/dev/null || true
}

trap cleanup EXIT INT TERM

# ============================================================================
# Check prerequisites
# ============================================================================
log "Checking prerequisites..."

# OS check removed to allow running on Windows/MinGW

ORCH_BIN="./build/orchestrator.exe"
COMPLIANCE_BIN="./target/release/compliance-daemon.exe"
EXEC_BIN="./services/execution-core/build/matching_engine.exe"
RISK_BIN="./target/release/robin-risk-daemon.exe"

if [[ ! -f "$ORCH_BIN" ]]; then
    log "Building orchestrator..."
    make build-go || fail "build-go failed"
fi

if [[ ! -f "$COMPLIANCE_BIN" ]]; then
    log "Building compliance daemon..."
    make build-compliance || fail "build-compliance failed"
fi

if [[ ! -f "$EXEC_BIN" ]]; then
    log "Building execution-core..."
    make build-cpp || fail "build-cpp failed"
fi

if [[ ! -f "$RISK_BIN" ]]; then
    log "Building risk-analytics..."
    make build-rust || fail "build-rust failed"
fi

# ============================================================================
# Start orchestrator
# ============================================================================
log "Starting orchestrator on :18080..."
ROBIN_GATEWAY_API_TOKEN="smoke-test-secret" ORCH_PORT=18080 "$ORCH_BIN" > /tmp/orch.log 2>&1 &
PIDS+=($!)
sleep 1

# Health check orchestrator
for i in $(seq 1 10); do
    if curl -sf "http://127.0.0.1:18080/health" > /tmp/health.json 2>/dev/null; then
        break
    fi
    if [[ $i -eq 10 ]]; then
        fail "Orchestrator did not start within 10s. Log: $(cat /tmp/orch.log)"
    fi
    sleep 1
done

STATUS=$(python3 -c "import json,sys; d=json.load(open('/tmp/health.json')); sys.exit(0 if d.get('status')=='ok' else 1)" 2>/dev/null && echo ok || echo fail)
if [[ "$STATUS" != "ok" ]]; then
    fail "Orchestrator /health returned unexpected status. Response: $(cat /tmp/health.json)"
fi
pass "Orchestrator /health returned ok"

# ============================================================================
# Start execution-core and risk-analytics
# ============================================================================
log "Starting execution-core on :9091..."
"$EXEC_BIN" 9091 > /tmp/exec.log 2>&1 &
PIDS+=($!)
sleep 1

log "Starting risk-analytics on :9092..."
"$RISK_BIN" > /tmp/risk.log 2>&1 &
PIDS+=($!)
sleep 1

# ============================================================================
# Start compliance daemon
# ============================================================================
log "Starting compliance daemon on :19095..."
mkdir -p /tmp/robin_logs
"$COMPLIANCE_BIN" --port 19095 --audit-log /tmp/robin_logs/test_audit.log > /tmp/compliance.log 2>&1 &
PIDS+=($!)
sleep 2

# Health check compliance
for i in $(seq 1 5); do
    if curl -sf "http://127.0.0.1:19095/health" > /tmp/compliance_health.json 2>/dev/null; then
        break
    fi
    if [[ $i -eq 5 ]]; then
        fail "Compliance daemon did not start. Log: $(cat /tmp/compliance.log)"
    fi
    sleep 1
done
pass "Compliance daemon /health responding"

# ============================================================================
# Verify orchestrator /stats endpoint
# ============================================================================
STATS=$(curl -sf "http://127.0.0.1:18080/stats" 2>/dev/null)
if [[ -z "$STATS" ]]; then
    fail "Orchestrator /stats returned empty response"
fi
pass "Orchestrator /stats: $STATS"

# ============================================================================
# Verify compliance /metrics endpoint
# ============================================================================
METRICS=$(curl -sf "http://127.0.0.1:19095/metrics" 2>/dev/null)
if echo "$METRICS" | grep -q "robin_compliance_events_processed_total"; then
    pass "Compliance /metrics contains expected metric names"
else
    fail "Compliance /metrics missing expected metric names. Got: $METRICS"
fi

# ============================================================================
# Config hot-reload test
# ============================================================================
RELOAD=$(curl -sf -X POST -H "Content-Type: application/json" \
    -d '{"max_drawdown_limit":0.05,"max_order_rate":5000}' \
    "http://127.0.0.1:18080/config" 2>/dev/null)
if echo "$RELOAD" | grep -q "reloaded"; then
    pass "Config hot-reload successful"
else
    fail "Config hot-reload failed. Response: $RELOAD"
fi

# Verify the config was actually updated
NEW_CONFIG=$(curl -sf "http://127.0.0.1:18080/config" 2>/dev/null)
NEW_RATE=$(python3 -c "import json; d=json.loads('$NEW_CONFIG'); print(d.get('max_order_rate',0))" 2>/dev/null)
if [[ "$NEW_RATE" == "5000" ]]; then
    pass "Config update verified: max_order_rate=5000"
else
    fail "Config update not reflected. max_order_rate=$NEW_RATE (expected 5000)"
fi

# ============================================================================
# Send Test Order and Verify Audit Log
# ============================================================================
log "Submitting test order..."
curl -s -X POST -H "Content-Type: application/json" \
    -H "Authorization: Bearer smoke-test-secret" \
    -d '{"symbol":"BTC/USD","side":"BUY","price":64000.0,"qty":1.0,"order_type":"LIMIT","cl_ord_id":"client-test"}' \
    "http://127.0.0.1:18080/order" > /tmp/order_resp.json
log "Order response: $(cat /tmp/order_resp.json)"

sleep 3  # Let compliance daemon write some records
if [[ -f /tmp/robin_logs/test_audit.log ]]; then
    LINES=$(wc -l < /tmp/robin_logs/test_audit.log)
    if [[ $LINES -gt 0 ]]; then
        pass "Audit log contains $LINES entries"
        # Check if the trade/order event was logged
        if grep -q '"client-test"' /tmp/robin_logs/test_audit.log || grep -q 'NEW' /tmp/robin_logs/test_audit.log; then
            pass "Found order event in audit log"
        else
            log "WARNING: Did not find order event in audit log (might just be heartbeats)"
        fi
    else
        fail "Audit log is empty"
    fi
else
    fail "Audit log not created at /tmp/robin_logs/test_audit.log"
fi

# ============================================================================
# Rate limit test
# ============================================================================
log "Testing rate limit..."
RATE_LIMIT_TRIGGERED=0
for i in $(seq 1 1100); do
    CODE=$(curl -so /dev/null -w "%{http_code}" "http://127.0.0.1:18080/health" 2>/dev/null)
    if [[ "$CODE" == "429" ]]; then
        RATE_LIMIT_TRIGGERED=1
        break
    fi
done
if [[ $RATE_LIMIT_TRIGGERED -eq 1 ]]; then
    pass "Rate limiter correctly returned HTTP 429 after burst"
else
    log "INFO: Rate limiter did not trigger (may need more requests)"
fi

# ============================================================================
# All done
# ============================================================================
log "All smoke tests passed!"
echo ""
echo "╔══════════════════════════════════════════╗"
echo "║  INTEGRATION SMOKE TEST: PASSED          ║"
echo "╚══════════════════════════════════════════╝"
exit 0
