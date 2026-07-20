@echo off
echo ========================================================
echo ROBIN MULTI-AGENT AUTONOMOUS QUANTITATIVE TRADING SYSTEM
echo ========================================================
echo Starting native processes...

cd /d C:\Robin

if "%ROBIN_GATEWAY_API_TOKEN%"=="" (
    echo Error: ROBIN_GATEWAY_API_TOKEN must be set
    exit /b 1
)

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
start "Robin Go Meta-Agent" cmd /c "go run ."
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
