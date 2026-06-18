#!/bin/bash
set -euo pipefail

PTP_CONFIG="/etc/linuxptp/ptp4l.conf"
PTP_LOG="/var/log/robin/ptp_sync.log"

log() { echo "[$(date '+%Y-%m-%dT%H:%M:%S%z')] $*" | tee -a "${PTP_LOG}"; }

check_hardware() {
    log "=== PTP Hardware Check ==="

    if command -v ethtool &>/dev/null; then
        for iface in eth0 eth1; do
            if ip link show "${iface}" &>/dev/null; then
                local ts_caps
                ts_caps=$(ethtool -T "${iface}" 2>/dev/null | grep -i "hardware.*transmit\|hardware.*receive\|hardware.*raw" || true)
                if [ -n "${ts_caps}" ]; then
                    log "[OK] ${iface}: Hardware timestamps supported"
                    echo "${ts_caps}" >> "${PTP_LOG}"
                else
                    log "[WARN] ${iface}: No hardware timestamp support"
                fi
            fi
        done
    fi

    if ls /dev/ptp* &>/dev/null; then
        log "[OK] PTP devices found: $(ls /dev/ptp* | tr '\n' ' ')"
    else
        log "[WARN] No PTP devices found"
    fi
}

generate_ptp_config() {
    log "=== Generating PTP Config ==="
    cat > "${PTP_CONFIG}" << 'PTPEOF'
[global]
# Enterprise profile for high-frequency trading
network_transport      L2
delay_mechanism        P2P
time_source            ptp

# Clock synchronization parameters
logSyncInterval        0
syncReceiptTimeout     2
followUpInfo           yes
twoStepFlag            1
logMinDelayReqInterval 0
logMinPdelayReqInterval  0

# Boundary clock / Ordinary clock settings
slaveOnly              1
clockClass             255
clockAccuracy          0xFE
priority1              128
priority2              128
domainNumber           0

# Hardware timestamping
tx_timestamp_timeout   10
check_fup_sync         0
pi_integral_const      0.0
pi_proportional_const  0.0
pi_integral_exponent   0.4
pi_proportional_scale  0.0

# Logging
logging_level          6
message_tag            ptp4l

# Paths
uds_address            /var/run/ptp4l
PTPEOF

    log "[OK] PTP config written: ${PTP_CONFIG}"
}

start_ptp_daemon() {
    log "=== Starting PTP Daemon ==="

    if pgrep ptp4l &>/dev/null; then
        log "[WARN] ptp4l already running, restarting..."
        pkill ptp4l 2>/dev/null || true
        sleep 1
    fi

    local ptp_iface="${1:-eth1}"

    nohup ptp4l -f "${PTP_CONFIG}" -i "${ptp_iface}" -m \
        > /var/log/robin/ptp4l.log 2>&1 &
    local pid=$!
    log "[OK] ptp4l started (PID: ${pid}) on ${ptp_iface}"

    nohup phc2sys -s "${ptp_iface}" -c CLOCK_REALTIME -O 0 -m \
        -N 1 -R 1 \
        > /var/log/robin/phc2sys.log 2>&1 &
    log "[OK] phc2sys started (PID: $!)"
}

monitor_sync_quality() {
    log "=== PTP Sync Quality Monitor ==="
    local monitor_seconds=${1:-60}
    local interval=5

    log "Monitoring for ${monitor_seconds}s (interval=${interval}s)"

    local max_offset=0
    local samples=0

    for i in $(seq 1 $((monitor_seconds / interval))); do
        local offset
        offset=$(tail -1 /var/log/robin/ptp4l.log 2>/dev/null | grep -oP 'offset\s+\K[-0-9.]+' || echo "N/A")

        if [ "${offset}" != "N/A" ] && [ -n "${offset}" ]; then
            local abs_offset
            abs_offset=$(echo "${offset#-}" | cut -d' ' -f1)
            if [ "${abs_offset%.*}" -gt "${max_offset%.*}" ] 2>/dev/null; then
                max_offset="${abs_offset}"
            fi
            samples=$((samples + 1))
        fi

        log "Sample ${i}: offset=${offset}ns (max=${max_offset}ns, samples=${samples})"
        sleep "${interval}"
    done

    log "=== PTP Sync Summary ==="
    log "Samples: ${samples} | Max offset: ${max_offset}ns"

    if [ -n "${max_offset}" ] && [ "${max_offset%.*}" -lt 100 ] 2>/dev/null; then
        log "[PASS] PTP sync within 100ns: ${max_offset}ns"
    else
        log "[WARN] PTP sync > 100ns: ${max_offset}ns"
    fi
}

test_timestamp_accuracy() {
    log "=== Timestamp Accuracy Test ==="

    if command -v pmc &>/dev/null; then
        log "Querying grandmaster dataset..."
        pmc -u -b 0 'GET GRANDMASTER_SETTINGS_NP' 2>/dev/null | tee -a "${PTP_LOG}" || true
    fi

    if command -v chronyc &>/dev/null; then
        log "Chrony tracking..."
        chronyc tracking 2>/dev/null | tee -a "${PTP_LOG}" || true
    fi
}

cleanup() {
    log "=== PTP Shutdown ==="
}

trap cleanup EXIT

log "============================================"
log "  PTP Clock Sync (IEEE 1588v2)"
log "  Target: <100ns to UTC (MiFID II RTS 25)"
log "============================================"

check_hardware
generate_ptp_config

if [ "${1:-}" = "--start" ]; then
    start_ptp_daemon "${2:-eth1}"
    monitor_sync_quality "${3:-60}"
    test_timestamp_accuracy
elif [ "${1:-}" = "--monitor" ]; then
    monitor_sync_quality "${2:-300}"
elif [ "${1:-}" = "--check" ]; then
    test_timestamp_accuracy
else
    log "Usage: $0 [--start [iface] [duration]] [--monitor [duration]] [--check]"
    log "Examples:"
    log "  $0 --start eth1 120   Start PTP, monitor 120s"
    log "  $0 --monitor 300       Monitor existing PTP for 300s"
    log "  $0 --check             Check timestamp accuracy"
fi
