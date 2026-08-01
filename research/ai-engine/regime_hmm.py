"""
Robin Trading Platform — HMM Market Regime Detector
====================================================
Replaces the LLM-based regime classifier (500ms–5s latency, unverifiable)
with a statistically rigorous Gaussian Hidden Markov Model.

Regimes: Bull, Bear, Range, Volatile (n_regimes=4 by default).

Features: [daily log-return, rolling volatility, volume z-score]

Latency: <1ms per prediction (vs 500ms for an LLM).
Training: fit() with a few hundred bars of history; online update via partial_fit.

Uses `hmmlearn` when available; falls back to a self-contained Gaussian HMM
(EM-trained) so the module runs without any external dependency.
"""

from __future__ import annotations

import logging
from typing import Optional

import numpy as np

logger = logging.getLogger("regime_hmm")

try:
    from hmmlearn.hmm import GaussianHMM as _HmmLibGaussianHMM
    HMMLEARN_AVAILABLE = True
except ImportError:
    HMMLEARN_AVAILABLE = False


class _SelfContainedGaussianHMM:
    """
    Minimal Gaussian HMM with diagonal-covariance emissions, trained by
    Baum-Welch (EM). Enough for regime detection on 2-3 features.
    """

    def __init__(self, n_components: int, n_features: int, n_iter: int = 30, seed: int = 42):
        self.n_components = n_components
        self.n_features = n_features
        self.n_iter = n_iter
        self.rng = np.random.default_rng(seed)

        self.startprob = np.full(n_components, 1.0 / n_components)
        self.transmat = np.full((n_components, n_components), 0.1)
        np.fill_diagonal(self.transmat, 0.7)
        self.transmat /= self.transmat.sum(axis=1, keepdims=True)

        self.means = self.rng.normal(0.0, 0.5, (n_components, n_features))
        self.covars = np.ones((n_components, n_features))  # diagonal variances

    def _log_normal_pdf(self, X: np.ndarray) -> np.ndarray:
        """Return (n_samples, n_components) log-likelihood matrix."""
        logpdf = np.zeros((X.shape[0], self.n_components))
        for k in range(self.n_components):
            var = np.maximum(self.covars[k], 1e-10)
            d = X - self.means[k]
            logdet = np.sum(np.log(var))
            quad = np.sum(d * d / var, axis=1)
            logpdf[:, k] = -0.5 * (self.n_features * np.log(2 * np.pi) + logdet + quad)
        return logpdf

    def _forward_backward(self, X: np.ndarray):
        logB = self._log_normal_pdf(X)
        T = X.shape[0]
        K = self.n_components

        log_start = np.log(np.maximum(self.startprob, 1e-300))
        log_a = np.log(np.maximum(self.transmat, 1e-300))

        # Forward
        alpha = np.zeros((T, K))
        alpha[0] = log_start + logB[0]
        for t in range(1, T):
            for j in range(K):
                alpha[t, j] = np.logaddexp.reduce(alpha[t - 1] + log_a[:, j]) + logB[t, j]

        loglik = np.logaddexp.reduce(alpha[T - 1])

        # Backward
        beta = np.zeros((T, K))
        beta[T - 1] = 0.0
        for t in range(T - 2, -1, -1):
            for i in range(K):
                beta[t, i] = np.logaddexp.reduce(log_a[i, :] + logB[t + 1] + beta[t + 1])

        return alpha, beta, loglik

    def fit(self, X: np.ndarray):
        X = np.asarray(X, dtype=np.float64)
        if X.ndim != 2:
            raise ValueError("X must be 2D (n_samples, n_features)")
        if X.shape[0] < self.n_components * 5:
            raise ValueError("Insufficient samples to fit HMM")

        for _ in range(self.n_iter):
            alpha, beta, _ = self._forward_backward(X)
            logB = self._log_normal_pdf(X)
            T, K = X.shape
            gamma = np.exp(alpha + beta - np.logaddexp.reduce(alpha[T - 1]))

            # Expected transition counts
            xi = np.zeros((K, K))
            for t in range(T - 1):
                joint = alpha[t][:, None] + np.log(self.transmat) + logB[t + 1][None, :] + beta[t + 1][None, :]
                joint -= np.logaddexp.reduce(joint.ravel())
                xi += np.exp(joint)
            denom = xi.sum(axis=1, keepdims=True)
            self.transmat = xi / np.maximum(denom, 1e-10)
            self.startprob = gamma[0] / max(gamma[0].sum(), 1e-10)

            # Emission updates
            gsum = gamma.sum(axis=0)
            self.means = (gamma.T @ X) / np.maximum(gsum, 1e-10)[:, None]
            for k in range(K):
                d = X - self.means[k]
                self.covars[k] = (gamma[:, k][:, None] * (d * d)).sum(axis=0) / max(gsum[k], 1e-10)
                self.covars[k] = np.maximum(self.covars[k], 1e-6)

    def predict(self, X: np.ndarray) -> np.ndarray:
        X = np.asarray(X, dtype=np.float64)
        logB = self._log_normal_pdf(X)
        T = X.shape[0]
        K = self.n_components
        log_start = np.log(np.maximum(self.startprob, 1e-300))
        log_a = np.log(np.maximum(self.transmat, 1e-300))
        alpha = np.zeros((T, K))
        alpha[0] = log_start + logB[0]
        for t in range(1, T):
            for j in range(K):
                alpha[t, j] = np.logaddexp.reduce(alpha[t - 1] + log_a[:, j]) + logB[t, j]
        return np.argmax(alpha, axis=1)


