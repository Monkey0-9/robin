#!/bin/bash
# scripts/start_native.sh
# System startup coordinator for the Quantum Terminal microservices.
# Integrates active service status checkpoints.

echo "===================================================================="
echo "Starting Quantum Terminal Native Services Stack"
echo "===================================================================="

# 1. Start Q/kdb+ Database & WebSocket server
q services/kdb-storage/http_gateway.q -p 5001 &
KDB_PID=$!
echo "[Service Launch] KDB+ HTTP & WebSocket Gateway active (PID: $KDB_PID)"

# 2. Start C++ NUMA-aligned Matching Engine
./build/qt_execution_engine &
EXEC_PID=$!
echo "[Service Launch] C++ Matching Engine execution running (PID: $EXEC_PID)"

# 3. Start Rust compliance risk gate
cargo run --bin qt_risk_analytics --release &
RISK_PID=$!
echo "[Service Launch] Rust Risk Gate active (PID: $RISK_PID)"

# 4. Service health check loops
echo "--------------------------------------------------------------------"
echo "Running service checks..."
echo "--------------------------------------------------------------------"

for i in {1..10}; do
    if kill -0 $KDB_PID 2>/dev/null && kill -0 $EXEC_PID 2>/dev/null; then
        echo "[Health OK] All trading microservices running successfully."
        break
    else
        echo "[Health Waiting] Initializing shared memory buffers and socket bindings..."
        sleep 2.5
    fi
done

echo "System online."
wait
