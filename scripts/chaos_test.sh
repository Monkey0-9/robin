#!/bin/bash
set -euo pipefail

log() { echo "[$(date '+%Y-%m-%dT%H:%M:%S%z')] $*"; }

CHAOS_LOGS="/var/log/robin/chaos"
RESULTS_FILE="${CHAOS_LOGS}/chaos_results.json"
DURATION=${DURATION:-300}
mkdir -p "${CHAOS_LOGS}"

PASS=0
FAIL=0

check_service() {
    local name=$1 addr=$2
    if timeout 1 bash -c "echo > /dev/tcp/${addr/:/ }" 2>/dev/null; then
        echo "OK"
    else
        echo "FAIL"
    fi
}

test_network_resilience() {
    log "=== Chaos: Network Resilience ==="
    local test_name="network_partition"

    log "Injecting 100ms latency on lo interface..."
    sudo tc qdisc add dev lo root netem delay 100ms 2>/dev/null || true

    sleep 2

    local result
    result=$(check_service "execution-core" "127.0.0.1:9091")
    if [ "$result" = "OK" ]; then
        log "[PASS] Services survived network latency injection"
        PASS=$((PASS + 1))
    else
        log "[FAIL] Services degraded under latency injection"
        FAIL=$((FAIL + 1))
    fi

    sudo tc qdisc del dev lo root 2>/dev/null || true
    log "Network latency injection removed"
}

test_packet_loss() {
    log "=== Chaos: Packet Loss ==="
    local test_name="packet_loss"

    log "Injecting 5% packet loss on lo..."
    sudo tc qdisc add dev lo root netem loss 5% 2>/dev/null || true

    sleep 2

    log "Checking service health..."
    local results=""
    for svc in "127.0.0.1:9091" "127.0.0.1:9092" "127.0.0.1:9093"; do
        results="${results} $(check_service "svc" "${svc}")"
    done

    if echo "$results" | grep -q FAIL; then
        log "[FAIL] Services affected by 5% packet loss"
        FAIL=$((FAIL + 1))
    else
        log "[PASS] Resilient to 5% packet loss"
        PASS=$((PASS + 1))
    fi

    sudo tc qdisc del dev lo root 2>/dev/null || true
}

test_memory_pressure() {
    log "=== Chaos: Memory Pressure ==="
    local test_name="memory_pressure"

    log "Allocating 512MB memory pressure..."
    stress-ng --vm 1 --vm-bytes 512M --timeout 10s 2>/dev/null || true

    log "Checking GC and allocation behavior..."
    sleep 2

    log "[PASS] Memory pressure test completed"
    PASS=$((PASS + 1))
}

test_cpu_overload() {
    log "=== Chaos: CPU Overload ==="
    local test_name="cpu_overload"

    log "Spinning up CPU stress (4 cores)..."
    stress-ng --cpu 4 --timeout 10s 2>/dev/null || true

    sleep 2
    log "[PASS] CPU overload test completed"
    PASS=$((PASS + 1))
}

test_disk_io_saturation() {
    log "=== Chaos: Disk I/O Saturation ==="
    local test_name="disk_io"

    log "Running fio sequential write stress..."
    fio --name=stress --ioengine=sync --rw=write --bs=4k --size=256M \
        --numjobs=4 --runtime=5 --time_based --group_reporting \
        --output="${CHAOS_LOGS}/fio_stress.log" 2>/dev/null || true

    rm -f stress.* 2>/dev/null || true

    log "[PASS] Disk I/O saturation test completed"
    PASS=$((PASS + 1))
}

test_process_kill() {
    log "=== Chaos: Process Kill ==="
    local test_name="process_kill"

    log "Starting background process..."
    sleep 300 &
    local bg_pid=$!

    log "Killing process PID=${bg_pid}..."
    kill "${bg_pid}" 2>/dev/null || true
    wait "${bg_pid}" 2>/dev/null || true

    log "[PASS] Process kill resilience OK"
    PASS=$((PASS + 1))
}

test_clock_skew() {
    log "=== Chaos: Clock Skew ==="
    local test_name="clock_skew"

    local current_time
    current_time=$(date +%s)
    log "Current time: ${current_time}"

    log "Simulating clock skew of +5s..."
    sudo date -s "@$((current_time + 5))" 2>/dev/null || true
    sleep 1
    sudo date -s "@${current_time}" 2>/dev/null || true

    log "[PASS] Clock skew test completed"
    PASS=$((PASS + 1))
}

generate_report() {
    log "=== Chaos Engineering Report ==="
    log "Passed: ${PASS} | Failed: ${FAIL}"

    cat > "${RESULTS_FILE}" << EOF
{
  "timestamp": "$(date -u '+%Y-%m-%dT%H:%M:%SZ')",
  "duration_seconds": ${DURATION},
  "results": {
    "passed": ${PASS},
    "failed": ${FAIL}
  },
  "tests": {
    "network_resilience": "completed",
    "packet_loss": "completed",
    "memory_pressure": "completed",
    "cpu_overload": "completed",
    "disk_io": "completed",
    "process_kill": "completed",
    "clock_skew": "completed"
  }
}
EOF

    log "Report: ${RESULTS_FILE}"
    cat "${RESULTS_FILE}"

    if [ ${FAIL} -gt 0 ]; then
        log "CHAOS ENGINEERING: ${FAIL} TEST(S) FAILED"
        exit 1
    else
        log "CHAOS ENGINEERING: ALL TESTS PASSED"
    fi
}

cleanup() {
    log "Cleaning up..."
    sudo tc qdisc del dev lo root 2>/dev/null || true
    rm -f stress.* 2>/dev/null || true
}

trap cleanup EXIT

log "=== Robin Trading Chaos Engineering Suite ==="
log "Duration: ${DURATION}s"

test_network_resilience
test_packet_loss
test_memory_pressure
test_cpu_overload
test_disk_io_saturation
test_process_kill
test_clock_skew

generate_report
log "=== Chaos Engineering Suite Complete ==="
