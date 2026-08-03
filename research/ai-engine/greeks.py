import math
from scipy.stats import norm
import logging

logging.basicConfig(level=logging.INFO)

class BlackScholesGreeks:
    """
    Calculates European Option Prices and Greeks (Delta, Gamma, Vega, Theta, Rho)
    """
    @staticmethod
    def calculate(S: float, K: float, T: float, r: float, sigma: float, option_type: str = "call"):
        if T <= 0 or sigma <= 0:
            return {"price": max(0.0, S - K if option_type == "call" else K - S), "delta": 0.0, "gamma": 0.0, "vega": 0.0, "theta": 0.0}

        d1 = (math.log(S / K) + (r + 0.5 * sigma ** 2) * T) / (sigma * math.sqrt(T))
        d2 = d1 - sigma * math.sqrt(T)

        pdf_d1 = norm.pdf(d1)

        if option_type.lower() == "call":
            price = S * norm.cdf(d1) - K * math.exp(-r * T) * norm.cdf(d2)
            delta = norm.cdf(d1)
            theta = (- (S * pdf_d1 * sigma) / (2 * math.sqrt(T)) 
                     - r * K * math.exp(-r * T) * norm.cdf(d2))
        else:
            price = K * math.exp(-r * T) * norm.cdf(-d2) - S * norm.cdf(-d1)
            delta = norm.cdf(d1) - 1.0
            theta = (- (S * pdf_d1 * sigma) / (2 * math.sqrt(T)) 
                     + r * K * math.exp(-r * T) * norm.cdf(-d2))

        gamma = pdf_d1 / (S * sigma * math.sqrt(T))
        vega = S * pdf_d1 * math.sqrt(T) / 100.0  # Normalized for 1% vol change

        return {
            "price": price,
            "delta": delta,
            "gamma": gamma,
            "vega": vega,
            "theta": theta / 365.0  # Daily theta decay
        }

if __name__ == "__main__":
    # Test case: BTC option at $60,000 spot, $60,000 strike, 30 days to expiry, 5% interest rate, 60% IV
    greeks = BlackScholesGreeks.calculate(S=60000.0, K=60000.0, T=30/365.0, r=0.05, sigma=0.60, option_type="call")
    logging.info("--- Black-Scholes Options Greeks ---")
    for key, val in greeks.items():
        logging.info(f"{key.capitalize():8s}: {val:.4f}")
