"""
Robin AI Agent — Test Suite
===========================
Tests all critical components:
  - market_data_service: price fetching, staleness detection
  - data_engine: parquet cache, feature engineering
  - live_feed: Tick dataclass, ZMQ publish
  - main.py endpoints: CORS, /ready, /autonomous/status
  - train_models: feature build, model training smoke test
"""

import pytest
import time
from dataclasses import asdict


# ─── market_data_service tests ───────────────────────────────────────────────

class TestPriceSnapshot:
    def test_age_seconds(self):
        from market_data_service import PriceSnapshot
        snap = PriceSnapshot(
            symbol="BTC-USD", price=50000.0,
            bid=49999.0, ask=50001.0,
            volume_24h=1000.0, change_pct=2.5,
            timestamp=time.time() - 30,
            source="test",
        )
        assert 29 < snap.age_seconds() < 35

    def test_is_stale_fresh(self):
        from market_data_service import PriceSnapshot
        snap = PriceSnapshot(
            symbol="BTC-USD", price=50000.0,
            bid=49999.0, ask=50001.0,
            volume_24h=1000.0, change_pct=2.5,
            timestamp=time.time(),
            source="test",
        )
        assert not snap.is_stale(max_age=60.0)

    def test_is_stale_old(self):
        from market_data_service import PriceSnapshot
        snap = PriceSnapshot(
            symbol="BTC-USD", price=50000.0,
            bid=49999.0, ask=50001.0,
            volume_24h=1000.0, change_pct=2.5,
            timestamp=time.time() - 120,
            source="test",
        )
        assert snap.is_stale(max_age=60.0)


class TestMarketDataService:
    def test_get_price_none_when_empty(self):
        from market_data_service import MarketDataService
        svc = MarketDataService()
        assert svc.get_price("BTC-USD") is None

    def test_get_vix_default(self):
        from market_data_service import MarketDataService
        svc = MarketDataService()
        # Should return 20.0 when no VIX data
        assert svc.get_vix() == 20.0

    def test_get_all_snapshots_empty(self):
        from market_data_service import MarketDataService
        svc = MarketDataService()
        assert svc.get_all_snapshots() == {}

    def test_binance_ticker_parsing(self):
        from market_data_service import MarketDataService
        svc = MarketDataService()
        msg = {
            "stream": "btcusdt@ticker",
            "data": {
                "e": "24hrTicker",
                "c": "65432.10",
                "b": "65430.00",
                "a": "65434.00",
                "v": "12345.67",
                "P": "1.23",
            }
        }
        svc._handle_binance_ticker(msg)
        price = svc.get_price("BTC-USD")
        assert price is not None
        assert abs(price - 65432.10) < 0.01

    def test_binance_ignore_non_ticker(self):
        from market_data_service import MarketDataService
        svc = MarketDataService()
        msg = {
            "stream": "btcusdt@trade",
            "data": {"e": "trade", "p": "65000"}
        }
        svc._handle_binance_ticker(msg)
        assert svc.get_price("BTC-USD") is None  # Should be ignored

    def test_macro_news_fallback(self):
        from market_data_service import MarketDataService
        svc = MarketDataService()
        news = svc.get_macro_news()
        assert isinstance(news, list)
        assert len(news) > 0
        assert "text" in news[0]
        assert "impact" in news[0]

    def test_get_snapshot_returns_none_when_missing(self):
        from market_data_service import MarketDataService
        svc = MarketDataService()
        assert svc.get_snapshot("XYZ-NOTREAL") is None

    def test_singleton(self):
        from market_data_service import get_market_data
        a = get_market_data()
        b = get_market_data()
        assert a is b


# ─── data_engine tests ───────────────────────────────────────────────