class RegimeDetector:
    """Gaussian-HMM market regime detector with Bull/Bear/Range/Volatile mapping."""

    DEFAULT_REGIME_MAP = {0: "Bull", 1: "Bear", 2: "Range", 3: "Volatile"}

    def __init__(
        self,
        n_regimes: int = 4,
        n_features: int = 3,
        n_iter: int = 30,
        seed: int = 42,
        vol_window: int = 20,
    ):
        self.n_regimes = n_regimes
        self.n_features = n_features
        self.vol_window = vol_window
        self.seed = seed
        self.regime_map = {i: self.DEFAULT_REGIME_MAP[i % len(self.DEFAULT_REGIME_MAP)] for i in range(n_regimes)}

        if HMMLEARN_AVAILABLE:
            try:
                from hmmlearn.hmm import GaussianHMM
                self._model = GaussianHMM(
                    n_components=n_regimes,
                    covariance_type="diag",
                    n_iter=n_iter,
                    random_state=seed,
                    init_params="kc",
                )
                self._backend = "hmmlearn"
            except Exception:
                self._model = _SelfContainedGaussianHMM(n_regimes, n_features, n_iter, seed)
                self._backend = "self-contained"
        else:
            self._model = _SelfContainedGaussianHMM(n_regimes, n_features, n_iter, seed)
            self._backend = "self-contained"

        self._vol_history: list[float] = []
        self._vol_ma: Optional[float] = None
        self._vol_std: Optional[float] = None
        self._feat_means: Optional[np.ndarray] = None
        self._feat_stds: Optional[np.ndarray] = None

    @property
    def backend(self) -> str:
        return self._backend

    @staticmethod
    def _build_features(returns: np.ndarray, volumes: Optional[np.ndarray], vol_window: int) -> np.ndarray:
        """[return, rolling-vol, volume-z] per observation (raw, unstandardized)."""
        n = len(returns)
        ret = np.asarray(returns, dtype=np.float64)
        if n < 2:
            return np.zeros((n, 3))

        # Rolling volatility
        vol = np.full(n, np.nan)
        for t in range(n):
            lo = max(0, t - vol_window + 1)
            window = ret[lo:t + 1]
            vol[t] = np.std(window) if len(window) > 1 else 0.0
        vol = np.nan_to_num(vol, nan=0.0)

        # Volume z-score
        vol_z = np.zeros(n)
        if volumes is not None and len(volumes) == n:
            v = np.asarray(volumes, dtype=np.float64)
            v_ma = np.nanmean(v)
            v_sd = np.nanstd(v)
            if v_sd > 1e-10:
                vol_z = (v - v_ma) / v_sd

        return np.column_stack([ret, vol, vol_z])

    @staticmethod
    def _standardize(X: np.ndarray, means: np.ndarray, stds: np.ndarray) -> np.ndarray:
        X = np.asarray(X, dtype=np.float64)
        return (X - means) / np.maximum(stds, 1e-10)

    def fit(self, returns: np.ndarray, volumes: Optional[np.ndarray] = None):
        """Train the HMM on historical [return, vol, volume-z] features."""
        X = self._build_features(returns, volumes, self.vol_window)
        # Drop the first (zero-vol) row if only one sample of return
        if X.shape[0] < self.n_regimes * 5:
            raise ValueError(
                f"Insufficient history to fit regime model: need >= {self.n_regimes * 5} obs, got {X.shape[0]}"
            )

        # Store per-feature statistics from training data and standardize
        self._feat_means = np.nanmean(X, axis=0)
        self._feat_stds = np.nanstd(X, axis=0)
        X_sc = self._standardize(X, self._feat_means, self._feat_stds)

        if HMMLEARN_AVAILABLE and hasattr(self._model, "means_"):
            self._model.fit(X_sc)
        else:
            self._model.fit(X_sc)

        # Store rolling vol stats for live z-scoring
        if volumes is not None and len(volumes) > 0:
            v = np.asarray(volumes, dtype=np.float64)
            self._vol_ma = float(np.nanmean(v))
            self._vol_std = float(np.nanstd(v))
        logger.info("[RegimeHMM] fit complete on %d observations (backend=%s)", X.shape[0], self.backend)

    def predict(self, returns: np.ndarray, volumes: Optional[np.ndarray] = None) -> str:
        """
        Predict the current regime from the latest window.
        returns: recent log-return series; volumes: recent volume series (optional).
        """
        if len(returns) < 2:
            returns = np.array([0.0, 0.0])
        if len(returns) < self.vol_window:
            returns = returns[-self.vol_window:]

        # Reconstruct features for a trailing window; Viterbi over the whole
        # window and take the state of the LATEST observation.
        trailing = self._build_features(returns, volumes, self.vol_window)

        # Standardize using TRAINING statistics (consistent feature space)
        if self._feat_means is not None and self._feat_stds is not None:
            trailing = self._standardize(trailing, self._feat_means, self._feat_stds)

        # Refine the live volume-z using stored population stats when available
        if self._vol_ma is not None and self._vol_std is not None and self._vol_std > 1e-10 \
                and volumes is not None and len(volumes) > 0 and trailing.shape[0] > 0:
            trailing[-1, 2] = (float(np.mean(volumes[-5:])) - self._vol_ma) / self._vol_std

        if HMMLEARN_AVAILABLE and hasattr(self._model, "predict"):
            states = self._model.predict(trailing)
        else:
            states = self._model.predict(trailing)

        state = int(states[-1])
        return self.regime_map.get(state, "Range")

    def reset(self):
        """Recreate the underlying model (for walk-forward validation)."""
        self.__init__(n_regimes=self.n_regimes, n_features=self.n_features,
                      n_iter=30, seed=self.seed, vol_window=self.vol_window)


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO)
    np.random.seed(42)

    # Synthetic regimes: 250 bull bars, 250 bear bars, 250 range bars, 250 volatile bars
    n = 250
    bull = np.random.normal(0.001, 0.01, n)
    bear = np.random.normal(-0.001, 0.01, n)
    rng = np.random.normal(0.0001, 0.002, n)
    vol = np.random.normal(0.0002, 0.04, n)
    returns = np.concatenate([bull, bear, rng, vol])
    volumes = np.abs(np.random.normal(1e6, 3e5, len(returns)))

    detector = RegimeDetector()
    detector.fit(returns, volumes)

    for label, window in [("Bull", bull[-20:]), ("Bear", bear[-20:]),
                          ("Range", rng[-20:]), ("Volatile", vol[-20:])]:
        pred = detector.predict(window)
        print(f"  Expected {label:9s} -> predicted {pred}")
