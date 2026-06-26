/ services/kdb-storage/correlation_matrix.q
/ Real-time Cross-Asset Pearson Correlation calculation in KDB+/q.
/ Computes rolling covariance and correlation between price feeds.
/
/ Python Implementation Plan (correlation_engine.py in research/strategy-engine/):
/ ----------------------------------------------------------------------------
/ A CorrelationEngine class in Python that mirrors this q logic but uses
/ Exponential Weighted Moving Average (EWMA) for real-time updates from
/ streaming tick data. Design:
/
/  1. On each tick, update EWMA means for each instrument:
/        μ_i(t) = λ * μ_i(t-1) + (1-λ) * price_i(t)
/
/  2. Update EWMA variances and covariances:
/        σ²_i(t) = λ * σ²_i(t-1) + (1-λ) * (price_i - μ_i)²
/        Cov_ij(t) = λ * Cov_ij(t-1) + (1-λ) * (price_i - μ_i)(price_j - μ_j)
/
/  3. Correlation derived on-demand:
/        ρ_ij = Cov_ij / (σ_i * σ_j)
/
/  4. Configurable λ (decay factor) and lookback window.
/
/  This avoids storing full price histories while still giving more weight
/  to recent observations — suitable for high-frequency environments.
/ ----------------------------------------------------------------------------

/ Generate mock tick history data
prices:([sym:`symbol; time:`timespan] price:`float);

insert[`prices]; (`AAPL; 09:30:00.000; 180.5);
insert[`prices]; (`AAPL; 09:31:00.000; 181.2);
insert[`prices]; (`AAPL; 09:32:00.000; 180.9);
insert[`prices]; (`MSFT; 09:30:00.000; 415.2);
insert[`prices]; (`MSFT; 09:31:00.000; 416.0);
insert[`prices]; (`MSFT; 09:32:00.000; 415.5);

/ Calculate returns for a symbol
getReturns:{[s]
    p: exec price from prices where sym=s;
    deltas[p] % p
 };

/ Pearson correlation between two lists
correlation:{[x;y]
    cov: avg[x * y] - avg[x] * avg[y];
    stdX: dev x;
    stdY: dev y;
    $[0.0 = stdX * stdY; 0.0; cov % (stdX * stdY)]
 };

/ Compute correlation matrix between AAPL and MSFT
calcCorrelationMatrix:{[]
    rAAPL: getReturns[`AAPL];
    rMSFT: getReturns[`MSFT];
    corr: correlation[rAAPL; rMSFT];
    show "Real-Time Correlation Matrix (AAPL vs MSFT):";
    show corr;
    corr
 };

calcCorrelationMatrix[];
exit 0;
