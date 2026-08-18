# Robin Quantitative Trading Platform — Canonical System Specification
**Document ID:** ARCH-SPEC-202608-01  
**Classification:** Institutional Architecture Specification  

---

## 1. Architectural Tiers & Path Separation

```
                     ┌────────────────────────────────────────────────────────┐
                     │                     CONTROL PLANE                      │
                     │  Go Gateway (8080) • Vault PKI • JWT/RBAC Auth • Admin │
                     └───────────────────────────┬────────────────────────────┘
                                                 │
 ┌───────────────────────────────────────────────┼───────────────────────────────────────────────┐
 │                                               ▼                                               │
 │  ┌─────────────────────────────────────────────────────────────────────────────────────────┐  │
 │  │                                       HOT PATH                                          │  │
 │  │                                                                                         │  │
 │  │    Exchange UDP Multicast                                                               │  │
 │  │             │                                                                           │  │
 │  │             ▼                                                                           │  │
 │  │    DPDK 23.11 PMD Ingestion Engine ──(Zero-Copy ITCH/XDP)──> /robin_ingest_risk (SHM)   │  │
 │  │                                                                     │                   │  │
 │  │                                                                     ▼                   │  │
 │  │    Rust Pre-Trade Risk Gate (16-Shard Lock-Free Ring) ──────> /robin_risk_match (SHM)  │  │
 │  │                                                                     │                   │  │
 │  │                                                                     ▼                   │  │
 │  │    C++20 Matching Engine (NUMA Slab Pools, LULD, OUCH) ────> /robin_match_storage (SHM) │  │
 │  └────────────────────────────────────────────┬────────────────────────────────────────────┘  │
 │                                               │                                               │
 └───────────────────────────────────────────────┼───────────────────────────────────────────────┘
                                                 │
 ┌───────────────────────────────────────────────┴───────────────────────────────────────────────┐
 │                                                                                               │
 │  ┌─────────────────────────────────────────────────────────────────────────────────────────┐  │
 │  │                                      WARM PATH                                          │  │
 │  │  • Portfolio Manager & Real-Time P&L Engine (Go/OCaml)                                  │  │
 │  │  • Vectorized Greeks & Monte Carlo VaR/CVaR Daemon (Rust AVX2)                          │  │
 │  │  • Smart Order Router (SOR) with Venue Fee/Rebate Optimization                          │  │
 │  │  • Real-Time Surveillance & Spoofing Detector (Rust)                                    │  │
 │  └────────────────────────────────────────────┬────────────────────────────────────────────┘  │
 │                                               │                                               │
 │  ┌────────────────────────────────────────────┴────────────────────────────────────────────┐  │
 │  │                                      COLD PATH                                          │  │
 │  │  • KDB+/Q Tickerplant, RDB & Historical Partitioned Database (HDB)                      │  │
 │  │  • SEC Rule 15c3-5 & MiFID II RTS 22/25 Periodic Exporters                             │  │
 │  │  • FINRA Rule 613 CAT Daily XML Exporter                                                │  │
 │  │  • Python Quantitative Strategy Backtester & Walk-Forward Optimizer                     │  │
 │  └─────────────────────────────────────────────────────────────────────────────────────────┘  │
 └───────────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 2. Path Isolation Contracts

### A. The Hot Path (<10 μs p99 SLA)
1. **Zero Heap Allocation:** No `malloc`, `free`, `new`, `delete`, or dynamically resizing containers on hot-path loops. All memory is pre-allocated in hugepage NUMA memory pools (`MAP_HUGETLB`).
2. **Lock-Free Concurrency:** Single-Producer Single-Consumer (SPSC) lock-free ring buffers using explicit `Release/Acquire` memory ordering fences.
3. **Hardware Monotonic Clocks:** Direct `rdtscp` hardware instruction timestamping synchronized to anchor offsets.
4. **No Blocking I/O:** Hot path never performs disk writes, network syscalls, synchronous DB queries, or mutex waits.

### B. The Warm Path (100 μs – 10 ms SLA)
1. **Optimistic Position Reservations:** Pre-trade margin and credit locks with automatic timeout rollback.
2. **Vectorized Risk Analytics:** Parallel Black-Scholes Greeks, Newton-Raphson IV solves, and 64-way parallel `xoshiro256+` Monte Carlo simulations.
3. **Surveillance & Anomaly Detection:** Real-time VPIN and order cancellation ratio tracking over sliding 5-second windows.

### C. The Cold Path (100 ms – Daily SLA)
1. **Regulatory Reporting:** Daily FINRA CAT XML, MiFID II RTS 22, and SEC Rule 15c3-5 CEO certification packages.
2. **Historical Time-Series:** KDB+/Q daily partition compaction with 12-byte integer compression.
3. **Quantitative Research:** Multi-factor backtesting, purged cross-validation, and parameter stability sweeps.

---

## 3. Order Lifecycle State Machine Contract

Every order in the Robin system progresses deterministically through a formal state machine:

```
                  ┌──────────────────┐
                  │       NEW        │
                  └────────┬─────────┘
                           │
                           ▼
                  ┌──────────────────┐
                  │    VALIDATING    │
                  └────────┬─────────┘
                           │
            ┌──────────────┴──────────────┐
            │                             │
            ▼                             ▼
   ┌─────────────────┐           ┌─────────────────┐
   │  RISK_APPROVED  │           │    REJECTED     │  (Terminal)
   └────────┬────────┘           └─────────────────┘
            │
            ▼
   ┌─────────────────┐
   │     ROUTING     │
   └────────┬────────┘
            │
            ▼
   ┌─────────────────┐
   │  ACKNOWLEDGED   │
   └────────┬────────┘
            │
      ┌─────┴──────────────────┬──────────────────┐
      │                        │                  │
      ▼                        ▼                  ▼
┌───────────┐          ┌───────────────┐   ┌────────────────┐
│  FILLED   │(Terminal)│ PARTIAL_FILL  │   │ CANCEL_PENDING │
└───────────┘          └───────┬───────┘   └───────┬────────┘
                               │                   │
                               ▼                   ▼
                         ┌───────────┐       ┌───────────┐
                         │  FILLED   │       │ CANCELED  │  (Terminal)
                         └───────────┘       └───────────┘
```

### Invariant Rules
1. **Cancel Integrity:** A client cancel request (`DELETE /order/{id}`) transitions state to `CANCEL_PENDING`, dispatches a cancellation command to the matching engine, and only transitions to `CANCELED` upon receiving matching engine acknowledgment.
2. **Financial Conservation:** $\text{Realized PnL} + \text{Unrealized PnL} - \text{Commissions} - \text{Slippage} \equiv \Delta \text{Equity}$.
3. **Position Monotonicity:** $\text{Position}_t = \text{Position}_{t-1} + \text{ExecQty}_{\text{Buy}} - \text{ExecQty}_{\text{Sell}}$.
