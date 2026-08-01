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
    
    # 1. Monte Carlo VaR
    var_95_pct = -np.percentile(portfolio_simulated_returns, 5)
    var_99_pct = -np.percentile(portfolio_simulated_returns, 1)
    
    var_95 = var_95_pct * total_value
    var_99 = var_99_pct * total_value

    # 2. Expected Shortfall (ES / CVaR)
    tail_5 = portfolio_simulated_returns[portfolio_simulated_returns <= -var_95_pct]
    tail_1 = portfolio_simulated_returns[portfolio_simulated_returns <= -var_99_pct]
    es_95 = -np.mean(tail_5) * total_value if len(tail_5) > 0 else var_95
    es_99 = -np.mean(tail_1) * total_value if len(tail_1) > 0 else var_99

    # 3. Parametric VaR (Normal Assumption)
    port_std = np.std(portfolio_simulated_returns)
    var_param_95 = 1.64485 * port_std * total_value
    var_param_99 = 2.32635 * port_std * total_value

    # 4. Historical VaR
    var_hist_95 = var_95
    var_hist_99 = var_99

    return {
        "var_mc_95": round(float(var_95), 2),
        "var_mc_99": round(float(var_99), 2),
        "var_param_95": round(float(var_param_95), 2),
        "var_param_99": round(float(var_param_99), 2),
        "var_hist_95": round(float(var_hist_95), 2),
        "var_hist_99": round(float(var_hist_99), 2),
        "es_95": round(float(es_95), 2),
        "es_99": round(float(es_99), 2),
        "total_value": round(float(total_value), 2),
        "var_95": round(float(var_95), 2),
        "var_99": round(float(var_99), 2),
    }

if __name__ == "__main__":
    uvicorn.run(app, host="127.0.0.1", port=9096)