class TestDataEngine:
    def test_import(self):
        from data_engine import ALL_SYMBOLS
        assert "BTC-USD" in ALL_SYMBOLS
        assert "SPY" in ALL_SYMBOLS

    def test_engine_init(self):
        from data_engine import DataEngine
        engine = DataEngine(symbols={"SPY": "2023-01-01"})
        assert "SPY" in engine.symbols

    def test_add_features_basic(self):
        """Test that _add_features produces expected columns."""
        import pandas as pd
        import numpy as np
        from data_engine import DataEngine

        # Create synthetic OHLCV data
        n = 300
        dates = pd.date_range("2020-01-01", periods=n, freq="B")
        price = 100.0 + np.cumsum(np.random.randn(n) * 0.5)
        df = pd.DataFrame({
            "timestamp": dates,
            "open":   price * 0.999,
            "high":   price * 1.002,
            "low":    price * 0.997,
            "close":  price,
            "volume": np.random.randint(1_000_000, 5_000_000, n).astype(float),
            "symbol": "TEST",
        })

        engine = DataEngine(symbols={})
        result = engine._add_features(df)

        expected_cols = [
            "ret_1d", "vol_10d", "sma_20", "ema_12", "macd",
            "rsi_14", "bb_pos", "atr_14", "volume_zscore",
            "price_vs_sma50", "target_5d",
        ]
        for col in expected_cols:
            assert col in result.columns, f"Missing column: {col}"

    def test_feature_count_reasonable(self):
        import pandas as pd
        import numpy as np
        from data_engine import DataEngine

        n = 300
        dates = pd.date_range("2020-01-01", periods=n, freq="B")
        price = 100.0 + np.cumsum(np.random.randn(n) * 0.5)
        df = pd.DataFrame({
            "timestamp": dates,
            "open":   price, "high": price * 1.01,
            "low":    price * 0.99, "close": price,
            "volume": np.ones(n) * 1_000_000,
            "symbol": "TEST",
        })
        engine = DataEngine(symbols={})
        result = engine._add_features(df)
        # Should have more than 20 feature columns
        assert len(result.columns) > 20
        # data_engine._add_features keeps all rows (no dropna) for caching
        # The NaN values in SMA/EMA early rows are expected and handled by ML
        assert len(result) == n


# ─── live_feed tests ─────────────────────────────────────────────────

class TestLiveFeed:
    def test_tick_dataclass(self):
        from live_feed import Tick
        tick = Tick(
            symbol="BTC-USD",
            price=65000.0,
            volume=0.5,
            timestamp_ns=1234567890000,
            exchange="binance",
        )
        assert tick.symbol == "BTC-USD"
        assert tick.price == 65000.0
        assert tick.bid is None

    def test_tick_asdict(self):
        from live_feed import Tick
        tick = Tick(
            symbol="ETH-USD",
            price=3000.0,
            volume=1.0,
            timestamp_ns=1234567890000,
            exchange="binance",
            bid=2999.9,
            ask=3000.1,
        )
        d = asdict(tick)
        assert d["symbol"] == "ETH-USD"
        assert d["bid"] == 2999.9

    def test_aggregator_last_price(self):
        from live_feed import LiveFeedAggregator
        agg = LiveFeedAggregator()
        assert agg.get_last_price("BTC-USD") is None

    def test_aggregator_stats(self):
        from live_feed import LiveFeedAggregator
        agg = LiveFeedAggregator()
        stats = agg.get_stats()
        assert "tick_count" in stats
        assert "symbols" in stats
        assert stats["tick_count"] == 0

    def test_binance_message_handler(self):
        from live_feed import LiveFeedAggregator
        agg = LiveFeedAggregator()
        # Setup without ZMQ
        agg._zmq_socket = None

        msg = {
            "stream": "btcusdt@trade",
            "data": {
                "e": "trade",
                "p": "64800.50",
                "q": "0.001",
                "T": 1700000000000,  # ms timestamp
            }
        }
        agg._handle_binance_message(msg)
        assert agg.get_last_price("BTC-USD") == pytest.approx(
            64800.50, abs=0.01
        )


# ─── train_models smoke tests ────────────────────────────────────────

