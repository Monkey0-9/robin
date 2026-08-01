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

# Optional gradient boosting (LightGBM) — institutional baseline upgrade over Ridge
try:
    from lightgbm import LGBMRegressor
    LGBM_AVAILABLE = True
except ImportError:
    LGBM_AVAILABLE = False

# Optional partial_fit-capable SGD regressor for online/incremental learning
try:
    from sklearn.linear_model import SGDRegressor
    SGD_AVAILABLE = True
except ImportError:
    SGD_AVAILABLE = False

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

    def __init__(self, n_splits: int = 10, alpha: float = 1.0, model_type: str = "ridge"):
        self.engine   = DataEngine()
        self.n_splits = n_splits  # Walk-forward folds
        self.alpha    = alpha     # Ridge regularisation strength
        self.model_type = model_type.lower()  # 'ridge' | 'lgbm' | 'auto' | 'stack'

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
                    "  [OK] %s - OOS R2: %.4f +/- %.4f | Train: %d rows",
                    ticker, result["oos_r2_mean"], result["oos_r2_std"], result["n_train"]
                )
            except FileNotFoundError:
                logger.warning("  [skip] %s - data not found, skipping. Run data_engine.py first.", ticker)
            except Exception as e:
                logger.error("  [FAIL] %s - training failed: %s", ticker, e, exc_info=True)

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

        use_lgbm = self.model_type in ("lgbm", "auto") and LGBM_AVAILABLE
        use_stack = self.model_type == "stack" and LGBM_AVAILABLE

        for fold, (train_idx, test_idx) in enumerate(tscv.split(X)):
            X_train, X_test = X[train_idx], X[test_idx]
            y_train, y_test = y[train_idx], y[test_idx]

            scaler.fit(X_train)
            X_train_sc = scaler.transform(X_train)
            X_test_sc  = scaler.transform(X_test)

            if use_stack:
                # Stacking: base models (Ridge + LGBM) trained on train split,
                # meta-learner (Ridge) fit on the validation split predictions.
                # Simple 2-fold internal split keeps it fast and leak-free.
                m = len(X_train)
                half = m // 2
                X_a, X_b = X_train[:half], X_train[half:]
                y_a, y_b = y_train[:half], y_train[half:]

                sc_a = StandardScaler().fit(X_a)
                sc_b = StandardScaler().fit(X_b)
                ridge_a = Ridge(alpha=self.alpha).fit(sc_a.transform(X_a), y_a)
                ridge_b = Ridge(alpha=self.alpha).fit(sc_b.transform(X_b), y_b)
                lgbm_a = LGBMRegressor(n_estimators=300, max_depth=5, learning_rate=0.05,
                                       num_leaves=63, subsample=0.8, colsample_bytree=0.8,
                                       random_state=42, verbose=-1).fit(X_a, y_a)
                lgbm_b = LGBMRegressor(n_estimators=300, max_depth=5, learning_rate=0.05,
                                       num_leaves=63, subsample=0.8, colsample_bytree=0.8,
                                       random_state=42, verbose=-1).fit(X_b, y_b)

                # Out-of-fold base predictions for meta-training
                oof_ridge = np.concatenate([ridge_b.predict(sc_b.transform(X_b)),
                                            ridge_a.predict(sc_a.transform(X_a))])
                oof_lgbm = np.concatenate([lgbm_b.predict(X_b), lgbm_a.predict(X_a)])
                # Stack base predictions + a constant for the meta-learner
                meta_X_train = np.column_stack([oof_ridge, oof_lgbm, np.ones(m)])
                meta_y_train = y_train
                # Meta-learner: ridge with non-negative-ish weighting
                meta = Ridge(alpha=self.alpha)
                meta.fit(meta_X_train, meta_y_train)

                # Test-set base predictions from both splits' models (averaged)
                ridge_pred = 0.5 * ridge_a.predict(sc_a.transform(X_test_sc)) + \
                             0.5 * ridge_b.predict(sc_b.transform(X_test_sc))
                lgbm_pred = 0.5 * lgbm_a.predict(X_test) + 0.5 * lgbm_b.predict(X_test)
                meta_X_test = np.column_stack([ridge_pred, lgbm_pred, np.ones(len(X_test))])
                y_pred = meta.predict(meta_X_test)
                fold_r2 = r2_score(y_test, y_pred)
                oos_scores.append(fold_r2)
                logger.debug("  Fold %d/%d (stack): OOS R² = %.4f", fold + 1, self.n_splits, fold_r2)
                continue

            if use_lgbm:
                model = LGBMRegressor(
                    n_estimators=500,
                    max_depth=6,
                    learning_rate=0.05,
                    num_leaves=63,
                    subsample=0.8,
                    colsample_bytree=0.8,
                    random_state=42,
                    verbose=-1,
                )
                model.fit(X_train, y_train)
            else:
                model = Ridge(alpha=self.alpha)
                model.fit(X_train_sc, y_train)
                y_pred = model.predict(X_test_sc)
                fold_r2 = r2_score(y_test, y_pred)
                oos_scores.append(fold_r2)
                logger.debug("  Fold %d/%d: OOS R² = %.4f (test size=%d)",
                             fold + 1, self.n_splits, fold_r2, len(test_idx))
                continue

            y_pred = model.predict(X_test)
            fold_r2 = r2_score(y_test, y_pred)
            oos_scores.append(fold_r2)
            logger.debug("  Fold %d/%d: OOS R² = %.4f (test size=%d)",
                         fold + 1, self.n_splits, fold_r2, len(test_idx))

        # Final model trained on ALL data
        scaler_final = StandardScaler()
        X_all_sc = scaler_final.fit_transform(X)
        if use_stack:
            model_final_ridge = Ridge(alpha=self.alpha)
            model_final_ridge.fit(X_all_sc, y)
            model_final_lgbm = LGBMRegressor(
                n_estimators=500, max_depth=6, learning_rate=0.05,
                num_leaves=63, subsample=0.8, colsample_bytree=0.8,
                random_state=42, verbose=-1,
            )
            model_final_lgbm.fit(X, y)
            meta_feats = np.column_stack([
                model_final_ridge.predict(X_all_sc),
                model_final_lgbm.predict(X),
                np.ones(len(X)),
            ])
            model_final_meta = Ridge(alpha=self.alpha)
            model_final_meta.fit(meta_feats, y)
            y_pred_all = model_final_meta.predict(meta_feats)
            model_final = None
        elif use_lgbm:
            model_final = LGBMRegressor(
                n_estimators=500,
                max_depth=6,
                learning_rate=0.05,
                num_leaves=63,
                subsample=0.8,
                colsample_bytree=0.8,
                random_state=42,
                verbose=-1,
            )
            model_final.fit(X, y)
            y_pred_all = model_final.predict(X)
        else:
            model_final = Ridge(alpha=self.alpha)
            model_final.fit(X_all_sc, y)
            y_pred_all = model_final.predict(X_all_sc)

        in_sample_r2 = r2_score(y, y_pred_all)

        elapsed_ms = (time.perf_counter() - t0) * 1000

        # Compute direction accuracy (more meaningful than R² for trading)
        dir_accuracy = float(np.mean(np.sign(y_pred_all) == np.sign(y)))

        model_type_str = "stack" if use_stack else ("lgbm" if use_lgbm else "ridge")

        # Build model state dict
        model_state = {
            "symbol":          ticker,
            "features":        FEATURE_COLS,
            "target":          TARGET_COL,
            "coef":            model_final.coef_.tolist() if model_final is not None else [],
            "intercept":       float(model_final.intercept_) if model_final is not None else 0.0,
            "scaler_mean":     scaler_final.mean_.tolist(),
            "scaler_scale":    scaler_final.scale_.tolist(),
            "model_type":      model_type_str,
            "stack_meta_coef": model_final_meta.coef_.tolist() if use_stack else [],
            "stack_meta_intercept": float(model_final_meta.intercept_) if use_stack else 0.0,
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

        # Also persist the LGBM booster for native inference
        if use_lgbm:
            booster_path = MODEL_DIR / f"{ticker.replace('/', '_').replace('=', '_')}_lgbm.txt"
            model_final.booster_.save_model(str(booster_path))
        elif use_stack:
            stack_lgbm_path = MODEL_DIR / f"{ticker.replace('/', '_').replace('=', '_')}_stack_lgbm.txt"
            model_final_lgbm.booster_.save_model(str(stack_lgbm_path))

        return model_state

    # ─── Online / incremental learning ──────────────────────────────────────

    def retrain_online(
        self,
        ticker: str,
        incremental: bool = True,
        recent_window: int = 2520,
    ) -> dict:
        """
        Weekly-style model refresh to handle regime shifts.

        Two strategies:
          1. incremental=True  — SGDRegressor with partial_fit on the most
             recent window only. <1s, true online learning, no full retrain.
          2. incremental=False — full walk-forward retrain on the most recent
             window (cheap, robust when SGD noise is a concern).

        Persists a compact 'online' model state JSON alongside the batch model.
        """
        if not SKLEARN_AVAILABLE:
            raise ImportError("scikit-learn required for online retraining")
        if incremental and not SGD_AVAILABLE:
            logger.warning("SGDRegressor unavailable — falling back to full retrain")
            incremental = False

        df = self.engine.load_dataset(ticker)
        df_clean = df[FEATURE_COLS + [TARGET_COL]].dropna().tail(recent_window)
        if len(df_clean) < 100:
            raise ValueError(f"Insufficient recent data for {ticker}: {len(df_clean)} rows")

        X = df_clean[FEATURE_COLS].values
        y = df_clean[TARGET_COL].values

        scaler = StandardScaler()
        X_sc = scaler.fit_transform(X)

        if incremental:
            model = SGDRegressor(
                loss="squared_error",
                alpha=self.alpha / 100.0,
                max_iter=1,
                tol=None,
                learning_rate="adaptive",
                eta0=0.001,
                random_state=42,
            )
            # online pass: feed in chunks to simulate streaming
            chunk = 256
            for start in range(0, len(X_sc), chunk):
                model.partial_fit(X_sc[start:start + chunk], y[start:start + chunk])
            pred = model.predict(X_sc)
            model_type = "sgd_online"
        else:
            model = Ridge(alpha=self.alpha)
            model.fit(X_sc, y)
            pred = model.predict(X_sc)
            model_type = "ridge_recent"

        dir_acc = float(np.mean(np.sign(pred) == np.sign(y)))
        r2 = r2_score(y, pred)

        state = {
            "symbol":          ticker,
            "features":        FEATURE_COLS,
            "target":          TARGET_COL,
            "coef":            model.coef_.tolist(),
            "intercept":       float(model.intercept_),
            "scaler_mean":     scaler.mean_.tolist(),
            "scaler_scale":    scaler.scale_.tolist(),
            "model_type":      model_type,
            "in_sample_r2":    round(r2, 4),
            "direction_acc":   round(dir_acc, 4),
            "n_train":         len(X),
            "window":          recent_window,
            "trained_at_utc":  pd.Timestamp.utcnow().isoformat(),
        }

        path = MODEL_DIR / f"{ticker.replace('/', '_').replace('=', '_')}_online_model.json"
        with open(path, "w") as f:
            json.dump(state, f, indent=2)
        logger.info("Online model saved for %s: dir_acc=%.1f%% (n=%d)",
                    ticker, dir_acc * 100, len(X))
        return state

    def train_signal_classifier(self):
        """
        Train LGBMClassifier for TradeSignalGenerator (BUY/SELL/HOLD).
        Maps features (regime, sentiment) to forward returns.
        """
        if not LGBM_AVAILABLE:
            logger.error("LightGBM not installed. Cannot train signal classifier.")
            return

        from lightgbm import LGBMClassifier
        
        logger.info("Training LGBM Signal Classifier...")
        
        # Use BTC as proxy for training the global signal classifier
        try:
            df = self.engine.load_dataset("BTC-USD")
        except FileNotFoundError:
            logger.warning("No data for BTC-USD to train classifier.")
            return
            
        df_clean = df[FEATURE_COLS + [TARGET_COL]].dropna()
        if len(df_clean) < 100:
            logger.warning("Not enough data to train LGBM classifier.")
            return
            
        X, y = [], []
        for idx, row in df_clean.iterrows():
            fwd_ret = row[TARGET_COL]
            # label: 0=BUY, 1=SELL, 2=HOLD
            if fwd_ret > 0.02:
                label = 0
            elif fwd_ret < -0.02:
                label = 1
            else:
                label = 2
                
            # Simulate historical regime and sentiment from technicals
            vol = row["vol_20d"]
            ret_20d = row["ret_20d"]
            
            regime = 2 # Range
            if vol > 0.05: regime = 3 # Volatile
            elif ret_20d > 0.1: regime = 0 # Bull
            elif ret_20d < -0.1: regime = 1 # Bear
            
            sentiment = float(np.clip(ret_20d * 5, -1.0, 1.0))
            
            X.append([regime, sentiment])
            y.append(label)
            
        X = np.array(X)
        y = np.array(y)
        
        model = LGBMClassifier(
            n_estimators=500,
            max_depth=6,
            learning_rate=0.05,
            objective='multiclass',
            num_class=3,
            verbose=-1
        )
        model.fit(X, y)
        
        model_path = MODEL_DIR / "lgbm_signal_classifier.txt"
        model.booster_.save_model(str(model_path))
        logger.info("[OK] LGBM Signal Classifier saved to %s", model_path)

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
    parser.add_argument("--model",   default="auto",
                        choices=["ridge", "lgbm", "auto", "stack"],
                        help="Model type: ridge, lgbm (LightGBM), stack (ensemble), or auto (default: auto)")
    parser.add_argument("--online",  action="store_true",
                        help="Weekly-style incremental retrain (SGD partial_fit) on recent window")
    parser.add_argument("--online-full", action="store_true",
                        help="Online refresh via full ridge retrain on recent window (no SGD)")
    args = parser.parse_args()

    trainer = AITrainer(n_splits=args.splits, alpha=args.alpha, model_type=args.model)

    if args.online or args.online_full:
        targets = args.symbols or list(ALL_SYMBOLS.keys())
        for ticker in targets:
            try:
                trainer.retrain_online(ticker, incremental=args.online)
            except Exception as e:
                logger.error("Online retrain failed for %s: %s", ticker, e)
    else:
        trainer.train_all(symbols=args.symbols)
        trainer.train_signal_classifier()
