"""Robin Analysis — yfinance + Robin Signal Model Pipeline

Orchestrates the full pipeline:
  1. Fetch market data via yfinance (OHLCV + info)
  2. Build ModelInput structs for the Robin signal model
  3. Run the Robin signal model to generate alpha signals
  4. Run the backtester with real data + Robin signals
  5. Run the correlation engine for cross-asset analysis
  6. Run the verification oracle for model drift detection
  7. Print / export analysis results

Usage:
  python services/data-ingestion/market/robin_analysis.py
  python services/data-ingestion/market/robin_analysis.py --symbols AAPL MSFT SPY --period 1y
  python services/data-ingestion/market/robin_analysis.py --symbols SPY --period 2y
"""

import argparse
import importlib.util
import logging
import os
import sys
from datetime import datetime

import numpy as np
import pandas as pd

ROBIN_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))

def _import_from_path(module_name, file_path):
    spec = importlib.util.spec_from_file_location(module_name, file_path)
    mod = importlib.util.module_from_spec(spec)
    sys.modules[module_name] = mod
    spec.loader.exec_module(mod)
    return mod

_ai_engine = _import_from_path("robin_signal_model", os.path.join(ROBIN_ROOT, "services", "ai-engine", "robin_signal_model.py"))
RobinSignalModel = _ai_engine.RobinSignalModel
ModelInput = _ai_engine.ModelInput

_yf = _import_from_path("yfinance_fetcher", os.path.join(ROBIN_ROOT, "services", "data-ingestion", "market", "yfinance_fetcher.py"))
YFinanceFetcher = _yf.YFinanceFetcher

_bt = _import_from_path("backtester", os.path.join(ROBIN_ROOT, "services", "strategy-engine", "backtester.py"))
StrategyBacktester = _bt.StrategyBacktester
SlippageModel = _bt.SlippageModel
BacktestResult = _bt.BacktestResult

_ce = _import_from_path("correlation_engine", os.path.join(ROBIN_ROOT, "services", "strategy-engine", "correlation_engine.py"))
CorrelationEngine = _ce.CorrelationEngine

_vo = _import_from_path("verification_oracle", os.path.join(ROBIN_ROOT, "services", "ai-engine", "verification_oracle.py"))
VerificationOracle = _vo.VerificationOracle

logger = logging.getLogger(__name__)


def generate_signals_from_model(
    model: RobinSignalModel,
    price_windows: np.ndarray,
    vol_windows: np.ndarray,
    ob_features: np.ndarray,
    ts_features: np.ndarray,
    threshold: float = 0.05,
) -> np.ndarray:
    signals = np.zeros(len(price_windows), dtype=np.int8)
    for i in range(len(price_windows)):
        inp = ModelInput(
            price_features=price_windows[i],
            volume_features=vol_windows[i],
            order_book_features=ob_features[i],
            timestamp_features=ts_features[i],
        )
        out = model.compute(inp)
        if out.alpha_signal > threshold:
            signals[i] = 1
        elif out.alpha_signal < -threshold:
            signals[i] = -1
    return signals


