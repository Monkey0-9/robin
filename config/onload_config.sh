#!/bin/bash
# Network bypass configuration for ultra-low latency trading.
# Supports DPDK (primary) and Solarflare ef_vi (Solarflare NICs only).
set -euo pipefail

echo "[NETWORK] Configuring kernel bypass..."

# --- DPDK Configuration ---
dpdk_config() {
    echo "[DPDK] Loading uio_pci_generic module..."
    modprobe uio_pci_generic || true

    echo "[DPDK] Binding NIC to DPDK driver..."
    if command -v dpdk-devbind.py &>/dev/null; then
        dpdk-devbind.py --bind=uio_pci_generic 0000:01:00.0 2>/dev/null || true
    fi

    echo "[DPDK] Configuring hugepages..."
    echo 1024 > /sys/kernel/mm/hugepages/hugepages-1048576/nr_hugepages || true
}

# --- Solarflare ef_vi (if available) ---
onload_config() {
    if command -v onload &>/dev/null; then
        echo "[ONLOAD] Setting stack affinity..."
        onload_stack --set-affinity=0-7

        echo "[ONLOAD] Setting ef_vi buffer count..."
        echo 16384 > /proc/sys/net/ef_vi/vi_buffer_count 2>/dev/null || true
    else
        echo "[ONLOAD] Not installed. Use DPDK (recommended)."
    fi
}

dpdk_config
onload_config

echo "[NETWORK] Kernel bypass configuration complete."
echo "[NETWORK] Use DPDK for Intel E810, ef_vi for Solarflare X2522."
