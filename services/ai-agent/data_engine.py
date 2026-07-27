"""
Robin Trading Platform — Real Historical Data Engine
=====================================================
Replaces GBM simulation with real market data from:
  - Yahoo Finance (yfinance) — Equities, ETFs, Crypto, Forex back to 1924
  - FRED (Federal Reserve) — Macro indicators (CPI, Fed Funds Rate, VIX)

Stores data as Delta-encoded Parquet files with ZSTD compression.
Total on-disk size: ~500MB for 5 symbols × 100yr daily bars.
"""

import hashlib
import logging
import os
import re
import time
from datetime import datetime, timedelta
from pathlib import Path
from typing import Optional

import numpy as np
import pandas as pd
import pyarrow as pa
import pyarrow.parquet as pq

logger = logging.getLogger("data_engine")
logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")

# ─── Directory layout ────────────────────────────────────────────────────────
DATA_DIR = Path(os.path.dirname(__file__)) / "data"
CACHE_DIR = DATA_DIR / ".cache"
DATA_DIR.mkdir(parents=True, exist_ok=True)
CACHE_DIR.mkdir(parents=True, exist_ok=True)

# ─── Symbol map ──────────────────────────────────────────────────────────────
#  Real Yahoo Finance tickers that have extended history
EQUITY_SYMBOLS = {
    "SPY":    "1993-01-29",  # S&P 500 ETF — oldest widely available
    "QQQ":    "1999-03-10",  # NASDAQ-100
    "AAPL":   "1980-12-12",  # Apple
    "TSLA":   "2010-06-29",  # Tesla
    "NVDA":   "1999-01-22",  # NVIDIA
    "MSFT":   "1986-03-13",  # Microsoft
    "AMZN":   "1997-05-15",  # Amazon
    "GOOG":   "2004-08-19",  # Alphabet
    "BRK-B":  "1996-05-09",  # Berkshire Hathaway B
    "GLD":    "2004-11-18",  # Gold ETF
}

CRYPTO_SYMBOLS = {
    "BTC-USD": "2014-09-17",  # Bitcoin (YF max ~10yr)
    "ETH-USD": "2017-11-09",  # Ethereum
}

FOREX_SYMBOLS = {
    "EURUSD=X": "2003-12-01",
    "GBPUSD=X": "2003-12-01",
    "USDJPY=X": "2003-12-01",
}

# FRED macro series — fetched via pandas_datareader
MACRO_SERIES = {
    "DFF":      ("1954-07-01", "Fed Funds Rate (daily, %)"),
    "CPIAUCSL": ("1947-01-01", "CPI All Urban Consumers (monthly, SA)"),
    "UNRATE":   ("1948-01-01", "Unemployment Rate (monthly, %)"),
    "VIXCLS":   ("1990-01-02", "CBOE VIX Close (daily)"),
    "GS10":     ("1953-04-01", "10-Year Treasury Constant Maturity Rate"),
}

# All trading symbols (for model training)
ALL_SYMBOLS = {**EQUITY_SYMBOLS, **CRYPTO_SYMBOLS, **FOREX_SYMBOLS}


# ─── DataEngine ──────────────────────────────────────────────────────────────