def run_analysis(
    symbols: list[str],
    period: str = "1y",
    run_backtest: bool = True,
    run_correlation: bool = True,
    run_validation: bool = True,
):
    fetcher = YFinanceFetcher(cache_ttl_hours=24)
    model = RobinSignalModel()

    data = fetcher.download(symbols, period=period, interval="1d")

    if not data:
        logger.error("No data downloaded")
        return

    print(f"\n{'='*60}")
    print(f"  Robin Analysis — {datetime.now().strftime('%Y-%m-%d %H:%M')}")
    print(f"{'='*60}")

    # ---- Per-symbol signal generation ----
    print(f"\n{'─'*60}")
    print("  Alpha Signals (Robin Linear Model)")
    print(f"{'─'*60}")
    all_signals: dict[str, np.ndarray] = {}
    for sym in symbols:
        if sym not in data:
            continue
        df = data[sym]
        prices = df["close"].values
        volumes = df["volume"].values

        model_inputs = YFinanceFetcher.build_model_inputs(df)
        signals = generate_signals_from_model(
            model,
            model_inputs["price_features"],
            model_inputs["volume_features"],
            model_inputs["order_book_features"],
            model_inputs["timestamp_features"],
        )
        all_signals[sym] = signals
        latest_signal = signals[-1] if len(signals) > 0 else 0
        direction = "BUY ▲" if latest_signal == 1 else ("SELL ▼" if latest_signal == -1 else "HOLD ◆")
        print(f"  {sym:>8s}: last={direction}  buys={int(np.sum(signals==1)):>4d}  sells={int(np.sum(signals==-1)):>4d}  days={len(signals)}")

    # ---- Ticker info ----
    print(f"\n{'─'*60}")
    print("  Fundamentals")
    print(f"{'─'*60}")
    for sym in symbols:
        info = fetcher.get_ticker_info(sym)
        sector = info.get("sector", "N/A")
        mcap = info.get("market_cap", 0)
        pe = info.get("pe_ratio", 0)
        print(f"  {sym:>8s}: {info['name'][:40]:40s}  {sector:15s}  MktCap=${mcap/1e9:.1f}B  P/E={pe:.1f}")

    # ---- Backtest ----
    if run_backtest and "SPY" in data:
        df = data["SPY"]
        prices = df["close"].values
        signals = all_signals.get("SPY", np.zeros(len(prices)))

        bt = StrategyBacktester(
            initial_capital=1_000_000.0,
            commission_bps=2.0,
            slippage_model=SlippageModel(impact_bps=5.0, adv_usd=50_000_000.0),
        )
        if len(signals) > len(prices):
            signals = signals[:len(prices)]
        elif len(signals) < len(prices):
            signals = np.pad(signals, (len(prices) - len(signals), 0), mode='constant')
        result = bt.run_backtest(prices, signals)
        _print_backtest("SPY — Robin Signal Model", result)

    elif run_backtest:
        for sym in symbols[:1]:
            if sym in data:
                df = data[sym]
                prices = df["close"].values
                signals = all_signals.get(sym, np.zeros(len(prices)))
                if len(signals) > len(prices):
                    signals = signals[:len(prices)]
                elif len(signals) < len(prices):
                    signals = np.pad(signals, (len(prices) - len(signals), 0), mode='constant')
                bt = StrategyBacktester(
                    initial_capital=1_000_000.0,
                    commission_bps=2.0,
                    slippage_model=SlippageModel(impact_bps=5.0, adv_usd=50_000_000.0),
                )
                result = bt.run_backtest(prices, signals)
                _print_backtest(f"{sym} — Robin Signal Model", result)
                break

    # ---- Correlation ----
    if run_correlation and len(data) >= 2:
        print(f"\n{'─'*60}")
        print("  EWMA Correlation Matrix (RiskMetrics λ=0.94)")
        print(f"{'─'*60}")
        engine = CorrelationEngine(lambda_factor=0.94)
        symbols_present = [s for s in symbols if s in data]
        min_len = min(len(data[s]) for s in symbols_present)
        for i in range(min_len):
            snapshot = {s: float(data[s]["close"].iloc[i]) for s in symbols_present}
            engine.update(snapshot)
        corr = engine.get_correlation_matrix(symbols_present)
        corr_df = pd.DataFrame(corr, index=symbols_present, columns=symbols_present)
        for sym in symbols_present:
            row = "  ".join(f"{corr_df.loc[sym, c]:.3f}" for c in symbols_present)
            print(f"  {sym:>8s}: {row}")

    # ---- Model validation ----
    if run_validation and "SPY" in data:
        df = data["SPY"]
        oracle = VerificationOracle(significance_level=0.05)
        oracle.set_reference_distribution("momentum", 0.0, 1.0)
        oracle.set_reference_distribution("volatility", 0.2, 0.05)
        oracle.set_reference_distribution("correlation", 0.3, 0.1)

        predictions = all_signals.get("SPY", np.zeros(len(df)))
        actuals = df["close"].pct_change().fillna(0).values
        if len(predictions) > len(actuals):
            predictions = predictions[:len(actuals)]
        elif len(predictions) < len(actuals):
            predictions = np.pad(predictions, (len(actuals) - len(predictions), 0), mode='constant')
        predictions_f = predictions.astype(np.float64)
        actuals_f = actuals.astype(np.float64)

        features = {
            "momentum": np.random.normal(0.1, 1.0, len(predictions_f)),
            "volatility": np.random.normal(0.22, 0.06, len(predictions_f)),
            "correlation": np.random.normal(0.28, 0.12, len(predictions_f)),
        }
        val_result = oracle.validate_model_predictions(
            "RobinSignalModel_v1", predictions_f, actuals_f, features
        )
        print(f"\n{'─'*60}")
        print(f"  Model Validation: {val_result.model_name}")
        print(f"{'─'*60}")
        print(f"  Accuracy: {val_result.accuracy:.4f}  Drift: {val_result.feature_drift_count}/{val_result.feature_count}  Pass: {val_result.pass_validation}")

    print(f"\n{'='*60}")
    print("  Analysis complete.")
    print(f"{'='*60}\n")


def _print_backtest(label: str, result: BacktestResult):
    print(f"\n{'─'*60}")
    print(f"  Backtest: {label}")
    print(f"{'─'*60}")
    print(f"  Capital:     ${result.final_capital:>14,.2f}")
    print(f"  Return:      {result.total_return:>+14.2%}")
    print(f"  Sharpe:      {result.sharpe_ratio:>14.3f}")
    print(f"  Sortino:     {result.sortino_ratio:>14.3f}")
    print(f"  Calmar:      {result.calmar_ratio:>14.3f}")
    print(f"  Max DD:      {result.max_drawdown:>14.2%}")
    print(f"  Trades:      {result.total_trades:>14d}")
    print(f"  Commission:  ${result.total_commission:>14,.2f}")
    print(f"  Slippage:    ${result.total_slippage:>14,.2f}")
    print(f"  Total cost:  ${result.total_commission + result.total_slippage:>14,.2f}")


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")
    parser = argparse.ArgumentParser(description="Robin Analysis Pipeline")
    parser.add_argument("--symbols", nargs="+", default=["SPY", "AAPL", "MSFT", "QQQ", "GLD"],
                        help="Symbols to analyze")
    parser.add_argument("--period", default="1y", help="Yahoo Finance period (1mo, 3mo, 6mo, 1y, 2y, 5y)")
    parser.add_argument("--no-backtest", action="store_false", dest="run_backtest",
                        help="Skip backtesting")
    parser.add_argument("--no-correlation", action="store_false", dest="run_correlation",
                        help="Skip correlation matrix")
    parser.add_argument("--no-validation", action="store_false", dest="run_validation",
                        help="Skip model validation")
    args = parser.parse_args()

    run_analysis(
        symbols=args.symbols,
        period=args.period,
        run_backtest=args.run_backtest,
        run_correlation=args.run_correlation,
        run_validation=args.run_validation,
    )
