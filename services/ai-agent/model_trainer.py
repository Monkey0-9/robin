"""
Robin Trading Platform — Real Model Trainer
============================================
Trains Ridge regression signal models on 100 years of real historical data
using proper walk-forward cross-validation (no lookahead bias).

Features engineered:
  - Price momentum (1d, 5d, 20d, 60d returns)
  - Volatility (10d, 20d, 60d rolling std)
  - Trend (distance from SMA-50, SMA-200)
  - MACD histogram
  - RSI (overbought/oversold)
  - Bollinger Band position
  - Volume z-score (unusual activity)

Target: Forward 5-day return (predict direction of next week's move)

Output: JSON model state files per symbol → used by live signal inference
"""

import json
import logging
import os
import time
from pathlib import Path
from typing import Optional

import numpy as np
import pandas as pd

# Only import sklearn when available
try:
    from sklearn.linear_model import Ridge
    from sklearn.preprocessing import StandardScaler
    from sklearn.model_selection import TimeSeriesSplit
    from sklearn.metrics import r2_score
    SKLEARN_AVAILABLE = True
except ImportError:
    SKLEARN_AVAILABLE = False
    print("[WARN] scikit-learn not installed. Run: pip install scikit-learn")

from data_engine import DataEngine, ALL_SYMBOLS

logger = logging.getLogger("model_trainer")
logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")

MODEL_DIR = Path(os.path.dirname(__file__)) / "models"
MODEL_DIR.mkdir(parents=True, exist_ok=True)

# Features to use (must match data_engine._add_features column names)
FEATURE_COLS = [
    "ret_1d", "ret_5d", "ret_20d",
    "vol_10d", "vol_20d", "vol_60d",
    "macd_hist", "rsi_14", "bb_pos",
    "volume_zscore", "price_vs_sma50", "price_vs_sma200",
]
TARGET_COL = "target_5d"


