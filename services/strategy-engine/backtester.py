import numpy as np
from typing import List, Optional
from dataclasses import dataclass
from enum import Enum

class OrderSide(Enum):
    BUY = 1
    SELL = -1
    HOLD = 0

@dataclass
class Trade:
    timestamp: int
    symbol: str
    side: OrderSide
    price: float
    qty: float
    notional: float
    commission: float = 0.0

@dataclass
class BacktestResult:
    final_capital: float
    total_return: float
    sharpe_ratio: float
    max_drawdown: float
    total_trades: int
    equity_curve: np.ndarray

class StrategyBacktester:
    def __init__(self, initial_capital: float = 1_000_000.0, commission_pct: float = 0.001):
        self.initial_capital = initial_capital
        self.commission_pct = commission_pct

    def run_backtest(self, prices: np.ndarray, signals: np.ndarray) -> BacktestResult:
        if len(prices) != len(signals):
            raise ValueError("prices and signals must match")
        n = len(prices)
        capital = float(self.initial_capital)
        position = 0.0
        equity = np.empty(n)
        trades = []
        for i in range(n):
            price = float(prices[i])
            sig = int(signals[i])
            if sig == 1 and capital > price:
                qty = (capital * 0.95) / price
                cost = qty * price
                capital -= cost + cost * self.commission_pct
                position += qty
                trades.append(Trade(i, 'A', OrderSide.BUY, price, qty, cost))
            elif sig == -1 and position > 0:
                proceeds = position * price
                capital += proceeds - proceeds * self.commission_pct
                trades.append(Trade(i, 'A', OrderSide.SELL, price, position, proceeds))
                position = 0.0
            equity[i] = capital + position * price

        ret = (equity[-1] - self.initial_capital) / self.initial_capital
        returns = np.diff(equity) / equity[:-1] if len(equity) > 1 else np.array([0.0])
        sharpe = np.mean(returns) / (np.std(returns) + 1e-10) * np.sqrt(252)
        peak = np.maximum.accumulate(equity)
        dd = np.max((peak - equity) / peak)
        return BacktestResult(equity[-1], ret, sharpe, dd, len(trades), equity)

    def run_vectorized(self, prices: np.ndarray, signals: np.ndarray) -> BacktestResult:
        prices = np.asarray(prices, dtype=float)
        s = np.asarray(signals, dtype=float)
        pos = np.cumsum(s)
        cash = self.initial_capital - np.cumsum(s * prices)
        equity = cash + pos * prices
        ret = (equity[-1] - self.initial_capital) / self.initial_capital
        r = np.diff(equity) / equity[:-1]
        sharpe = np.mean(r) / (np.std(r) + 1e-10) * np.sqrt(252)
        peak = np.maximum.accumulate(equity)
        dd = np.max((peak - equity) / peak)
        return BacktestResult(equity[-1], ret, sharpe, dd, int(np.sum(np.abs(np.diff(s, prepend=0)) > 0)), equity)

if __name__ == "__main__":
    np.random.seed(42)
    n = 2520
    prices = 100.0 * np.exp(np.cumsum(np.random.normal(0.0001, 0.01, n)))
    signals = np.random.choice([-1, 0, 1], n, p=[0.05, 0.90, 0.05])
    bt = StrategyBacktester()
    r = bt.run_backtest(prices, signals)
    print(f"Capital=${r.final_capital:,.2f} Ret={r.total_return:.2%} Sharpe={r.sharpe_ratio:.3f} DD={r.max_drawdown:.2%} Trades={r.total_trades}")
