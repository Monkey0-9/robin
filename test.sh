#!/bin/bash
services/execution-core/build/matching_engine.exe 9091 > ./exec.log 2>&1 &
EXEC_PID=$!
target/release/robin-risk-daemon.exe > ./risk.log 2>&1 &
RISK_PID=$!
export ROBIN_GATEWAY_API_TOKEN=smoke-test-secret
export ORCH_PORT=18080
build/orchestrator.exe > ./orch.log 2>&1 &
ORCH_PID=$!

sleep 2

python test_order.py > ./curl.log 2>&1

kill -9 $EXEC_PID $RISK_PID $ORCH_PID 2>/dev/null
