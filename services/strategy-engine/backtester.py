"""
Robin Trading Platform — Strategy Backtester
============================================
Realistic backtester with:
- Per-trade commission (basis points)
- Market impact / slippage model (sqrt-law)
- Daily P&L and drawdown tracking
- Annualised Sharpe, Calmar, Sortino ratios

NOTE: This is a research tool only. It does NOT constitute investment advice.
      Simulated results are NOT indicative of future performance.
"""
import numpy as np
from typing import List, Optional
from dataclasses import dataclass, field
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
    slippage: float = 0.0

    @property
    def total_cost(self) -> float:
        return self.commission + abs(self.slippage)


@dataclass
class BacktestResult:
    final_capital: float
    total_return: float
    sharpe_ratio: float
    sortino_ratio: float
    calmar_ratio: float
    max_drawdown: float
    total_trades: int
    total_commission: float
    total_slippage: float
    equity_curve: np.ndarray
    trades: List[Trade] = field(default_factory=list)


class SlippageModel:
    """
    Square-root market impact model (Almgren et al., 2005).
    slippage_bps = impact_bps * sqrt(notional / adv)
    where adv = average daily volume in dollars.
    """
    def __init__(self, impact_bps: float = 5.0, adv_usd: float = 50_000_000.0):
        self.impact_bps = impact_bps
        self.adv_usd = adv_usd

    def estimate(self, notional: float) -> float:
        """Returns slippage as a fraction of notional (signed, always a cost)."""
        participation = notional / max(self.adv_usd, 1.0)
        slippage_frac = (self.impact_bps / 10_000.0) * np.sqrt(participation)
        return slippage_frac * notional  # Always positive cost


class StrategyBacktester:
    def __init__(
        self,
        initial_capital: float = 1_000_000.0,
        commission_bps: float = 2.0,         # 2bps per trade (institutional rate)
        slippage_model: Optional[SlippageModel] = None,
    ):
        self.initial_capital = initial_capital
        self.commission_bps = commission_bps
        self.slippage_model = slippage_model or SlippageModel()

    def _calc_transaction_costs(self, notional: float) -> tuple[float, float]:
        commission = notional * (self.commission_bps / 10_000.0)
        slippage = self.slippage_model.estimate(notional)
        return commission, slippage

    def run_backtest(self, prices: np.ndarray, signals: np.ndarray) -> BacktestResult:
        if len(prices) != len(signals):
            raise ValueError("prices and signals must be same length")

        n = len(prices)
        capital = float(self.initial_capital)
        position = 0.0
        equity = np.empty(n)
        trades: List[Trade] = []
        total_commission = 0.0
        total_slippage = 0.0

        for i in range(n):
            price = float(prices[i])
            sig = int(signals[i])

            if sig == 1 and capital > price:
                qty = (capital * 0.95) / price
                notional = qty * price
                commission, slippage = self._calc_transaction_costs(notional)
                total_cost = notional + commission + slippage
                if total_cost > capital:
                    # Reduce size to fit within available capital
                    qty *= capital / total_cost
                    notional = qty * price
                    commission, slippage = self._calc_transaction_costs(notional)
                    total_cost = notional + commission + slippage

                capital -= total_cost
                position += qty
                total_commission += commission
                total_slippage += slippage
                trades.append(Trade(i, 'SYM', OrderSide.BUY, price, qty, notional,
                                    commission, slippage))

            elif sig == -1 and position > 0:
                notional = position * price
                commission, slippage = self._calc_transaction_costs(notional)
                proceeds = notional - commission - slippage
                capital += proceeds
                total_commission += commission
                total_slippage += slippage
                trades.append(Trade(i, 'SYM', OrderSide.SELL, price, position, notional,
                                    commission, slippage))
                position = 0.0

            equity[i] = capital + position * price

        # Risk metrics
        ret = (equity[-1] - self.initial_capital) / self.initial_capital
        daily_returns = np.diff(equity) / equity[:-1] if len(equity) > 1 else np.array([0.0])
        ann_factor = np.sqrt(252)

        mean_ret = np.mean(daily_returns)
        std_ret = np.std(daily_returns) + 1e-10
        sharpe = mean_ret / std_ret * ann_factor

        downside = daily_returns[daily_returns < 0]
        sortino_denom = np.std(downside) + 1e-10 if len(downside) > 0 else 1e-10
        sortino = mean_ret / sortino_denom * ann_factor

        peak = np.maximum.accumulate(equity)
        drawdowns = (peak - equity) / (peak + 1e-10)
        max_dd = float(np.max(drawdowns))
        calmar = (ret / max_dd) if max_dd > 1e-8 else 0.0

        return BacktestResult(
            final_capital=equity[-1],
            total_return=ret,
            sharpe_ratio=sharpe,
            sortino_ratio=sortino,
            calmar_ratio=calmar,
            max_drawdown=max_dd,
            total_trades=len(trades),
            total_commission=total_commission,
            total_slippage=total_slippage,
            equity_curve=equity,
            trades=trades,
        )


if __name__ == "__main__":
    np.random.seed(42)
    n = 2520  # 10 years of daily bars
    prices = 100.0 * np.exp(np.cumsum(np.random.normal(0.0001, 0.01, n)))
    signals = np.random.choice([-1, 0, 1], n, p=[0.05, 0.90, 0.05])

    bt = StrategyBacktester(
        initial_capital=1_000_000.0,
        commission_bps=2.0,
        slippage_model=SlippageModel(impact_bps=5.0, adv_usd=50_000_000.0),
    )
    r = bt.run_backtest(prices, signals)

    print(f"Capital:     ${r.final_capital:>14,.2f}")
    print(f"Return:      {r.total_return:>+14.2%}")
    print(f"Sharpe:      {r.sharpe_ratio:>14.3f}")
    print(f"Sortino:     {r.sortino_ratio:>14.3f}")
    print(f"Calmar:      {r.calmar_ratio:>14.3f}")
    print(f"Max DD:      {r.max_drawdown:>14.2%}")
    print(f"Trades:      {r.total_trades:>14d}")
    print(f"Commission:  ${r.total_commission:>14,.2f}")
    print(f"Slippage:    ${r.total_slippage:>14,.2f}")
    print(f"Total cost:  ${r.total_commission + r.total_slippage:>14,.2f}")
