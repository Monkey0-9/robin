#!/bin/bash
set -euo pipefail

PARALLEL=$(nproc 2>/dev/null || echo 4)
BUILD_TYPE=${BUILD_TYPE:-Release}

log() { echo "[$(date '+%Y-%m-%dT%H:%M:%S%z')] $*"; }

check_compiler() {
    log "Checking compiler..."
    if command -v g++-12 &>/dev/null; then
        CXX=g++-12
        CC=gcc-12
    elif command -v g++ &>/dev/null; then
        CXX=g++
        CC=gcc
    else
        log "FATAL: No C++ compiler found"
        exit 1
    fi
    log "Using: ${CXX}"
    ${CXX} --version | head -1
}

check_rust() {
    log "Checking Rust..."
    if ! command -v cargo &>/dev/null; then
        log "Installing Rust..."
        curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
        source "${HOME}/.cargo/env"
    fi
    log "Using: $(cargo --version)"
}

check_go() {
    log "Checking Go..."
    if ! command -v go &>/dev/null; then
        log "Go not found, install from https://go.dev/dl/"
    else
        log "Using: $(go version)"
    fi
}

check_ocaml() {
    log "Checking OCaml..."
    if ! command -v opam &>/dev/null; then
        log "OCaml not found, install from https://opam.ocaml.org/"
    else
        log "Using: $(opam --version)"
    fi
}

install_system_deps() {
    log "Installing system dependencies..."
    if command -v apt-get &>/dev/null; then
        sudo apt-get update -qq
        sudo apt-get install -y -qq \
            build-essential cmake g++-12 \
            libnuma-dev libtbb-dev libboost-dev \
            libpcap-dev linux-tools-common \
            linux-tools-generic pkg-config \
            libssl-dev libelf-dev
    elif command -v yum &>/dev/null; then
        sudo yum install -y \
            gcc-c++ cmake numactl-devel tbb-devel \
            boost-devel libpcap-devel \
            openssl-devel elfutils-libelf-devel
    fi
}

build_cpp() {
    log "=== Building C++ Components ==="
    local dirs=(
        "services/execution-core"
        "services/network-bridge"
        "services/ingestion"
        "services/hardware-fpga"
        "research/pricing"
        "research/ai-engine"
    )

    for dir in "${dirs[@]}"; do
        if [ -f "${dir}/CMakeLists.txt" ]; then
            log "Building ${dir}..."
            mkdir -p "${dir}/build"
            (cd "${dir}/build" && \
             cmake .. -DCMAKE_BUILD_TYPE=${BUILD_TYPE} -DCMAKE_CXX_COMPILER=${CXX} && \
             cmake --build . --parallel ${PARALLEL})
            log "[OK] ${dir} built"
        fi
    done
}

build_rust() {
    log "=== Building Rust Components ==="
    source "${HOME}/.cargo/env"
    local dirs=(
        "services/risk-analytics"
        "services/compliance"
    )

    for dir in "${dirs[@]}"; do
        if [ -f "${dir}/Cargo.toml" ]; then
            log "Building ${dir}..."
            (cd "${dir}" && cargo build --release)
            log "[OK] ${dir} built"
        fi
    done
}

build_go() {
    log "=== Building Go Components ==="
    if [ -f "services/gateway/go.mod" ] && command -v go &>/dev/null; then
        mkdir -p services/gateway/bin
        (cd services/gateway && go build -ldflags="-s -w" -o bin/orchestrator .)
        log "[OK] Go gateway built"
    fi
}

build_ocaml() {
    log "=== Building OCaml Components ==="
    if command -v dune &>/dev/null; then
        local dirs=(
            "research/portfolio"
            "services/compliance"
        )
        for dir in "${dirs[@]}"; do
            if [ -f "${dir}/dune" ]; then
                log "Building ${dir}..."
                (cd "${dir}" && dune build)
                log "[OK] ${dir} built"
            fi
        done
    fi
}

run_tests() {
    log "=== Running Tests ==="
    source "${HOME}/.cargo/env"

    log "Rust tests..."
    for dir in services/risk-analytics services/compliance; do
        if [ -f "${dir}/Cargo.toml" ]; then
            (cd "${dir}" && cargo test --release) && log "[OK] ${dir} tests passed" || log "[WARN] ${dir} tests"
        fi
    done
}

log "============================================"
log "  Robin Trading Platform - Bare Metal Build"
log "  $(date)"
log "  Build type: ${BUILD_TYPE}"
log "  Parallel: ${PARALLEL}"
log "============================================"

check_compiler
check_rust
check_go
check_ocaml
install_system_deps

build_cpp
build_rust
build_go
build_ocaml
run_tests

log "============================================"
log "  BUILD COMPLETE"
log "============================================"
log ""
log "To start services: sudo ./scripts/start_native.sh"
log "To run benchmarks: make benchmark"
log "To run chaos: make chaos"
