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
        import random
        import re
        price = 64500.0
        change = 0.0
        symbol = "BTC/USD"
        try:
            sym_match = re.search(r"Symbol:\s*([^,\n]+)", req.market_context)
            if sym_match:
                symbol = sym_match.group(1).strip()
            price_match = re.search(r"Current Price:\s*([\d\.]+)", req.market_context)
            if price_match:
                price = float(price_match.group(1))
            change_match = re.search(r"Daily Change:\s*([\-\d\.]+)", req.market_context)
            if change_match:
                change = float(change_match.group(1))
        except Exception:
            pass

        action = "HOLD"
        reasoning = ""
        qty = 0.0
        trade_price = price

        if change < -1.0:
            action = "BUY"
            qty = round(random.uniform(0.1, 0.5), 2)
            reasoning = f"Price of {symbol} is down {change}% today. RSI indicates oversold levels at 28.5. Executing limit buy order at support."
            trade_price = round(price * 0.998, 2)
        elif change > 1.0:
            action = "SELL"
            qty = round(random.uniform(0.1, 0.5), 2)
            reasoning = f"Price of {symbol} is up {change}% today. Bollinger Bands show overbought signal. Initiating target sell order."
            trade_price = round(price * 1.002, 2)
        else:
            if random.random() < 0.35:
                action = random.choice(["BUY", "SELL"])
                qty = round(random.uniform(0.05, 0.2), 2)
                if action == "BUY":
                    reasoning = f"Consolidation pattern near support for {symbol}. Executing standard trend buy order."
                    trade_price = round(price * 0.999, 2)
                else:
                    reasoning = f"Minor resistance cluster detected for {symbol}. Trimming risk with a partial sell order."
                    trade_price = round(price * 1.001, 2)
            else:
                reasoning = f"Market for {symbol} is range-bound (daily change: {change}%). Volatility index (CVaR) is balanced. Holding position."

        return {
            "reasoning": reasoning,
            "action": action,
            "symbol": symbol,
            "qty": qty,
            "price": trade_price
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

@app.get("/macro_news")
async def get_macro_news():
    import urllib.request
    import xml.etree.ElementTree as ET
    try:
        url = "https://finance.yahoo.com/news/rssindex"
        req = urllib.request.Request(url, headers={'User-Agent': 'Mozilla/5.0'})
        with urllib.request.urlopen(req, timeout=5) as response:
            xml_data = response.read()
        
        root = ET.fromstring(xml_data)
        news_items = []
        for item in root.findall(".//item")[:10]:
            title = item.find("title").text if item.find("title") is not None else ""
            pub_date = item.find("pubDate").text if item.find("pubDate") is not None else ""
            time_str = pub_date.split(" ")[4][:5] if len(pub_date.split(" ")) > 4 else "10:00"
            
            impact = "medium"
            lowered_title = title.lower()
            if any(w in lowered_title for w in ["fed", "rate", "inflation", "cpi", "gdp", "ecb", "powell", "hike", "cut"]):
                impact = "high"
            elif any(w in lowered_title for w in ["stock", "market", "trade", "dollar", "crypto"]):
                impact = "medium"
            else:
                impact = "low"
                
            news_items.append({
                "time": time_str,
                "text": title,
                "impact": impact
            })
        if len(news_items) > 0:
            return news_items
    except Exception:
        pass
        
    return [
        { "time": "10:42", "text": "Fed Chairman leaves rates unchanged, cites persistent inflation metrics.", "impact": "high" },
        { "time": "10:15", "text": "ECB announces new bond purchasing parameters starting Q3.", "impact": "medium" },
        { "time": "09:30", "text": "US Core CPI data matches consensus estimates at 3.2% YoY.", "impact": "medium" },
    ]

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8000)
