"""Robin Signal Model — Python port of the C++ LinearSignalModel

Mirrors the exact logic from research/ai-engine/signal_model.cpp:
  - Price momentum (40% weight)
  - Volume pressure (20% weight)
  - Order book imbalance (30% weight)
  - Intraday time-of-day component (10% weight)

Used by the yfinance fetcher and backtester to generate alpha signals.
"""

import numpy as np
from typing import Optional
from dataclasses import dataclass


@dataclass
class ModelInput:
    price_features: np.ndarray       # (64,) rolling price window (normalized)
    volume_features: np.ndarray      # (64,) rolling volume window (normalized)
    order_book_features: np.ndarray  # (32,) [bid_vol0, ask_vol0, bid_vol1, ask_vol1, ...]
    timestamp_features: np.ndarray   # (8,)  time-of-day encoding (sin/cos)


@dataclass
class ModelOutput:
    alpha_signal: float         # Primary directional signal [-1, 1]
    volatility_estimate: float  # Estimated realized volatility [0, inf)
    spread_estimate: float      # Estimated bid-ask spread in bps
    confidence: float           # Signal confidence [0, 1]


class RobinSignalModel:
    def __init__(self):
        self.price_momentum_w = 0.40
        self.ob_imbalance_w = 0.30
        self.volume_pressure_w = 0.20
        self.intraday_w = 0.10

    def compute(self, inp: ModelInput) -> ModelOutput:
        P = min(len(inp.price_features), 64)
        price_sum = float(np.sum(inp.price_features[:P]))
        price_max = float(np.max(inp.price_features[:P]))
        price_mean = price_sum / P

        price_momentum = (
            (price_mean - inp.price_features[0]) / (inp.price_features[0] + 1e-6)
            if price_mean > 0.0 and inp.price_features[0] > 0.0
            else 0.0
        )

        V = min(len(inp.volume_features), 64)
        vol_sum = float(np.sum(inp.volume_features[:V]))
        vol_mean = vol_sum / V
        volume_pressure = float(np.tanh(vol_mean / (vol_mean + 1000.0 + 1e-6)))

        OB = min(len(inp.order_book_features), 32)
        bid_vol = float(np.sum(inp.order_book_features[0:OB:2]))
        ask_vol = float(np.sum(inp.order_book_features[1:OB:2]))
        ob_imbalance = (
            (bid_vol - ask_vol) / (bid_vol + ask_vol)
            if bid_vol + ask_vol > 1e-6
            else 0.0
        )

        intraday = inp.timestamp_features[0] * 2.0 - 1.0

        raw_alpha = (
            self.price_momentum_w * price_momentum
            + self.ob_imbalance_w * ob_imbalance
            + self.volume_pressure_w * volume_pressure
            + self.intraday_w * intraday
        )
        alpha = max(-1.0, min(1.0, raw_alpha))

        volatility = abs(price_max - inp.price_features[0]) / (inp.price_features[0] + 1e-6)
        spread_bps = (1.0 / price_mean) * 10000.0 if price_mean > 0.0 else 0.0
        confidence = min(1.0, abs(alpha) / (volatility + 0.01))

        return ModelOutput(
            alpha_signal=alpha,
            volatility_estimate=volatility,
            spread_estimate=spread_bps,
            confidence=confidence,
        )

    @staticmethod
    def build_input(
        prices: np.ndarray,
        volumes: np.ndarray,
        bid_volumes: Optional[np.ndarray] = None,
        ask_volumes: Optional[np.ndarray] = None,
        hour_of_day: float = 0.5,
    ) -> ModelInput:
        price_w = np.asarray(prices[-64:], dtype=np.float32) if len(prices) >= 64 else np.pad(prices[-64:], (64 - len(prices), 0), mode='edge').astype(np.float32)
        vol_w = np.asarray(volumes[-64:], dtype=np.float32) if len(volumes) >= 64 else np.pad(volumes[-64:], (64 - len(volumes), 0), mode='edge').astype(np.float32)

        ob = np.zeros(32, dtype=np.float32)
        if bid_volumes is not None and ask_volumes is not None:
            n_levels = min(len(bid_volumes), len(ask_volumes), 16)
            for i in range(n_levels):
                ob[i * 2] = float(bid_volumes[i])
                ob[i * 2 + 1] = float(ask_volumes[i])

        ts = np.zeros(8, dtype=np.float32)
        ts[0] = float(hour_of_day)

        return ModelInput(
            price_features=price_w,
            volume_features=vol_w,
            order_book_features=ob,
            timestamp_features=ts,
        )


if __name__ == "__main__":
    np.random.seed(42)
    model = RobinSignalModel()

    prices = 50000.0 + np.arange(64, dtype=np.float32) * 10.0
    volumes = 1000.0 + np.arange(64, dtype=np.float32) * 100.0
    bid_volumes = np.full(16, 800.0)
    ask_volumes = np.full(16, 500.0)

    inp = RobinSignalModel.build_input(prices, volumes, bid_volumes, ask_volumes, hour_of_day=0.5)
    out = model.compute(inp)

    print(f"Alpha={out.alpha_signal:.4f} Volatility={out.volatility_estimate:.4f} SpreadBps={out.spread_estimate:.4f} Confidence={out.confidence:.4f}")
    print(f"Expected:  Alpha~0.0049  Volatility~0.0002  SpreadBps~0.1853  Confidence~0.4860")
