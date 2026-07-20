"""
Robin Trading Platform — Strategy Engine
=========================================
Pluggable strategy framework with:
  1. MeanReversionStrategy  — Bollinger Band Z-score (technical only)
  2. MomentumStrategy       — Trend-following with ATR filter
  3. AIEnhancedStrategy     — Combines LLM regime + FinBERT sentiment + technical

Signal confluence rule: Both technical AND AI signals must agree before trading.
This prevents the AI from trading on regime alone (reduces false positives).
"""

import asyncio
import logging
import time
from abc import ABC, abstractmethod
from collections import deque
from dataclasses import dataclass, field
from enum import Enum
from typing import Optional

import numpy as np

logger = logging.getLogger("strategy_engine")


class Side(Enum):
    BUY  = "BUY"
    SELL = "SELL"


@dataclass
class Bar:
    """Single OHLCV bar — the atomic unit of strategy input."""
    timestamp_ns: int
    symbol:       str
    open:         float
    high:         float
    low:          float
    close:        float
    volume:       float

    def to_summary(self) -> str:
        """Human-readable summary for LLM context."""
        return (
            f"Symbol={self.symbol} Close={self.close:.4f} "
            f"High={self.high:.4f} Low={self.low:.4f} "
            f"Volume={self.volume:.0f}"
        )


@dataclass
class Signal:
    """A trading signal produced by a strategy."""
    side:         Side
    strength:     float          # [0.0, 1.0] — used for position sizing confidence
    symbol:       str
    strategy:     str
    reason:       str
    bar:          Optional[Bar]  = None
    generated_ns: int            = field(default_factory=time.time_ns)


@dataclass
class Fill:
    """Execution confirmation for an order."""
    cl_ord_id:  str
    symbol:     str
    side:       Side
    qty:        float
    price:      float
    fee:        float
    timestamp_ns: int


# ─── Base Strategy ────────────────────────────────────────────────────────────

class Strategy(ABC):
    """
    Abstract base for all strategies.
    Strategies are stateful and receive bars one at a time.
    """

    def __init__(self, name: str, symbol: str):
        self.name   = name
        self.symbol = symbol

    @abstractmethod
    def on_bar(self, bar: Bar) -> Optional[Signal]:
        """Called on each new bar. Return Signal or None."""
        ...

    def on_fill(self, fill: Fill) -> None:
        """Called when an order is filled. Override to update P&L state."""
        pass

    def reset(self) -> None:
        """Reset all strategy state (for walk-forward validation)."""
        pass


# ─── Strategy 1: Mean Reversion ───────────────────────────────────────────────

class MeanReversionStrategy(Strategy):
    """
    Buy when price drops >z_threshold standard deviations below rolling mean.
    Sell when price rises >z_threshold standard deviations above rolling mean.
    Uses Bollinger Band z-score on closing price.
    """

    def __init__(
        self,
        symbol:      str,
        lookback:    int   = 20,
        z_threshold: float = 2.0,
    ):
        super().__init__("MeanReversion", symbol)
        self.lookback    = lookback
        self.z_threshold = z_threshold
        self._prices: deque[float] = deque(maxlen=lookback)

    def on_bar(self, bar: Bar) -> Optional[Signal]:
        self._prices.append(bar.close)

        if len(self._prices) < self.lookback:
            return None

        arr = np.array(self._prices)
        ma  = arr.mean()
        std = arr.std()

        if std < 1e-10:
            return None

        z = (bar.close - ma) / std

        if z < -self.z_threshold:
            return Signal(
                side=Side.BUY,
                strength=min(abs(z) / (self.z_threshold * 2), 1.0),
                symbol=self.symbol,
                strategy=self.name,
                reason=f"Bollinger z={z:.2f} < -{self.z_threshold} — mean-reversion BUY",
                bar=bar,
            )
        elif z > self.z_threshold:
            return Signal(
                side=Side.SELL,
                strength=min(abs(z) / (self.z_threshold * 2), 1.0),
                symbol=self.symbol,
                strategy=self.name,
                reason=f"Bollinger z={z:.2f} > +{self.z_threshold} — mean-reversion SELL",
                bar=bar,
            )
        return None

    def reset(self):
        self._prices.clear()


