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

:: ── [3.5/4] Rust Risk Analytics Engine ───────────────────────────────────────
echo [3.5/4] Starting Rust Risk Analytics Engine (port 9092)...
cd services\risk-analytics
start "Robin Risk Engine" cmd /c "..\..\target\release\robin-risk-daemon.exe"
cd ..\..

:: ── [4/4] Go Gateway / OMS ───────────────────────────────────────────────────
:: Launched via dedicated batch file so .env vars are properly scoped and
:: the JWT key paths are set before go run compiles and starts.
echo [4/5] Starting Go Gateway ^& OMS (port 8080)...
start "Robin Gateway OMS" cmd /k "C:\Robin\start_gateway.bat"

:: ── [5/5] Robin Swarm Engine ──────────────────────────────────────────────
echo [5/5] Starting Robin Swarm Engine (port 3001 / 5001)...
cd services\robin-swarm
start "Robin Swarm Engine" cmd /k "set PORT=3001 && npm run dev"
cd ..\..

:: ── [6/6] Robin Main Frontend ───────────────────────────────────────────────
echo [6/6] Starting Robin Main Frontend (port 3000)...
cd frontend
start "Robin Frontend" cmd /k "npm run dev"
cd ..

echo ========================================================
echo ALL AGENTS BOOTED NATIVELY.
echo   AI Agent    : separate window
echo   Matching Eng: separate window  (port 9091)
echo   Live Feed   : separate window  (if built)
echo   Go Gateway  : separate window  (port 8080)
echo   Robin Swarm : separate window  (port 3001/5001)
echo   Robin UI    : separate window  (port 3000)
echo ========================================================
echo Robin Frontend: http://localhost:3000
echo Robin Swarm UI: http://localhost:3001
echo ========================================================
pause
