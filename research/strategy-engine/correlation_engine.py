"""Correlation Engine — Rolling Correlation Matrix from Tick Data

Uses Exponential Weighted Moving Average (EWMA) for real-time updates
to maintain a live correlation/covariance matrix across instruments.

Design
------
The engine maintains running EWMA estimates of:
  - Mean price for each instrument
  - Variance for each instrument
  - Covariance between each pair of instruments

From these, the correlation is computed on-demand as:
    ρ_ij = Cov_ij / (σ_i * σ_j)

The lookback window is controlled by the decay factor λ (lambda):
  - λ close to 1  → long memory (slow decay, smoother)
  - λ close to 0  → short memory (fast decay, more responsive)
"""

import logging
from typing import Optional

import numpy as np

logger = logging.getLogger(__name__)


class CorrelationEngine:
    """Maintains an EWMA-based rolling correlation matrix from streaming tick data."""

    def __init__(self, lambda_factor: float = 0.94, lookback: int = 100):
        """
        Args:
            lambda_factor: Decay factor (0 < λ < 1). Default 0.94 matches RiskMetrics.
            lookback: Approximate effective window length used for initialization sizing.
        """
        if not 0 < lambda_factor < 1:
            raise ValueError("lambda_factor must be between 0 and 1 exclusive")
        self.lambda_ = lambda_factor
        self.lookback = lookback

        self._instruments: list[str] = []
        self._means: dict[str, float] = {}
        self._vars: dict[str, float] = {}
        self._covs: dict[tuple[str, str], float] = {}

    @property
    def instruments(self) -> list[str]:
        return list(self._instruments)

    def _ensure_instrument(self, sym: str):
        if sym not in self._means:
            self._instruments.append(sym)
            self._means[sym] = 0.0
            self._vars[sym] = 0.0
            for other in self._instruments:
                self._covs[(sym, other)] = 0.0
                self._covs[(other, sym)] = 0.0

    def update(self, trade_data: dict[str, float]):
        """Update the correlation matrix with a new tick snapshot.

        Args:
            trade_data: Mapping of instrument symbol -> latest price.
        """
        for sym, price in trade_data.items():
            self._ensure_instrument(sym)

        for sym, price in trade_data.items():
            prev_mean = self._means.get(sym, 0.0)
            innovation = price - prev_mean
            self._means[sym] = prev_mean + (1 - self.lambda_) * innovation
            self._vars[sym] = self.lambda_ * self._vars.get(sym, 0.0) + (1 - self.lambda_) * innovation ** 2

            for other, other_price in trade_data.items():
                if sym == other:
                    continue
                other_innovation = other_price - self._means.get(other, 0.0)
                self._covs[(sym, other)] = (
                    self.lambda_ * self._covs.get((sym, other), 0.0)
                    + (1 - self.lambda_) * innovation * other_innovation
                )

    def get_correlation(self, instrument_a: str, instrument_b: str) -> float:
        """Return the EWMA correlation between two instruments."""
        if instrument_a == instrument_b:
            return 1.0
        key = (instrument_a, instrument_b)
        cov = self._covs.get(key, 0.0)
        std_a = np.sqrt(max(self._vars.get(instrument_a, 0.0), 0.0))
        std_b = np.sqrt(max(self._vars.get(instrument_b, 0.0), 0.0))
        denom = std_a * std_b
        if denom == 0.0:
            logger.debug("Zero std dev for %s / %s, returning 0.0", instrument_a, instrument_b)
            return 0.0
        return float(cov / denom)

    def get_covariance_matrix(self, instruments: Optional[list[str]] = None) -> np.ndarray:
        """Return the EWMA covariance matrix as a numpy array.

        Args:
            instruments: Subset of instruments. If None, uses all known instruments.

        Returns:
            N x N covariance matrix.
        """
        symbols = instruments or self._instruments
        n = len(symbols)
        mat = np.zeros((n, n), dtype=np.float64)
        for i, a in enumerate(symbols):
            for j, b in enumerate(symbols):
                mat[i, j] = self._covs.get((a, b), 0.0)
        return mat

    def get_correlation_matrix(self, instruments: Optional[list[str]] = None) -> np.ndarray:
        """Return the EWMA correlation matrix as a numpy array.

        Args:
            instruments: Subset of instruments. If None, uses all known instruments.

        Returns:
            N x N correlation matrix.
        """
        symbols = instruments or self._instruments
        n = len(symbols)
        mat = np.zeros((n, n), dtype=np.float64)
        for i, a in enumerate(symbols):
            for j, b in enumerate(symbols):
                mat[i, j] = self.get_correlation(a, b)
        return mat
