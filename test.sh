#!/bin/bash
services/execution-core/build/matching_engine.exe 9091 > ./exec.log 2>&1 &
EXEC_PID=$!
target/release/robin-risk-daemon.exe > ./risk.log 2>&1 &
RISK_PID=$!
ROBIN_GATEWAY_API_TOKEN=smoke-test-secret ORCH_PORT=18080 build/orchestrator.exe > ./orch.log 2>&1 &
ORCH_PID=$!

sleep 2

curl -m 5 -v -X POST -H 'Authorization: Bearer smoke-test-secret' -H 'Content-Type: application/json' -d '{"symbol":"BTC/USD","side":"BUY","price":64000.0,"qty":1.0,"order_type":"LIMIT","cl_ord_id":"client-test"}' http://127.0.0.1:8080/order > ./curl.log 2>&1

kill -9 $EXEC_PID $RISK_PID $ORCH_PID 2>/dev/null
