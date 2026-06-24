param(
    [switch]$Build,
    [switch]$NoBuild,
    [switch]$Kill
)

$Root = "C:\Robin"
$LogDir = "$Root\logs"
$null = New-Item -ItemType Directory -Force -Path $LogDir
$TimeStamp = { Get-Date -Format "yyyy-MM-ddTHH:mm:ss" }

function Log { Write-Host "[$(& $TimeStamp)] $args" }
function Fail { Write-Host "[FAIL] $args" -ForegroundColor Red; exit 1 }

# Kill existing processes
if ($Kill) {
    Log "Stopping all Robin processes..."
    Get-Process -Name "matching_engine" -ErrorAction SilentlyContinue | Stop-Process -Force
    Get-Process -Name "orchestrator" -ErrorAction SilentlyContinue | Stop-Process -Force
    Get-Process -Name "compliance-daemon" -ErrorAction SilentlyContinue | Stop-Process -Force
    Get-Process -Name "mc" -ErrorAction SilentlyContinue | Stop-Process -Force
    Log "All processes stopped"
    return
}

Log "===== Robin Trading Platform ====="
Log "Starting all services natively (no Docker)..."

# Build phase
if (!$NoBuild -or $Build) {
    $Build = $true
}

if ($Build) {
    Log "--- Build Phase ---"

    # C++ Execution Core
    Log "Building C++ matching engine..."
    Push-Location "$Root\services\execution-core"
    cmake -S . -B build -DCMAKE_BUILD_TYPE=Release 2>&1 | Out-Null
    if ($?) {
        cmake --build build --config Release 2>&1 | Out-Null
        if ($?) { Log "  Matching engine: PASS" } else { Log "  Matching engine: FAIL (cmake build)" }
    } else {
        Log "  Matching engine: SKIP (cmake configure failed)"
    }
    Pop-Location

    # Rust Risk Analytics
    if (Get-Command "cargo" -ErrorAction SilentlyContinue) {
        Log "Building Rust risk analytics..."
        Push-Location "$Root\services\risk-analytics"
        cargo build --release 2>&1 | Out-Null
        if ($?) { Log "  Risk analytics: PASS" } else { Log "  Risk analytics: FAIL" }
        Pop-Location
    } else {
        Log "  Risk analytics: SKIP (cargo not found)"
    }

    # Rust Compliance
    if (Get-Command "cargo" -ErrorAction SilentlyContinue) {
        Log "Building Rust compliance daemon..."
        Push-Location "$Root\services\compliance"
        cargo build --release 2>&1 | Out-Null
        if ($?) { Log "  Compliance daemon: PASS" } else { Log "  Compliance daemon: FAIL" }
        Pop-Location
    } else {
        Log "  Compliance daemon: SKIP (cargo not found)"
    }

    # Go Gateway
    if (Get-Command "go" -ErrorAction SilentlyContinue) {
        Log "Building Go gateway..."
        Push-Location "$Root\services\gateway"
        go build -o "$Root\build\orchestrator.exe" . 2>&1 | Out-Null
        if ($?) { Log "  Go gateway: PASS" } else { Log "  Go gateway: FAIL" }
        Pop-Location
    } else {
        Log "  Go gateway: SKIP (go not found)"
    }

    # C++ Monte Carlo pricing
    Log "Building C++ Monte Carlo pricing..."
    Push-Location "$Root\services\pricing"
    cmake -S . -B build -DCMAKE_BUILD_TYPE=Release 2>&1 | Out-Null
    if ($?) {
        cmake --build build --config Release 2>&1 | Out-Null
        if ($?) { Log "  Monte Carlo: PASS" } else { Log "  Monte Carlo: FAIL" }
    } else { Log "  Monte Carlo: SKIP" }
    Pop-Location

    # Python backtester validation
    if (Get-Command "python" -ErrorAction SilentlyContinue) {
        Log "Validating Python backtester..."
        python "$Root\services\strategy-engine\backtester.py" 2>&1 | Out-Null
        if ($?) { Log "  Backtester: PASS" } else { Log "  Backtester: FAIL" }
    }

    Log "Build phase complete."
    if ($BuildOnly) { return }
}

# Run phase
Log "--- Run Phase ---"
$PIDs = @()

