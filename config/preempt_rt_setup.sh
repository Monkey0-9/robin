#!/bin/bash
set -euo pipefail

log() { echo "[$(date '+%Y-%m-%dT%H:%M:%S%z')] $*"; }

configure_grub() {
    log "=== GRUB Configuration ==="

    local cmdline="isolcpus=2-15 nohz_full=2-15 rcu_nocbs=2-15"
    cmdline+=" intel_idle.max_cstate=0 processor.max_cstate=0"
    cmdline+=" intel_pstate=disable mce=off"
    cmdline+=" skew_tick=1 rcutree.kthread_prio=99"
    cmdline+=" nmi_watchdog=0 audit=0 nosoftlockup"
    cmdline+=" tsc=reliable clocksource=tsc"
    cmdline+=" transparent_hugepage=never"

    if [ -f /etc/default/grub ]; then
        sed -i "s/GRUB_CMDLINE_LINUX_DEFAULT=\".*\"/GRUB_CMDLINE_LINUX_DEFAULT=\"${cmdline}\"/" /etc/default/grub
        update-grub
        log "[OK] GRUB updated with RT parameters"
    else
        log "[WARN] /etc/default/grub not found"
    fi
}

set_kernel_params() {
    log "=== Runtime Kernel Parameters ==="

    sysctl -w kernel.numa_balancing=0
    sysctl -w kernel.sched_rt_runtime_us=-1
    sysctl -w kernel.sched_rr_timeslice_ms=1
    sysctl -w kernel.sched_min_granularity_ns=1000000
    sysctl -w kernel.sched_wakeup_granularity_ns=2000000
    sysctl -w kernel.sched_migration_cost_ns=500000
    sysctl -w kernel.sched_autogroup_enabled=0

    sysctl -w vm.swappiness=1
    sysctl -w vm.dirty_ratio=5
    sysctl -w vm.dirty_background_ratio=2
    sysctl -w vm.page-cluster=0
    sysctl -w vm.stat_interval=10

    sysctl -w net.core.rmem_max=134217728
    sysctl -w net.core.wmem_max=134217728
    sysctl -w net.core.netdev_budget=600
    sysctl -w net.core.netdev_budget_usecs=2000
    sysctl -w net.ipv4.tcp_fastopen=3

    log "[OK] Kernel parameters set"
}

disable_irqbalance() {
    log "=== IRQ Balance ==="
    systemctl stop irqbalance 2>/dev/null || true
    systemctl disable irqbalance 2>/dev/null || true
    log "[OK] irqbalance disabled"
}

setup_ksoftirqd() {
    log "=== ksoftirqd Configuration ==="
    for pid in $(pgrep ksoftirqd); do
        chrt -f -p 99 "${pid}" 2>/dev/null || true
    done
    log "[OK] ksoftirqd set to FIFO priority 99"
}

disable_power_savings() {
    log "=== Power Savings ==="
    for cpu in /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor; do
        echo performance > "${cpu}" 2>/dev/null || true
    done

    echo 0 > /sys/devices/system/cpu/intel_pstate/no_turbo 2>/dev/null || true
    echo 0 > /sys/devices/system/cpu/cpuidle/current_driver 2>/dev/null || true

    log "[OK] Power savings disabled"
}

log "=== PREEMPT_RT Setup ==="
echo "Target: Real-time Linux kernel for trading"
uname -r

case "${1:-all}" in
    all)
        set_kernel_params
        disable_irqbalance
        setup_ksoftirqd
        disable_power_savings
        configure_grub
        ;;
    runtime)
        set_kernel_params
        disable_irqbalance
        setup_ksoftirqd
        disable_power_savings
        ;;
    grub)
        configure_grub
        ;;
    *)
        echo "Usage: $0 [all|runtime|grub]"
        ;;
esac

log "[OK] PREEMPT_RT setup complete. Reboot recommended."
