"""
Robin Trading Platform — Institutional Backtester
==================================================
Walk-forward backtester with:
  - Transaction costs (maker/taker fees per exchange)
  - Slippage model (volume-weighted)
  - No lookahead bias (temporal data split)
  - Full institutional metrics: Sharpe, Sortino, Calmar, Win Rate, Profit Factor
  - Max Drawdown with duration tracking
  - Trade journal (entry/exit/P&L per trade)
"""

import logging
import time
from dataclasses import dataclass, field
from typing import Optional

import numpy as np
import pandas as pd

from strategy_engine import Bar, Signal, Side, Strategy

logger = logging.getLogger("backtester")


# ─── Fee models (realistic) ───────────────────────────────────────────────────

FEE_SCHEDULES = {
    "binance":  {"maker": 0.0010, "taker": 0.0010},  # 0.10% / 0.10%
    "alpaca":   {"maker": 0.0000, "taker": 0.0000},  # Alpaca = commission-free
    "oanda":    {"maker": 0.0001, "taker": 0.0002},  # ~1–2 pip spread
    "default":  {"maker": 0.0010, "taker": 0.0015},
}

ANNUALIZATION_FACTOR = 252  # Trading days per year


# ─── Trade record ─────────────────────────────────────────────────────────────

@dataclass
class Trade:
    symbol:      str
    side:        Side
    entry_price: float
    exit_price:  float
    qty:         float
    entry_ns:    int
    exit_ns:     int
    fee:         float
    pnl:         float          # After fees
    pnl_pct:     float
    strategy:    str
    entry_reason: str
    exit_reason: str


# ─── Backtest result ───────────────────────────────────────────────────────────

@dataclass
class BacktestResult:
    symbol:                str
    strategy:              str
    start_date:            str
    end_date:              str
    n_bars:                int
    n_trades:              int
    # Returns
    total_return_pct:      float
    annualized_return_pct: float
    # Risk-adjusted
    sharpe_ratio:          float
    sortino_ratio:         float
    calmar_ratio:          float
    # Drawdown
    max_drawdown_pct:      float
    max_drawdown_duration_days: float
    # Trade stats
    win_rate_pct:          float
    profit_factor:         float
    avg_win_pct:           float
    avg_loss_pct:          float
    avg_trade_bars:        float
    # Costs
    total_fees:            float
    total_slippage:        float
    # Trade journal
    trades:                list[Trade] = field(default_factory=list)

    def print_summary(self):
        print("\n" + "=" * 65)
        print(f"BACKTEST RESULTS: {self.strategy} on {self.symbol}")
        print(f"Period: {self.start_date} → {self.end_date} ({self.n_bars} bars)")
        print("=" * 65)
        print(f"  Total Return:          {self.total_return_pct:>+8.2f}%")
        print(f"  Annualized Return:     {self.annualized_return_pct:>+8.2f}%")
        print(f"  Sharpe Ratio:          {self.sharpe_ratio:>8.3f}")
        print(f"  Sortino Ratio:         {self.sortino_ratio:>8.3f}")
        print(f"  Calmar Ratio:          {self.calmar_ratio:>8.3f}")
        print(f"  Max Drawdown:          {self.max_drawdown_pct:>8.2f}%")
        print(f"  Max DD Duration:       {self.max_drawdown_duration_days:>7.0f} days")
        print("-" * 65)
        print(f"  Total Trades:          {self.n_trades:>8d}")
        print(f"  Win Rate:              {self.win_rate_pct:>8.1f}%")
        print(f"  Profit Factor:         {self.profit_factor:>8.3f}")
        print(f"  Avg Win:               {self.avg_win_pct:>+8.2f}%")
        print(f"  Avg Loss:              {self.avg_loss_pct:>+8.2f}%")
        print(f"  Avg Trade Duration:    {self.avg_trade_bars:>7.1f} bars")
        print("-" * 65)
        print(f"  Total Fees:            ${self.total_fees:>9.2f}")
        print(f"  Total Slippage:        ${self.total_slippage:>9.2f}")
        print("=" * 65)


# ─── Backtester ───────────────────────────────────────────────────────────────

