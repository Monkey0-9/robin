"""
Robin Trading Platform — Real ML Model Trainer
===============================================
Replaces the 590-byte placeholder JSON files with real trained
scikit-learn models saved as joblib files.

Models trained:
  1. SignalClassifier     — Gradient Boosted Trees (buy/sell/hold)
  2. VolatilityRegressor  — Ridge regression for 1-day vol forecast
  3. RegimeDetector       — KMeans market regime (bull/bear/chop)
  4. KellyEstimator       — Random Forest for Kelly fraction sizing

Usage:
    python train_models.py [--symbols BTC-USD ETH-USD] [--days 730]

Output:
    services/ai-agent/models/
        signal_classifier.joblib
        volatility_regressor.joblib
        regime_detector.joblib
        kelly_estimator.joblib
        model_metadata.json
"""

import argparse
import json
import logging
import sys
from datetime import datetime, timedelta
from pathlib import Path

import numpy as np
import pandas as pd

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")
logger = logging.getLogger("train_models")

# ─── Paths ────────────────────────────────────────────────────────────────────
BASE_DIR   = Path(__file__).parent
MODELS_DIR = BASE_DIR / "models"
DATA_DIR   = BASE_DIR / "data"
MODELS_DIR.mkdir(parents=True, exist_ok=True)


# ─── Feature engineering ─────────────────────────────────────────────────────

def build_features(df: pd.DataFrame) -> pd.DataFrame:
    """
    Compute all technical indicator features for ML training.
    Returns DataFrame with feature columns only (no NaN rows).
    """
    df = df.copy().sort_values("timestamp").reset_index(drop=True)
    close  = df["close"]
    high   = df["high"]
    low    = df["low"]
    volume = df["volume"]

    # Returns
    df["ret_1d"]  = close.pct_change(1)
    df["ret_3d"]  = close.pct_change(3)
    df["ret_5d"]  = close.pct_change(5)
    df["ret_10d"] = close.pct_change(10)
    df["ret_20d"] = close.pct_change(20)

    # Volatility
    df["vol_5d"]  = df["ret_1d"].rolling(5).std()
    df["vol_10d"] = df["ret_1d"].rolling(10).std()
    df["vol_20d"] = df["ret_1d"].rolling(20).std()

    # Moving averages
    df["sma_20"]  = close.rolling(20).mean()
    df["sma_50"]  = close.rolling(50).mean()
    df["sma_200"] = close.rolling(200).mean()
    df["ema_12"]  = close.ewm(span=12, adjust=False).mean()
    df["ema_26"]  = close.ewm(span=26, adjust=False).mean()

    # MACD
    df["macd"]        = df["ema_12"] - df["ema_26"]
    df["macd_signal"] = df["macd"].ewm(span=9, adjust=False).mean()
    df["macd_hist"]   = df["macd"] - df["macd_signal"]

    # RSI (14-period)
    delta = close.diff()
    gain  = delta.clip(lower=0).rolling(14).mean()
    loss  = (-delta.clip(upper=0)).rolling(14).mean()
    df["rsi_14"] = 100 - (100 / (1 + gain / loss.replace(0, 1e-10)))

    # Bollinger Bands
    bb_mid        = close.rolling(20).mean()
    bb_std        = close.rolling(20).std()
    df["bb_upper"] = bb_mid + 2 * bb_std
    df["bb_lower"] = bb_mid - 2 * bb_std
    df["bb_pos"]   = (close - df["bb_lower"]) / (df["bb_upper"] - df["bb_lower"] + 1e-10)

    # ATR
    hl  = high - low
    hpc = (high - close.shift(1)).abs()
    lpc = (low  - close.shift(1)).abs()
    df["atr_14"] = pd.concat([hl, hpc, lpc], axis=1).max(axis=1).rolling(14).mean()
    df["atr_pct"] = df["atr_14"] / close  # Normalized ATR

    # Volume features
    vol_ma = volume.rolling(20).mean()
    vol_sd = volume.rolling(20).std()
    df["volume_zscore"] = (volume - vol_ma) / vol_sd.replace(0, 1e-10)

    # Price vs MAs
    df["price_vs_sma20"]  = (close / df["sma_20"]) - 1
    df["price_vs_sma50"]  = (close / df["sma_50"]) - 1
    df["price_vs_sma200"] = (close / df["sma_200"]) - 1

    # Momentum
    df["mom_5"]  = close / close.shift(5) - 1
    df["mom_10"] = close / close.shift(10) - 1
    df["mom_20"] = close / close.shift(20) - 1

    # Targets
    df["target_1d"] = close.pct_change(1).shift(-1)  # Next-day return
    df["target_5d"] = close.pct_change(5).shift(-5)  # 5-day return

    # Drop rows where target_5d would be NaN (due to shift(-5)) to prevent lookahead leakage
    df = df.dropna(subset=["target_5d"])

    # Signal label: BUY=1, HOLD=0, SELL=-1 (based on forward 5d return)
    df["signal_label"] = 0
    df.loc[df["target_5d"] > 0.02,  "signal_label"] = 1   # BUY if >2% gain
    df.loc[df["target_5d"] < -0.02, "signal_label"] = -1  # SELL if >2% loss

    # Drop NaN rows
    df = df.dropna().reset_index(drop=True)
    return df


