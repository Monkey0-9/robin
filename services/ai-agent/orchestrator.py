import asyncio
import importlib.util
import logging
import os
import sys
from typing import Optional

from agents import MarketRegimeDetector, NewsSentimentAnalyst, TradeSignalGenerator

logger = logging.getLogger("orchestrator")


class HardwareConstrainedOrchestrator:
    """
    Core AI Orchestrator enforcing 4GB VRAM limits via sequential execution.
    Only ONE large language model is loaded into memory at any given time.

    Regime detection now uses a Gaussian HMM (MarketRegimeDetector) — statistically
    rigorous and <1ms latency — instead of an LLM prompt.
    """

    def __init__(self, use_llm_regime: bool = False):
        self.sentiment_agent = NewsSentimentAnalyst()
        self.signal_agent = TradeSignalGenerator()

        # HMM regime detector — fit lazily on first price history
        self.regime_hmm = MarketRegimeDetector()
        self._price_history: list[float] = []
        self._volume_history: list[float] = []
        self._hmm_fitted = False

        # State
        self.current_regime = "Range"
        self.current_sentiment = 0.0
        self.latest_signals = []

        # Hard-coded Risk Guardian Rules (Cannot be overridden by AI)
        self.MAX_DAILY_LOSS_PCT = -0.02
        self.MAX_POSITION_SIZE_PCT = 0.05
        self.MAX_VIX = 40.0

    # ─── Price history feed (for HMM regime features) ─────────────────────────

    def feed_price_history(self, prices: list[float], volumes: Optional[list[float]] = None):
        """Provide recent price/volume history so the HMM can be fit once."""
        if not prices:
            return
        self._price_history = list(prices)[-500:]
        if volumes:
            self._volume_history = list(volumes)[-500:]
        if self.regime_hmm is not None and not self._hmm_fitted and len(self._price_history) >= 40:
            try:
                import numpy as np
                prices_arr = np.asarray(self._price_history, dtype=np.float64)
                returns = np.diff(np.log(np.maximum(prices_arr, 1e-9)))
                vol_arr = np.asarray(self._volume_history or [1.0] * len(returns), dtype=np.float64)
                self.regime_hmm.fit(returns, vol_arr)
                self._hmm_fitted = True
                logger.info("[Orchestrator] HMM regime model fitted on %d observations", len(returns))
            except Exception as e:
                logger.warning("[Orchestrator] HMM fit failed (%s) — using defaults", e)

    def _hmm_predict(self, current_price: float) -> str:
        """Predict regime from trailing price/volume history. Sub-ms."""
        if self.regime_hmm is None or not self._hmm_fitted:
            return "Range"
        try:
            self._price_history = (self._price_history + [current_price])[-500:]
            
            import numpy as np
            prices_arr = np.asarray(self._price_history, dtype=np.float64)
            returns = np.diff(np.log(np.maximum(prices_arr, 1e-9))).tolist()
            
            # Use self._volume_history properly padded
            volumes = self._volume_history.copy()
            if len(volumes) < len(self._price_history):
                volumes = [1.0] * (len(self._price_history) - len(volumes)) + volumes
            
            # Ensure volumes matches returns length
            volumes_input = volumes[-len(returns):] if len(returns) > 0 else []
            
            return self.regime_hmm.detect_regime(returns, volumes_input)
        except Exception as e:
            logger.warning("[Orchestrator] HMM predict failed (%s) — Range", e)
            return "Range"

    # ─── Risk guardian ────────────────────────────────────────────────────────

    def risk_guardian_check(self, simulated_vix: float, daily_pnl_pct: float) -> bool:
        """Defense-in-depth: Rule-based checks before any AI execution."""
        if simulated_vix > self.MAX_VIX:
            print("[RISK GUARDIAN] VIX > 40. Crisis mode active. AI Trading halted.")
            return False
        if daily_pnl_pct <= self.MAX_DAILY_LOSS_PCT:
            print("[RISK GUARDIAN] Max daily loss (2%) reached. AI Trading halted.")
            return False
        return True

    async def execute_sequential_pipeline(
        self,
        market_summary: str = "",
        headlines: Optional[list] = None,
        current_price: float = 0.0,
        price_history: Optional[list] = None,
        volume_history: Optional[list] = None,
    ):
        """
        Executes the AI pipeline sequentially to stay under 4GB VRAM limit.

        Steps:
          1. Regime detection — HMM (<1ms) or optional LLM fallback
          2. News sentiment — FinBERT ONNX (CPU)
          3. Trade signal generation — regime × sentiment matrix
        """
        headlines = headlines or []
        print("\n--- Starting Sequential AI Pipeline Cycle ---")

        # 1. Market Regime Detection
        if price_history:
            self.feed_price_history(price_history, volume_history)
            
        self.regime_hmm.load()
        if self.regime_hmm.is_loaded:
            self.current_regime = self._hmm_predict(current_price)
        else:
            self.current_regime = "Range"
        self.regime_hmm.unload()

        # 2. News Sentiment Analysis (CPU ONNX — FinBERT)
        self.sentiment_agent.load()
        self.current_sentiment = self.sentiment_agent.analyze_headlines(headlines)
        self.sentiment_agent.unload()

        # 3. Trade Signal Generation
        self.signal_agent.load()
        signal = self.signal_agent.generate_signal(
            self.current_regime, self.current_sentiment, current_price
        )
        self.signal_agent.unload()

        if signal["action"] != "HOLD":
            self.latest_signals.append(signal)

        print(f"--- Cycle Complete | Signal: {signal['action']} "
              f"(regime={self.current_regime}, sentiment={self.current_sentiment:+.2f}) ---")
        return signal


if __name__ == "__main__":
    import numpy as np

    logging.basicConfig(level=logging.INFO)
    orchestrator = HardwareConstrainedOrchestrator()

    # Simulate a safe market environment
    is_safe = orchestrator.risk_guardian_check(simulated_vix=18.5, daily_pnl_pct=0.01)

    if is_safe:
        np.random.seed(42)
        hist = 50000.0 * np.exp(np.cumsum(np.random.normal(0.0005, 0.015, 200)))
        vols = np.abs(np.random.normal(1e6, 3e5, 200)).tolist()
        asyncio.run(
            orchestrator.execute_sequential_pipeline(
                market_summary="Market seeing massive growth and surge in tech stocks.",
                headlines=["FDA approval granted", "Earnings beat expectations"],
                current_price=float(hist[-1]),
                price_history=hist.tolist(),
                volume_history=vols,
            )
        )
