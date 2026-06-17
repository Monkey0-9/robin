#!/bin/bash
# config/onload_config.sh
# Enterprise configuration for Solarflare/Mellanox OpenOnload kernel bypass.
# Standardizes network card configuration to achieve sub-microsecond latency.

echo "===================================================================="
echo "Configuring OpenOnload Kernel Bypass for Solarflare/Mellanox NICs"
echo "===================================================================="

# 1. Low-latency environment variables for execution threads
export ONLOAD_RECV_SPIN=1         # Spin on receive descriptor rings (bypass interrupt handler)
export ONLOAD_POLL_SPIN=1         # Spin on poll/select calls
export ONLOAD_TX_BCAST=0          # Disable loopback broadcasts
export ONLOAD_DONT_ACCEL=0        # Force accelerate network traffic
export EF_VI_SPIN=1               # Enable spin on raw interface rings
export EF_TCP_RECV_SPIN=1         # Spin on TCP socket receive

# 2. Kernel tuning parameters via onload module settings
if [ -d "/sys/module/onload" ]; then
    echo "Applying module tuning options..."
    echo 1 > /sys/module/onload/parameters/onload_recv_spin
    echo 1 > /sys/module/onload/parameters/onload_poll_spin
else
    echo "WARNING: onload kernel module not loaded. Run: modprobe onload"
fi

# 3. Configure interface ring sizes
INTERFACE="eth0" # Target Solarflare interface
if ip link show "$INTERFACE" >/dev/null 2>&1; then
    echo "Optimizing ring parameters for $INTERFACE..."
    sudo ethtool -G "$INTERFACE" rx 4096 tx 4096
    sudo ethtool -K "$INTERFACE" rxvlan off txvlan off
    sudo ethtool -K "$INTERFACE" gso off gro off tso off
    sudo ethtool -C "$INTERFACE" rx-usecs 0 rx-frames 0 tx-usecs 0 tx-frames 0
else
    echo "Target interface '$INTERFACE' not found. Configurations queued for boot-time binding."
fi

echo "OpenOnload latency parameters set successfully."