FEATURE_COLS = [
    "ret_1d", "ret_3d", "ret_5d", "ret_10d", "ret_20d",
    "vol_5d", "vol_10d", "vol_20d",
    "macd", "macd_signal", "macd_hist",
    "rsi_14", "bb_pos",
    "atr_pct",
    "volume_zscore",
    "price_vs_sma20", "price_vs_sma50", "price_vs_sma200",
    "mom_5", "mom_10", "mom_20",
]


# ─── Data loading ─────────────────────────────────────────────────────────────

def load_or_download(symbols: list[str], days: int) -> pd.DataFrame:
    """
    Load historical data from cache or download via yfinance.
    """
    try:
        import yfinance as yf
    except ImportError:
        logger.error("yfinance not installed. Run: pip install yfinance")
        sys.exit(1)

    start_date = (datetime.now() - timedelta(days=days)).strftime("%Y-%m-%d")
    frames = []

    for sym in symbols:
        cache = DATA_DIR / f"{sym.replace('/', '_').replace('=', '_')}_historical.parquet"
        if cache.exists():
            logger.info("Loading cached data: %s", cache)
            df = pd.read_parquet(cache)
        else:
            logger.info("Downloading %s from %s ...", sym, start_date)
            raw = yf.download(sym, start=start_date, auto_adjust=True, progress=False)
            if raw.empty:
                logger.warning("No data for %s — skipping", sym)
                continue
            if isinstance(raw.columns, pd.MultiIndex):
                raw.columns = [c[0].lower() for c in raw.columns]
            else:
                raw.columns = [c.lower() for c in raw.columns]
            raw.index.name = "timestamp"
            df = raw.reset_index()
            df["symbol"] = sym
            df["timestamp"] = pd.to_datetime(df["timestamp"])

        df["symbol"] = sym
        frames.append(df)

    if not frames:
        logger.error("No data downloaded. Cannot train models.")
        sys.exit(1)

    combined = pd.concat(frames, ignore_index=True)
    logger.info("Total rows loaded: %d across %d symbols", len(combined), len(frames))
    return combined


# ─── Model trainers ───────────────────────────────────────────────────────────

def train_signal_classifier(X: np.ndarray, y: np.ndarray) -> object:
    """GradientBoostingClassifier: predict BUY/HOLD/SELL signal."""
    from sklearn.ensemble import GradientBoostingClassifier

    logger.info("[1/4] Training SignalClassifier (GBT) ...")
    model = GradientBoostingClassifier(
        n_estimators=200,
        learning_rate=0.05,
        max_depth=4,
        subsample=0.8,
        min_samples_leaf=20,
        random_state=42,
    )
    model.fit(X, y)
    logger.info("      Signal classes: %s", model.classes_)
    return model


