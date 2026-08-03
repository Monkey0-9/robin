import pandas as pd
import logging
from dataclasses import dataclass
from typing import List, Dict

logging.basicConfig(level=logging.INFO)

@dataclass
class ShockScenario:
    name: str
    equity_shock: float        # e.g., -0.30 for 30% drop
    rates_shock: float         # bps change
    vol_shock: float           # multiplier (e.g. 2.0 for 200% vol)
    fx_shock: float            # e.g., 0.10 for 10% USD weakening

class StressTester:
    def __init__(self):
        self.scenarios = [
            ShockScenario("Black Monday (1987)", -0.226, 0.0, 3.0, 0.0),
            ShockScenario("Global Financial Crisis (2008)", -0.40, -400.0, 4.0, 0.15),
            ShockScenario("COVID-19 Crash (2020)", -0.30, -150.0, 5.0, 0.05),
            ShockScenario("Interest Rate Spike", -0.15, 200.0, 1.5, -0.05)
        ]

    def evaluate_portfolio(self, portfolio: Dict[str, float], delta: float, gamma: float) -> Dict[str, float]:
        """
        Evaluate a simplified portfolio against historical stress scenarios using
        second-order Taylor approximation (Delta-Gamma) for equity shocks.
        portfolio map represents asset classes (e.g., 'equity': 1000000)
        """
        results = {}
        eq_value = portfolio.get('equity', 0.0)
        
        logging.info(f"--- Running Stress Tests on Portfolio Equity: ${eq_value:,.2f} ---")
        
        for scenario in self.scenarios:
            # Taylor Expansion: dP = Delta * dS + 0.5 * Gamma * (dS)^2
            dS = eq_value * scenario.equity_shock
            pnl = (delta * dS) + (0.5 * gamma * (dS ** 2))
            
            # Simple FX impact
            fx_pnl = portfolio.get('fx_exposure', 0.0) * scenario.fx_shock
            
            # Simple Rates impact (Duration approx: dP = -Duration * dY * Value)
            # Assuming average duration of 5.0 for fixed income
            rates_pnl = portfolio.get('fixed_income', 0.0) * -5.0 * (scenario.rates_shock / 10000.0)
            
            total_shock_pnl = pnl + fx_pnl + rates_pnl
            results[scenario.name] = total_shock_pnl
            
            logging.info(f"Scenario: {scenario.name:30} | Est. PnL Impact: ${total_shock_pnl:,.2f}")
            
        return results

if __name__ == "__main__":
    tester = StressTester()
    # Dummy portfolio with delta = 1.0 (delta-one) and gamma = 0.0 (linear)
    tester.evaluate_portfolio(
        portfolio={
            'equity': 1_000_000.0, 
            'fixed_income': 500_000.0, 
            'fx_exposure': 200_000.0
        }, 
        delta=1.0, 
        gamma=0.0
    )
