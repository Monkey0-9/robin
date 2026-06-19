param(
    [switch]$BuildOnly,
    [switch]$Test
)

$Root = "C:\Robin"
$LogDir = "$Root\logs"
$null = New-Item -ItemType Directory -Force -Path $LogDir
$TimeStamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ss"

function Log($msg) { Write-Host "[$TimeStamp] $msg" }

Log "===== Robin Trading Platform - Local Startup ====="

# Build available components
Log "--- Build Phase ---"

Log "C++ Monte Carlo..."
g++ -std=c++20 -O3 -march=native "$Root\services\pricing\src\MonteCarlo.cpp" -o "$Root\build\mc.exe" -static 2>&1 | Out-Null
if ($?) { Log "  PASS" } else { Log "  SKIP (not critical)" }

Log "C++ Matching Engine..."
Push-Location "$Root\services\execution-core"
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release 2>&1 | Out-Null
cmake --build build --config Release 2>&1 | Out-Null
Pop-Location
if ($?) { Log "  PASS" } else { Log "  FAIL" }

Log "Rust Risk Analytics..."
Push-Location "$Root\services\risk-analytics"
cargo build --release 2>&1 | Out-Null
Pop-Location
if ($?) { Log "  PASS" } else { Log "  FAIL" }

Log "Go Gateway..."
Push-Location "$Root\services\gateway"
go build -o "$Root\build\orchestrator.exe" . 2>&1 | Out-Null
Pop-Location
if ($?) { Log "  PASS" } else { Log "  FAIL" }

if ($BuildOnly) { Log "Build complete."; return }

# Run services
Log "--- Run Phase ---"

if (Test-Path "$Root\build\orchestrator.exe") {
    Log "Starting Go Gateway on :8080..."
    $gwJob = Start-Job -ScriptBlock { & "C:\Robin\build\orchestrator.exe" }
    Start-Sleep 2
    try {
        $resp = Invoke-WebRequest -Uri "http://localhost:8080/health" -UseBasicParsing -TimeoutSec 2
        Log "  Gateway health: $($resp.Content)"
    } catch { Log "  Gateway not responding yet" }
}

if (Test-Path "$Root\build\mc.exe") {
    Log "Monte Carlo: $( & "$Root\build\mc.exe" 2>&1 )"
}

if ($Test) {
    if ($gwJob) { Stop-Job $gwJob; Remove-Job $gwJob }
} else {
    if ($gwJob) { Wait-Job $gwJob | Out-Null }
}

Log "===== Done ====="
