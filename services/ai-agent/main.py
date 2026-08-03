"""
Robin Trading Platform — AI Agent Microservice
===============================================
FIXED:
  - CORS restricted to localhost (was ["*"])
  - Random price replaced with real yfinance + Binance WebSocket data
  - VIX fetched live from Yahoo Finance for risk guardian
  - Macro news fetched from Yahoo Finance RSS
  - Autonomous loop feeds real prices into pipeline
  - All hardcoded 65000.0 price constants eliminated
"""

import asyncio
import logging
import os

import aiohttp
from contextlib import asynccontextmanager
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi import WebSocket, WebSocketDisconnect
from typing import List
from pydantic import BaseModel

# SHM Bridge for low-latency OMS routing
from shm_bridge import ShmBridge
SHM_OMS = ShmBridge("robin_ai_oms.shm")

class ConnectionManager:
    def __init__(self):
        self.active_connections: List[WebSocket] = []

    async def connect(self, websocket: WebSocket):
        await websocket.accept()
        self.active_connections.append(websocket)

    def disconnect(self, websocket: WebSocket):
        if websocket in self.active_connections:
            self.active_connections.remove(websocket)

    async def broadcast(self, message: dict):
        for connection in self.active_connections:
            try:
                await connection.send_json(message)
            except Exception:
                pass

manager = ConnectionManager()

# Hardware-Constrained Sequential Orchestrator
from orchestrator import HardwareConstrainedOrchestrator

# Real market data service (replaces random.uniform noise)
from market_data_service import get_market_data

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")
logger = logging.getLogger("robin.main")

# ─── Global state ─────────────────────────────────────────────────────────────

AUTONOMOUS_ENABLED = False
ORCHESTRATOR       = HardwareConstrainedOrchestrator()
AUTONOMOUS_TRADES  = []
MARKET_DATA        = get_market_data()


# ─── Autonomous trading loop ──────────────────────────────────────────────────

async def autonomous_trading_loop():
    """
    Main autonomous trading loop.
    Runs every 30 seconds on 4GB VRAM hardware.
    Uses REAL market prices from Binance WebSocket / yfinance.
    """
    global AUTONOMOUS_TRADES

    # Primary trading symbol (crypto — available 24/7, real-time, free)
    PRIMARY_SYMBOL = "BTC-USD"

    while True:
        if AUTONOMOUS_ENABLED:
            # ── 1. Get REAL current price ──────────────────────────────────
            current_price = MARKET_DATA.get_price(PRIMARY_SYMBOL)
            if current_price is None:
                logger.warning(
                    "[Loop] No live price for %s — skipping cycle", PRIMARY_SYMBOL
                )
                await asyncio.sleep(15)
                continue

            # ── 2. Get REAL VIX for risk guardian ─────────────────────────
            live_vix = MARKET_DATA.get_vix()

            # ── 3. Get REAL news for sentiment ────────────────────────────
            news_items = MARKET_DATA.get_macro_news()
            headlines  = [item["text"] for item in news_items[:5]]
            market_summary = (
                f"Live market update: {PRIMARY_SYMBOL} @ ${current_price:,.2f}. "
                f"VIX={live_vix:.1f}. "
                + (headlines[0] if headlines else "No recent headlines.")
            )

            # ── 4. Run Defense-in-Depth Guardian (now with real VIX) ──────
            #    daily_pnl_pct: derive from portfolio P&L if available
            try:
                port = os.environ.get("ORCH_PORT", "8080")
                async with aiohttp.ClientSession() as session:
                    async with session.get(f"http://127.0.0.1:{port}/api/portfolio/summary", timeout=aiohttp.ClientTimeout(total=2)) as resp:
                        pnl = (await resp.json()).get("total_pnl_pct", 0.0)
            except Exception as e:
                logger.warning(f"[Loop] Failed to fetch P&L: {e}")
                pnl = 0.0

            is_safe = ORCHESTRATOR.risk_guardian_check(
                simulated_vix=live_vix,
                daily_pnl_pct=pnl,
            )

            if is_safe:
                # ── 5. Execute Sequential AI Pipeline ─────────────────────
                signal = await ORCHESTRATOR.execute_sequential_pipeline(
                    market_summary, headlines, current_price
                )

                if signal["action"] != "HOLD":
                    snap = MARKET_DATA.get_snapshot(PRIMARY_SYMBOL)
                    trade_payload = {
                        "action":     signal["action"],
                        "symbol":     PRIMARY_SYMBOL,
                        "reason":     signal["reason"],
                        "entry":      signal["entry_target"],
                        "live_price": current_price,
                        "vix":        live_vix,
                        "timestamp":  asyncio.get_event_loop().time(),
                        "source":     snap.source if snap else "unknown",
                    }
                    AUTONOMOUS_TRADES.append(trade_payload)
                    
                    # Broadcast to frontend AI panel
                    await manager.broadcast(trade_payload)
                    
                    # Send via SHM to Rust OMS for zero-copy routing
                    side_code = 1 if signal["action"] == "BUY" else 2
                    qty = int((1000.0 / current_price) * 1e8)
                    price = int(current_price * 1e8)
                    SHM_OMS.send_order(
                        instrument_id=1,  # Assuming BTC-USD is 1
                        price=price,
                        qty=qty,
                        side=side_code
                    )

                # Keep history bounded to last 100 trades
                if len(AUTONOMOUS_TRADES) > 100:
                    AUTONOMOUS_TRADES.pop(0)
            else:
                logger.warning(
                    "[Loop] Risk guardian blocked cycle — VIX=%.1f", live_vix
                )

        # 30-second cycle (hardware-constrained for 4GB VRAM sequential pipeline)
        await asyncio.sleep(30)


