import math
import logging

logging.basicConfig(level=logging.INFO)

class MarketImpactModel:
    """
    Implements the Square-Root Law of Market Impact (Almgren et al.)
    Impact = gamma * sigma * (Order_Volume / Daily_Volume)^0.5
    """
    def __init__(self, gamma: float = 0.5):
        self.gamma = gamma

    def estimate_impact(self, order_size: float, daily_volume: float, volatility: float, price: float) -> dict:
        if daily_volume <= 0 or order_size <= 0:
            return {"impact_price": price, "slippage_bps": 0.0, "dollar_impact": 0.0}

        participation_rate = order_size / daily_volume
        pct_impact = self.gamma * volatility * math.sqrt(participation_rate)
        
        dollar_impact = price * pct_impact
        impact_price = price + dollar_impact
        slippage_bps = pct_impact * 10000.0

        return {
            "impact_price": impact_price,
            "slippage_bps": slippage_bps,
            "dollar_impact": dollar_impact,
            "participation_rate": participation_rate
        }

if __name__ == "__main__":
    impact_model = MarketImpactModel()
    # Estimate impact for buying 100 BTC when ADV is 10,000 BTC and daily volatility is 3%
    res = impact_model.estimate_impact(order_size=100.0, daily_volume=10000.0, volatility=0.03, price=60000.0)
    logging.info("--- Almgren-Chriss Market Impact Model ---")
    for k, v in res.items():
        logging.info(f"{k:20s}: {v:.4f}")
