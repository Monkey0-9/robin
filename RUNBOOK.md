# Robin Trading Platform — Developer Runbook

## Quick Start (3 commands)

```powershell
# 1. Clone / navigate to project root
cd C:\Robin

# 2. Start all services
start_all.bat

# 3. Open browser
Start-Process "http://localhost:3000"
```

**Default login**: set `SEED_ADMIN_PASSWORD` / `SEED_TRADER_PASSWORD` before first start — no hardcoded credentials.

---

## Service Map

| Service | Port | Start Command | Health Check |
|---|---|---|---|
| Go Gateway | 8080 | `start_gateway.bat` | `GET http://localhost:8080/health` |
| Python AI Agent | 8000 | `start_ai.bat` or `cd services/ai-agent && python main.py` | `GET http://localhost:8000/health` |
| Next.js Frontend | 3000 | `cd frontend && npm run dev` | `http://localhost:3000` |

---

## First-Time Setup

### Prerequisites
- Go 1.21+ (`go version`)
- Python 3.10+ (`python --version`)
- Node.js 18+ (`node --version`)
- npm 9+ (`npm --version`)

### Install Dependencies

```powershell
# Go gateway
cd C:\Robin\services\gateway
go mod download

# Python AI agent
cd C:\Robin\services\ai-agent
pip install -r requirements.txt

# Next.js frontend
cd C:\Robin\frontend
npm install
```

### Start Services

```powershell
# Option A: One script (all three)
cd C:\Robin
.\start_all.bat

# Option B: Individual windows
.\start_gateway.bat      # Window 1 — Go gateway on :8080
.\start_ai.bat           # Window 2 — Python AI on :8000
cd frontend && npm run dev  # Window 3 — Next.js on :3000
```

---

## Authentication

### Login Flow (how it works)
1. Frontend hits `POST http://localhost:8080/api/auth/login` with `{username, password}`
2. Gateway validates against SQLite `users` table using bcrypt
3. Returns a short-lived RS256 JWT (8h expiry in dev mode)
4. Token stored **in-memory only** — cleared on page refresh/logout
5. All subsequent API calls attach `Authorization: Bearer <token>` header

### User Seeding (env-var enforced, no hardcoded credentials)

> ⚠️ There are **no default `admin / admin` credentials**. Users are seeded on first run
> **only** when the `SEED_ADMIN_PASSWORD` / `SEED_TRADER_PASSWORD` env vars are set.
> If neither var is present, no users are created and login is disabled until a user exists.

| Env Var | User created | Role |
|---|---|---|
| `SEED_ADMIN_PASSWORD` | `admin` | admin |
| `SEED_TRADER_PASSWORD` | `trader` | trader |

Example (PowerShell):
```powershell
$env:SEED_ADMIN_PASSWORD="CHANGE_ME_STRONG"
$env:SEED_TRADER_PASSWORD="CHANGE_ME_TOO"
.\start_gateway.bat
```

### Create a New User (SQLite)
```powershell
cd C:\Robin\services\gateway
# Using sqlite3 CLI (or any SQLite client)
sqlite3 robin.db "INSERT INTO users (username, password_hash, role, created_at_ns)
  VALUES ('newuser', '<bcrypt_hash>', 'trader', strftime('%s','now') * 1000000000)"
```

---

## Key Endpoints

### Public (no auth)
- `GET /health` — gateway health (services up/degraded/failed counts)
- `GET /live` — liveness probe (always 200)
- `GET /ready` — readiness probe (503 if any service failed)
- `GET /api/assets` — canonical tradable symbol list
- `GET /api/candles?symbol=BTC/USD&resolution=1m` — OHLCV candle data
- `GET /api/screener` — asset screener data
- `GET /api/heatmap` — sector heatmap data
- `POST /api/auth/login` — authenticate and get JWT

### Trader Role (JWT required)
- `POST /order` — submit a trading order
- `GET /api/positions` — position summary
- `GET /api/portfolio` — portfolio summary
- `GET /api/alpaca/account` — Alpaca paper account
- `GET /api/alpaca/positions` — Alpaca positions
- `POST /api/ai/chat` — AI assistant chat
- `POST /api/ai/trade_decision` — AI trade evaluation

