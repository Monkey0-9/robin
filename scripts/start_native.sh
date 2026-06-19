#!/bin/bash
# Development startup script for Robin trading platform prototype.
# Starts available services - will skip components that aren't built.
set -euo pipefail

BASE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$BASE_DIR"

echo "[Robin] Starting trading platform prototype..."
echo "[Robin] NOTE: Only built services will start. Missing binaries are skipped."

mkdir -p pids logs

# Start Go orchestrator (if built)
if [ -f ./build/orchestrator ]; then
    echo "[Robin] Starting Go orchestrator..."
    ./build/orchestrator &
    echo $! > pids/orch.pid
    echo "[Robin] Orchestrator started (PID $(cat pids/orch.pid))"
else
    echo "[Robin] Orchestrator not built. Run 'make build-go' first."
fi

# Start C++ matching engine (if built)
if [ -f ./services/execution-core/build/matching_engine ]; then
    echo "[Robin] Starting matching engine..."
    ./services/execution-core/build/matching_engine &
    echo $! > pids/match.pid
    echo "[Robin] Matching engine started (PID $(cat pids/match.pid))"
else
    echo "[Robin] Matching engine not built. Run 'make build-cpp' first."
fi

echo "[Robin] Startup complete."
echo "  PID files in: pids/"
echo "  Logs in:      logs/"

trap 'echo "Shutting down..."; for f in pids/*.pid; do kill $(cat "$f" 2>/dev/null) 2>/dev/null || true; done; exit' SIGINT SIGTERM
wait
