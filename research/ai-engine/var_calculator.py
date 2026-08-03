import numpy as np
import pandas as pd
from scipy.stats import norm
import logging

logging.basicConfig(level=logging.INFO)

class VaRCalculator:
    def __init__(self, confidence_level=0.99, horizon_days=1):
        self.confidence_level = confidence_level
        self.horizon_days = horizon_days
        self.alpha = 1 - confidence_level

    def historical_var(self, returns: pd.Series, portfolio_value: float) -> float:
        """Calculate Value at Risk using the Historical Simulation method."""
        if len(returns) == 0:
            return 0.0
        # Sort returns and find the percentile
        percentile_val = np.percentile(returns, self.alpha * 100)
        var = portfolio_value * percentile_val * np.sqrt(self.horizon_days)
        # VaR is typically expressed as a positive number (a loss amount)
        return abs(var) if var < 0 else 0.0

    def parametric_var(self, returns: pd.Series, portfolio_value: float) -> float:
        """Calculate Value at Risk using the Variance-Covariance (Parametric) method."""
        if len(returns) == 0:
            return 0.0
        mu = np.mean(returns)
        sigma = np.std(returns)
        
        # Z-score for the given confidence level
        z_score = norm.ppf(self.alpha)
        
        var = portfolio_value * (mu - z_score * sigma) * np.sqrt(self.horizon_days)
        return abs(var)

    def monte_carlo_var(self, returns: pd.Series, portfolio_value: float, simulations=10000) -> float:
        """Calculate Value at Risk using Monte Carlo Simulation."""
        if len(returns) == 0:
            return 0.0
        mu = np.mean(returns)
        sigma = np.std(returns)
        
        # Generate random scenarios assuming normal distribution
        scenarios = np.random.normal(mu, sigma, simulations)
        
        percentile_val = np.percentile(scenarios, self.alpha * 100)
        var = portfolio_value * percentile_val * np.sqrt(self.horizon_days)
        return abs(var) if var < 0 else 0.0

    def compute_all(self, returns: pd.Series, portfolio_value: float):
        hist_var = self.historical_var(returns, portfolio_value)
        param_var = self.parametric_var(returns, portfolio_value)
        mc_var = self.monte_carlo_var(returns, portfolio_value)
        
        logging.info(f"VaR (99%, 1-day) - Portfolio Value: ${portfolio_value:,.2f}")
        logging.info(f" Historical:   ${hist_var:,.2f}")
        logging.info(f" Parametric:   ${param_var:,.2f}")
        logging.info(f" Monte Carlo:  ${mc_var:,.2f}")
        
        return {
            "historical": hist_var,
            "parametric": param_var,
            "monte_carlo": mc_var
        }

if __name__ == "__main__":
    # Generate dummy returns for testing
    np.random.seed(42)
    # 252 trading days of returns (mean=0.0005, std=0.015)
    dummy_returns = pd.Series(np.random.normal(0.0005, 0.015, 252))
    portfolio_val = 1_000_000.0  # $1 Million
    
    calculator = VaRCalculator(confidence_level=0.99, horizon_days=1)
    calculator.compute_all(dummy_returns, portfolio_val)
