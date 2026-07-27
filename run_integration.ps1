$ErrorActionPreference = "Continue"

Write-Host "Starting Robin Platform Integration Test..."

# Start matching engine
$MatchingEngine = Start-Process -FilePath "C:\Robin\services\execution-core\build\matching_engine.exe" -ArgumentList "9091" -PassThru -NoNewWindow -RedirectStandardOutput "exec_out.log" -RedirectStandardError "exec_err.log"
Write-Host "Matching Engine PID: $($MatchingEngine.Id)"

# Start risk daemon
Set-Location C:\Robin\services\risk-analytics
$RiskDaemon = Start-Process -FilePath "cargo" -ArgumentList "run --bin robin-risk-daemon" -PassThru -NoNewWindow -RedirectStandardOutput "..\..\risk_out.log" -RedirectStandardError "..\..\risk_err.log"
Write-Host "Risk Daemon PID: $($RiskDaemon.Id)"

# Start gateway and live feed
Set-Location C:\Robin\services\gateway
$env:ROBIN_JWT_PUBKEY_FILE="C:\Robin\config\keys\public.pem"
$env:ROBIN_MASTER_KEY="12345678901234567890123456789012"
$env:ROBIN_DB_PASSPHRASE="12345678901234567890123456789012"
$env:ORCH_MTLS_ENABLED="1"
$env:ORCH_TLS_CERT="C:\Robin\config\certs\server.crt"
$env:ORCH_TLS_KEY="C:\Robin\config\certs\server.key"
$env:ORCH_CA_CERT="C:\Robin\config\certs\ca.crt"

$Gateway = Start-Process -FilePath "cmd.exe" -ArgumentList "/c `"..\execution-core\build\live_feed.exe | robin-gateway.exe`"" -PassThru -NoNewWindow -RedirectStandardOutput "..\..\orch_out.log" -RedirectStandardError "..\..\orch_err.log"
Write-Host "Gateway/Feed Pipeline PID: $($Gateway.Id)"

Set-Location C:\Robin

Write-Host "Waiting 10 seconds for services to initialize..."
Start-Sleep -Seconds 10

Write-Host "Running single test order..."
python test_order.py
Write-Host "Running short load test..."
python tests\load_test.py --target https://localhost:8080 --scenario baseline --duration 3

Write-Host "Cleaning up processes..."
Stop-Process -Id $MatchingEngine.Id -Force -ErrorAction SilentlyContinue
Stop-Process -Id $RiskDaemon.Id -Force -ErrorAction SilentlyContinue
Stop-Process -Id $Gateway.Id -Force -ErrorAction SilentlyContinue
# Taskkill any lingering children
taskkill /F /IM robin-risk-daemon.exe /T 2>$null
taskkill /F /IM robin-gateway.exe /T 2>$null
taskkill /F /IM live_feed.exe /T 2>$null
taskkill /F /IM matching_engine.exe /T 2>$null

Write-Host "Integration Test Complete."
