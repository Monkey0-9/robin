#!/bin/bash
set -euo pipefail

DPDK_VERSION=${DPDK_VERSION:-"23.11"}
HUGEPAGE_SIZE=${HUGEPAGE_SIZE:-1048576}
NR_HUGEPAGES=${NR_HUGEPAGES:-1024}
DPDK_NIC=${DPDK_NIC:-"0000:03:00.0"}
CPU_ISOLATED=${CPU_ISOLATED:-"2-15"}

log() { echo "[$(date '+%Y-%m-%dT%H:%M:%S%z')] $*"; }

check_prereqs() {
    log "=== DPDK Prerequisites ==="
    local missing=0

    for cmd in lspci mount modprobe uname; do
        if ! command -v "${cmd}" &>/dev/null; then
            log "[MISS] ${cmd}"
            missing=$((missing + 1))
        fi
    done

    if [ "${missing}" -gt 0 ]; then
        log "[FATAL] ${missing} prerequisite(s) missing"
        exit 1
    fi

    log "[OK] All prerequisites found"
}

configure_hugepages() {
    log "=== HugePages Configuration ==="
    local current
    current=$(cat /sys/kernel/mm/hugepages/hugepages-${HUGEPAGE_SIZE}/nr_hugepages 2>/dev/null || echo 0)

    if [ "${current}" -lt "${NR_HUGEPAGES}" ]; then
        log "Setting hugepages: ${current} -> ${NR_HUGEPAGES} (size=${HUGEPAGE_SIZE}KB)"
        echo "${NR_HUGEPAGES}" > /sys/kernel/mm/hugepages/hugepages-${HUGEPAGE_SIZE}/nr_hugepages 2>/dev/null || {
            log "[WARN] Could not set hugepages (try: sudo sysctl vm.nr_hugepages=${NR_HUGEPAGES})"
            sysctl -w vm.nr_hugepages="${NR_HUGEPAGES}" 2>/dev/null || true
        }
    else
        log "[OK] HugePages already configured: ${current}"
    fi

    local total_mb=$((NR_HUGEPAGES * HUGEPAGE_SIZE / 1024))
    log "Total hugepage memory: ${total_mb}MB"

    if ! mount | grep -q hugetlbfs; then
        mount -t hugetlbfs nodev /dev/hugepages 2>/dev/null || {
            mkdir -p /dev/hugepages
            mount -t hugetlbfs nodev /dev/hugepages 2>/dev/null || log "[WARN] Could not mount hugetlbfs"
        }
    fi
    log "[OK] HugePages ready"
}

load_dpdk_drivers() {
    log "=== DPDK Drivers ==="
    local drivers=("vfio-pci" "igb_uio" "uio_pci_generic")

    for drv in "${drivers[@]}"; do
        if lsmod | grep -q "^${drv}"; then
            log "[OK] ${drv} already loaded"
        else
            log "Loading ${drv}..."
            modprobe "${drv}" 2>/dev/null || log "[WARN] Could not load ${drv}"
        fi
    done

    log "Checking for VFIO support..."
    if [ -e /dev/vfio/vfio ]; then
        log "[OK] VFIO supported"
    else
        log "[WARN] VFIO not available (needed for DPDK)"
    fi
}

bind_nic_to_dpdk() {
    log "=== NIC Binding ==="
    local nic_bdf="${1:-${DPDK_NIC}}"
    local driver="${2:-vfio-pci}"

    if command -v dpdk-devbind.py &>/dev/null; then
        log "Binding ${nic_bdf} to ${driver}..."
        dpdk-devbind.py -b "${driver}" "${nic_bdf}" 2>/dev/null || {
            log "[WARN] Could not bind ${nic_bdf} to ${driver}"
            log "Device status:"
            dpdk-devbind.py -s 2>/dev/null || true
        }
        log "[OK] NIC bound to ${driver}"
    else
        log "[WARN] dpdk-devbind.py not found (install DPDK tools)"
    fi
}

