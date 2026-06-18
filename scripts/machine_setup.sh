#!/bin/bash
# Bare-metal machine setup for Robin Trading Platform
# Run as root on fresh Ubuntu 22.04 LTS installation
set -euo pipefail

ROBIN_USER="${SUDO_USER:-$USER}"
ROBIN_HOME=$(eval echo "~$ROBIN_USER")

echo "[ROBIN] Starting machine setup..."

apt-get update && apt-get upgrade -y

apt-get install -y \
    build-essential cmake g++-12 \
    libnuma-dev libpcap-dev libssl-dev \
    linux-tools-common linux-tools-generic \
    pkg-config python3 python3-pip \
    lld clang-format clang-tidy \
    git curl wget htop iotop \
    sysstat net-tools ethtool \
    rdma-core libibverbs-dev \
    librdmacm-dev perftest

if command -v rustc &>/dev/null; then
    rustup update stable
else
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
    source "$ROBIN_HOME/.cargo/env"
fi

pip3 install numpy pandas scipy scikit-learn pytest

if command -v ocaml &>/dev/null; then
    opam update
else
    bash -c "sh <(curl -fsSL https://raw.githubusercontent.com/ocaml/opam/master/shell/install.sh)"
fi

cat >> /etc/security/limits.conf <<EOF
$ROBIN_USER soft memlock unlimited
$ROBIN_USER hard memlock unlimited
$ROBIN_USER soft nofile 1048576
$ROBIN_USER hard nofile 1048576
$ROBIN_USER soft rtprio 99
$ROBIN_USER hard rtprio 99
EOF

cat >> /etc/sysctl.conf <<EOF
net.core.rmem_max=134217728
net.core.wmem_max=134217728
net.core.rmem_default=16777216
net.core.wmem_default=16777216
net.ipv4.tcp_rmem=4096 87380 134217728
net.ipv4.tcp_wmem=4096 65536 134217728
net.core.netdev_budget=600
net.core.netdev_budget_usecs=4000
vm.nr_hugepages=32768
vm.max_map_count=262144
kernel.numa_balancing=0
EOF
sysctl -p

echo "[ROBIN] Setup complete. Reboot recommended for RT kernel / hugepages."