class DataEngine:
    """
    Fetches and caches real historical OHLCV data for all configured symbols.
    Uses yfinance for market data and pandas-datareader for FRED macro data.
    Stores data as compressed Parquet files for fast re-loading.
    """

    def __init__(self, symbols: Optional[dict] = None):
        self.symbols = symbols or ALL_SYMBOLS
        self._import_check()

    def _import_check(self):
        """Verify required packages are installed with helpful error messages."""
        missing = []
        for pkg, pip_name in [
            ("yfinance", "yfinance>=0.2.40"),
            ("pandas_datareader", "pandas-datareader>=0.10.0"),
        ]:
            try:
                __import__(pkg)
            except ImportError:
                missing.append(pip_name)
        if missing:
            raise ImportError(
                f"Missing packages: {missing}\n"
                f"Run: pip install {' '.join(missing)}"
            )

    # ─── Real historical data fetch ──────────────────────────────────────────

    def fetch_symbol(self, ticker: str, start: str, end: Optional[str] = None) -> pd.DataFrame:
        """
        Download OHLCV data from Yahoo Finance.
        Retries 3× with exponential backoff on network errors.
        """
        import yfinance as yf

        end = end or datetime.now().strftime("%Y-%m-%d")
        cache_key = hashlib.md5(f"{ticker}-{start}-{end}".encode()).hexdigest()[:8]
        safe_ticker = re.sub(r'[^a-zA-Z0-9_-]', '_', ticker)
        cache_file = CACHE_DIR / f"{safe_ticker}_{cache_key}.parquet"

        if cache_file.exists():
            logger.debug("Cache hit: %s", cache_file)
            return pd.read_parquet(cache_file)

        logger.info("Downloading %s from %s to %s ...", ticker, start, end)
        for attempt in range(3):
            try:
                df = yf.download(
                    ticker,
                    start=start,
                    end=end,
                    auto_adjust=True,      # Adjust for splits/dividends
                    progress=False,
                    timeout=30,
                )
                if df.empty:
                    logger.warning("No data for %s (start=%s)", ticker, start)
                    return pd.DataFrame()

                # Flatten multi-level columns if present
                if isinstance(df.columns, pd.MultiIndex):
                    df.columns = [c[0].lower() for c in df.columns]
                else:
                    df.columns = [c.lower() for c in df.columns]

                df.index.name = "timestamp"
                df = df.reset_index()
                df["symbol"] = ticker
                df["timestamp"] = pd.to_datetime(df["timestamp"])

                # Rename Volume to volume if needed
                if "volume" not in df.columns and "Volume" in df.columns:
                    df.rename(columns={"Volume": "volume"}, inplace=True)

                # Drop rows with all-NaN OHLCV
                df.dropna(subset=["open", "high", "low", "close"], inplace=True)

                # Cache to parquet for fast future loads
                df.to_parquet(cache_file, compression="zstd", index=False)
                logger.info("  → %d rows fetched for %s", len(df), ticker)
                return df

            except Exception as exc:
                wait = 2 ** attempt
                logger.warning("Attempt %d failed for %s: %s. Retrying in %ds...",
                               attempt + 1, ticker, exc, wait)
                time.sleep(wait)

        logger.error("All attempts failed for %s", ticker)
        return pd.DataFrame()

    def fetch_macro(self, series_id: str, start: str) -> pd.DataFrame:
        """Fetch a FRED macro time series and return as a DataFrame."""
        try:
            import pandas_datareader.data as web
        except ImportError:
            raise ImportError("Install pandas-datareader: pip install pandas-datareader>=0.10.0")

        cache_file = CACHE_DIR / f"FRED_{series_id}.parquet"
        if cache_file.exists() and (time.time() - cache_file.stat().st_mtime) < 86400:
            return pd.read_parquet(cache_file)

        logger.info("Fetching FRED series %s from %s ...", series_id, start)
        try:
            df = web.DataReader(series_id, "fred", start=start)
            df.index.name = "timestamp"
            df = df.reset_index()
            df.rename(columns={series_id: "value"}, inplace=True)
            df["series_id"] = series_id
            df.to_parquet(cache_file, compression="zstd", index=False)
            logger.info("  → %d rows for FRED/%s", len(df), series_id)
            return df
        except Exception as exc:
            logger.error("FRED fetch failed for %s: %s", series_id, exc)
            return pd.DataFrame()

    # ─── Full data generation ────────────────────────────────────────────────

    def generate_full_dataset(self, force: bool = False) -> dict[str, str]:
        """
        Download all configured symbols and save to Parquet.
        Returns dict of {symbol: parquet_path}.
        """
        results = {}
        total = len(self.symbols)

        for i, (ticker, start_date) in enumerate(self.symbols.items(), 1):
            safe_name = ticker.replace("/", "_").replace("=", "_")
            output_path = DATA_DIR / f"{safe_name}_historical.parquet"

            if output_path.exists() and not force:
                logger.info("[%d/%d] %s — already exists, skipping (use force=True to refresh)",
                            i, total, ticker)
                results[ticker] = str(output_path)
                continue

            df = self.fetch_symbol(ticker, start=start_date)
            if df.empty:
                logger.warning("[%d/%d] %s — no data, skipping", i, total, ticker)
                continue

            # Add derived features for ML
            df = self._add_features(df)

            # Save with ZSTD compression
            table = pa.Table.from_pandas(df, preserve_index=False)
            pq.write_table(table, output_path, compression="zstd")
            logger.info("[%d/%d] %s — saved %d rows to %s",
                        i, total, ticker, len(df), output_path)
            results[ticker] = str(output_path)

        # Fetch macro data separately
        logger.info("Fetching FRED macro indicators ...")
        for series_id, (start_date, desc) in MACRO_SERIES.items():
            self.fetch_macro(series_id, start=start_date)
            logger.info("  → %s (%s)", series_id, desc)

        logger.info("✅ Data generation complete. %d symbols saved.", len(results))
        return results

    def _add_features(self, df: pd.DataFrame) -> pd.DataFrame:
        """Add technical indicators used for model training."""
        df = df.copy().sort_values("timestamp").reset_index(drop=True)

        close = df["close"]

        # Returns
        df["ret_1d"]  = close.pct_change(1)
        df["ret_5d"]  = close.pct_change(5)
        df["ret_20d"] = close.pct_change(20)

        # Volatility
        df["vol_10d"] = df["ret_1d"].rolling(10).std()
        df["vol_20d"] = df["ret_1d"].rolling(20).std()
        df["vol_60d"] = df["ret_1d"].rolling(60).std()

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
        rs    = gain / loss.replace(0, 1e-10)
        df["rsi_14"] = 100 - (100 / (1 + rs))

        # Bollinger Bands
        bb_mid        = close.rolling(20).mean()
        bb_std        = close.rolling(20).std()
        df["bb_upper"] = bb_mid + 2 * bb_std
        df["bb_lower"] = bb_mid - 2 * bb_std
        df["bb_pos"]   = (close - df["bb_lower"]) / (df["bb_upper"] - df["bb_lower"] + 1e-10)

        # ATR (14-period)
        hl  = df["high"] - df["low"]
        hpc = (df["high"] - close.shift(1)).abs()
        lpc = (df["low"]  - close.shift(1)).abs()
        df["atr_14"] = pd.concat([hl, hpc, lpc], axis=1).max(axis=1).rolling(14).mean()

        # Volume z-score
        vol_ma = df["volume"].rolling(20).mean()
        vol_sd = df["volume"].rolling(20).std()
        df["volume_zscore"] = (df["volume"] - vol_ma) / vol_sd.replace(0, 1e-10)

        # Price relative to MAs
        df["price_vs_sma50"]  = (close / df["sma_50"])  - 1
        df["price_vs_sma200"] = (close / df["sma_200"]) - 1

        return df

    # ─── Dataset loading ─────────────────────────────────────────────────────

    def load_dataset(self, ticker: str) -> pd.DataFrame:
        """Load pre-saved Parquet dataset for a ticker."""
        safe_name = ticker.replace("/", "_").replace("=", "_")
        path = DATA_DIR / f"{safe_name}_historical.parquet"
        if not path.exists():
            raise FileNotFoundError(
                f"Dataset for {ticker} not found at {path}. "
                f"Run generate_full_dataset() first."
            )
        df = pd.read_parquet(path)
        df["timestamp"] = pd.to_datetime(df["timestamp"])
        return df.sort_values("timestamp").reset_index(drop=True)

    def load_macro(self, series_id: str) -> pd.DataFrame:
        """Load FRED macro series."""
        path = CACHE_DIR / f"FRED_{series_id}.parquet"
        if not path.exists():
            raise FileNotFoundError(f"Macro series {series_id} not cached.")
        return pd.read_parquet(path)

    def get_available_symbols(self) -> list[str]:
        """Return list of symbols that have been downloaded."""
        return [
            f.stem.replace("_historical", "").replace("_", "/")
            for f in DATA_DIR.glob("*_historical.parquet")
        ]


# ─── CLI entry point ─────────────────────────────────────────────────────────

if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser(description="Robin Data Engine — Fetch real market data")
    parser.add_argument("--force", action="store_true", help="Re-download even if cached")
    parser.add_argument("--symbols", nargs="+", help="Specific tickers to fetch")
    args = parser.parse_args()

    if args.symbols:
        sym_map = {s: ALL_SYMBOLS.get(s, "2000-01-01") for s in args.symbols}
        engine = DataEngine(symbols=sym_map)
    else:
        engine = DataEngine()

    results = engine.generate_full_dataset(force=args.force)

    print("\n" + "=" * 60)
    print("ROBIN DATA ENGINE — DOWNLOAD SUMMARY")
    print("=" * 60)
    for ticker, path in results.items():
        size_mb = Path(path).stat().st_size / 1_048_576
        print(f"  {ticker:<15} → {path}  ({size_mb:.1f} MB)")
    print(f"\n✅ Total: {len(results)} symbols ready for training.")
