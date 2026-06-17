#!/bin/bash
# build_bare_metal.sh
# System dependency resolution and native builds driver for all Quantum Terminal components.

set -e

echo "===================================================================="
echo "Initializing Quantum Terminal Bare-Metal Toolchains & Compilation"
echo "===================================================================="

# 1. System packages
if command -v apt-get &> /dev/null; then
    echo "Installing platform dependencies..."
    sudo apt-get update
    sudo apt-get install -y build-essential cmake pkg-config libssl-dev opam libboost-all-dev
fi

# 2. Rust toolchain setup
if ! command -v cargo &> /dev/null; then
    echo "Installing Rust compiler..."
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
    source "$HOME/.cargo/env"
fi

# 3. OCaml package manager setup
if command -v opam &> /dev/null; then
    echo "Configuring OCaml environment..."
    opam init -a -y --disable-sandboxing
    eval $(opam env)
    opam install -y dune
fi

# 4. Trigger main Makefile targets
echo "Compiling C++, Rust, and OCaml modules..."
make all

echo "===================================================================="
echo "Quantum Terminal compiled successfully. Ready for low-latency loop."
echo "===================================================================="
exit 0
