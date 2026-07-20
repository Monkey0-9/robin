from fastapi import FastAPI
from pydantic import BaseModel
from fastapi.middleware.cors import CORSMiddleware
from contextlib import asynccontextmanager
import asyncio
import random

# Hardware-Constrained Sequential Orchestrator
from orchestrator import HardwareConstrainedOrchestrator

# Global state for Autonomous Agent
AUTONOMOUS_ENABLED = False
ORCHESTRATOR = HardwareConstrainedOrchestrator()
AUTONOMOUS_TRADES = []


async def autonomous_trading_loop():
    global AUTONOMOUS_TRADES

    while True:
        if AUTONOMOUS_ENABLED:
            # Simulate real-time market data
            mock_market_summary = (
                "Market seeing massive growth and surge in tech stocks."
            )
            mock_headlines = [
                "FDA approval granted",
                "Earnings beat expectations",
                "Inflation dropping",
            ]
            current_price = 65000.0 + random.uniform(-100, 100)

            # 1. Run the Defense-in-Depth Guardian check (Hardcoded rules)
            is_safe = ORCHESTRATOR.risk_guardian_check(
                simulated_vix=18.5,  # Safe VIX
                daily_pnl_pct=0.01,  # Safe PnL
            )

            if is_safe:
                # 2. Execute Sequential Pipeline (Load -> Infer -> Unload)
                signal = await ORCHESTRATOR.execute_sequential_pipeline(
                    mock_market_summary, mock_headlines, current_price
                )

                if signal["action"] != "HOLD":
                    AUTONOMOUS_TRADES.append(
                        {
                            "action": signal["action"],
                            "symbol": "BTC/USD",
                            "reason": signal["reason"],
                            "entry": signal["entry_target"],
                        }
                    )

                # Keep history bounded
                if len(AUTONOMOUS_TRADES) > 100:
                    AUTONOMOUS_TRADES.pop(0)

        # Wait 30 seconds between full sequential pipeline executions
        # (simulating realistic constraints on 4GB VRAM)
        await asyncio.sleep(15)


@asynccontextmanager
async def lifespan(app: FastAPI):
    # Startup logic
    print("Starting Robin Hardware-Constrained AI Agent Microservice...")
    loop_task = asyncio.create_task(autonomous_trading_loop())
    yield
    # Shutdown logic
    loop_task.cancel()
    print("Shutting down gracefully...")


app = FastAPI(title="Robin AI Agent Microservice", lifespan=lifespan)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


class ChatRequest(BaseModel):
    message: str


class TradeDecisionRequest(BaseModel):
    market_context: str


@app.get("/live")
async def live():
    return {"status": "ok"}


@app.get("/ready")
async def ready():
    return {"status": "ready"}


@app.post("/autonomous/toggle")
async def toggle_autonomous():
    global AUTONOMOUS_ENABLED
    AUTONOMOUS_ENABLED = not AUTONOMOUS_ENABLED
    return {"autonomous_enabled": AUTONOMOUS_ENABLED}


@app.get("/autonomous/status")
async def autonomous_status():
    return {
        "enabled": AUTONOMOUS_ENABLED,
        "macro_pulse": ORCHESTRATOR.current_sentiment,
        "optimal_weights": {
            "BTC": 0.4,
            "ETH": 0.3,
            "USD": 0.3,
        },  # Mocked for UI stability
        "recent_trades": AUTONOMOUS_TRADES[-10:],
    }


@app.post("/chat")
async def chat(req: ChatRequest):
    return {
        "reply": "Chat via Gemini is disabled in Hardware-Constrained Mode. Use local GGUF models via Orchestrator."
    }


@app.post("/trade_decision")
async def trade_decision(req: TradeDecisionRequest):
    # Route via the Sequential Orchestrator
    signal = await ORCHESTRATOR.execute_sequential_pipeline(
        "Market summary", ["News"], 65000.0
    )
    return {
        "reasoning": signal["reason"],
        "action": signal["action"],
        "symbol": "BTC/USD",
        "qty": 0.1,
        "price": signal["entry_target"],
    }


@app.get("/macro_news")
async def get_macro_news():
    return [
        {
            "time": "Today, 10:42",
            "text": "Fed Chairman leaves rates unchanged, cites persistent inflation metrics.",
            "impact": "high",
        },
        {
            "time": "Today, 09:30",
            "text": "US Core CPI data matches consensus estimates at 3.2% YoY.",
            "impact": "medium",
        },
    ]


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=8000)
