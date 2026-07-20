"""
Robin Trading Platform — Kelly Criterion Position Sizer
========================================================
Computes optimal position sizes using the Half-Kelly criterion,
constrained by portfolio risk limits.

Institutional standard:
  - Half-Kelly to reduce variance vs. full-Kelly
  - Hard cap of 5% of portfolio per position
  - Minimum position = 0 (never short via position sizer alone)
  - Integrates with ModelRiskMonitor for AI signal confidence weighting
"""

import logging
from dataclasses import dataclass
from typing import Optional

logger = logging.getLogger("position_sizer")


@dataclass
class SizingResult:
    fraction:        float     # Fraction of portfolio to allocate (0.0–1.0)
    notional:        float     # Dollar amount to trade
    qty:             float     # Units / shares / contracts
    reason:          str       # Human-readable explanation
    kelly_full:      float     # Raw Kelly fraction (before half/cap)
    kelly_half:      float     # Half-Kelly fraction (before cap)
    capped:          bool      # True if result was capped at max_fraction


class KellyPositionSizer:
    """
    Half-Kelly position sizer with institutional risk limits.

    Usage:
        sizer = KellyPositionSizer(portfolio_value=100_000)
        result = sizer.compute(
            win_rate=0.55,
            avg_win_pct=0.02,
            avg_loss_pct=0.01,
            price=65_000,
            confidence=0.75,    # AI signal confidence
        )
        print(f"Trade ${result.notional:.2f} at {result.fraction:.1%} of portfolio")
    """

    def __init__(
        self,
        portfolio_value: float = 100_000.0,
        max_fraction: float    = 0.05,   # Hard cap: 5% per position (institutional)
        min_fraction: float    = 0.001,  # Minimum meaningful position size
        max_notional: float    = 50_000, # Hard cap in dollars (absolute)
        risk_free_rate: float  = 0.043,  # Approx US 1yr T-Bill rate
    ):
        self.portfolio_value = portfolio_value
        self.max_fraction    = max_fraction
        self.min_fraction    = min_fraction
        self.max_notional    = max_notional
        self.risk_free_rate  = risk_free_rate

    def compute(
        self,
        win_rate:     float,
        avg_win_pct:  float,
        avg_loss_pct: float,
        price:        float,
        confidence:   float = 1.0,
        symbol:       str   = "UNKNOWN",
    ) -> SizingResult:
        """
        Compute a Half-Kelly position size.

        Args:
            win_rate:     Historical win rate [0.0, 1.0]
            avg_win_pct:  Average winning trade return [0.0, ∞]
            avg_loss_pct: Average losing trade return [0.0, ∞] (positive value)
            price:        Current asset price (for qty calculation)
            confidence:   AI signal confidence [0.0, 1.0] (scales kelly fraction)
            symbol:       Symbol name (for logging)

        Returns:
            SizingResult with position sizing details
        """
        # Input validation
        win_rate     = float(max(0.01, min(0.99, win_rate)))
        avg_win_pct  = float(max(1e-6, avg_win_pct))
        avg_loss_pct = float(max(1e-6, avg_loss_pct))
        confidence   = float(max(0.0, min(1.0, confidence)))
        loss_rate    = 1.0 - win_rate

        # Full Kelly fraction: f* = (p*b - q) / b
        #   p = win_rate, q = loss_rate, b = avg_win / avg_loss
        b = avg_win_pct / avg_loss_pct
        kelly_full = (win_rate * b - loss_rate) / b

        # If Kelly is negative, the edge is negative — don't trade
        if kelly_full <= 0:
            return SizingResult(
                fraction   = 0.0,
                notional   = 0.0,
                qty        = 0.0,
                reason     = f"Negative Kelly ({kelly_full:.4f}) — no statistical edge. Skip trade.",
                kelly_full = kelly_full,
                kelly_half = 0.0,
                capped     = False,
            )

        # Half-Kelly: reduces variance significantly, small return sacrifice
        kelly_half = kelly_full / 2.0

        # Scale by AI signal confidence (lower confidence → smaller position)
        kelly_adj = kelly_half * confidence

        # Apply institutional hard cap
        capped   = kelly_adj > self.max_fraction
        fraction = min(kelly_adj, self.max_fraction)
        fraction = max(fraction, self.min_fraction if kelly_adj > 0 else 0.0)

        # Compute dollar notional
        notional = self.portfolio_value * fraction
        notional = min(notional, self.max_notional)

        # Compute quantity (units)
        qty = notional / price if price > 0 else 0.0

        reason = (
            f"Kelly={kelly_full:.3f} → Half-Kelly={kelly_half:.3f} "
            f"× confidence={confidence:.2f} = {kelly_adj:.3f}"
            + (f" (capped at {self.max_fraction:.0%})" if capped else "")
        )

        logger.debug(
            "[Kelly] %s: full=%.3f half=%.3f adj=%.3f → "
            "fraction=%.2f%% notional=$%.2f qty=%.6f",
            symbol, kelly_full, kelly_half, kelly_adj,
            fraction * 100, notional, qty
        )

        return SizingResult(
            fraction   = round(fraction, 6),
            notional   = round(notional, 2),
            qty        = round(qty, 8),
            reason     = reason,
            kelly_full = round(kelly_full, 6),
            kelly_half = round(kelly_half, 6),
            capped     = capped,
        )

    def from_backtest_stats(
        self,
        backtest_stats: dict,
        price: float,
        confidence: float = 1.0,
        symbol: str = "UNKNOWN",
    ) -> SizingResult:
        """
        Convenience method: compute sizing from a BacktestResult stats dict.
        Expects keys: win_rate_pct, avg_win_pct, avg_loss_pct
        """
        return self.compute(
            win_rate     = backtest_stats.get("win_rate_pct", 50.0) / 100.0,
            avg_win_pct  = backtest_stats.get("avg_win_pct",   1.5) / 100.0,
            avg_loss_pct = backtest_stats.get("avg_loss_pct",  1.0) / 100.0,
            price        = price,
            confidence   = confidence,
            symbol       = symbol,
        )

    def update_portfolio_value(self, new_value: float):
        """Update portfolio value (call daily or after each fill)."""
        if new_value <= 0:
            raise ValueError("Portfolio value must be positive")
        self.portfolio_value = new_value
        logger.info("[Kelly] Portfolio value updated: $%.2f", new_value)