# ─── Strategy 2: Momentum ─────────────────────────────────────────────────────

class MomentumStrategy(Strategy):
    """
    Buy when price is in a confirmed uptrend (above SMA with positive momentum).
    Sell when price is in a confirmed downtrend.
    Uses SMA crossover + ATR volatility filter.
    """

    def __init__(
        self,
        symbol:      str,
        fast:        int   = 20,
        slow:        int   = 50,
        atr_period:  int   = 14,
        min_atr_pct: float = 0.005,  # Min ATR/price ratio to trade (0.5%)
    ):
        super().__init__("Momentum", symbol)
        self.fast       = fast
        self.slow       = slow
        self.atr_period = atr_period
        self.min_atr_pct = min_atr_pct
        self._closes: deque[float] = deque(maxlen=slow + 1)
        self._highs:  deque[float] = deque(maxlen=atr_period + 1)
        self._lows:   deque[float] = deque(maxlen=atr_period + 1)

    def _atr(self) -> float:
        if len(self._closes) < 2:
            return 0.0
        tr_list = []
        closes = list(self._closes)
        highs  = list(self._highs)
        lows   = list(self._lows)
        for i in range(1, min(len(closes), self.atr_period)):
            tr = max(
                highs[-i] - lows[-i],
                abs(highs[-i] - closes[-(i+1)]),
                abs(lows[-i]  - closes[-(i+1)]),
            )
            tr_list.append(tr)
        return np.mean(tr_list) if tr_list else 0.0

    def on_bar(self, bar: Bar) -> Optional[Signal]:
        self._closes.append(bar.close)
        self._highs.append(bar.high)
        self._lows.append(bar.low)

        if len(self._closes) < self.slow:
            return None

        closes   = np.array(self._closes)
        sma_fast = closes[-self.fast:].mean()
        sma_slow = closes[-self.slow:].mean()
        atr      = self._atr()

        # Volatility filter: skip during very low volatility
        if bar.close > 0 and (atr / bar.close) < self.min_atr_pct:
            return None

        strength = abs(sma_fast - sma_slow) / (sma_slow + 1e-10)

        if sma_fast > sma_slow * 1.001:
            return Signal(
                side=Side.BUY,
                strength=min(strength * 20, 1.0),
                symbol=self.symbol,
                strategy=self.name,
                reason=f"SMA{self.fast}={sma_fast:.4f} > SMA{self.slow}={sma_slow:.4f} — uptrend",
                bar=bar,
            )
        elif sma_fast < sma_slow * 0.999:
            return Signal(
                side=Side.SELL,
                strength=min(strength * 20, 1.0),
                symbol=self.symbol,
                strategy=self.name,
                reason=f"SMA{self.fast}={sma_fast:.4f} < SMA{self.slow}={sma_slow:.4f} — downtrend",
                bar=bar,
            )
        return None

    def reset(self):
        self._closes.clear()
        self._highs.clear()
        self._lows.clear()


# ─── Strategy 3: AI-Enhanced ─────────────────────────────────────────────────

