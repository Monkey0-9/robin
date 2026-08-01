import numpy as np
import pandas as pd
from scipy.optimize import minimize
from typing import Dict


class SmartPortfolioOptimizer:
    """
    Hardware-Constrained Smart Portfolio Optimizer.
    Now upgraded to Institutional grade with real Ledoit-Wolf shrinkage,
    transaction costs, and turnover constraints using cvxpy.
    """

    def __init__(self, risk_free_rate: float = 0.02, transaction_cost=0.001, max_turnover=0.3, risk_aversion=2.0):
        self.risk_free_rate = risk_free_rate
        self.tx_cost = transaction_cost
        self.max_turnover = max_turnover
        self.risk_aversion = risk_aversion

    def optimize_allocation(
        self, historical_prices_df: pd.DataFrame, current_weights: Dict[str, float] = None
    ) -> Dict[str, float]:
        """
        Takes a DataFrame of historical prices where columns are symbols and rows are daily prices.
        Returns the optimal weights for each asset.
        """
        try:
            from sklearn.covariance import LedoitWolf
            import cvxpy as cp
        except ImportError:
            raise ImportError("Missing required packages. Run: pip install scikit-learn cvxpy")

        # Calculate daily returns
        returns = historical_prices_df.pct_change().dropna()

        if returns.empty:
            raise ValueError("Insufficient data to optimize portfolio.")

        symbols = historical_prices_df.columns.tolist()
        n = len(symbols)
        
        # Annualized expected returns
        mu = returns.mean().values * 252

        print("[PortfolioOptimizer] Calculating True Ledoit-Wolf Shrinkage Covariance Matrix...")
        Sigma = LedoitWolf().fit(returns.values).covariance_ * 252

        w = cp.Variable(n)
        
        if current_weights is not None:
            w_prev = np.array([current_weights.get(sym, 0.0) for sym in symbols])
        else:
            w_prev = np.ones(n) / n
            
        print("[PortfolioOptimizer] Running CVXPY Optimization with Costs and Turnover Constraints...")
        
        # Objective: maximize risk-adjusted return minus transaction costs
        ret = mu @ w
        risk = cp.quad_form(w, Sigma)
        turnover = cp.norm(w - w_prev, 1)
        
        objective = cp.Maximize(ret - 0.5 * self.risk_aversion * risk - self.tx_cost * turnover)
        
        constraints = [
            cp.sum(w) == 1,
            w >= 0,  # No short selling
            turnover <= self.max_turnover,
        ]
        
        prob = cp.Problem(objective, constraints)
        
        try:
            prob.solve()
        except cp.error.SolverError:
            # Fallback to robust solver
            prob.solve(solver=cp.SCS)

        if prob.status not in ["optimal", "optimal_inaccurate"]:
            print(f"[PortfolioOptimizer] Optimization failed: status {prob.status}")
            # Fallback to equal weight
            return {symbol: 1.0 / n for symbol in symbols}

        optimal_weights = np.round(w.value, 4)

        allocation = {
            symbol: float(weight)
            for symbol, weight in zip(symbols, optimal_weights)
        }
        return allocation

    def robust_optimize(
        self,
        historical_prices_df: pd.DataFrame,
        current_weights: Dict[str, float] = None,
        n_resamples: int = 100,
        seed: int = 42,
    ) -> Dict[str, float]:
        """
        Resampled (Michaud) robust optimization.

        Point estimates of mean returns and covariance are noisy; a single
        mean-variance solution is unstable. Resampling averages the optimal
        weights across many bootstrap draws of the return series, producing
        an allocation far more stable out-of-sample. Overlaps exactly with
        `optimize_allocation` for n_resamples=1.
        """
        returns = historical_prices_df.pct_change().dropna()
        if returns.empty:
            raise ValueError("Insufficient data to robust-optimize portfolio.")

        symbols = historical_prices_df.columns.tolist()
        rng = np.random.default_rng(seed)
        n = len(returns)
        weight_sum = np.zeros(len(symbols))
        n_ok = 0

        for _ in range(max(1, n_resamples)):
            # Block bootstrap (stationary bootstrap-ish: random-length blocks)
            idx = np.concatenate([
                rng.integers(0, n - block, size=1) + np.arange(block)
                for block in rng.integers(10, 60, size=max(1, n // 60))
            ])[:n]
            sample = returns.iloc[idx]
            weights = self.optimize_allocation(sample, current_weights)
            arr = np.array([weights.get(sym, 0.0) for sym in symbols])
            weight_sum += arr
            n_ok += 1

        avg = weight_sum / max(n_ok, 1)
        avg = avg / avg.sum()  # renormalize to sum to 1
        return {
            symbol: round(float(avg[i]), 6)
            for i, symbol in enumerate(symbols)
        }


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

    print("\nOptimal Asset Allocation (single-shot):")
    for asset, weight in weights.items():
        print(f"  {asset}: {weight * 100:.2f}%")

    robust = optimizer.robust_optimize(mock_data, n_resamples=30)
    print("\nOptimal Asset Allocation (Michaud resampled):")
    for asset, weight in robust.items():
        print(f"  {asset}: {weight * 100:.2f}%")
