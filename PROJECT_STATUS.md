# Robin Trading Platform - Project Status & Architecture Guide

This document provides a comprehensive overview of the Robin Trading Platform research prototype. It is designed to assist AI models and developers in understanding the codebase structure, execution paths, and current status.

---

## 1. System Architecture

The system is structured as a low-latency quantitative trading prototype with three pipeline tiers:

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

## 2. Component Directory Map

### Core Infrastructure & Hot Path
- [C++ Execution Core](file:///c:/Robin/services/execution-core) (`services/execution-core`): Lock-free SPSC, price-time priority order book matching engine.
- [Rust Risk Gate](file:///c:/Robin/services/risk-analytics) (`services/risk-analytics`): Pre-trade risk check (7 hard blocks, 2 soft blocks) with Prom metrics and local state persistence.
- [C++ Ingestion](file:///c:/Robin/services/ingestion) (`services/ingestion`): UDP multicast receiver pushing ITCH/OUCH updates to shared memory.
- [Shared Memory Config](file:///c:/Robin/services/shared) (`services/shared`): Common IPC definitions (`config.h` and `config.rs`).

### Warm & Cold Paths
- [Go Gateway & Orchestrator](file:///c:/Robin/services/gateway) (`services/gateway`): HTTP services registry, JWT + RBAC authorization, SQLite persistence, supervisory approvals, and real-time WebSockets hub.
- [Compliance Daemon](file:///c:/Robin/services/compliance) (`services/compliance`): Spoofing detector, WORM SHA-256 audit logger.
- [Python Strategy Engine & Backtester](file:///c:/Robin/research/strategy-engine) (`research/strategy-engine`): Backtester with fees and market impact metrics.
- [AI Model & Signal Engine](file:///c:/Robin/research/ai-engine) (`research/ai-engine`): Momentum, imbalance, and volume pressure indicators.
- [OCaml Portfolio Optimizer](file:///c:/Robin/services/portfolio) (`services/portfolio`): Sharpe ratio gradient-descent optimizer.

---

## 3. Latest Project Completion Updates

1. **Multi-Service Build Verification**: Resolved build and linking issues across Go Gateway (`oms.go`), Rust Risk (`gpio_kill_switch.rs`, `gate.rs`), and C++ Execution (`dpdk_ingest.cpp`).
2. **CORS & Preflight Authorization**: Added robust CORS middleware and `OPTIONS` preflight handling across Go Gateway API endpoints, aligning with frontend JWT bearer token transmission.
3. **CEO Demo Integration**: Integrated live Coinbase WebSocket feed ingestion, `lightweight-charts` visualization, and real-time risk/compliance metric streams.
4. **Gate Security Restored**: Bypasses have been removed from Go Orchestrator's `jwtAuthMiddleware` and `rbacMiddleware` to enforce standard authorization.
5. **Auto-Migrations**: SQLite database initialization runs migration statements to ensure schema coherence before index creation.

---

## 4. How to Verify & Test

- **Go Gateway Tests**: Run `go test -v ./...` under `services/gateway`.
- **Rust Risk Tests**: Run `cargo test` under `services/risk-analytics`.
- **Compliance Tests**: Run `cargo test` under `services/compliance`.
- **Python Backtester**: Run `python research/strategy-engine/backtester.py`.
- **Frontend Dashboard Build**: Run `npm run build` under `frontend`.
