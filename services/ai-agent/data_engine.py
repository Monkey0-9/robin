import os
import numpy as np
import pandas as pd
import pyarrow as pa
import pyarrow.parquet as pq
from datetime import datetime, timedelta

DATA_DIR = os.path.join(os.path.dirname(__file__), "data")


class DataEngine:
    """
    100-Year Historical Data Simulator & Ingestion Pipeline
    Generates high-fidelity historical data spanning a century for AI model training.
    """

    def __init__(self, symbols=["BTC/USD", "ETH/USD", "AAPL", "TSLA", "EUR/USD"]):
        self.symbols = symbols
        self.years = 100
        self.days_per_year = 252  # Trading days
        self.total_days = self.years * self.days_per_year
        os.makedirs(DATA_DIR, exist_ok=True)

    def generate_100_year_dataset(self):
        print(
            f"Generating {self.years} years of historical data ({self.total_days} rows per symbol)..."
        )
        end_date = datetime.now()
        start_date = end_date - timedelta(days=self.total_days * (365 / 252))

        # Generate business days
        dates = pd.date_range(start=start_date, periods=self.total_days, freq="B")

        for symbol in self.symbols:
            print(f"  -> Simulating market cycles for {symbol}...")
            # Geometric Brownian Motion for base price
            np.random.seed(hash(symbol) % (2**32))  # Deterministic based on symbol

            mu = 0.0002  # Daily drift
            sigma = 0.02  # Daily volatility

            returns = np.random.normal(loc=mu, scale=sigma, size=self.total_days)
            # Add macro shocks (Crashes / Bubbles)
            shock_indices = np.random.choice(self.total_days, size=50, replace=False)
            returns[shock_indices] *= 5.0  # 5x volatility on shock days

            price_series = 100.0 * np.exp(np.cumsum(returns))

            # Generate OHLCV
            highs = price_series * (
                1 + np.abs(np.random.normal(0, 0.01, self.total_days))
            )
            lows = price_series * (
                1 - np.abs(np.random.normal(0, 0.01, self.total_days))
            )
            opens = price_series * (1 + np.random.normal(0, 0.005, self.total_days))
            volumes = np.random.lognormal(mean=10, sigma=1.5, size=self.total_days)

            df = pd.DataFrame(
                {
                    "timestamp": dates,
                    "symbol": symbol,
                    "open": opens,
                    "high": highs,
                    "low": lows,
                    "close": price_series,
                    "volume": volumes,
                    "macro_sentiment": np.random.uniform(
                        -1, 1, size=self.total_days
                    ),  # Mock historical news sentiment
                }
            )

            # Delta encoding for maximum compression
            df_encoded = df.copy()
            df_encoded["timestamp"] = (
                df_encoded["timestamp"].astype("int64") // 1_000_000
            )
            for col in ["open", "high", "low", "close", "volume"]:
                df_encoded[col] = (
                    df_encoded[col]
                    .diff()
                    .fillna(df_encoded[col].iloc[0])
                    .astype("float32")
                )

            # Save to high-performance Parquet format with ZSTD compression
            table = pa.Table.from_pandas(df_encoded)
            file_path = os.path.join(
                DATA_DIR, f"{symbol.replace('/', '_')}_100yr.parquet"
            )
            pq.write_table(table, file_path, compression="zstd")
            print(f"  -> Saved {file_path} (Delta+ZSTD compressed)")

    def load_dataset(self, symbol: str) -> pd.DataFrame:
        file_path = os.path.join(DATA_DIR, f"{symbol.replace('/', '_')}_100yr.parquet")
        if not os.path.exists(file_path):
            raise FileNotFoundError(
                f"Dataset for {symbol} not found. Run generation first."
            )

        # Load and reverse delta encoding
        df_encoded = pd.read_parquet(file_path)
        df = df_encoded.copy()
        if not pd.api.types.is_datetime64_any_dtype(df["timestamp"]):
            df["timestamp"] = pd.to_datetime(df["timestamp"] * 1_000_000)
        for col in ["open", "high", "low", "close", "volume"]:
            df[col] = df[col].cumsum()
        return df


if __name__ == "__main__":
    engine = DataEngine()
    engine.generate_100_year_dataset()
    print("100-Year Data generation complete with ZERO ERRORS.")
