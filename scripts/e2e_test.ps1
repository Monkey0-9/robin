# Robin E2E Integration Test — PowerShell
# Tests the full login → order → WebSocket flow against running local services.
# Run: powershell -ExecutionPolicy Bypass -File scripts\e2e_test.ps1

$ErrorActionPreference = "Stop"
$GATEWAY = "http://localhost:8080"
$AI_AGENT = "http://localhost:8000"
$FRONTEND = "http://localhost:3000"
$PASS = 0
$FAIL = 0

function Test-Assert($name, $cond) {
    if ($cond) {
        Write-Host "  [PASS] $name" -ForegroundColor Green
        $global:PASS++
    } else {
        Write-Host "  [FAIL] $name" -ForegroundColor Red
        $global:FAIL++
    }
}

Write-Host ""
Write-Host "Robin E2E Integration Tests" -ForegroundColor Cyan
Write-Host "=============================" -ForegroundColor Cyan

# ── P1: Gateway Health ─────────────────────────────────────────────────────────
Write-Host "`n[1/7] Gateway Health" -ForegroundColor Yellow
try {
    $res = Invoke-RestMethod -Uri "$GATEWAY/health" -Method GET
    Test-Assert "Gateway /health returns 200" ($res.status -eq "ok")
} catch {
    Test-Assert "Gateway /health reachable" $false
}

try {
    $res = Invoke-RestMethod -Uri "$GATEWAY/live" -Method GET -ErrorAction Stop
    Test-Assert "Gateway /live returns ok" $true
} catch {
    Test-Assert "Gateway /live reachable" $false
}

# ── P2: Assets Endpoint ────────────────────────────────────────────────────────
Write-Host "`n[2/7] Assets Endpoint (no auth)" -ForegroundColor Yellow
try {
    $assets = Invoke-RestMethod -Uri "$GATEWAY/api/assets" -Method GET
    Test-Assert "GET /api/assets returns array" ($assets -is [array])
    Test-Assert "Assets contains BTC/USD" ($assets | Where-Object { $_.symbol -eq "BTC/USD" } | Measure-Object).Count -gt 0
} catch {
    Test-Assert "GET /api/assets reachable" $false
    $assets = @()
}

# ── P3: Login Flow ─────────────────────────────────────────────────────────────
Write-Host "`n[3/7] Login Flow (admin/admin)" -ForegroundColor Yellow
$TOKEN = $null
try {
    $body = '{"username":"admin","password":"admin"}'
    $loginRes = Invoke-RestMethod -Uri "$GATEWAY/api/auth/login" -Method POST `
        -ContentType "application/json" -Body $body
    Test-Assert "POST /api/auth/login returns token" ($loginRes.token -ne $null -and $loginRes.token.Length -gt 20)
    Test-Assert "Login returns role=admin" ($loginRes.role -eq "admin")
    $TOKEN = $loginRes.token
} catch {
    Test-Assert "Login endpoint reachable" $false
    Write-Host "  ERROR: $_" -ForegroundColor Red
}

# ── P4: Trader Login ───────────────────────────────────────────────────────────
Write-Host "`n[4/7] Trader Login (trader/trader)" -ForegroundColor Yellow
$TRADER_TOKEN = $null
try {
    $body = '{"username":"trader","password":"trader"}'
    $traderRes = Invoke-RestMethod -Uri "$GATEWAY/api/auth/login" -Method POST `
        -ContentType "application/json" -Body $body
    Test-Assert "Trader login returns token" ($traderRes.token -ne $null)
    Test-Assert "Trader login returns role=trader" ($traderRes.role -eq "trader")
    $TRADER_TOKEN = $traderRes.token
} catch {
    Test-Assert "Trader login endpoint reachable" $false
}

# ── P5: Candles Endpoint ───────────────────────────────────────────────────────
Write-Host "`n[5/7] Candles Endpoint (no auth)" -ForegroundColor Yellow
try {
    $candles = Invoke-RestMethod -Uri "$GATEWAY/api/candles?symbol=BTC/USD&resolution=1m" -Method GET
    Test-Assert "GET /api/candles returns array" ($candles -is [array])
    Test-Assert "Candles have 100 bars" ($candles.Count -eq 100)
    Test-Assert "Candles have time field" ($candles[0].time -ne $null)
} catch {
    Test-Assert "GET /api/candles reachable" $false
}

# ── P6: Authenticated Endpoints ───────────────────────────────────────────────
Write-Host "`n[6/7] Authenticated Endpoints" -ForegroundColor Yellow
if ($TOKEN) {
    $headers = @{ Authorization = "Bearer $TOKEN" }
    try {
        $stats = Invoke-RestMethod -Uri "$GATEWAY/stats" -Headers $headers -Method GET
        Test-Assert "GET /stats with admin token returns 200" ($stats.orders -ne $null)
    } catch {
        Test-Assert "GET /stats with admin token" $false
    }
    
    try {
        $config = Invoke-RestMethod -Uri "$GATEWAY/config" -Headers $headers -Method GET
        Test-Assert "GET /config with admin token returns 200" ($config -ne $null)
    } catch {
        Test-Assert "GET /config with admin token" $false
    }
} else {
    Write-Host "  [SKIP] Skipping auth tests - login failed" -ForegroundColor Gray
}

# ── P7: AI Agent ───────────────────────────────────────────────────────────────
Write-Host "`n[7/7] AI Agent Health" -ForegroundColor Yellow
try {
    $aiHealth = Invoke-RestMethod -Uri "$AI_AGENT/health" -Method GET -TimeoutSec 3
    Test-Assert "AI Agent /health reachable" $true
} catch {
    Write-Host "  [SKIP] AI Agent not running (non-critical)" -ForegroundColor Gray
}

# ── Summary ────────────────────────────────────────────────────────────────────
$TOTAL = $PASS + $FAIL
Write-Host "`n===============================" -ForegroundColor Cyan
Write-Host "Results: $PASS/$TOTAL passed" -ForegroundColor $(if ($FAIL -eq 0) { "Green" } else { "Yellow" })
if ($FAIL -gt 0) {
    Write-Host "  $FAIL test(s) FAILED" -ForegroundColor Red
    exit 1
} else {
    Write-Host "  All tests PASSED!" -ForegroundColor Green
    exit 0
}
