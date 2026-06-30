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

curl -m 5 -v -X POST -H 'Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJyb2Jpbi1zZXJ2aWNlcyIsImV4cCI6MTgxNDE4Nzk4OCwiaXNzIjoicm9iaW4tZ2F0ZXdheSIsInJvbGUiOiJ0cmFkZXIifQ.siyGhCSAyHQ1RUE_W9saaJiUu-G2e7waxvLH4IVoPoQ' -H 'Content-Type: application/json' -d '{"symbol":"BTC/USD","side":"BUY","price":64000.0,"qty":1.0,"order_type":"LIMIT","cl_ord_id":"client-test"}' http://127.0.0.1:18080/order > ./curl.log 2>&1

kill -9 $EXEC_PID $RISK_PID $ORCH_PID 2>/dev/null