# Start Matching Engine (port 9091)
$enginePath = "$Root\services\execution-core\build\Release\matching_engine.exe"
if (!(Test-Path $enginePath)) {
    $enginePath = "$Root\services\execution-core\build\matching_engine.exe"
}
if (Test-Path $enginePath) {
    Log "Starting C++ matching engine on :9091..."
    $proc = Start-Process -FilePath $enginePath -NoNewWindow -PassThru -RedirectStandardOutput "$LogDir\engine.log" -RedirectStandardError "$LogDir\engine_err.log"
    $PIDs += $proc.Id
    Log "  PID: $($proc.Id)"
} else {
    Log "  WARNING: matching_engine.exe not found at $enginePath"
    Log "  Run with -Build flag first, or build manually"
}

Start-Sleep 2

# Start Go Gateway (port 8080)
$gatewayPath = "$Root\build\orchestrator.exe"
if (Test-Path $gatewayPath) {
    Log "Starting Go gateway on :8080..."
    $env:ORCH_PORT = "8080"
    $proc = Start-Process -FilePath $gatewayPath -NoNewWindow -PassThru -RedirectStandardOutput "$LogDir\gateway.log" -RedirectStandardError "$LogDir\gateway_err.log"
    $PIDs += $proc.Id
    Log "  PID: $($proc.Id)"
} else {
    Log "  WARNING: orchestrator.exe not found at $gatewayPath"
}

Start-Sleep 2

# Start Compliance Daemon (port 9095)
$compliancePath = "$Root\services\compliance\target\release\compliance-daemon.exe"
if (!(Test-Path $compliancePath)) {
    $compliancePath = "$Root\services\compliance\target\release\compliance-daemon.exe"
}
if (Test-Path $compliancePath) {
    Log "Starting compliance daemon on :9095..."
    $proc = Start-Process -FilePath $compliancePath -ArgumentList "--port 9095 --audit-log $LogDir\audit.log" -NoNewWindow -PassThru -RedirectStandardOutput "$LogDir\compliance.log" -RedirectStandardError "$LogDir\compliance_err.log"
    $PIDs += $proc.Id
    Log "  PID: $($proc.Id)"
} else {
    Log "  Compliance daemon not built (optional for demo)"
}

# Verify services are running
Start-Sleep 2
Log "--- Health Checks ---"

$healthy = 0
$total = 0

# Gateway check
$total++
try {
    $resp = Invoke-WebRequest -Uri "http://localhost:8080/health" -UseBasicParsing -TimeoutSec 2
    if ($resp.Content -match "ok") {
        Log "  Gateway (8080): HEALTHY"
        $healthy++
    } else { Log "  Gateway (8080): UNHEALTHY - $($resp.Content)" }
} catch { Log "  Gateway (8080): NOT RESPONDING" }

# Matching engine check
$total++
try {
    $client = New-Object System.Net.Sockets.TcpClient('127.0.0.1', 9091)
    $stream = $client.GetStream()
    $writer = New-Object System.IO.StreamWriter($stream)
    $writer.WriteLine("health")
    $writer.Flush()
    $reader = New-Object System.IO.StreamReader($stream)
    $resp = $reader.ReadLine()
    $client.Close()
    if ($resp -match "ok") {
        Log "  Engine (9091): HEALTHY"
        $healthy++
    } else { Log "  Engine (9091): ERROR - $resp" }
} catch { Log "  Engine (9091): NOT RESPONDING" }

Log "---"
Log "Services healthy: $healthy / $total"

if ($healthy -eq 0) {
    Log "WARNING: No services responded. Something went wrong."
    Log "Check logs in: $LogDir"
} elseif ($healthy -eq $total) {
    Log "All services running!"
}

Log ""
Log "Frontend: cd frontend && npm run dev  (port 3000)"
Log "Gateway:  http://localhost:8080/health"
Log "Engine:   nc 127.0.0.1 9091 (send JSON orders)"
Log "Logs:     $LogDir"
Log ""
Log "Press Ctrl+C to stop all services."
Log "Or run: .\scripts\run_all.ps1 -Kill"

# Wait for processes (user presses Ctrl+C)
if ($PIDs.Count -gt 0) {
    try {
        foreach ($pid in $PIDs) {
            $proc = Get-Process -Id $pid -ErrorAction SilentlyContinue
            if ($proc) { $proc.WaitForExit() | Out-Null }
        }
    } catch {
        Log "Shutting down..."
    }
}