# ─── FastAPI lifecycle ────────────────────────────────────────────────────────

@asynccontextmanager
async def lifespan(app: FastAPI):
    logger.info("Starting Robin AI Agent — initialising live market data ...")

    # Start live market data feeds (Binance WS + yfinance polls)
    MARKET_DATA.start_in_thread()

    loop_task = asyncio.create_task(autonomous_trading_loop())
    logger.info("Robin AI Agent online. Real-time feeds active.")
    yield

    loop_task.cancel()
    MARKET_DATA.stop()
    logger.info("Robin AI Agent shutdown complete.")


# ─── App ─────────────────────────────────────────────────────────────────────

app = FastAPI(
    title="Robin AI Agent",
    description="Multi-agent quantitative trading AI with real live market data",
    version="2.0.0",
    lifespan=lifespan,
)

# ── CORS: localhost only (was ["*"] — security fix) ──────────────────────────
_ALLOWED_ORIGINS = [
    "http://localhost:3000",    # Next.js dev
    "http://localhost:3001",
    "http://localhost:8080",    # Go gateway
    "http://127.0.0.1:3000",
    "http://127.0.0.1:8080",
]

app.add_middleware(
    CORSMiddleware,
    allow_origins=_ALLOWED_ORIGINS,
    allow_credentials=True,
    allow_methods=["GET", "POST", "PUT", "DELETE"],
    allow_headers=["Authorization", "Content-Type", "X-Request-ID"],
)


# ─── Request models ───────────────────────────────────────────────────────────

class ChatRequest(BaseModel):
    message: str

class TradeDecisionRequest(BaseModel):
    market_context: str
    symbol: str = "BTC-USD"


# ─── Health endpoints ─────────────────────────────────────────────────────────

@app.get("/live", tags=["health"])
async def live():
    return {"status": "ok"}

@app.get("/ready", tags=["health"])
async def ready():
    prices = MARKET_DATA.get_all_snapshots()
    return {
        "status": "ready",
        "live_prices_count": len(prices),
        "btc_price": MARKET_DATA.get_price("BTC-USD"),
        "vix": MARKET_DATA.get_vix(),
    }


# ─── Autonomous trading ───────────────────────────────────────────────────────

@app.post("/autonomous/toggle", tags=["trading"])
async def toggle_autonomous():
    global AUTONOMOUS_ENABLED
    AUTONOMOUS_ENABLED = not AUTONOMOUS_ENABLED
    logger.info("Autonomous mode: %s", AUTONOMOUS_ENABLED)
    return {"autonomous_enabled": AUTONOMOUS_ENABLED}