class AIEnhancedStrategy(Strategy):
    """
    Combines technical mean-reversion signal with AI regime + sentiment.
    Confluence required: both technical AND AI must agree on direction.
    This reduces false positives while preserving edge.

    AI pipeline runs every `ai_interval_bars` bars to avoid VRAM pressure.
    Between AI updates, technical signal alone governs entry/exit.
    """

    def __init__(
        self,
        symbol:           str,
        orchestrator,                  # HardwareConstrainedOrchestrator
        ai_interval_bars: int  = 12,  # Run AI every 12 bars (~3h on 15min bars)
        lookback:         int  = 20,
        z_threshold:      float = 1.8,
    ):
        super().__init__("AI-Enhanced", symbol)
        self._orchestrator = orchestrator
        self._technical    = MeanReversionStrategy(symbol, lookback, z_threshold)
        self._ai_interval  = ai_interval_bars
        self._bar_count    = 0
        self._last_regime  = "Range"
        self._last_sentiment = 0.0
        self._last_ai_action = "HOLD"
        self._pending_headlines: list[str] = []

    def add_headlines(self, headlines: list[str]):
        """Feed news headlines to the strategy for next AI update."""
        self._pending_headlines = headlines

    def on_bar(self, bar: Bar) -> Optional[Signal]:
        """Synchronous path — used in backtester."""
        self._bar_count += 1
        tech_signal = self._technical.on_bar(bar)

        # Between AI updates: use last known AI action as filter
        if tech_signal is None:
            return None

        # Confluence check: technical must agree with last AI signal
        if self._last_ai_action == "HOLD":
            return None  # AI says sit out

        if (tech_signal.side == Side.BUY  and self._last_ai_action == "BUY") or \
           (tech_signal.side == Side.SELL and self._last_ai_action == "SELL"):
            # Boost strength by AI confluence
            tech_signal.strength = min(tech_signal.strength * 1.3, 1.0)
            tech_signal.strategy = self.name
            tech_signal.reason   = (
                f"AI+Technical confluence: {self._last_ai_action} "
                f"(regime={self._last_regime}, sentiment={self._last_sentiment:+.2f}) | "
                f"{tech_signal.reason}"
            )
            return tech_signal

        return None  # Conflict — skip

    async def on_bar_async(self, bar: Bar) -> Optional[Signal]:
        """Async path — used in live trading. Triggers AI update at intervals."""
        self._bar_count += 1

        # Update AI every N bars
        if self._bar_count % self._ai_interval == 0:
            try:
                ai_result = await asyncio.wait_for(
                    self._orchestrator.execute_sequential_pipeline(
                        mock_market_summary=bar.to_summary(),
                        mock_headlines=self._pending_headlines or ["No news"],
                        current_price=bar.close,
                    ),
                    timeout=30.0,  # 30s timeout — sequential LLM pipeline
                )
                self._last_ai_action  = ai_result["action"]
                self._last_regime     = ai_result.get("regime", "Range")
                self._last_sentiment  = ai_result.get("sentiment", 0.0)
                logger.info(
                    "[AI] Updated: action=%s regime=%s sentiment=%+.2f",
                    self._last_ai_action, self._last_regime, self._last_sentiment
                )
            except asyncio.TimeoutError:
                logger.warning("[AI] Pipeline timeout — keeping last signal: %s", self._last_ai_action)

        return self.on_bar(bar)

    def reset(self):
        self._technical.reset()
        self._bar_count      = 0
        self._last_ai_action = "HOLD"


# ─── Strategy Registry ────────────────────────────────────────────────────────

STRATEGY_REGISTRY = {
    "mean_reversion": MeanReversionStrategy,
    "momentum":       MomentumStrategy,
    "ai_enhanced":    AIEnhancedStrategy,
}


def create_strategy(name: str, symbol: str, **kwargs) -> Strategy:
    cls = STRATEGY_REGISTRY.get(name)
    if cls is None:
        raise ValueError(f"Unknown strategy: {name}. Available: {list(STRATEGY_REGISTRY)}")
    return cls(symbol=symbol, **kwargs)


# ─── Standalone test ──────────────────────────────────────────────────────────

if __name__ == "__main__":
    import random
    logging.basicConfig(level=logging.INFO)

    strategy = MeanReversionStrategy("BTC-USD", lookback=20, z_threshold=2.0)

    print("Testing MeanReversionStrategy on 200 simulated bars ...")
    signals = []
    price = 65000.0

    for i in range(200):
        price *= (1 + random.gauss(0, 0.018))
        bar = Bar(
            timestamp_ns=int(time.time() * 1e9) + i * 60_000_000_000,
            symbol="BTC-USD",
            open=price * 0.999,
            high=price * 1.005,
            low=price  * 0.995,
            close=price,
            volume=random.uniform(1e6, 5e6),
        )
        signal = strategy.on_bar(bar)
        if signal:
            signals.append(signal)
            print(f"  Bar {i:3d}: {signal.side.value:5s} @ {price:,.2f} | "
                  f"strength={signal.strength:.2f} | {signal.reason}")

    print(f"\n✅ {len(signals)} signals generated from 200 bars.")
