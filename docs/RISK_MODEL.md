# Robin Quantitative Trading Platform — Pre-Trade Risk Model & Engine
**Document ID:** SPEC-RISK-202608-01  
**Classification:** Institutional Risk Architecture  

---

## 1. 7 Hard Pre-Trade Risk Blocks (Executed in Sequence)

```
INBOUND ORDER
      │
      ▼
[1. Kill Switch Check] ──────────(Engaged?)─────────► REJECT: "KILL_SWITCH_ACTIVE"
      │
      ▼
[2. Circuit Breaker] ────────────(Tripped?)─────────► REJECT: "CIRCUIT_BREAKER_TRIPPED"
      │
      ▼
[3. Fat-Finger Limits] ──────────(Price/Qty OOB?)───► REJECT: "FAT_FINGER_VIOLATION"
      │
      ▼
[4. Credit / Notional Limit] ────(Limit Exceeded?)──► REJECT: "CREDIT_LIMIT_BREACH"
      │
      ▼
[5. Restricted Symbol List] ─────(Symbol Banned?)───► REJECT: "RESTRICTED_SYMBOL"
      │
      ▼
[6. Duplicate Order ID] ─────────(ID Seen <1s?)─────► REJECT: "DUPLICATE_ORDER_ID"
      │
      ▼
[7. Price Collar (±5% NBBO)] ────(Outside Band?)────► REJECT: "PRICE_COLLAR_BREACH"
      │
      ▼
OUTBOUND TO MATCHING (SPSC RING)
```

---

## 2. 2 Soft Pre-Trade Blocks
* **Block 8: Position Limit:** Maximum absolute position per instrument across all sub-accounts.
* **Block 9: Velocity Limit:** Maximum orders allowed per second (default: 100 orders/s per account).

---

## 3. Concurrency & Optimistic Position Reservation
To eliminate race conditions across concurrent order threads:
1. **CHECK & RESERVE:** The gate atomically reserves needed credit/margin before dispatching the order.
2. **MATCH / EXECUTE:** When filled, the reservation converts into an actual open position.
3. **ROLLBACK:** If rejected by the matching engine or cancelled before execution, the reserved margin is released immediately.

---

## 4. Vectorized Portfolio Risk Analytics
* **Black-Scholes Greeks:** $\Delta, \Gamma, \Theta, \mathcal{V}, \rho$ vectorized across 8 options simultaneously via AVX2.
* **Monte Carlo VaR & CVaR:** 64-way parallel `xoshiro256+` PRNG path generation with 99% confidence intervals calculated in sub-millisecond execution times.
* **Stress Scenarios:** Simulated multi-asset shocks (e.g. Market -10%, Volatility +50%, Correlation $\to 1.0$).