### Admin Role (JWT required, role=admin)
- `GET /stats` — order/trade/latency statistics
- `GET /config` — current risk configuration
- `POST /config` — update risk parameters
- `GET /api/killswitch/status` — kill switch status
- `POST /api/killswitch/system/trip` — emergency halt
- `GET /api/compliance/*` — compliance endpoints
- `GET /api/surveillance/*` — surveillance alerts
- `GET /metrics` — Prometheus metrics

---

## WebSocket

Connect to `ws://localhost:8080/ws` to receive real-time events.

**Message types:**
```json
// Order book update
{ "type": "orderbook", "data": { "symbol": "BTC/USD", "bids": [[price, size]], "asks": [[price, size]] } }

// Trade fill
{ "type": "trade", "data": { "id": "EXEC-...", "symbol": "BTC/USD", "side": "BUY", "qty": 0.01, "price": 64500.0, "timestamp": 1706000000000 } }
```

---

## Run Tests

```powershell
# Go unit tests
cd C:\Robin\services\gateway
go test ./... -v -timeout 30s

# E2E integration tests (requires all services running)
cd C:\Robin
powershell -ExecutionPolicy Bypass -File scripts\e2e_test.ps1
```

---

## Troubleshooting

### Gateway won't start
```
ERROR: JWT init failed: no JWT key configured
```
**Fix**: The `start_gateway.bat` must be used (it sets `ROBIN_JWT_PUBKEY_FILE` and `ROBIN_JWT_PRIVKEY_FILE`).
If keys don't exist: `cd C:\Robin\keys && .\generate_keys.ps1` (or the gateway auto-generates in dev mode).

### Frontend shows "Verifying session..." indefinitely
The frontend redirects to `/login` if no token is in memory. After a page refresh, you must log in again (tokens are intentionally not persisted to disk).

### Order returns `MATCHING_ENGINE_UNAVAILABLE` (503)
The C++ matching engine is not running. This is expected in dev mode. Start it if available:
```powershell
cd C:\Robin\services\execution-core
.\matching_engine.exe
```

### AI chat returns "failed to reach python ai-agent"
The Python AI agent (port 8000) is not running. Start it:
```powershell
cd C:\Robin\services\ai-agent
python main.py
```

### CORS errors in browser console
Ensure the gateway is started with `ROBIN_CORS_ORIGIN` unset (dev mode allows `localhost:3000`):
```powershell
# Check if CORS_ORIGIN is set (should be empty for dev)
echo %ROBIN_CORS_ORIGIN%
```

---

## Architecture Overview

```
Browser (localhost:3000)
    │  HTTP + WebSocket
    ▼
Go Gateway (localhost:8080)      ← JWT auth, rate limiting, routing
    ├── SQLite (robin.db)        ← Orders, trades, users, audit log
    ├── Python AI Agent (:8000)  ← Chat, trade signals, macro feed
    └── C++ Matching Engine      ← Optional; TCP on :9090
         (MATCHING_ENGINE_UNAVAILABLE if not running)
```

---

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `ORCH_PORT` | `8080` | Gateway HTTP port |
| `ROBIN_JWT_PUBKEY_FILE` | (none) | Path to RSA public key PEM |
| `ROBIN_JWT_PRIVKEY_FILE` | (none) | Path to RSA private key PEM |
| `ROBIN_CORS_ORIGIN` | (empty) | CORS allowed origin. Empty = allow localhost:3000 |
| `ROBIN_BYPASS_AUTH` | `0` | **Never set to 1** — auth bypass removed |
| `ALPACA_API_ENDPOINT` | `https://paper-api.alpaca.markets/v2` | Alpaca API base |
| `NEXT_PUBLIC_GATEWAY_URL` | `http://localhost:8080` | Frontend gateway URL |

---

*Last updated: 2026-07-29 | Robin Platform v1.5*
