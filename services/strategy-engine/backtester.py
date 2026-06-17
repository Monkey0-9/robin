# services/strategy-engine/backtester.py
# Historical Backtesting Engine (Reference: Schwab thinkorswim analyze tab)
# Replays KDB+ tick records to verify trading strategies.

import pandas as pd
import numpy as np

class StrategyBacktester:
    def __init__(self, initial_capital=100000.0):
        self.capital = initial_capital
        self.positions = 0.0
        self.equity_curve = []

    def run_backtest(self, price_series, signals):
        """
        Runs strategy simulation over price series.
        signals: List of integer values: 1 (Buy), -1 (Sell), 0 (Hold)
        """
        for price, sig in zip(price_series, signals):
            if sig == 1 and self.capital > price:
                # Buy 1 unit
                self.positions += 1.0
                self.capital -= price
                print(f"[Backtest Buy] Executed at ${price:.2f}")
            elif sig == -1 and self.positions > 0:
                # Sell 1 unit
                self.positions -= 1.0
                self.capital += price
                print(f"[Backtest Sell] Executed at ${price:.2f}")
            
            portfolio_value = self.capital + (self.positions * price)
            self.equity_curve.append(portfolio_value)

        final_val = self.capital + (self.positions * price_series[-1])
        return final_val, self.equity_curve

if __name__ == "__main__":
    prices = [100.0, 101.5, 99.8, 102.3, 105.0]
    signals = [1, 0, 0, -1, 0] # Simple Buy at 100, Sell at 102.3
    
    backtester = StrategyBacktester(10000.0)
    final_val, curve = backtester.run_backtest(prices, signals)
    
    print(f"[Backtest Done] Final Capital Valuation: ${final_val:.2f} | Net Profit: ${(final_val - 10000.0):.2f}")
