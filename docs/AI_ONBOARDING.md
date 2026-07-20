# Robin Trading Platform - Comprehensive AI & Developer Onboarding Guide

Welcome to the Robin Trading Platform research prototype. This guide provides a detailed, granular walkthrough of the codebase, components, data flows, IPC mechanisms, database schemas, and current limitations. It is designed to allow any AI coding agent or human developer to instantly understand the system and begin modifying or maintaining it.

---

## 1. System Architecture Overview

Robin is designed as a low-latency quantitative trading system consisting of three pipeline paths:

1. **Hot Path (Microsecond Target)**: Optimized for fast parsing, risk validation, and execution. Written in C++20 and Rust.
2. **Warm Path (1ms - 10ms)**: Handles database persistence, real-time dashboards, WebSockets, portfolio optimization, and rate-limiting.
3. **Cold Path (Offline/Batch)**: Includes backtesting, R analytics, and historical query engines.

```
                         HOT PATH (Microsecond Targets)
┌─────────────────────────────────────────────────────────────────┐
│  [UDP Multicast :5000] ──ITCH/OUCH parse──► [C++ Ingestion]     │
│                                                    │             │
│                          /dev/shm/robin_ingest_risk              │
│                          (POSIX SPSC ring — 65,536 × 64B slots)  │
│                                                    │             │
│                                                    ▼             │
│                                        [Rust RiskGate]           │
│                               7 Hard Blocks (checked in order):  │
│                         1. Kill switch  2. Circuit breaker        │
│                         3. Fat finger   4. Credit limit           │
│                         5. Symbol restrict  6. Duplicate         │
│                         7. Price collar ±5%                      │
│                               2 Soft Blocks:                     │
│                         8. Position limit  9. Velocity (100/s)   │
│                                                    │             │
│                           /dev/shm/robin_risk_match               │
│                                                    │             │
│                                                    ▼             │
│                               [C++ MatchingEngine]               │
│                    Price-time priority, 256 levels/side           │
│                    Lock-free SPSC in/out, no heap on hot path     │
└─────────────────────────────────────────────────────────────────┘
                                │
          ┌─────────────────────┼──────────────────────┐
          ▼                     ▼                       ▼
   WARM PATH                COLD PATH             OBSERVABILITY
   ──────────               ──────────            ─────────────
   KDB+ Tick DB             Python Backtester     Go Orchestrator :8080
   OCaml Portfolio          R Risk Analytics        /health  /stats
   (gradient descent)       Compliance Daemon       /config  /metrics
                            SHA-256 WORM audit log  Rust metrics :9092
                            (append-only)           Compliance :9095
```

---

## 2. Directory & Component Map

Below is a walkthrough of where specific elements reside:

*   **`services/execution-core/`**: C++20 Matching Engine.
    *   `src/matching_engine.cpp`: Receives orders from shared memory, processes trades, and writes executions to the outgoing ring buffer.
    *   `src/order_book.hpp`: Implements the order book with price-time priority support (256 price levels per side) and order cancellation/matching.
*   **`services/risk-analytics/`**: Rust Pre-Trade Risk Gate.
    *   `src/gate.rs`: Contains the primary risk checks (hard and soft checks).
    *   `src/risk_gate_fast.rs`: Lock-free atomic reference price updates and price collar checking.
    *   `src/metrics.rs`: Formats and exposes Prometheus metrics on port `9092`.
    *   `src/shm_bridge.rs`: Shared memory bridge managing atomic memory mapping and read/write volatile indexes.
*   **`services/ingestion/`**: C++ network bridge parsing ITCH/OUCH updates over UDP multicast on port `5000` and pushing parsed structs to `/robin_ingest_risk`.
*   **`services/gateway/`**: Go Gateway and Orchestrator.
    *   `orchestrator.go`: Entry point. Exposes administrative APIs on port `8080`, enforces token-bucket rate limiting (1000 req/s), coordinates service health probes, and manages WebSockets.
    *   `auth.go` & `supervisory_api.go`: Houses JWT generation/verification, Role-Based Access Control (RBAC), and multi-manager supervisor approval logic.
    *   `best_execution.go` & `sor.go`: Houses smart order routing (SOR) logic.
*   **`services/compliance/`**: Rust Compliance Daemon.
    *   `src/main.rs`: Listens on port `9095` for health check and compliance metrics. Integrates spoofing detection algorithms and logs all events to an append-only WORM-compliant audit log.
*   **`services/shared/`**: IPC configuration header.
    *   `config.h` (C/C++) and `config.rs` (Rust): Common mapping names, magic numbers, queues capacities, ports, and limits.
*   **`services/portfolio/`**: OCaml Portfolio Optimizer.
    *   Computes maximum Sharpe ratio via gradient descent with simplex projection.
