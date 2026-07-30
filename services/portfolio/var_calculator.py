import os
import json
import uvicorn
import numpy as np
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from typing import List, Dict

app = FastAPI(title="Robin VaR Calculator")

class Position(BaseModel):
    symbol: str
    size: float
    current_price: float

class Portfolio(BaseModel):
    positions: List[Position]

@app.post("/api/var")
def calculate_var(portfolio: Portfolio):
    if not portfolio.positions:
        return {"var_95": 0.0, "var_99": 0.0, "total_value": 0.0}

    total_value = 0.0
    weights = []
    prices = []
    
    for p in portfolio.positions:
        val = p.size * p.current_price
        total_value += val
        prices.append(p.current_price)
    
    if total_value == 0:
        return {"var_95": 0.0, "var_99": 0.0, "total_value": 0.0}

    for p in portfolio.positions:
        val = p.size * p.current_price
        weights.append(val / total_value)

    # In a production system, we'd fetch actual historical returns.
    # For prototype, simulate 10,000 scenarios using a simple random walk with drift
    num_assets = len(portfolio.positions)
    num_simulations = 10000
    
    # Assume 30-day volatility (annualized) of 40% for crypto, drift of 0%
    mu = 0.0
    sigma = 0.40 / np.sqrt(365) # daily volatility
    
    # Generate random returns
    simulated_returns = np.random.normal(mu, sigma, (num_simulations, num_assets))
    
    # Calculate portfolio returns for each simulation
    weights_array = np.array(weights)
    portfolio_simulated_returns = np.dot(simulated_returns, weights_array)
    
    # Sort returns to find percentiles for VaR
    portfolio_simulated_returns.sort()
    
    # VaR at 95% and 99% confidence (1-day)
    var_95_pct = -np.percentile(portfolio_simulated_returns, 5)
    var_99_pct = -np.percentile(portfolio_simulated_returns, 1)
    
    var_95 = var_95_pct * total_value
    var_99 = var_99_pct * total_value

    return {
        "var_95": round(var_95, 2),
        "var_99": round(var_99, 2),
        "total_value": round(total_value, 2),
        "var_95_pct": round(var_95_pct * 100, 2),
        "var_99_pct": round(var_99_pct * 100, 2)
    }

if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.1", port=9096)
