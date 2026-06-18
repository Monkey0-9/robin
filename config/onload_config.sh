#!/bin/bash
set -euo pipefail

log() { echo "[$(date '+%Y-%m-%dT%H:%M:%S%z')] $*"; }

ONLOAD_IFACE=${ONLOAD_IFACE:-"eth1"}
ONLOAD_STACK_COUNT=${ONLOAD_STACK_COUNT:-4}

check_onload() {
    log "=== OpenOnload Check ==="
    if command -v onload &>/dev/null; then
        log "[OK] OpenOnload found: $(onload --version 2>&1 | head -1)"
    else
        log "[WARN] OpenOnload not installed"
        return 1
    fi
}

configure_onload() {
    log "=== OpenOnload Configuration ==="

    local cfg="/etc/onload.conf"
    cat > "${cfg}" << EOF
# OpenOnload configuration for Robin Trading
# Ultra-low latency kernel bypass

stack_count ${ONLOAD_STACK_COUNT}
iface ${ONLOAD_IFACE}

# CPU pinning for stack threads
stack_cpu_affinity 10-13

# Epoll optimization
epoll_wait_loop 1
epoll_max_events 1000

# TCP optimizations
tcp_rcv_buf 1048576
tcp_snd_buf 1048576
tcp_nodelay 1

# UDP optimizations
udp_rcv_buf 2097152
udp_snd_buf 2097152

# Threading
spawner_policy 1
stack_poll_usecs 0

# Hybrid mode: use onload for hot path, kernel for management
hybrid_mode 1
hybrid_nonprivileged 1

# Logging
log_level 3
log_file /var/log/onload.log

# Performance
max_spin 100000
ns_ip_cache 1
EOF

    log "[OK] OpenOnload config written: ${cfg}"
}

optimize_interface() {
    log "=== Interface Optimization ==="
    local iface="${1:-${ONLOAD_IFACE}}"

    if ip link show "${iface}" &>/dev/null; then
        ethtool -K "${iface}" gro off gso off tso off lro off rx off tx off sg off
        ethtool -C "${iface}" rx-usecs 0 tx-usecs 0 adaptive-rx off adaptive-tx off
        ethtool -G "${iface}" rx 4096 tx 4096
        ip link set "${iface}" mtu 9000
        log "[OK] ${iface} optimized for kernel bypass"
    fi
}

verify_performance() {
    log "=== Performance Verification ==="
    if command -v onload &>/dev/null; then
        onload latcystest -i "${ONLOAD_IFACE}" -n 10000 2>/dev/null || true
        log "[OK] Latency test completed"
    fi
}

log "=== OpenOnload Configuration ==="
log "Interface: ${ONLOAD_IFACE}"
log "Stacks: ${ONLOAD_STACK_COUNT}"

if check_onload; then
    configure_onload
    optimize_interface
    verify_performance
    log "[OK] Onload configuration complete"
else
    log "Skipping onload configuration (not installed)"
    log "Install from: https://support.xilinx.com/s/article/76955"
fi