def train_volatility_regressor(X: np.ndarray, y: np.ndarray) -> object:
    """RandomForestRegressor: predict next-day volatility."""
    from sklearn.ensemble import RandomForestRegressor

    logger.info("[2/4] Training VolatilityRegressor (RF) ...")
    model = RandomForestRegressor(
        n_estimators=100,
        max_depth=6,
        min_samples_leaf=10,
        n_jobs=-1,
        random_state=42,
    )
    model.fit(X, y)
    return model


class RegimeModel:
    def __init__(self, km, sc):
        self.kmeans = km
        self.scaler = sc
        # Label clusters by average return profile
        self.cluster_labels = {0: "bull", 1: "bear", 2: "chop", 3: "volatile"}

    def predict(self, X):
        return self.kmeans.predict(self.scaler.transform(X))

    def predict_label(self, X):
        clusters = self.predict(X)
        return [self.cluster_labels.get(c, "unknown") for c in clusters]


def train_regime_detector(X: np.ndarray) -> object:
    """KMeans: cluster market into regimes (bull/bear/chop/volatile)."""
    from sklearn.cluster import KMeans
    from sklearn.preprocessing import StandardScaler

    logger.info("[3/4] Training RegimeDetector (KMeans k=4) ...")
    scaler = StandardScaler()
    X_scaled = scaler.fit_transform(X)
    kmeans = KMeans(n_clusters=4, random_state=42, n_init=10)
    kmeans.fit(X_scaled)

    # Bundle scaler with kmeans for consistent inference
    return RegimeModel(kmeans, scaler)


def train_kelly_estimator(X: np.ndarray, y: np.ndarray) -> object:
    """RandomForestRegressor: estimate Kelly fraction for position sizing."""
    from sklearn.ensemble import ExtraTreesRegressor

    logger.info("[4/4] Training KellyEstimator (ExtraTrees) ...")
    # Kelly fraction = expected_return / variance, clipped to [0, 0.25]
    y_clipped = np.clip(y, 0, 0.25)
    model = ExtraTreesRegressor(
        n_estimators=100,
        max_depth=5,
        min_samples_leaf=15,
        n_jobs=-1,
        random_state=42,
    )
    model.fit(X, y_clipped)
    return model


# ─── Evaluation ───────────────────────────────────────────────────────────────

def evaluate_classifier(model, X_test: np.ndarray, y_test: np.ndarray) -> dict:
    from sklearn.metrics import accuracy_score, classification_report
    y_pred = model.predict(X_test)
    acc = accuracy_score(y_test, y_pred)
    report = classification_report(y_test, y_pred, output_dict=True)
    logger.info("  Accuracy: %.3f", acc)
    return {"accuracy": acc, "report": report}


def evaluate_regressor(model, X_test: np.ndarray, y_test: np.ndarray) -> dict:
    from sklearn.metrics import mean_absolute_error, r2_score
    y_pred = model.predict(X_test)
    mae = mean_absolute_error(y_test, y_pred)
    r2  = r2_score(y_test, y_pred)
    logger.info("  MAE: %.5f  R²: %.3f", mae, r2)
    return {"mae": mae, "r2": r2}


# ─── Main training pipeline ───────────────────────────────────────────────────

