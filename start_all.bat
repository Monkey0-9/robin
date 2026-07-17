@echo off
echo ========================================================
echo ROBIN MULTI-AGENT AUTONOMOUS QUANTITATIVE TRADING SYSTEM
echo ========================================================
echo Starting native processes...

cd /d C:\Robin

set ROBIN_GATEWAY_API_TOKEN=super_secret_test_key

echo [1/8] Starting Risk Gate (Rust)...
cd services\risk-analytics
start "Robin Risk Gate (Rust)" cmd /c "cargo run --release"
cd ..\..

echo [2/8] Starting Execution Core (C++)...
cd services\execution-core
start "Robin Execution Core" cmd /c "build\matching_engine.exe 9091"
cd ..\..

echo [3/8] Starting Go Meta-Agent Gateway...
cd services\gateway
start "Robin Go Meta-Agent" cmd /c "go run orchestrator.go main.go auth.go config.go tracing.go websocket.go fix_gateway.go ai_prompt.go"
cd ..\..

echo [4/4] Starting Unified Python AI Agent...
cd services\ai-agent
start "Robin Unified AI Agent" cmd /c "python main.py"
cd ..\..

echo ========================================================
echo ALL AGENTS BOOTED NATIVELY.
echo See individual terminal windows for logs.
echo ========================================================
pause
