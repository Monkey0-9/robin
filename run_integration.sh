#!/usr/bin/env bash
set -e

echo "Starting Robin Platform Integration Test..."

# Start matching engine
echo "Starting Matching Engine..."
./services/execution-core/build/matching_engine 9091 > exec_out.log 2> exec_err.log &
MATCHING_ENGINE_PID=$!
echo "Matching Engine PID: $MATCHING_ENGINE_PID"

# Start risk daemon
echo "Starting Risk Daemon..."
cd services/risk-analytics
../../target/release/robin-risk-daemon > ../../risk_out.log 2> ../../risk_err.log &
RISK_DAEMON_PID=$!
echo "Risk Daemon PID: $RISK_DAEMON_PID"
cd ../..

# Start gateway and live feed
echo "Starting Gateway/Feed Pipeline..."
export ROBIN_JWT_PUBKEY_FILE="./config/keys/public.pem"
export ROBIN_MASTER_KEY="12345678901234567890123456789012"
export ROBIN_DB_PASSPHRASE="12345678901234567890123456789012"
export ORCH_MTLS_ENABLED="1"
export ORCH_TLS_CERT="./config/certs/server.crt"
export ORCH_TLS_KEY="./config/certs/server.key"
export ORCH_CA_CERT="./config/certs/ca.crt"

cd services/gateway
../execution-core/build/live_feed | ./robin-gateway > ../../orch_out.log 2> ../../orch_err.log &
GATEWAY_PID=$!
echo "Gateway/Feed Pipeline PID: $GATEWAY_PID"
cd ../..

echo "Waiting 10 seconds for services to initialize..."
sleep 10

echo "Running single test order..."
python3 test_order.py

echo "Running short load test..."
python3 tests/load_test.py --target https://localhost:8080 --scenario baseline --duration 3

echo "Cleaning up processes..."
kill $MATCHING_ENGINE_PID 2>/dev/null || true
kill $RISK_DAEMON_PID 2>/dev/null || true
kill $GATEWAY_PID 2>/dev/null || true
pkill -f robin-risk-daemon || true
pkill -f robin-gateway || true
pkill -f live_feed || true
pkill -f matching_engine || true

echo "Integration Test Complete."
