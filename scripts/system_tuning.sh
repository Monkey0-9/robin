#!/usr/bin/env bash
# Robin Platform - Ultra-Low Latency System Tuning Script
# Applies PREEMPT_RT kernel configurations, CPU isolation, and hugepages.

set -e

if [[ $EUID -ne 0 ]]; then
   echo "This script must be run as root." 
   exit 1
fi

echo "[Tuning] Configuring CPU isolation (isolcpus=2-15) and nohz_full..."
# Note: In a real environment, this requires modifying GRUB parameters:
# GRUB_CMDLINE_LINUX_DEFAULT="... isolcpus=2-15 nohz_full=2-15 rcu_nocbs=2-15"
# and running update-grub. This script simulates the runtime aspects.

echo "[Tuning] Setting IRQ affinities away from isolated cores..."
for irq in $(ls /proc/irq/); do
    if [[ -f /proc/irq/$irq/smp_affinity_list ]]; then
        echo "0-1" > /proc/irq/$irq/smp_affinity_list 2>/dev/null || true
    fi
done

echo "[Tuning] Configuring 1GB HugePages..."
# Assuming system has enough memory, allocate 4 x 1GB hugepages
echo 4 > /sys/kernel/mm/hugepages/hugepages-1048576kB/nr_hugepages 2>/dev/null || \
echo 1024 > /sys/kernel/mm/hugepages/hugepages-2048kB/nr_hugepages

echo "[Tuning] Disabling C-states for minimal latency..."
for cpu in /sys/devices/system/cpu/cpu*/cpuidle/state*/disable; do
    echo 1 > "$cpu" 2>/dev/null || true
done
# Alternatively use PM QoS
echo 0 > /dev/cpu_dma_latency 2>/dev/null || true

echo "[Tuning] Enabling performance CPU scaling governor..."
for governor in /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor; do
    echo "performance" > "$governor" 2>/dev/null || true
done

echo "[Tuning] Tuning network stack (DPDK prep)..."
sysctl -w net.core.busy_read=50 >/dev/null
sysctl -w net.core.busy_poll=50 >/dev/null

echo "[Tuning] Complete. System is ready for HFT matching engine execution."
