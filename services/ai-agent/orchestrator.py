import asyncio
from agents import MarketRegimeDetector, NewsSentimentAnalyst, TradeSignalGenerator


class HardwareConstrainedOrchestrator:
    """
    Core AI Orchestrator enforcing 4GB VRAM limits via sequential execution.
    Only ONE large language model is loaded into memory at any given time.
    """

    def __init__(self):
        self.regime_agent = MarketRegimeDetector()
        self.sentiment_agent = NewsSentimentAnalyst()
        self.signal_agent = TradeSignalGenerator()

        # State
        self.current_regime = "Unknown"
        self.current_sentiment = 0.0
        self.latest_signals = []

        # Hard-coded Risk Guardian Rules (Cannot be overridden by AI)
        self.MAX_DAILY_LOSS_PCT = -0.02
        self.MAX_POSITION_SIZE_PCT = 0.05
        self.MAX_VIX = 40.0

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
        self, mock_market_summary: str, mock_headlines: list, current_price: float
    ):
        """
        Executes the AI pipeline sequentially to stay under 4GB VRAM limit.
        """
        print("\n--- Starting Sequential AI Pipeline Cycle ---")

        # 1. Market Regime Detection
        self.regime_agent.load()
        self.current_regime = self.regime_agent.detect_regime(mock_market_summary)
        self.regime_agent.unload()

        # 2. News Sentiment Analysis (CPU ONNX)
        self.sentiment_agent.load()
        self.current_sentiment = self.sentiment_agent.analyze_headlines(mock_headlines)
        self.sentiment_agent.unload()

        # 3. Trade Signal Generation
        self.signal_agent.load()
        signal = self.signal_agent.generate_signal(
            self.current_regime, self.current_sentiment, current_price
        )
        self.signal_agent.unload()

        if signal["action"] != "HOLD":
            self.latest_signals.append(signal)

        print(f"--- Cycle Complete | Signal: {signal['action']} ---")
        return signal


if __name__ == "__main__":
    orchestrator = HardwareConstrainedOrchestrator()

    # Simulate a safe market environment
    is_safe = orchestrator.risk_guardian_check(simulated_vix=18.5, daily_pnl_pct=0.01)

    if is_safe:
        asyncio.run(
            orchestrator.execute_sequential_pipeline(
                mock_market_summary="Market seeing massive growth and surge in tech stocks.",
                mock_headlines=["FDA approval granted", "Earnings beat expectations"],
                current_price=65000.0,
            )
        )
