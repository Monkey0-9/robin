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

:: ── Load .env into the current cmd session ───────────────────────────────────
:: Each non-comment, non-blank KEY=VALUE line is exported as an env var.
for /F "usebackq tokens=1,* delims==" %%A in (".env") do (
    set "line=%%A"
    if not "!line:~0,1!"=="#" (
        if not "%%A"=="" (
            set "%%A=%%B"
        )
    )
)

:: ── [1/4] Python AI / Data Engine ────────────────────────────────────────────
echo [1/4] Starting Python AI Engine (Background)...
cd services\ai-agent
start "Robin AI Agent" cmd /c "python main.py"
cd ..\..

:: ── [2/4] C++ Matching Engine ─────────────────────────────────────────────────
echo [2/4] Starting C++ Matching Engine (Hot Path)...
cd services\execution-core
start "Robin Matching Engine" cmd /c "build\matching_engine.exe 9091"
cd ..\..

:: ── [3/4] C++ Live Feed (standalone, pipes to gateway via TCP) ───────────────
echo [3/4] Starting C++ Live Feed...
cd services\execution-core
if exist build\live_feed.exe (
    start "Robin Live Feed" cmd /c "build\live_feed.exe"
) else (
    echo   [SKIP] live_feed.exe not found - skipping feed process
)
cd ..\..

:: ── [4/4] Go Gateway / OMS ───────────────────────────────────────────────────
:: Launched via dedicated batch file so .env vars are properly scoped and
:: the JWT key paths are set before go run compiles and starts.
echo [4/4] Starting Go Gateway ^& OMS (port 8080)...
start "Robin Gateway OMS" cmd /k "C:\Robin\start_gateway.bat"

echo ========================================================
echo ALL AGENTS BOOTED NATIVELY.
echo   AI Agent    : separate window
echo   Matching Eng: separate window  (port 9091)
echo   Live Feed   : separate window  (if built)
echo   Go Gateway  : separate window  (port 8080)
echo ========================================================
echo Frontend: http://localhost:3000
echo ========================================================
pause