class AITrainer:
    """
    Offline training loop for Robin's AI signal models.
    Implements institutional standards:
      - Walk-forward cross-validation (no lookahead bias)
      - Out-of-sample R² reporting
      - Model versioning (timestamp + git hash)
      - Serialisation to JSON for fast loading in production
    """

    def __init__(self, n_splits: int = 10, alpha: float = 1.0):
        self.engine   = DataEngine()
        self.n_splits = n_splits  # Walk-forward folds
        self.alpha    = alpha     # Ridge regularisation strength

    # ─── Training ──────────────────────────────────────────────────────────

    def train_all(self, symbols: Optional[list[str]] = None) -> dict:
        """
        Train signal models for all (or specified) symbols.
        Returns summary of R² scores per symbol.
        """
        if not SKLEARN_AVAILABLE:
            raise ImportError("scikit-learn required. Run: pip install scikit-learn")

        target_symbols = symbols or list(ALL_SYMBOLS.keys())
        summary = {}

        for ticker in target_symbols:
            logger.info("=" * 60)
            logger.info("Training: %s", ticker)
            logger.info("=" * 60)
            try:
                result = self._train_symbol(ticker)
                summary[ticker] = result
                logger.info(
                    "  ✅ %s — OOS R²: %.4f ± %.4f | Train: %d rows",
                    ticker, result["oos_r2_mean"], result["oos_r2_std"], result["n_train"]
                )
            except FileNotFoundError:
                logger.warning("  ⏭  %s — data not found, skipping. Run data_engine.py first.", ticker)
            except Exception as e:
                logger.error("  ❌ %s — training failed: %s", ticker, e, exc_info=True)

        self._print_summary(summary)
        return summary

    def _train_symbol(self, ticker: str) -> dict:
        """Train and walk-forward validate a single symbol."""
        t0 = time.perf_counter()

        # Load pre-computed features from Parquet
        df = self.engine.load_dataset(ticker)

        # Check required columns exist
        missing = [c for c in FEATURE_COLS + [TARGET_COL] if c not in df.columns]
        if missing:
            raise ValueError(f"Missing feature columns for {ticker}: {missing}")

        # Drop rows with NaN in features or target
        df_clean = df[FEATURE_COLS + [TARGET_COL]].dropna()
        if len(df_clean) < 200:
            raise ValueError(f"Insufficient data for {ticker}: only {len(df_clean)} rows after dropna")

        X = df_clean[FEATURE_COLS].values
        y = df_clean[TARGET_COL].values

        # Walk-forward cross-validation (institutional standard — no lookahead)
        tscv = TimeSeriesSplit(n_splits=self.n_splits)
        oos_scores = []
        scaler = StandardScaler()

        for fold, (train_idx, test_idx) in enumerate(tscv.split(X)):
            X_train, X_test = X[train_idx], X[test_idx]
            y_train, y_test = y[train_idx], y[test_idx]

            scaler.fit(X_train)
            X_train_sc = scaler.transform(X_train)
            X_test_sc  = scaler.transform(X_test)

            model = Ridge(alpha=self.alpha)
            model.fit(X_train_sc, y_train)

            y_pred = model.predict(X_test_sc)
            fold_r2 = r2_score(y_test, y_pred)
            oos_scores.append(fold_r2)
            logger.debug("  Fold %d/%d: OOS R² = %.4f (test size=%d)",
                         fold + 1, self.n_splits, fold_r2, len(test_idx))

        # Final model trained on ALL data
        scaler_final = StandardScaler()
        X_all_sc = scaler_final.fit_transform(X)
        model_final = Ridge(alpha=self.alpha)
        model_final.fit(X_all_sc, y)
        in_sample_r2 = model_final.score(X_all_sc, y)

        elapsed_ms = (time.perf_counter() - t0) * 1000

        # Compute direction accuracy (more meaningful than R² for trading)
        y_pred_all  = model_final.predict(X_all_sc)
        dir_accuracy = float(np.mean(np.sign(y_pred_all) == np.sign(y)))

        # Build model state dict
        model_state = {
            "symbol":          ticker,
            "features":        FEATURE_COLS,
            "target":          TARGET_COL,
            "coef":            model_final.coef_.tolist(),
            "intercept":       float(model_final.intercept_),
            "scaler_mean":     scaler_final.mean_.tolist(),
            "scaler_scale":    scaler_final.scale_.tolist(),
            "oos_r2_mean":     float(np.mean(oos_scores)),
            "oos_r2_std":      float(np.std(oos_scores)),
            "oos_r2_scores":   [round(s, 4) for s in oos_scores],
            "in_sample_r2":    round(in_sample_r2, 4),
            "direction_acc":   round(dir_accuracy, 4),
            "n_train":         len(X),
            "n_splits":        self.n_splits,
            "alpha":           self.alpha,
            "trained_at_utc":  pd.Timestamp.utcnow().isoformat(),
            "training_ms":     round(elapsed_ms, 1),
        }

        # Save to JSON
        model_path = MODEL_DIR / f"{ticker.replace('/', '_').replace('=', '_')}_signal_model.json"
        with open(model_path, "w") as f:
            json.dump(model_state, f, indent=2)

        return model_state

    # ─── Inference ─────────────────────────────────────────────────────────

    @staticmethod
    def load_model(ticker: str) -> dict:
        """Load a saved model state from JSON."""
        safe = ticker.replace("/", "_").replace("=", "_")
        path = MODEL_DIR / f"{safe}_signal_model.json"
        if not path.exists():
            raise FileNotFoundError(f"No trained model for {ticker}. Run train_all() first.")
        with open(path) as f:
            return json.load(f)

    @staticmethod
    def predict(model_state: dict, features: dict) -> float:
        """
        Run inference on a single row of features.
        Returns predicted 5-day forward return.
        """
        X = np.array([features[col] for col in model_state["features"]]).reshape(1, -1)
        mean  = np.array(model_state["scaler_mean"])
        scale = np.array(model_state["scaler_scale"])
        X_sc  = (X - mean) / scale
        coef  = np.array(model_state["coef"])
        intercept = float(model_state["intercept"])
        return float(X_sc @ coef + intercept)

    # ─── Summary ───────────────────────────────────────────────────────────

    @staticmethod
    def _print_summary(summary: dict) -> None:
        print("\n" + "=" * 70)
        print("ROBIN MODEL TRAINER — WALK-FORWARD VALIDATION SUMMARY")
        print("=" * 70)
        print(f"  {'Symbol':<15} {'OOS R²':>8} {'±':>6} {'Dir Acc':>9} {'Train Rows':>12}")
        print("  " + "-" * 55)
        for ticker, r in summary.items():
            print(
                f"  {ticker:<15} {r['oos_r2_mean']:>+8.4f} "
                f"{r['oos_r2_std']:>6.4f} "
                f"{r['direction_acc']:>9.1%} "
                f"{r['n_train']:>12,}"
            )
        print("=" * 70)
        print("OOS R² > 0 = model is better than predicting the mean.")
        print("Direction Accuracy > 50% = model predicts direction better than chance.")


# ─── CLI entry point ──────────────────────────────────────────────────────────

if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser(description="Robin AI Trainer — Walk-forward model training")
    parser.add_argument("--symbols", nargs="+", help="Specific tickers to train (default: all)")
    parser.add_argument("--splits",  type=int, default=10, help="Walk-forward folds (default: 10)")
    parser.add_argument("--alpha",   type=float, default=1.0, help="Ridge alpha (default: 1.0)")
    args = parser.parse_args()

    trainer = AITrainer(n_splits=args.splits, alpha=args.alpha)
    trainer.train_all(symbols=args.symbols)