*   **`research/`**: Python strategy engines, backtesters, and R analysis scripts.
    *   `research/strategy-engine/backtester.py`: Backtester with per-trade commission fees and Almgren square-root market impact model.
*   **`frontend/`**: Next.js 14 and Tailwind Dashboard for web visualizations.

---

## 3. Shared Memory IPC Architecture

The hot path components communicate via POSIX Shared Memory (`mmap`) mapped lock-free Single-Producer Single-Consumer (SPSC) ring buffers.

### Memory Mapped Files

| Shared Memory File Path | Producer | Consumer | Buffer Details / Capacity |
|---|---|---|---|
| `/robin_ingest_risk` | C++ Ingestion | Rust Risk Gate | 65,536 slots × 64 bytes per slot |
| `/robin_risk_match` | Rust Risk Gate | C++ Matching Engine | 65,536 slots × 64 bytes per slot |
| `/robin_match_storage` | C++ Matching Engine | Compliance Daemon / KDB+ | 65,536 slots × 64 bytes per slot |

### Layout & Optimization
*   **Volatile Accesses / Memory Barriers**: Ring buffer write/read positions are synchronized using cache-line aligned atomic index pointers to prevent CPU cache-line bouncing (false sharing).
*   **Shared Magic Numbers**: Defined as `0x524f42494e484d5f`.

---

## 4. Pre-Trade Risk Gate Checks

The Rust Risk Gate (`services/risk-analytics`) implements 7 Hard Blocks and 2 Soft Blocks. If any hard check fails, the order is rejected immediately. If soft checks fail, warnings are dispatched but the order continues.

### Hard Checks (In Order)
1.  **Kill Switch Active**: Checked globally from active shared memory state.
2.  **Circuit Breaker**: Tripped if max drawdown limit (e.g., 5%) is exceeded.
3.  **Fat Finger Check**: Quantity must not exceed 1,000,000 shares.
4.  **Credit Limit Check**: Price × Quantity must not exceed the account credit limit.
5.  **Symbol Restriction**: Rejects symbols on the restricted trading list.
6.  **Duplicate Order Check**: Order IDs must be unique within a sliding 1ms window.
7.  **Price Collar Check**: Rejects orders outside of a ±5% range from the reference price.

### Soft Checks
8.  **Position Limits**: Warns if net position exceeds 100,000 shares.
9.  **Velocity Check**: Warns if message rate exceeds 100 orders per second.

---

## 5. Persistence & SQLite Schema

Robin uses SQLite for local state storage, located at `robin.db` or defined in the gateway environment.

### Core Tables
1.  **`orders`**: Tracks order statuses (`NEW`, `FILLED`, `CANCELED`, etc.) and regulatory identifiers (CAT's FDID, RFID, and MiFID's Algo ID).
2.  **`trades`**: Execution ledger containing slippage (bps) and transaction costs.
3.  **`risk_positions`**: Net position snapshots and realized P&L per account.
4.  **`audit_log`**: An immutable WORM ledger. Chained via SHA-256 hashes:
    $$\text{chain\_hash}_n = \text{SHA256}(\text{record}_n + \text{chain\_hash}_{n-1})$$

### SEC Rule 17a-4 Enforcement Triggers
SQLite database level triggers block updates or deletions inside retention windows:
```sql
CREATE TRIGGER IF NOT EXISTS trg_audit_log_no_update BEFORE UPDATE ON audit_log
BEGIN
    SELECT RAISE(ABORT, 'audit_log records are immutable (SEC 17a-4 WORM requirement)');
END;
```

---

## 6. Build & Test Instructions

### Build Command
Compile all services including Go Gateway, Rust Risk Gate, and C++ Matching Engine:
```bash
make build
```

### Run Unit & Integration Tests
*   **Full Test Suite**: `make test`
*   **Rust Risk Gate Tests**: `make test-rust`
*   **Compliance Tests**: `make test-compliance`
*   **Go Gateway Tests**: `make test-go`
*   **Integration Smoke Test**: `make test-integration` (executes end-to-end execution path validation on Linux/WSL).

### Administrative REST Endpoints
*   Health State: `GET http://localhost:8080/health`
*   Statistics Summary: `GET http://localhost:8080/stats`
*   Prometheus Risk Gate Metrics: `GET http://localhost:9092/metrics`
*   Compliance Status: `GET http://localhost:9095/health`

---

## 7. Simulated vs. Real Components

As a research prototype, several components are high-fidelity simulations:

*   **FPGA Acceleration**: CPU software simulation (using `std::memcpy` on DRAM). `is_hardware_fpga()` returns `false`.
*   **Kernel Kill Switch**: Simulated using Linux netfilter port blocks instead of actual physical GPIO pins.
*   **Linear Signal Model**: C++ momentum/imbalance indicator model (no active ONNX runtime / Deep Learning execution).
*   **DPDK Ingestion**: Linux standard UDP sockets. No raw kernel-bypass network card integration.