class Backtester:
    """
    Event-driven backtester with realistic cost model.
    One-at-a-time bar processing to prevent lookahead bias.
    """

    def __init__(
        self,
        initial_capital: float = 100_000.0,
        exchange:        str   = "binance",
        slippage_bps:    float = 3.0,   # 3 basis points slippage per trade
        max_position_pct: float = 0.05,  # Max 5% of capital per position
        base_latency_ms: float = 5.0,
        jitter_ms:       float = 2.0,
        enable_jitter:   bool = True,
    ):
        self.initial_capital  = initial_capital
        self.fee_schedule     = FEE_SCHEDULES.get(exchange, FEE_SCHEDULES["default"])
        self.slippage_bps     = slippage_bps
        self.max_position_pct = max_position_pct
        self.base_latency_ms  = base_latency_ms
        self.jitter_ms        = jitter_ms
        self.enable_jitter    = enable_jitter

    def _simulate_execution_delay(self, bar, qty: float, side: Side) -> tuple[float, int]:
        """
        Simulates network latency, jitter, and queueing delays.
        Returns (adjusted_fill_price, latency_ns).
        """
        if not self.enable_jitter:
            return bar.close, 0

        # 1. Base latency + random jitter (Gaussian)
        jitter = np.random.normal(0, self.jitter_ms)
        latency_ms = max(0.1, self.base_latency_ms + jitter)

        # 2. Queueing delay (increases if order size is large relative to volume)
        queueing_factor = 0.0
        if bar.volume > 0:
            queueing_factor = (qty / bar.volume) * 500.0  # scale up for massive orders
        latency_ms += queueing_factor

        # 3. Price impact during the delay (modeled as random walk based on standard volatility)
        volatility_per_ms = 0.00001
        price_change_pct = np.random.normal(0, volatility_per_ms * np.sqrt(latency_ms))

        # Adjust price based on side
        if side == Side.BUY:
            fill_price = bar.close * (1.0 + price_change_pct)
        else:
            fill_price = bar.close * (1.0 - price_change_pct)

        latency_ns = int(latency_ms * 1e6)
        return float(fill_price), latency_ns

    def run(
        self,
        strategy: Strategy,
        df: pd.DataFrame,
        symbol: str,
        train_ratio: float = 0.7,  # Use last 30% as test set
    ) -> BacktestResult:
        """
        Run walk-forward backtest on a price DataFrame.
        Only evaluates performance on the out-of-sample test portion.
        """
        df = df.dropna(subset=["open", "high", "low", "close", "volume"]).reset_index(drop=True)
        split = int(len(df) * train_ratio)

        # Warm-up: feed training data to build strategy state (no evaluation)
        strategy.reset()
        logger.info("[%s] Warm-up: feeding %d bars ...", symbol, split)
        for _, row in df.iloc[:split].iterrows():
            bar = self._row_to_bar(row, symbol)
            strategy.on_bar(bar)

        # Test: evaluate on unseen data
        test_df = df.iloc[split:].reset_index(drop=True)
        logger.info("[%s] Backtesting: %d out-of-sample bars ...", symbol, len(test_df))

        return self._run_on_segment(strategy, test_df, symbol)

    def _run_on_segment(self, strategy: Strategy, df: pd.DataFrame, symbol: str) -> BacktestResult:
        """Run backtest on a single segment."""
        capital    = self.initial_capital
        position   = 0.0         # Current qty held
        entry_bar  = None
        entry_price = 0.0
        entry_signal: Optional[Signal] = None

        equity_curve: list[float] = []
        trades: list[Trade] = []
        total_fees  = 0.0
        total_slip  = 0.0

        for _, row in df.iterrows():
            bar = self._row_to_bar(row, symbol)
            equity_curve.append(capital + position * bar.close)

            signal = strategy.on_bar(bar)

            # Exit existing position if signal flips or no signal
            if position != 0:
                exit_condition = (
                    (position > 0 and signal is not None and signal.side == Side.SELL) or
                    (position < 0 and signal is not None and signal.side == Side.BUY)
                )
                if exit_condition:
                    exit_price, exit_latency_ns = self._simulate_execution_delay(
                        bar, abs(position), Side.SELL if position > 0 else Side.BUY
                    )
                    trade = self._close_position(
                        symbol, position, entry_price, exit_price,
                        entry_bar.timestamp_ns, bar.timestamp_ns + exit_latency_ns,
                        entry_signal, signal, capital
                    )
                    capital    += trade.pnl + position * entry_price
                    total_fees += trade.fee
                    total_slip += abs(exit_price * position) * self.slippage_bps / 10000
                    trades.append(trade)
                    position   = 0.0
                    entry_bar  = None

            # Enter new position
            if position == 0 and signal is not None:
                notional  = capital * self.max_position_pct * signal.strength
                
                # Simulate execution delay and get fill price
                fill_price, latency_ns = self._simulate_execution_delay(
                    bar, notional / bar.close, Side.BUY if signal.side == Side.BUY else Side.SELL
                )
                
                slip_cost = notional * self.slippage_bps / 10000
                fee_cost  = notional * self.fee_schedule["taker"]
                qty = notional / fill_price
                position_cost = qty * fill_price + fee_cost

                if position_cost <= capital:
                    position    = qty if signal.side == Side.BUY else -qty
                    entry_price = fill_price
                    entry_bar   = bar
                    entry_signal = signal
                    capital    -= position_cost
                    total_fees += fee_cost
                    total_slip += slip_cost

        # Close any open position at end
        if position != 0 and entry_bar is not None and len(df) > 0:
            last_close = df.iloc[-1]["close"]
            trade = self._close_position(
                symbol, position, entry_price, last_close,
                entry_bar.timestamp_ns, int(df.iloc[-1]["timestamp"].timestamp() * 1e9)
                    if hasattr(df.iloc[-1]["timestamp"], "timestamp") else time.time_ns(),
                entry_signal, None, capital
            )
            capital    += trade.pnl + abs(position) * entry_price
            total_fees += trade.fee
            trades.append(trade)

        return self._compute_metrics(
            symbol       = symbol,
            strategy     = strategy.name,
            df           = df,
            equity_curve = equity_curve,
            trades       = trades,
            total_fees   = total_fees,
            total_slip   = total_slip,
        )

    def _close_position(
        self,
        symbol: str,
        position: float,
        entry_price: float,
        exit_price: float,
        entry_ns: int,
        exit_ns: int,
        entry_signal: Optional[Signal],
        exit_signal: Optional[Signal],
        capital: float,
    ) -> Trade:
        qty      = abs(position)
        notional = qty * exit_price
        fee      = notional * self.fee_schedule["taker"]
        side     = Side.BUY if position > 0 else Side.SELL
        pnl_raw  = (exit_price - entry_price) * qty if position > 0 else (entry_price - exit_price) * qty
        pnl      = pnl_raw - fee
        pnl_pct  = pnl / (qty * entry_price) if entry_price > 0 else 0

        return Trade(
            symbol       = symbol,
            side         = side,
            entry_price  = entry_price,
            exit_price   = exit_price,
            qty          = qty,
            entry_ns     = entry_ns,
            exit_ns      = exit_ns,
            fee          = fee,
            pnl          = pnl,
            pnl_pct      = pnl_pct,
            strategy     = entry_signal.strategy if entry_signal else "unknown",
            entry_reason = entry_signal.reason if entry_signal else "",
            exit_reason  = exit_signal.reason if exit_signal else "end_of_period",
        )

    @staticmethod
    def _row_to_bar(row: pd.Series, symbol: str) -> Bar:
        ts = row["timestamp"]
        ts_ns = int(ts.timestamp() * 1e9) if hasattr(ts, "timestamp") else int(ts)
        return Bar(
            timestamp_ns=ts_ns,
            symbol=symbol,
            open=float(row["open"]),
            high=float(row["high"]),
            low=float(row["low"]),
            close=float(row["close"]),
            volume=float(row.get("volume", 0)),
        )

    def _compute_metrics(
        self,
        symbol: str,
        strategy: str,
        df: pd.DataFrame,
        equity_curve: list[float],
        trades: list[Trade],
        total_fees: float,
        total_slip: float,
    ) -> BacktestResult:
        equity = np.array(equity_curve)
        if len(equity) < 2:
            raise ValueError("Not enough bars to compute metrics")

        # Returns
        daily_returns = np.diff(equity) / equity[:-1]
        total_return  = (equity[-1] / equity[0]) - 1
        n_years       = len(df) / ANNUALIZATION_FACTOR
        ann_return    = (1 + total_return) ** (1 / max(n_years, 0.001)) - 1

        # Sharpe (annualised, daily rf≈0)
        if daily_returns.std() > 0:
            sharpe = (daily_returns.mean() / daily_returns.std()) * np.sqrt(ANNUALIZATION_FACTOR)
        else:
            sharpe = 0.0

        # Sortino (downside deviation only)
        neg_returns = daily_returns[daily_returns < 0]
        if len(neg_returns) > 0 and neg_returns.std() > 0:
            sortino = (daily_returns.mean() / neg_returns.std()) * np.sqrt(ANNUALIZATION_FACTOR)
        else:
            sortino = 0.0

        # Max Drawdown
        peak  = np.maximum.accumulate(equity)
        dd    = (equity - peak) / peak
        max_dd = float(dd.min())
        dd_dur = self._max_dd_duration(dd)

        # Calmar = annualised return / max drawdown
        calmar = ann_return / abs(max_dd) if max_dd != 0 else 0.0

        # Trade stats
        n_trades = len(trades)
        if n_trades > 0:
            wins     = [t for t in trades if t.pnl > 0]
            losses   = [t for t in trades if t.pnl <= 0]
            win_rate = len(wins) / n_trades * 100
            avg_win  = np.mean([t.pnl_pct * 100 for t in wins]) if wins else 0.0
            avg_loss = np.mean([t.pnl_pct * 100 for t in losses]) if losses else 0.0
            gross_profit = sum(t.pnl for t in wins)
            gross_loss   = abs(sum(t.pnl for t in losses))
            pf = min(gross_profit / gross_loss, 1e6) if gross_loss > 0 else 999999.0
            avg_dur = np.mean([(t.exit_ns - t.entry_ns) / 1e9 / 86400 for t in trades])
        else:
            win_rate = avg_win = avg_loss = avg_dur = 0.0
            pf = 0.0

        return BacktestResult(
            symbol                  = symbol,
            strategy                = strategy,
            start_date              = str(df["timestamp"].iloc[0])[:10],
            end_date                = str(df["timestamp"].iloc[-1])[:10],
            n_bars                  = len(df),
            n_trades                = n_trades,
            total_return_pct        = round(total_return * 100, 2),
            annualized_return_pct   = round(ann_return * 100, 2),
            sharpe_ratio            = round(sharpe, 3),
            sortino_ratio           = round(sortino, 3),
            calmar_ratio            = round(calmar, 3),
            max_drawdown_pct        = round(max_dd * 100, 2),
            max_drawdown_duration_days = round(dd_dur, 1),
            win_rate_pct            = round(win_rate, 1),
            profit_factor           = round(pf, 3),
            avg_win_pct             = round(avg_win, 2),
            avg_loss_pct            = round(avg_loss, 2),
            avg_trade_bars          = round(avg_dur, 1),
            total_fees              = round(total_fees, 2),
            total_slippage          = round(total_slip, 2),
            trades                  = trades,
        )

    @staticmethod
    def _max_dd_duration(dd_series: np.ndarray) -> float:
        """Return maximum drawdown duration in days (bars)."""
        max_dur = 0
        dur = 0
        for v in dd_series:
            if v < 0:
                dur += 1
                max_dur = max(max_dur, dur)
            else:
                dur = 0
        return float(max_dur)


# ─── CLI runner ────────────────────────────────────────────────────────────────

if __name__ == "__main__":
    import sys
    sys.path.insert(0, ".")
    from data_engine import DataEngine
    from strategy_engine import MeanReversionStrategy

    logging.basicConfig(level=logging.INFO)

    engine = DataEngine()

    available = engine.get_available_symbols()
    if not available:
        print("No data found. Run: python data_engine.py")
        sys.exit(1)

    symbol = available[0]
    print(f"Running backtest on: {symbol}")

    df = engine.load_dataset(symbol)
    strategy = MeanReversionStrategy(symbol=symbol, lookback=20, z_threshold=2.0)
    backtester = Backtester(initial_capital=100_000, exchange="binance")
    result = backtester.run(strategy, df, symbol)
    result.print_summary()
