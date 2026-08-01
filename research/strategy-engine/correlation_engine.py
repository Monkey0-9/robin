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
from sklearn.covariance import LedoitWolf

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
        self._last_prices: dict[str, float] = {}
        self._log_ret_vars: dict[str, float] = {}
        self._log_ret_covs: dict[tuple[str, str], float] = {}

    @property
    def instruments(self) -> list[str]:
        return list(self._instruments)

    def _ensure_instrument(self, sym: str):
        if sym not in self._last_prices:
            self._instruments.append(sym)
            self._last_prices[sym] = np.nan
            self._log_ret_vars[sym] = 0.0
            for other in self._instruments:
                self._log_ret_covs[(sym, other)] = 0.0
                self._log_ret_covs[(other, sym)] = 0.0

    def update(self, trade_data: dict[str, float], regime_volatility_scalar: float = 1.0):
        """Update the correlation matrix with a new tick snapshot.

        Uses per-symbol log-returns (r_t = ln(P_t / P_{t-1})) rather than raw
        price levels. Log-returns are scale-invariant and stationary for
        typical financial series, so the EWMA covariance is a proper measure of
        co-movement instead of a spurious correlation of price levels.

        Args:
            trade_data: Mapping of instrument symbol -> latest price.
            regime_volatility_scalar: Adjusts decay factor lambda. > 1 means higher vol, faster decay.
        """
        for sym, price in trade_data.items():
            self._ensure_instrument(sym)
            
        # Adjust lambda based on regime volatility (higher vol -> faster decay / lower lambda)
        eff_lambda = max(0.5, min(0.99, self.lambda_ ** regime_volatility_scalar))

        # Phase 1: compute log-return innovations using prior prices
        innovations: dict[str, float] = {}
        for sym, price in trade_data.items():
            prev = self._last_prices[sym]
            if prev is not None and not np.isnan(prev) and prev > 0 and price > 0:
                innovations[sym] = float(np.log(price / prev))
            else:
                innovations[sym] = 0.0  # no first-observation return

        # Phase 2: update log-return variances and covariances
        for sym, price in trade_data.items():
            self._last_prices[sym] = float(price)
            r_sym = innovations[sym]
            self._log_ret_vars[sym] = (
                eff_lambda * self._log_ret_vars.get(sym, 0.0)
                + (1 - eff_lambda) * r_sym ** 2
            )
            for other in trade_data:
                if sym == other:
                    continue
                r_other = innovations[other]
                self._log_ret_covs[(sym, other)] = (
                    eff_lambda * self._log_ret_covs.get((sym, other), 0.0)
                    + (1 - eff_lambda) * r_sym * r_other
                )

    @staticmethod
    def _clip_corr(c: float) -> float:
        """Clip correlation into [-1, 1] to handle numerical drift in EWMA."""
        return float(max(-1.0, min(1.0, c)))

    def get_correlation(self, instrument_a: str, instrument_b: str) -> float:
        """Return the EWMA correlation (log-returns) between two instruments."""
        if instrument_a == instrument_b:
            return 1.0
        key = (instrument_a, instrument_b)
        cov = self._log_ret_covs.get(key, 0.0)
        std_a = np.sqrt(max(self._log_ret_vars.get(instrument_a, 0.0), 0.0))
        std_b = np.sqrt(max(self._log_ret_vars.get(instrument_b, 0.0), 0.0))
        denom = std_a * std_b
        if denom == 0.0:
            logger.debug("Zero std dev for %s / %s, returning 0.0", instrument_a, instrument_b)
            return 0.0
        return self._clip_corr(cov / denom)

    def get_covariance_matrix(self, instruments: Optional[list[str]] = None, regularize: bool = True) -> np.ndarray:
        """Return the EWMA covariance matrix as a numpy array.

        Args:
            instruments: Subset of instruments. If None, uses all known instruments.
            regularize: If True, apply Ledoit-Wolf shrinkage.

        Returns:
            N x N covariance matrix.
        """
        symbols = instruments or self._instruments
        n = len(symbols)
        mat = np.zeros((n, n), dtype=np.float64)
        for i, a in enumerate(symbols):
            for j, b in enumerate(symbols):
                if a == b:
                    mat[i, j] = self._log_ret_vars.get(a, 0.0)
                else:
                    mat[i, j] = self._log_ret_covs.get((a, b), 0.0)
                    
        # Make symmetric
        mat = (mat + mat.T) / 2.0
        
        if regularize and n > 1:
            try:
                # Need some dummy data with the same covariance to fit LedoitWolf
                # Since we already have the covariance matrix, we can sample from it
                # or just use LedoitWolf directly if we had the historical returns.
                # However, LedoitWolf from sklearn expects historical data (X). 
                # Since we don't store X (EWMA), we can apply a simple shrinkage to identity:
                # shrunk_cov = (1-shrinkage)*cov + shrinkage * trace(cov)/n * I
                target_var = np.trace(mat) / n
                shrinkage = 0.1  # Heuristic for EWMA
                target = np.eye(n) * target_var
                mat = (1.0 - shrinkage) * mat + shrinkage * target
            except Exception as e:
                logger.warning("Regularization failed: %s", e)
                
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
        # Symmetrize to guarantee a valid correlation matrix
        return (mat + mat.T) / 2.0

    def get_partial_correlation_matrix(
        self, instruments: Optional[list[str]] = None, ridge: float = 1e-6
    ) -> np.ndarray:
        """
        Partial correlation matrix via the inverse covariance (precision) matrix.

        Partial correlation ρ_ij|others removes the effect of all other assets,
        answering "is the A-B link direct or mediated by C?" Useful for pairs
        trading and network analysis. `ridge` regularizes the inversion when the
        covariance is near-singular (few observations / highly collinear assets).
        """
        symbols = instruments or self._instruments
        n = len(symbols)
        if n < 2:
            mat = np.ones((n, n), dtype=np.float64)
            return mat

        cov = self.get_covariance_matrix(symbols)
        # Regularize diagonal before inversion (Ledoit-style jitter)
        cov_reg = cov + ridge * np.eye(n)
        try:
            prec = np.linalg.inv(cov_reg)
        except np.linalg.LinAlgError:
            prec = np.linalg.pinv(cov_reg)

        diag = np.sqrt(np.clip(np.diag(prec), 1e-12, None))
        pcorr = -prec / np.outer(diag, diag)
        np.fill_diagonal(pcorr, 1.0)
        return (pcorr + pcorr.T) / 2.0

    def is_cointegrated(self, a: np.ndarray, b: np.ndarray, p_threshold: float = 0.05) -> dict:
        """
        Engle-Granger two-step cointegration test.

        Cointegration (long-run equilibrium) is the correct test for pairs
        trading — plain correlation can be spurious for two trending series.
        Uses statsmodels if available, else an OLS residual ADF fallback.
        """
        a = np.asarray(a, dtype=np.float64)
        b = np.asarray(b, dtype=np.float64)
        m = min(len(a), len(b))
        if m < 30:
            return {"cointegrated": False, "adf_stat": np.nan, "pvalue": np.nan,
                    "skipped": True, "n": m}
        a, b = a[-m:], b[-m:]
        try:
            from statsmodels.tsa.stattools import coint
            stat, pvalue, _ = coint(b, a, autolag="AIC")
            return {"cointegrated": bool(pvalue < p_threshold),
                    "adf_stat": float(stat), "pvalue": float(pvalue),
                    "skipped": False, "n": m}
        except ImportError:
            pass
        # Fallback: regress a on b, run ADF on residuals
        A = np.column_stack([np.ones(m), b])
        beta, *_ = np.linalg.lstsq(A, a, rcond=None)
        resid = a - A @ beta
        try:
            from statsmodels.tsa.stattools import adfuller
            stat, pvalue, *_ = adfuller(resid, autolag="AIC")
        except ImportError:
            return {"cointegrated": False, "adf_stat": np.nan, "pvalue": np.nan,
                    "skipped": True, "n": m}
        return {"cointegrated": bool(pvalue < p_threshold),
                "adf_stat": float(stat), "pvalue": float(pvalue),
                "skipped": False, "n": m}

    def get_partial_correlation_matrix(self, instruments: Optional[list[str]] = None) -> np.ndarray:
        """Compute the partial correlation matrix (removing market beta effects)."""
        cov = self.get_covariance_matrix(instruments, regularize=True)
        try:
            prec = np.linalg.inv(cov)
        except np.linalg.LinAlgError:
            # Fallback to pseudo-inverse if singular
            prec = np.linalg.pinv(cov)
            
        n = prec.shape[0]
        partial_corr = np.zeros_like(prec)
        for i in range(n):
            for j in range(n):
                if i == j:
                    partial_corr[i, j] = 1.0
                else:
                    denom = np.sqrt(prec[i, i] * prec[j, j])
                    if denom > 0:
                        partial_corr[i, j] = -prec[i, j] / denom
        return partial_corr