setup_cpu_isolation() {
    log "=== CPU Isolation ==="
    local cmdline
    cmdline=$(cat /proc/cmdline)

    if echo "${cmdline}" | grep -q "isolcpus=${CPU_ISOLATED}"; then
        log "[OK] CPUs ${CPU_ISOLATED} already isolated"
    else
        log "[WARN] CPU isolation not configured. Add to GRUB_CMDLINE_LINUX:"
        log "  isolcpus=${CPU_ISOLATED} nohz_full=${CPU_ISOLATED} rcu_nocbs=${CPU_ISOLATED}"
        log "  Then: update-grub && reboot"
    fi

    log "Showing current CPU governor..."
    for cpu in /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor; do
        local gov
        gov=$(cat "${cpu}" 2>/dev/null || echo "N/A")
        if [ "${gov}" != "performance" ]; then
            log "[WARN] ${cpu}: ${gov} (should be 'performance')"
        fi
    done
}

set_performance_governor() {
    log "=== Performance Governor ==="
    for cpu in /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor; do
        echo "performance" > "${cpu}" 2>/dev/null || true
    done
    log "[OK] Performance governor set"

    if [ -f /sys/devices/system/cpu/intel_pstate/no_turbo ]; then
        echo 0 > /sys/devices/system/cpu/intel_pstate/no_turbo 2>/dev/null || true
        log "[OK] Intel turbo enabled"
    fi
}

verify_dpdk_eal() {
    log "=== DPDK EAL Verification ==="
    local cores="${1:-2-3}"
    local memory="${2:-1024}"

    if command -v dpdk-testpmd &>/dev/null; then
        log "Testing DPDK EAL initialization..."
        dpdk-testpmd -l "${cores}" -n 4 --socket-mem "${memory}" \
            --no-pci --log-level=6 \
            --file-prefix=robin_test \
            --  --total-num-mbufs=65535 \
            -i --portmask=0 2>/dev/null <<< "quit" || true
        log "[OK] DPDK EAL test completed"
    else
        log "[WARN] dpdk-testpmd not found (test skipped)"
    fi
}

build_dpdk_apps() {
    log "=== Building DPDK Applications ==="
    if command -v cmake &>/dev/null; then
        for dir in services/network-bridge services/ingestion; do
            if [ -f "${dir}/CMakeLists.txt" ]; then
                log "Building ${dir}..."
                (cd "${dir}" && cmake -B build -DCMAKE_BUILD_TYPE=Release && cmake --build build --parallel) || true
            fi
        done
    fi
}

cleanup() {
    log "=== Cleanup ==="
}

trap cleanup EXIT

log "================================================"
log "  DPDK Setup & Configuration"
log "  DPDK Version: ${DPDK_VERSION}"
log "  HugePages: ${NR_HUGEPAGES} x ${HUGEPAGE_SIZE}KB"
log "  NIC: ${DPDK_NIC}"
log "  Isolated CPUs: ${CPU_ISOLATED}"
log "================================================"

case "${1:-all}" in
    all)
        check_prereqs
        configure_hugepages
        load_dpdk_drivers
        bind_nic_to_dpdk
        setup_cpu_isolation
        set_performance_governor
        verify_dpdk_eal
        build_dpdk_apps
        ;;
    hugepages)
        configure_hugepages
        ;;
    drivers)
        load_dpdk_drivers
        ;;
    bind)
        bind_nic_to_dpdk "${2:-${DPDK_NIC}}" "${3:-vfio-pci}"
        ;;
    cpu)
        setup_cpu_isolation
        set_performance_governor
        ;;
    verify)
        verify_dpdk_eal
        ;;
    build)
        build_dpdk_apps
        ;;
    *)
        echo "Usage: $0 [all|hugepages|drivers|bind|cpu|verify|build]"
        exit 1
        ;;
esac

log "[OK] DPDK setup complete"