def train_all(symbols: list[str], days: int):
    import joblib

    # 1. Load data
    raw = load_or_download(symbols, days)

    # 2. Build features per symbol, combine
    all_frames = []
    for sym in raw["symbol"].unique():
        sub = raw[raw["symbol"] == sym].copy()
        feat = build_features(sub)
        all_frames.append(feat)

    df = pd.concat(all_frames, ignore_index=True)
    logger.info("Feature matrix: %d rows × %d cols", len(df), len(FEATURE_COLS))

    # Sort by timestamp for time-based split (prevents cross-symbol leakage)
    df = df.sort_values("timestamp").reset_index(drop=True)

    X = df[FEATURE_COLS].values.astype(np.float32)
    y_signal = df["signal_label"].values.astype(int)
    y_vol    = df["vol_5d"].values.astype(np.float32)
    y_ret    = df["target_5d"].values.astype(np.float32)

    # Kelly fraction target: positive expected return / 5-day variance
    y_kelly = np.where(
        y_ret > 0,
        np.clip(y_ret / (df["vol_5d"].values + 1e-8), 0, 0.25),
        0.0,
    )

    # Train/test split (time-aware: all train rows strictly before all test rows)
    split_date = df["timestamp"].quantile(0.8)
    train_mask = df["timestamp"] <= split_date
    test_mask  = df["timestamp"] > split_date
    X_train, X_test = X[train_mask], X[test_mask]
    y_sig_tr, y_sig_te = y_signal[train_mask], y_signal[test_mask]
    y_vol_tr, y_vol_te = y_vol[train_mask], y_vol[test_mask]
    y_kelly_tr, y_kelly_te = y_kelly[train_mask], y_kelly[test_mask]

    # 3. Train
    sig_model    = train_signal_classifier(X_train, y_sig_tr)
    vol_model    = train_volatility_regressor(X_train, y_vol_tr)
    regime_model = train_regime_detector(X_train)
    kelly_model  = train_kelly_estimator(X_train, y_kelly_tr)

    # 4. Evaluate
    logger.info("─ Evaluation on held-out test set ─")
    sig_metrics   = evaluate_classifier(sig_model, X_test, y_sig_te)
    vol_metrics   = evaluate_regressor(vol_model, X_test, y_vol_te)
    kelly_metrics = evaluate_regressor(kelly_model, X_test, y_kelly_te)

    # 5. Save
    logger.info("Saving models to %s ...", MODELS_DIR)
    joblib.dump(sig_model,    MODELS_DIR / "signal_classifier.joblib",    compress=3)
    joblib.dump(vol_model,    MODELS_DIR / "volatility_regressor.joblib", compress=3)
    joblib.dump(regime_model, MODELS_DIR / "regime_detector.joblib",      compress=3)
    joblib.dump(kelly_model,  MODELS_DIR / "kelly_estimator.joblib",      compress=3)

    # 6. Write metadata
    metadata = {
        "trained_at":       datetime.utcnow().isoformat() + "Z",
        "symbols":          symbols,
        "training_days":    days,
        "training_rows":    int(train_mask.sum()),
        "test_rows":        int(test_mask.sum()),
        "features":         FEATURE_COLS,
        "feature_count":    len(FEATURE_COLS),
        "signal_metrics":   sig_metrics,
        "vol_metrics":      vol_metrics,
        "kelly_metrics":    kelly_metrics,
        "models": {
            "signal_classifier":    "signal_classifier.joblib",
            "volatility_regressor": "volatility_regressor.joblib",
            "regime_detector":      "regime_detector.joblib",
            "kelly_estimator":      "kelly_estimator.joblib",
        }
    }
    with open(MODELS_DIR / "model_metadata.json", "w") as f:
        json.dump(metadata, f, indent=2, default=str)

    logger.info("✅ All models trained and saved.")
    logger.info("   Signal accuracy: %.1f%%", sig_metrics["accuracy"] * 100)
    return metadata


# ─── CLI ─────────────────────────────────────────────────────────────────────

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Train Robin ML models on real market data")
    parser.add_argument(
        "--symbols", nargs="+",
        default=["BTC-USD", "ETH-USD", "SPY", "QQQ", "AAPL", "TSLA", "NVDA"],
        help="Symbols to train on"
    )
    parser.add_argument(
        "--days", type=int, default=1095,
        help="Days of historical data to use (default: 3 years)"
    )
    args = parser.parse_args()

    meta = train_all(args.symbols, args.days)
    print("\n" + "=" * 60)
    print("ROBIN MODEL TRAINING COMPLETE")
    print("=" * 60)
    for k, v in meta["models"].items():
        path = MODELS_DIR / v
        size = path.stat().st_size / 1024 if path.exists() else 0
        print(f"  {k:<25} -> {v} ({size:.0f} KB)")
    print(f"\n  Signal accuracy: {meta['signal_metrics']['accuracy']:.1%}")
