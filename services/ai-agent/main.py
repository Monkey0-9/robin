import os
import json
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
import google.generativeai as genai
from prompt import QUANTITATIVE_SYSTEM_PROMPT

from fastapi.middleware.cors import CORSMiddleware

from contextlib import asynccontextmanager

@asynccontextmanager
async def lifespan(app: FastAPI):
    # Startup logic
    print("Starting Robin AI Agent Microservice...")
    yield
    # Shutdown logic
    print("Shutting down Robin AI Agent Microservice gracefully...")

app = FastAPI(title="Robin AI Agent Microservice", lifespan=lifespan)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# Configure Gemini
api_key = os.getenv("GEMINI_API_KEY")
if api_key:
    genai.configure(api_key=api_key)

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

@app.post("/chat")
async def chat(req: ChatRequest):
    if not api_key:
        return {"reply": "Mock response from Python: " + req.message}
    
    try:
        model = genai.GenerativeModel('gemini-2.5-flash')
        prompt = f"{QUANTITATIVE_SYSTEM_PROMPT}\n\nThe user says: {req.message}"
        response = model.generate_content(prompt)
        return {"reply": response.text}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

@app.post("/trade_decision")
async def trade_decision(req: TradeDecisionRequest):
    if not api_key:
        return {
            "reasoning": "Mock mode: CVaR is elevated, triggering a conservative hold from Python service.",
            "action": "HOLD",
            "symbol": "BTC/USD",
            "qty": 0.0,
            "price": 0.0
        }
    
    try:
        model = genai.GenerativeModel('gemini-2.5-flash')
        prompt = f"{QUANTITATIVE_SYSTEM_PROMPT}\n\nThe current market state is as follows:\n{req.market_context}\n\nBased on this, you must output your decision ONLY in the following exact JSON format (without any markdown code blocks, just raw JSON):\n{{\"reasoning\": \"your mathematical reasoning here\", \"action\": \"BUY|SELL|HOLD\", \"symbol\": \"BTC/USD\", \"qty\": 1.0, \"price\": 64500}}"
        
        response = model.generate_content(prompt)
        text = response.text.strip()
        if text.startswith("```json"):
            text = text[7:]
        if text.endswith("```"):
            text = text[:-3]
        text = text.strip()
        
        decision = json.loads(text)
        return decision
    except Exception as e:
        return {
            "reasoning": f"Failed to parse LLM JSON: {str(e)}",
            "action": "HOLD",
            "symbol": "BTC/USD",
            "qty": 0.0,
            "price": 0.0
        }

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8000)
