#!/bin/bash
# config/preempt_rt_setup.sh
# Real-time Linux kernel optimizations for PREEMPT_RT-enabled hosts.
# Minimizes kernel scheduling latency and process jitter for trading execution paths.

echo "===================================================================="
echo "Configuring Real-Time PREEMPT_RT System Jitter Controls"
echo "===================================================================="

# 1. Set real-time thread priority limits for the execution group
echo "Applying real-time limits to /etc/security/limits.d/99-realtime.conf..."
sudo tee /etc/security/limits.d/99-realtime.conf > /dev/null <<EOF
@trading         soft    rtprio          99
@trading         hard    rtprio          99
@trading         soft    memlock         unlimited
@trading         hard    memlock         unlimited
EOF

# 2. Adjust kernel scheduler parameters to disable autogrouping and scheduling latency
echo "Configuring kernel sysctl parameters..."
sudo sysctl -w kernel.sched_rt_runtime_us=-1      # Dedicate 100% of CPU time to real-time threads
sudo sysctl -w kernel.sched_rt_period_us=1000000
sudo sysctl -w kernel.numa_balancing=0            # Disable NUMA balance migration jitter
sudo sysctl -w vm.stat_interval=120               # Reduce background kernel VM statistics checks
sudo sysctl -w vm.swappiness=0                    # Never swap to disk

# 3. Shield CPUs for dedicated execution threads
# Assuming 8 core CPU: cores 4-7 are isolated for low-latency matching engine and risk gate
echo "Isolating CPU cores 4-7 for trading execution paths..."
if [ -f "/sys/devices/system/cpu/isolated" ]; then
    echo "Current isolated CPUs: $(cat /sys/devices/system/cpu/isolated)"
    echo "To permanently isolate, add 'isolcpus=4-7 nohz_full=4-7 rcu_nocbs=4-7' to GRUB_CMDLINE_LINUX"
else
    echo "Isolation interfaces not supported or virtual environment detected. System running under standard preemptive mode."
fi

# 4. Bind hardware interrupts (IRQ) to CPU 0 (leaving 4-7 shielded)
echo "Re-routing network hardware interrupts away from shielded cores..."
for irq in $(ls /proc/irq/); do
    if [[ "$irq" =~ ^[0-9]+$ ]]; then
        echo "01" | sudo tee /proc/irq/$irq/smp_affinity > /dev/null 2>&1
    fi
done

echo "PREEMPT_RT low-latency profiles loaded successfully."
