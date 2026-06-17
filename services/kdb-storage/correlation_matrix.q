/ services/kdb-storage/correlation_matrix.q
/ Real-time Cross-Asset Pearson Correlation calculation in KDB+/q.
/ Computes rolling covariance and correlation between price feeds.

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
