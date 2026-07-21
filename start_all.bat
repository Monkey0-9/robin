@echo off
setlocal EnableDelayedExpansion

echo ========================================================
echo ROBIN MULTI-AGENT AUTONOMOUS QUANTITATIVE TRADING SYSTEM
echo ========================================================
echo Starting native processes for max efficiency...

cd /d C:\Robin

:: Check environment
if not exist .env (
    echo Error: .env file not found
    exit /b 1
)

:: 1. Start Python AI / Data Engine in the background
echo [1/3] Starting Python AI Engine (Background)...
cd services\ai-agent
start "Robin AI Agent" cmd /c "python main.py"
cd ..\..

:: 2. Start C++ Matching Engine (if running paper/sandbox)
echo [2/3] Starting C++ Matching Engine (Hot Path)...
cd services\execution-core
start "Robin Matching Engine" cmd /c "build\matching_engine.exe 9091"
cd ..\..

:: 3. Start IPC Pipeline: C++ Live Feed -> Go OMS
echo [3/3] Starting High-Speed Pipeline (C++ Feed -> Go OMS)...
:: The C++ live feed outputs JSON signals to stdout
:: The Go OMS reads JSON signals from stdin
start "Robin Strategy & OMS Pipeline" cmd /c "set ROBIN_JWT_PUBKEY_FILE=C:\Robin\config\keys\public.pem&& cd services\execution-core && build\live_feed.exe | cd ..\gateway && go run ."

echo ========================================================
echo ALL AGENTS BOOTED NATIVELY.
echo See individual terminal windows for logs.
echo ========================================================
pause
