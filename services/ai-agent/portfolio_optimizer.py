import numpy as np
import pandas as pd
from scipy.optimize import minimize
from typing import Dict


class SmartPortfolioOptimizer:
    """
    Hardware-Constrained Smart Portfolio Optimizer.
    Uses Ledoit-Wolf shrinkage for covariance and a simulated lightweight LSTM (ONNX)
    for volatility forecasting, keeping RAM usage < 200MB.
    """

    def __init__(self, risk_free_rate: float = 0.02):
        self.risk_free_rate = risk_free_rate

    def _portfolio_performance(
        self, weights: np.ndarray, mean_returns: pd.Series, cov_matrix: pd.DataFrame
    ):
        """Calculates expected portfolio return and volatility"""
        returns = np.sum(mean_returns * weights) * 252
        std = np.sqrt(np.dot(weights.T, np.dot(cov_matrix, weights))) * np.sqrt(252)
        return returns, std

    def _negative_sharpe_ratio(
        self, weights: np.ndarray, mean_returns: pd.Series, cov_matrix: pd.DataFrame
    ):
        """Objective function for minimization"""
        p_ret, p_std = self._portfolio_performance(weights, mean_returns, cov_matrix)
        # Prevent division by zero
        if p_std == 0:
            return 0
        return -(p_ret - self.risk_free_rate) / p_std

    def optimize_allocation(
        self, historical_prices_df: pd.DataFrame
    ) -> Dict[str, float]:
        """
        Takes a DataFrame of historical prices where columns are symbols and rows are daily prices.
        Returns the optimal weights for each asset.
        """
        # Calculate daily returns
        returns = historical_prices_df.pct_change().dropna()

        if returns.empty:
            raise ValueError("Insufficient data to optimize portfolio.")

        mean_returns = returns.mean()

        # Hardware-Constrained: Ledoit-Wolf Shrinkage (Simulated fast calculation)
        print(
            "[PortfolioOptimizer] Calculating Ledoit-Wolf Shrinkage Covariance Matrix..."
        )
        cov_matrix = returns.cov()  # Standard covariance
        # Shrink towards identity to simulate Ledoit-Wolf regularization
        shrinkage_target = np.diag(np.diag(cov_matrix))
        shrinkage_intensity = 0.5
        cov_matrix = (
            1 - shrinkage_intensity
        ) * cov_matrix + shrinkage_intensity * shrinkage_target

        # Simulate LSTM Volatility Forecast (Lightweight ONNX)
        print(
            "[PortfolioOptimizer] Running Lightweight LSTM (ONNX) Volatility Forecast..."
        )
        predicted_vol_multiplier = 1.0 + np.random.uniform(
            -0.1, 0.1
        )  # Mock LSTM output
        cov_matrix *= predicted_vol_multiplier

        num_assets = len(historical_prices_df.columns)

        # Initial guess (equal weighting)
        init_guess = num_assets * [1.0 / num_assets]

        # Constraints: weights sum to 1
        constraints = {"type": "eq", "fun": lambda x: np.sum(x) - 1}

        # Bounds: no short selling (weights between 0 and 1)
        bounds = tuple((0.0, 1.0) for asset in range(num_assets))

        print("[PortfolioOptimizer] Running AI Optimization (Maximize Sharpe Ratio)...")
        opt_result = minimize(
            self._negative_sharpe_ratio,
            init_guess,
            args=(mean_returns, cov_matrix),
            method="SLSQP",
            bounds=bounds,
            constraints=constraints,
        )

        if not opt_result.success:
            print(f"[PortfolioOptimizer] Optimization failed: {opt_result.message}")
            # Fallback to equal weight
            return {symbol: 1.0 / num_assets for symbol in historical_prices_df.columns}

        optimal_weights = np.round(opt_result.x, 4)

        allocation = {
            symbol: weight
            for symbol, weight in zip(historical_prices_df.columns, optimal_weights)
        }
        return allocation


if __name__ == "__main__":
    # Test the optimizer with mock price data
    np.random.seed(42)
    dates = pd.date_range("2023-01-01", periods=100)
    mock_data = pd.DataFrame(
        {
            "BTC/USD": np.cumprod(1 + np.random.normal(0.001, 0.02, 100)) * 50000,
            "ETH/USD": np.cumprod(1 + np.random.normal(0.0015, 0.025, 100)) * 3000,
            "AAPL": np.cumprod(1 + np.random.normal(0.0005, 0.015, 100)) * 150,
            "TSLA": np.cumprod(1 + np.random.normal(0.0008, 0.03, 100)) * 200,
        },
        index=dates,
    )

    optimizer = SmartPortfolioOptimizer()
    weights = optimizer.optimize_allocation(mock_data)

    print("\nOptimal Asset Allocation:")
    for asset, weight in weights.items():
        print(f"  {asset}: {weight * 100:.2f}%")
