#!/bin/bash
# Production startup script for Robin trading platform.
# Starts all hot-path services in order with proper NUMA pinning.
set -euo pipefail

export LD_LIBRARY_PATH=/usr/local/lib:$LD_LIBRARY_PATH
export RUST_LOG=info

BASE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$BASE_DIR"

echo "[Robin] Starting trading platform..."

# Phase 1: Shared memory initialization
echo "[Robin] Initializing shared memory..."
SHM_DIR=/dev/shm/robin
mkdir -p "$SHM_DIR"
# Create mmap-backed ring buffers for zero-FFI IPC between C++, Rust, OCaml
# Each buffer: header (64B) + 65536 messages x 64B = 4MB per ring
for ring in risk_to_match match_to_risk network_to_risk; do
    dd if=/dev/zero of="$SHM_DIR/$ring" bs=4194304 count=1 2>/dev/null
done
echo "[Robin] Shared memory rings created in $SHM_DIR"

# Phase 2: DPDK network layer (NUMA node 0, cores 2-3)
echo "[Robin] Starting DPDK network ingest..."
numactl --cpubind=0 --membind=0 \
    ./build/dpdk_ingest &
PID_DPDK=$!
echo $PID_DPDK > pids/dpdk.pid

# Phase 3: Rust risk gate (NUMA node 0, cores 4-5)
echo "[Robin] Starting Rust risk gate..."
numactl --cpubind=0 --membind=0 \
    ./build/robin_risk &
PID_RISK=$!
echo $PID_RISK > pids/risk.pid

# Phase 4: OCaml matching engine (NUMA node 0, cores 6-7)
echo "[Robin] Starting OCaml matching engine..."
numactl --cpubind=0 --membind=0 \
    ./build/robin_match &
PID_MATCH=$!
echo $PID_MATCH > pids/match.pid

# Phase 5: KDB+ tick database (WARM PATH, cores 8-9)
echo "[Robin] Starting KDB+ tick database (warm path)..."
numactl --cpubind=0 --membind=0 \
    q services/kdb-storage/tick_db.q &
PID_KDB=$!
echo $PID_KDB > pids/kdb.pid

# Phase 6: Go orchestrator
echo "[Robin] Starting Go orchestrator..."
numactl --cpubind=0 --membind=0 \
    ./build/orchestrator &
PID_ORCH=$!
echo $PID_ORCH > pids/orch.pid

echo "[Robin] All services started."
echo "  DPDK:     $PID_DPDK"
echo "  Risk:     $PID_RISK"
echo "  Match:    $PID_MATCH"
echo "  KDB+:     $PID_KDB"
echo "  Orchestr: $PID_ORCH"

# Trap for graceful shutdown
trap 'echo "Shutting down..."; kill $PID_DPDK $PID_RISK $PID_MATCH $PID_KDB $PID_ORCH 2>/dev/null; exit' SIGINT SIGTERM

wait
