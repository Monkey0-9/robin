#!/bin/bash
set -euo pipefail

log() { echo "[$(date '+%Y-%m-%dT%H:%M:%S%z')] $*"; }

COLO_NETWORK=${COLO_NETWORK:-"10.0.0.0/24"}
COLO_VLAN=${COLO_VLAN:-100}
PTP_IFACE=${PTP_IFACE:-"eth1"}
MULTICAST_GROUPS="224.0.0.1 239.255.0.1 239.255.0.2"

setup_interfaces() {
    log "=== Network Interface Configuration ==="

    for iface in eth0 eth1 eth2; do
        if ip link show "${iface}" &>/dev/null; then
            log "Configuring ${iface}..."

            # Enable jumbo frames
            ip link set "${iface}" mtu 9000 2>/dev/null || true

            # Disable GRO/GSO/TSO for low latency
            ethtool -K "${iface}" gro off gso off tso off 2>/dev/null || true
            ethtool -K "${iface}" rx off tx off 2>/dev/null || true

            # Set coalesce to minimal
            ethtool -C "${iface}" rx-usecs 0 tx-usecs 0 2>/dev/null || true

            # Increase ring buffer size
            ethtool -G "${iface}" rx 4096 tx 4096 2>/dev/null || true

            log "[OK] ${iface} optimized"
        fi
    done
}

setup_vlan() {
    log "=== VLAN Configuration ==="
    local vlan_iface="eth0.${COLO_VLAN}"

    if ! ip link show "${vlan_iface}" &>/dev/null; then
        ip link add link eth0 name "${vlan_iface}" type vlan id "${COLO_VLAN}"
        ip addr add "${COLO_NETWORK}" dev "${vlan_iface}"
        ip link set "${vlan_iface}" up
        log "[OK] VLAN ${COLO_VLAN} configured on ${vlan_iface}"
    else
        log "[OK] VLAN ${COLO_VLAN} already exists"
    fi
}

setup_multicast() {
    log "=== Multicast Configuration ==="
    for group in ${MULTICAST_GROUPS}; do
        ip maddr add "${group}" dev eth1 2>/dev/null || true
        log "[OK] Joined multicast group: ${group}"
    done
}

setup_routing() {
    log "=== Routing Configuration ==="
    local routes=(
        "10.0.0.0/8 via 10.0.0.1"
        "172.16.0.0/12 via 10.0.0.1"
        "192.168.0.0/16 via 10.0.0.1"
    )

    for route in "${routes[@]}"; do
        ip route add ${route} 2>/dev/null || true
    done

    # Enable IP forwarding
    echo 1 > /proc/sys/net/ipv4/ip_forward 2>/dev/null || true
    log "[OK] Routing configured"
}

setup_iptables() {
    log "=== Firewall Configuration ==="

    # Allow internal traffic
    iptables -A INPUT -i eth0 -j ACCEPT 2>/dev/null || true
    iptables -A OUTPUT -o eth0 -j ACCEPT 2>/dev/null || true

    # Allow PTP traffic
    iptables -A INPUT -p udp --dport 319 -j ACCEPT 2>/dev/null || true
    iptables -A INPUT -p udp --dport 320 -j ACCEPT 2>/dev/null || true

    # Allow multicast market data
    iptables -A INPUT -p udp -m multiport --dports 9000-9010 -j ACCEPT 2>/dev/null || true

    # Drop everything else by default
    iptables -P INPUT DROP 2>/dev/null || true

    # Allow loopback
    iptables -A INPUT -i lo -j ACCEPT 2>/dev/null || true

    log "[OK] Firewall configured"
}

setup_ptp() {
    log "=== PTP Configuration ==="
    if command -v ptp4l &>/dev/null; then
        log "Configuring PTP on ${PTP_IFACE}..."

        cat > /etc/linuxptp/ptp4l.conf << 'EOF'
[global]
network_transport      L2
delay_mechanism        P2P
slaveOnly              1
logSyncInterval        0
twoStepFlag            1
domainNumber           0
EOF

        log "[OK] PTP config written"
    else
        log "[WARN] ptp4l not installed"
    fi
}

disable_services() {
    log "=== Disabling Non-Essential Services ==="
    local services=(
        "cups" "cups-browsed" "bluetooth" "avahi-daemon"
        "NetworkManager" "ModemManager" "accounts-daemon"
        "whoopsie" "unattended-upgrades" "snapd"
    )

    for svc in "${services[@]}"; do
        if systemctl is-active --quiet "${svc}" 2>/dev/null; then
            systemctl stop "${svc}" 2>/dev/null || true
            systemctl disable "${svc}" 2>/dev/null || true
            log "[OK] Disabled ${svc}"
        fi
    done
}

set_kernel_params() {
    log "=== Kernel Parameters ==="
    sysctl -w net.core.rmem_default=134217728 2>/dev/null || true
    sysctl -w net.core.wmem_default=134217728 2>/dev/null || true
    sysctl -w net.core.rmem_max=268435456 2>/dev/null || true
    sysctl -w net.core.wmem_max=268435456 2>/dev/null || true
    sysctl -w net.core.netdev_budget=600 2>/dev/null || true
    sysctl -w net.core.netdev_budget_usecs=4000 2>/dev/null || true
    sysctl -w net.ipv4.tcp_fastopen=3 2>/dev/null || true
    sysctl -w kernel.numa_balancing=0 2>/dev/null || true
    log "[OK] Kernel parameters set"
}

log "============================================"
log "  Co-Location Environment Setup"
log "  Network: ${COLO_NETWORK}"
log "  VLAN: ${COLO_VLAN}"
log "  PTP IFace: ${PTP_IFACE}"
log "============================================"

case "${1:-all}" in
    all)
        setup_interfaces
        setup_vlan
        setup_multicast
        setup_routing
        setup_iptables
        setup_ptp
        disable_services
        set_kernel_params
        ;;
    network)
        setup_interfaces
        setup_vlan
        setup_multicast
        setup_routing
        ;;
    security)
        setup_iptables
        ;;
    ptp)
        setup_ptp
        ;;
    optimize)
        disable_services
        set_kernel_params
        ;;
    *)
        echo "Usage: $0 [all|network|security|ptp|optimize]"
        exit 1
        ;;
esac

log "=== Co-Location setup complete ==="
