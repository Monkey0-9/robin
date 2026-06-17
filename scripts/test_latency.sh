#!/bin/bash
# scripts/test_latency.sh
# Latency benchmark suite for matching engine and risk gate loops.
# Measures execution times and fails if average latency exceeds 1 millisecond.

echo "===================================================================="
echo "Running Low-Latency Execution Core Performance Benchmark"
echo "===================================================================="

# Compile execution engine if needed
if [ -f "./services/execution-core/src/matching_engine.cpp" ]; then
    echo "Compiling matching engine benchmark target..."
    g++ -O3 -march=native -std=c++20 \
        -I./services/execution-core/src \
        ./services/execution-core/src/matching_engine.cpp \
        -o ./services/execution-core/matching_engine_bench -lpthread
else
    echo "ERROR: Matching engine source not found."
    exit 1
fi

# Run matching engine bench and capture execution logs
echo "Injecting test orders into lock-free execution ring buffers..."
START_TIME=$(date +%s%N)
./services/execution-core/matching_engine_bench > /dev/null
END_TIME=$(date +%s%N)

ELAPSED_NS=$((END_TIME - START_TIME))
ELAPSED_MS=$((ELAPSED_NS / 1000000))

echo "Benchmark execution completed in $ELAPSED_MS ms."

# Simple latency calculation verification (1000 orders simulated)
AVG_LATENCY_NS=$((ELAPSED_NS / 1000))
AVG_LATENCY_US=$((AVG_LATENCY_NS / 1000))

echo "--------------------------------------------------------------------"
echo "Average latency per order: $AVG_LATENCY_US microseconds"
echo "--------------------------------------------------------------------"

if [ $AVG_LATENCY_US -gt 1000 ]; then
    echo "CRITICAL WARNING: Average latency exceeds 1ms target threshold!"
    exit 1
else
    echo "SUCCESS: Latency within acceptable boundary (<1ms)."
    exit 0
fi
