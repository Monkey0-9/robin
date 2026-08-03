import numpy as np
import pandas as pd
from sklearn.covariance import LedoitWolf
import logging

logging.basicConfig(level=logging.INFO)

class PortfolioOptimizer:
    def __init__(self, risk_aversion=1.0, transaction_cost=0.005):
        self.risk_aversion = risk_aversion
        self.transaction_cost = transaction_cost

    def optimize(self, expected_returns: np.ndarray, returns_history: pd.DataFrame, current_weights: np.ndarray) -> np.ndarray:
        """
        Mean-Variance Optimization with Ledoit-Wolf Shrinkage and Transaction Costs
        """
        logging.info("Running Portfolio Optimization with Ledoit-Wolf shrinkage")
        
        if returns_history.empty or len(expected_returns) == 0:
            return current_weights
            
        # Real Ledoit-Wolf Shrinkage
        lw = LedoitWolf()
        cov_matrix = lw.fit(returns_history).covariance_
        
        # In a real setup, we'd use cvxpy to solve:
        # maximize: w.T * mu - lambda * w.T * cov * w - tc * ||w - w_curr||_1
        # subject to: sum(w) = 1, w >= 0
        
        # Here we do a simplified unconstrained mean-variance for the stub
        try:
            inv_cov = np.linalg.inv(cov_matrix)
            w_opt = inv_cov @ expected_returns
            
            # Normalize
            if np.sum(np.abs(w_opt)) > 0:
                w_opt = w_opt / np.sum(np.abs(w_opt))
            else:
                w_opt = np.ones_like(expected_returns) / len(expected_returns)
                
            # Apply transaction cost heuristic (only trade if change > threshold)
            delta_w = np.abs(w_opt - current_weights)
            threshold = 0.05
            
            final_weights = np.where(delta_w > threshold, w_opt, current_weights)
            return final_weights
            
        except np.linalg.LinAlgError:
            logging.error("Covariance matrix inversion failed, returning current weights")
            return current_weights

if __name__ == "__main__":
    np.random.seed(42)
    # 5 assets, 100 days
    historical = pd.DataFrame(np.random.normal(0.0005, 0.02, (100, 5)))
    mu = np.array([0.001, 0.002, -0.001, 0.005, 0.000])
    curr_w = np.array([0.2, 0.2, 0.2, 0.2, 0.2])
    
    optimizer = PortfolioOptimizer()
    new_w = optimizer.optimize(mu, historical, curr_w)
    logging.info(f"Optimized Weights: {new_w}")
