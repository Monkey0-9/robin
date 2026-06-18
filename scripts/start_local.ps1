param(
    [switch]$BuildOnly,
    [switch]$NoGateway,
    [switch]$Test
)

$Root = "C:\Robin"
$LogDir = "$Root\logs"
$null = New-Item -ItemType Directory -Force -Path $LogDir
$TimeStamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ss"
$env:Path = "$env:USERPROFILE\scoop\apps\mingw\current\bin;$env:USERPROFILE\.cargo\bin;$env:USERPROFILE\scoop\shims;$env:Path"

function Log($msg) { Write-Host "[$TimeStamp] $msg" }

Log "===== Robin Trading Platform - Local Startup ====="
Log "Root: $Root"

# Phase 1: Build everything
Log "`n--- Build Phase ---"

Log "C++ Monte Carlo..."
g++ -std=c++20 -O3 -march=native "$Root\services\pricing\src\MonteCarlo.cpp" -o "$Root\build\mc.exe" -static 2>&1 | Out-Null
if ($?) { Log "  PASS" } else { Log "  FAIL" }

Log "C++ ONNX Inference..."
g++ -std=c++20 -O3 -march=native "$Root\services\ai-engine\src\onnx_inference.cpp" -o "$Root\build\onnx.exe" -static 2>&1 | Out-Null
if ($?) { Log "  PASS" } else { Log "  FAIL" }

Log "C++ Matching Engine..."
g++ -std=c++20 -O3 -march=native "$Root\services\execution-core\src\matching_engine.cpp" -o "$Root\build\me.exe" -static "-Wl,--stack=16777216" 2>&1 | Out-Null
if ($?) { Log "  PASS" } else { Log "  FAIL" }

Log "C++ Ingestion..."
g++ -std=c++20 -O3 -march=native "$Root\services\ingestion\cpp_ingestion.cpp" -o "$Root\build\cppi.exe" -static "-Wl,--stack=16777216" 2>&1 | Out-Null
if ($?) { Log "  PASS" } else { Log "  FAIL" }

Log "C++ Kernel Bypass..."
g++ -std=c++20 -O3 -march=native "$Root\services\network-bridge\kernel_bypass_ingest.cpp" -o "$Root\build\kbi.exe" -static "-Wl,--stack=16777216" 2>&1 | Out-Null
if ($?) { Log "  PASS" } else { Log "  FAIL" }

Log "Rust Risk Analytics..."
Push-Location "$Root\services\risk-analytics"
cargo build --release 2>&1 | Out-Null
Pop-Location
if ($?) { Log "  PASS" } else { Log "  FAIL" }

Log "Rust Compliance..."
Push-Location "$Root\services\compliance"
cargo build --release 2>&1 | Out-Null
Pop-Location
if ($?) { Log "  PASS" } else { Log "  FAIL" }

Log "Go Gateway..."
Push-Location "$Root\services\gateway"
go build -o "$Root\build\orchestrator.exe" . 2>&1 | Out-Null
Pop-Location
if ($?) { Log "  PASS" } else { Log "  FAIL" }

if ($BuildOnly) { Log "`nBuild complete. Use -NoGateway to skip."; return }

# Phase 2: Run services
Log "`n--- Run Phase ---"

# Start Go Gateway in background
if (-not $NoGateway) {
    Log "Starting Go Gateway on :8080..."
    $gwJob = Start-Job -ScriptBlock { & "C:\Robin\build\orchestrator.exe" }
    Start-Sleep 2
    try {
        $resp = Invoke-WebRequest -Uri "http://localhost:8080/health" -UseBasicParsing -TimeoutSec 2
        Log "  Gateway health: $($resp.Content)"
    } catch { Log "  Gateway not responding yet" }
}

# Run C++ executables
Log "`n--- Running C++ Services ---"
Log "Monte Carlo: $( & "$Root\build\mc.exe" 2>&1 )"
Log "ONNX: $( & "$Root\build\onnx.exe" 2>&1 )"
Log "Matching Engine: $( & "$Root\build\me.exe" 2>&1 )"
Log "Ingestion: $( & "$Root\build\cppi.exe" 2>&1 )"

# Run Python services
Log "`n--- Running Python Services ---"
Log "Backtester: $( python "$Root\services\strategy-engine\backtester.py" 2>&1 | Select-Object -First 1 )"
Log "Oracle: $( python "$Root\services\ai-engine\verification_oracle.py" 2>&1 | Select-Object -First 1 )"
Log "Alt Data: $( python "$Root\services\data-ingestion\alternative\alternative_data_loader.py" 2>&1 | Select-Object -First 1 )"

# Phase 3: Verify
Log "`n--- Verification ---"
if (-not $NoGateway) {
    try {
        $svc = Invoke-WebRequest -Uri "http://localhost:8080/services" -UseBasicParsing -TimeoutSec 2
        Log "Gateway Services: $($svc.Content)"
        $stats = Invoke-WebRequest -Uri "http://localhost:8080/stats" -UseBasicParsing -TimeoutSec 2
        Log "Gateway Stats: $($stats.Content)"
    } catch { Log "Gateway unavailable" }
}

Log "`n===== Robin Trading Platform running locally ====="
Log "Gateway: http://localhost:8080"
Log "Health:  http://localhost:8080/health"
Log "Metrics: http://localhost:8080/metrics"
Log "Stats:   http://localhost:8080/stats"
Log "`nPress Ctrl+C to stop all services."

if (-not $NoGateway) {
    if ($Test) {
        Log "Stopping Gateway job in test mode..."
        Stop-Job $gwJob
        Remove-Job $gwJob
    } else {
        Wait-Job $gwJob | Out-Null
    }
}