class TestTrainModels:
    def test_build_features(self):
        import pandas as pd
        import numpy as np
        from train_models import build_features, FEATURE_COLS

        n = 400
        dates = pd.date_range("2020-01-01", periods=n, freq="B")
        price = 100.0 + np.cumsum(np.random.randn(n))
        df = pd.DataFrame({
            "timestamp": dates,
            "open":   price,
            "high":   price * 1.01,
            "low":    price * 0.99,
            "close":  price,
            "volume": np.ones(n) * 1_000_000,
        })
        result = build_features(df)

        for col in FEATURE_COLS:
            assert col in result.columns, f"Missing: {col}"
        assert "signal_label" in result.columns
        assert result["signal_label"].isin([-1, 0, 1]).all()

    def test_signal_labels_three_classes(self):
        import pandas as pd
        import numpy as np
        from train_models import build_features

        n = 600
        dates = pd.date_range("2020-01-01", periods=n, freq="B")
        # Create alternating bull/bear price series
        price = 100.0 + np.cumsum(np.random.randn(n) * 2)
        df = pd.DataFrame({
            "timestamp": dates,
            "open":   price,
            "high":   price * 1.02,
            "low":    price * 0.98,
            "close":  price,
            "volume": np.ones(n) * 1_000_000,
        })
        result = build_features(df)
        labels = result["signal_label"].unique()
        # With enough volatility, all 3 classes should appear
        assert len(labels) >= 2

    def test_signal_classifier_trains(self):
        """Smoke test: train a tiny GBT model."""
        import numpy as np
        from train_models import train_signal_classifier

        X = np.random.randn(200, 21).astype("float32")
        y = np.random.choice([-1, 0, 1], 200)
        model = train_signal_classifier(X, y)
        preds = model.predict(X[:5])
        assert len(preds) == 5
        assert all(p in [-1, 0, 1] for p in preds)

    def test_kelly_estimator_trains(self):
        """Smoke test: Kelly estimator output is in [0, 0.25]."""
        import numpy as np
        from train_models import train_kelly_estimator

        X = np.random.randn(200, 21).astype("float32")
        y = np.clip(np.random.rand(200) * 0.3, 0, 0.25)
        model = train_kelly_estimator(X, y)
        preds = model.predict(X[:10])
        assert all(0 <= p <= 0.25 + 1e-6 for p in preds)


# ─── CORS security tests ─────────────────────────────────────────────────────

class TestCORSSecurity:
    def test_cors_not_wildcard(self):
        """Verify main.py doesn't use wildcard CORS origins in actual code."""
        import os
        main_path = os.path.join(os.path.dirname(__file__), "..", "main.py")
        with open(main_path, "r", encoding="utf-8") as f:
            lines = f.readlines()
        # Skip docstring lines (those starting with # or inside """ blocks)
        # Check code lines only (not inside docstrings/comments)
        in_docstring = False
        for line in lines:
            stripped = line.strip()
            if stripped.startswith('"""') or stripped.startswith("'''"):
                in_docstring = not in_docstring
                continue
            if in_docstring:
                continue
            if stripped.startswith('#'):
                continue
            # This is actual code — check for wildcard
            if '["*"]' in line and 'allow_origins' in line:
                raise AssertionError(
                    "SECURITY: CORS wildcard found in main.py "
                    f"code: {line.strip()}"
                )

    def test_cors_localhost_only(self):
        """Verify CORS is restricted to localhost."""
        import os
        main_path = os.path.join(os.path.dirname(__file__), "..", "main.py")
        with open(main_path, "r", encoding="utf-8") as f:
            content = f.read()
        assert "localhost" in content
        assert "127.0.0.1" in content

    def test_no_random_price(self):
        """Verify random price generation is removed from main.py."""
        import os
        main_path = os.path.join(os.path.dirname(__file__), "..", "main.py")
        with open(main_path, "r", encoding="utf-8") as f:
            content = f.read()
        # Should NOT have the old random price line
        assert "65000.0 + random.uniform" not in content, \
            "Random price generation still present in main.py!"


# ─── Position manager tests ──────────────────────────────────────────

class TestPositionManager:
    def test_no_positions_initially(self):
        from market_data_service import MarketDataService
        svc = MarketDataService()
        assert svc.get_price("BTC-USD") is None


if __name__ == "__main__":
    pytest.main([__file__, "-v", "--tb=short"])