@app.get("/autonomous/status", tags=["trading"])
async def autonomous_status():
    # Real portfolio weights from OCaml optimizer (via orchestrator)
    weights = getattr(ORCHESTRATOR, "last_weights", None) or {
        "BTC": 0.40, "ETH": 0.30, "SPY": 0.20, "USD": 0.10
    }
    btc_snap = MARKET_DATA.get_snapshot("BTC-USD")
    return {
        "enabled":        AUTONOMOUS_ENABLED,
        "macro_pulse":    getattr(ORCHESTRATOR, "current_sentiment", "neutral"),
        "optimal_weights": weights,
        "recent_trades":  AUTONOMOUS_TRADES[-10:],
        "market": {
            "btc_price":  btc_snap.price if btc_snap else None,
            "btc_change": btc_snap.change_pct if btc_snap else None,
            "vix":        MARKET_DATA.get_vix(),
            "data_source": btc_snap.source if btc_snap else "offline",
            "data_age_s": round(btc_snap.age_seconds(), 1) if btc_snap else None,
        }
    }


# ─── Market data ─────────────────────────────────────────────────────────────

@app.get("/market_data", tags=["market"])
async def get_market_data_endpoint():
    """Return all live prices with source and age metadata."""
    return MARKET_DATA.get_all_snapshots()

@app.get("/market_data/{symbol}", tags=["market"])
async def get_symbol_price(symbol: str):
    snap = MARKET_DATA.get_snapshot(symbol.upper())
    if not snap:
        return {"error": f"No data for {symbol}", "available": list(MARKET_DATA.get_all_snapshots().keys())}
    from dataclasses import asdict
    return asdict(snap)

@app.get("/macro_news", tags=["market"])
async def get_macro_news():
    """Fetch real macro headlines from Yahoo Finance RSS."""
    return MARKET_DATA.get_macro_news()


# ─── Trade decision ───────────────────────────────────────────────────────────

@app.post("/trade_decision", tags=["trading"])
async def trade_decision(req: TradeDecisionRequest):
    """Run the full AI pipeline with REAL current market price."""
    symbol = req.symbol.upper()

    # Get live price — critical: no more hardcoded 65000.0
    current_price = MARKET_DATA.get_price(symbol)
    if current_price is None:
        return {
            "error":  f"No live price available for {symbol}",
            "symbol": symbol,
            "action": "HOLD",
        }

    headlines = [item["text"] for item in MARKET_DATA.get_macro_news()[:3]]
    signal = await ORCHESTRATOR.execute_sequential_pipeline(
        req.market_context or f"Live: {symbol} @ ${current_price:,.2f}",
        headlines,
        current_price,
    )
    return {
        "reasoning":  signal["reason"],
        "action":     signal["action"],
        "confidence": signal.get("confidence", 0.0),
        "regime":     signal.get("regime", "Range"),
        "sentiment":  signal.get("sentiment", 0.0),
        "symbol":     symbol,
        "qty":        round(1000.0 / current_price, 6),  # $1000 notional
        "price":      current_price,
        "entry_target": signal.get("entry_target", current_price),
        "data_source":  "live",
    }


@app.websocket("/ws/signals")
async def websocket_endpoint(websocket: WebSocket):
    await manager.connect(websocket)
    try:
        while True:
            await websocket.receive_text()
    except WebSocketDisconnect:
        manager.disconnect(websocket)

# ─── Chat ─────────────────────────────────────────────────────────────────────

@app.post("/chat", tags=["ai"])
async def chat(req: ChatRequest):
    return {
        "reply": (
            "Chat via Gemini is disabled in Hardware-Constrained Mode. "
            "Use local GGUF models via Orchestrator. "
            f"Current BTC: ${MARKET_DATA.get_price('BTC-USD') or 'unavailable':,}"
        )
    }


# ─── Entry point ──────────────────────────────────────────────────────────────

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="127.0.0.1", port=8000)