# ─── Standalone test ──────────────────────────────────────────────────────────

if __name__ == "__main__":
    logging.basicConfig(level=logging.DEBUG)

    sizer = KellyPositionSizer(portfolio_value=100_000.0)

    test_cases = [
        # (win_rate, avg_win_pct, avg_loss_pct, price, confidence, symbol)
        (0.55, 0.015, 0.010, 65000.0, 0.75, "BTC-USD"),  # Good edge, high confidence
        (0.52, 0.010, 0.010, 180.0,   0.60, "AAPL"),      # Marginal edge
        (0.45, 0.020, 0.010, 2500.0,  0.80, "ETH-USD"),   # Negative Kelly
        (0.60, 0.025, 0.012, 500.0,   0.90, "NVDA"),      # Strong edge
    ]

    print("\n" + "=" * 80)
    print("KELLY CRITERION POSITION SIZER — TEST RESULTS")
    print("=" * 80)
    print(f"{'Symbol':<12} {'WinRate':>8} {'Action':>8} {'Fraction':>9} {'Notional':>12} {'Qty':>12}")
    print("-" * 70)

    for win_rate, avg_win, avg_loss, price, conf, symbol in test_cases:
        r = sizer.compute(win_rate, avg_win, avg_loss, price, conf, symbol)
        action = "TRADE" if r.fraction > 0 else "SKIP"
        print(
            f"{symbol:<12} {win_rate:>7.0%} {action:>8} "
            f"{r.fraction:>8.2%} ${r.notional:>11,.2f} {r.qty:>12.6f}"
        )

    print("=" * 80)
