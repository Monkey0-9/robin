"""Corporate Actions & Dividends Engine

Handles dividend processing, stock splits, mergers, tracking error,
and dividend reinvestment for the strategy engine.
"""

from __future__ import annotations

import math
from dataclasses import dataclass, field
from enum import Enum
from typing import Dict, List, Optional, Tuple


class CorporateActionType(Enum):
    DIVIDEND = "dividend"
    STOCK_SPLIT = "split"
    MERGER = "merger"
    SPINOFF = "spinoff"


@dataclass
class CorporateAction:
    symbol: str
    ex_date: str
    pay_date: str
    type: CorporateActionType
    ratio: float = 1.0
    amount: float = 0.0


@dataclass
class Position:
    symbol: str
    qty: float
    avg_price: float
    cash_balance: float = 0.0
    dividend_income: float = 0.0


@dataclass
class PortfolioReturns:
    dates: List[str]
    returns: List[float]


class CorporateActionsEngine:
    def __init__(self):
        self.dividend_income: Dict[str, float] = {}

    def process_dividend(self, position: Position, dividend: CorporateAction) -> Position:
        amount = position.qty * dividend.amount
        position.cash_balance += amount
        position.dividend_income += amount
        self.dividend_income[position.symbol] = (
            self.dividend_income.get(position.symbol, 0.0) + amount
        )
        print(f"[CA] Dividend ${amount:.2f} credited to {position.symbol}")
        return position

    def process_stock_split(self, position: Position, ratio: float) -> Position:
        if ratio <= 0:
            raise ValueError(f"Invalid split ratio: {ratio}")
        old_qty = position.qty
        position.qty *= ratio
        position.avg_price /= ratio
        print(f"[CA] Stock split {ratio:.2f}:1 on {position.symbol}: "
              f"{old_qty:.0f} -> {position.qty:.0f} shares")
        return position

    def process_merger(
        self,
        position: Position,
        target_symbol: str,
        acquiring_symbol: str,
        exchange_ratio: float,
    ) -> Position:
        new_qty = position.qty * exchange_ratio
        new_avg_price = position.avg_price / exchange_ratio
        print(f"[CA] Merger: {position.symbol} acquired by {acquiring_symbol}. "
              f"{position.qty:.0f} shares -> {new_qty:.0f} shares of {acquiring_symbol}")
        return Position(
            symbol=acquiring_symbol,
            qty=new_qty,
            avg_price=new_avg_price,
            cash_balance=position.cash_balance,
            dividend_income=position.dividend_income,
        )

    @staticmethod
    def tracking_error_vs_benchmark(
        portfolio_returns: List[float],
        benchmark_returns: List[float],
    ) -> float:
        if len(portfolio_returns) != len(benchmark_returns):
            raise ValueError("Return series must have equal length")
        if len(portfolio_returns) < 2:
            return 0.0
        n = len(portfolio_returns)
        diffs = [p - b for p, b in zip(portfolio_returns, benchmark_returns)]
        mean_diff = sum(diffs) / n
        variance = sum((d - mean_diff) ** 2 for d in diffs) / (n - 1)
        return math.sqrt(variance)

    @staticmethod
    def dividend_reinvestment(
        position: Position,
        dividend: CorporateAction,
        current_price: float,
    ) -> Position:
        cash_dividend = position.qty * dividend.amount
        if current_price <= 0:
            print(f"[CA] Cannot reinvest: invalid price {current_price}")
            return position
        additional_shares = cash_dividend / current_price
        position.qty += additional_shares
        position.cash_balance += cash_dividend
        position.dividend_income += cash_dividend
        print(f"[CA] DRIP: ${cash_dividend:.2f} reinvested into "
              f"{additional_shares:.4f} shares of {position.symbol} at ${current_price:.2f}")
        return position

    def get_total_dividend_income(self) -> float:
        return sum(self.dividend_income.values())


def main():
    engine = CorporateActionsEngine()

    pos = Position(symbol="AAPL", qty=100.0, avg_price=150.0)
    print(f"Initial: {pos}")

    div = CorporateAction(
        symbol="AAPL",
        ex_date="2025-06-15",
        pay_date="2025-06-30",
        type=CorporateActionType.DIVIDEND,
        amount=0.50,
    )
    pos = engine.process_dividend(pos, div)
    print(f"After dividend: cash={pos.cash_balance:.2f} income={pos.dividend_income:.2f}")

    pos = engine.process_stock_split(pos, 4.0)
    print(f"After 4:1 split: qty={pos.qty:.0f} avg_price={pos.avg_price:.2f}")

    pos = engine.process_merger(pos, "AAPL", "NEWCO", 0.5)
    print(f"After merger: symbol={pos.symbol} qty={pos.qty:.0f} avg_price={pos.avg_price:.2f}")

    port_returns = [0.01, 0.02, -0.01, 0.03, 0.005]
    bench_returns = [0.008, 0.015, -0.005, 0.025, 0.01]
    te = CorporateActionsEngine.tracking_error_vs_benchmark(port_returns, bench_returns)
    print(f"Tracking error: {te:.6f}")

    drip_pos = Position(symbol="MSFT", qty=50.0, avg_price=350.0)
    drip_div = CorporateAction(
        symbol="MSFT", ex_date="", pay_date="",
        type=CorporateActionType.DIVIDEND, amount=0.75,
    )
    drip_pos = CorporateActionsEngine.dividend_reinvestment(drip_pos, drip_div, 355.0)
    print(f"After DRIP: qty={drip_pos.qty:.4f} income={drip_pos.dividend_income:.2f}")

    print(f"Total dividend income: ${engine.get_total_dividend_income():.2f}")


if __name__ == "__main__":
    main()
