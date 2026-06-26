$ErrorActionPreference = "Continue"

Write-Host "[TEST] Stopping existing processes..."
taskkill /F /IM orchestrator.exe 2>$null
taskkill /F /IM compliance-daemon.exe 2>$null
taskkill /F /IM robin-risk-daemon.exe 2>$null
taskkill /F /IM matching_engine.exe 2>$null

Write-Host "[TEST] Starting execution-core on :9091..."
Start-Process -FilePath ".\services\execution-core\build\matching_engine.exe" -ArgumentList "9091" -RedirectStandardOutput "exec_out.log" -RedirectStandardError "exec_err.log" -NoNewWindow

Write-Host "[TEST] Starting compliance-daemon on :19095..."
Start-Process -FilePath ".\target\release\compliance-daemon.exe" -ArgumentList "--port 19095" -RedirectStandardOutput "compliance_out.log" -RedirectStandardError "compliance_err.log" -NoNewWindow

Write-Host "[TEST] Starting risk-analytics on :9092..."
Start-Process -FilePath ".\target\release\robin-risk-daemon.exe" -RedirectStandardOutput "risk_out.log" -RedirectStandardError "risk_err.log" -NoNewWindow

Write-Host "[TEST] Starting orchestrator on :18080..."
$env:ROBIN_GATEWAY_API_TOKEN="smoke-test-secret"
$env:ORCH_PORT="18080"
Start-Process -FilePath ".\build\orchestrator.exe" -RedirectStandardOutput "orch_out.log" -RedirectStandardError "orch_err.log" -NoNewWindow

Write-Host "[TEST] Waiting for services to start..."
Start-Sleep -Seconds 5

Write-Host "[TEST] Sending test order using Python..."
python test_order.py

Write-Host "[TEST] Waiting for logs to flush..."
Start-Sleep -Seconds 2

Write-Host "[TEST] Stopping services..."
taskkill /F /IM orchestrator.exe 2>$null
taskkill /F /IM compliance-daemon.exe 2>$null
taskkill /F /IM robin-risk-daemon.exe 2>$null
taskkill /F /IM matching_engine.exe 2>$null

Write-Host "[TEST] Done."
