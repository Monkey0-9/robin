"""Robin Signal Model — Python port of the C++ LinearSignalModel

Institutional-grade feature engineering:
  - Price momentum: MACD-style EMA12/EMA26 crossover on log prices (normalized)
  - Volume pressure: z-score of volume vs rolling average, bounded by tanh
  - Order book imbalance: depth-weighted (price-level decay)
  - Intraday time-of-day component
  - Features standardized online (running mean/std) to comparable scales
  - Confidence derived from |alpha| relative to recent realized volatility
    (non-circular: volatility measured on returns, not on alpha)

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


def _ema(values: np.ndarray, span: int) -> float:
    if len(values) == 0:
        return 0.0
    alpha = 2.0 / (span + 1.0)
    ema = float(values[0])
    for v in values[1:]:
        ema = alpha * float(v) + (1.0 - alpha) * ema
    return ema


class RobinSignalModel:
    def __init__(
        self,
        lookback: int = 64,
        vol_window: int = 20,
        adaptive_lookback: bool = True,
        vol_quantile: float = 0.75,
        max_lookback: int = 100,
        min_lookback: int = 20,
    ):
        """
        Args:
            lookback: Base rolling price window.
            adaptive_lookback: If True, shrink the effective window in high-vol
                regimes and extend it in low-vol regimes. Markets are
                non-stationary; a fixed window is wrong in both directions.
            vol_quantile: Rolling vol above this quantile of the recent vol
                history triggers the short (high-vol) window.
            max_lookback / min_lookback: Bounds for the adaptive window.
        """
        self.lookback = lookback
        self.vol_window = vol_window
        self.adaptive_lookback = adaptive_lookback
        self.vol_quantile = vol_quantile
        self.max_lookback = max_lookback
        self.min_lookback = min_lookback
        self.price_momentum_w = 0.40
        self.ob_imbalance_w = 0.30
        self.volume_pressure_w = 0.20
        self.intraday_w = 0.10

        # Online feature standardization (running mean/std)
        self._n_obs = 0
        self._feature_means = np.zeros(4, dtype=np.float64)
        self._feature_m2 = np.zeros(4, dtype=np.float64)

        # Running realized volatility (for confidence + lookback adaptation)
        self._realized_vol = 0.0

    def _standardize(self, features: np.ndarray) -> np.ndarray:
        """Update running statistics and z-score the feature vector."""
        n = self._n_obs
        f = np.asarray(features, dtype=np.float64)
        if n == 0:
            self._feature_means = f.copy()
            self._feature_m2 = np.zeros_like(f)
            self._n_obs = 1
            return np.zeros_like(f)

        delta = f - self._feature_means
        self._feature_means += delta / (n + 1)
        delta2 = f - self._feature_means
        self._feature_m2 += delta * delta2
        self._n_obs += 1

        if self._n_obs < 2:
            return np.zeros_like(f)
        var = self._feature_m2 / (self._n_obs - 1)
        std = np.sqrt(np.maximum(var, 1e-10))
        return (f - self._feature_means) / std

    def compute(self, inp: ModelInput) -> ModelOutput:
        P = min(len(inp.price_features), self.lookback)
        V = min(len(inp.volume_features), self.lookback)

        if self.adaptive_lookback and P >= 10:
            # Regime-adaptive window: high recent vol -> shorter lookback
            # (react faster), low vol -> longer lookback (smooth out noise).
            log_all = np.log(np.maximum(np.asarray(inp.price_features, dtype=np.float64), 1e-9))
            rets = np.diff(log_all)
            short_vol = float(np.std(rets[-self.vol_window:]))
            hist_vol = float(np.std(rets))
            if hist_vol > 1e-9:
                vol_ratio = short_vol / hist_vol
                if vol_ratio > self.vol_quantile:
                    P = max(self.min_lookback, int(P * 0.5))  # high vol: shorter
                else:
                    P = min(self.max_lookback, int(P * 1.5))  # low vol: longer
                V = min(P, len(inp.volume_features))
            P = min(P, len(inp.price_features))

        prices = np.asarray(inp.price_features[:P], dtype=np.float64)
        volumes = np.asarray(inp.volume_features[:V], dtype=np.float64)

        # --- Price momentum: log-return based, MACD-style ---
        if P >= 2:
            log_prices = np.log(np.maximum(prices, 1e-9))
            ema12 = _ema(log_prices, 12)
            ema26 = _ema(log_prices, min(26, P))
            momentum = (ema12 - ema26) / max(abs(ema26), 1e-9)
        else:
            momentum = 0.0

        # --- Volume pressure: z-score vs rolling average ---
        w = min(self.vol_window, V)
        if w >= 2:
            recent = volumes[-w:]
            vol_ma = float(np.mean(recent))
            vol_std = float(np.std(recent))
            current = float(volumes[-1])
            volume_z = (current - vol_ma) / max(vol_std, 1e-10)
            volume_pressure = float(np.tanh(volume_z))
        else:
            volume_pressure = 0.0

        # --- Order book imbalance: depth-weighted ---
        OB = min(len(inp.order_book_features), 32)
        bid_w, ask_w = 0.0, 0.0
        levels = OB // 2
        for i in range(levels):
            weight = 1.0 / (1.0 + i)
            bid_w += float(inp.order_book_features[i * 2]) * weight
            ask_w += float(inp.order_book_features[i * 2 + 1]) * weight
        ob_imbalance = (
            (bid_w - ask_w) / (bid_w + ask_w)
            if bid_w + ask_w > 1e-6
            else 0.0
        )

        # --- Intraday time-of-day ---
        intraday = float(inp.timestamp_features[0]) * 2.0 - 1.0

        # --- Standardize all features (zero mean, unit variance) ---
        raw_features = np.array([momentum, volume_pressure, ob_imbalance, intraday])
        features = self._standardize(raw_features)

        alpha = (
            self.price_momentum_w * features[0]
            + self.ob_imbalance_w * features[2]
            + self.volume_pressure_w * features[1]
            + self.intraday_w * features[3]
        )
        alpha = float(max(-1.0, min(1.0, alpha)))

        # --- Volatility estimate: log-return realized vol (non-circular) ---
        if P >= 2:
            log_rets = np.diff(np.log(np.maximum(prices, 1e-9)))
            realized_vol = float(np.std(log_rets))
        else:
            realized_vol = 0.0

        # --- Spread estimate in bps (1/price proxy) ---
        price_mean = float(np.mean(prices))
        spread_bps = (1.0 / price_mean) * 10000.0 if price_mean > 0.0 else 0.0

        # --- Confidence: signal strength vs realized vol, not alpha circularity ---
        if realized_vol > 1e-9:
            confidence = float(min(1.0, abs(alpha) * 0.5 / (realized_vol + 1e-4)))
        else:
            confidence = abs(alpha)
        confidence = float(max(0.0, min(1.0, confidence)))

        return ModelOutput(
            alpha_signal=alpha,
            volatility_estimate=realized_vol,
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
