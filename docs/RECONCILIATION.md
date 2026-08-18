# Robin Quantitative Trading Platform — State Reconciliation & Invariant Checks
**Document ID:** SPEC-REC-202608-01  
**Classification:** Post-Trade Integrity Specification  

---

## 1. Three-Way Reconciliation Model

```
       ┌──────────────────────────────────────────────┐
       │             Broker / Exchange State          │
       │      (REST / FIX Drop Copy / Open Orders)    │
       └──────────────────────┬───────────────────────┘
                              │
                      [Reconciliation]
                              │
                              ▼
       ┌──────────────────────────────────────────────┐
       │             Internal State Machine           │
       │         (RAM Orders / Cached Positions)      │
       └──────────────────────┬───────────────────────┘
                              │
                      [Reconciliation]
                              │
                              ▼
       ┌──────────────────────────────────────────────┐
       │             Database / Audit WAL             │
       │           (SQLite / Postgres / KDB+)         │
       └──────────────────────────────────────────────┘
```

---

## 2. Invariant Rules
1. **No Orphaned Orders:** An order marked as open in the database must exist in the internal matching engine.
2. **Execution Fill Equality:** $\sum \text{Fill Qty} \equiv \text{Total Executed Qty}$ across venue execution reports and internal position blotters.
3. **Cash Balance Conservation:** $\text{Initial Cash} + \sum \text{Realized PnL} - \sum \text{Fees} + \sum \text{Dividends} \equiv \text{Current Cash}$.
