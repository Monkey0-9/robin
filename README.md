# Robin Trading Platform

Research prototype for ultra-low latency quantitative trading concepts.

> [!WARNING]
> **DISCLAIMER: This is a research prototype / proof-of-concept. It is NOT production-ready and MUST NOT be used for live trading.**
> - No active regulatory compliance (SEC 15c3-5, MiFID II, FINRA 3110 are design targets only — not certified)
> - No production security (TLS is configurable in the gateway; mTLS, HSM, Vault are not implemented)
> - FPGA acceleration is a CPU software simulation (`SoftwareOrderMatchSimulator` using `std::memcpy` on DRAM — not synthesized Xilinx hardware)
> - All sub-microsecond latency figures are aspirational targets based on similar HFT systems in published literature, not measured results from this codebase
> - Many components are prototypes or incomplete

---

## Current Implementation Status

| Component | Status | Language | What It Does | Known Gaps |
|---|---|---|---|---|
| **C++ Matching Engine** | ✅ Working | C++20 | Price-time priority order book (256 levels/side), lock-free SPSC queues, no heap alloc on hot path, overflow guard, `best_bid()`/`best_ask()`/`cancel_order()` | No real market data feed integration |
| **Rust Risk Gate** | ✅ Working | Rust | 7 hard blocks + 2 soft blocks, fixed sliding-window velocity check, credit limit (price×qty), position rollback, 23 unit tests (all pass) | No persistence across restarts |
| **Rust Risk Metrics** | ✅ Working | Rust | Prometheus text exposition format on `:9092` — orders, rejections, latency avg/max, rejection breakdown by reason | No histogram buckets (avg/max only) |
| **Shared IPC Config** | ✅ Working | C/Rust | `services/shared/config.h` + `config.rs` — single source of truth for all SHM paths, ports, risk limits | Go equivalent not yet added |
| **Network Ingestion** | ✅ Working | C++ | UDP multicast socket + ITCH/OUCH protocol parser, SHM ring push | No DPDK kernel bypass |
| **FPGA Simulator** | ⚠️ Software Sim | C++ | CPU-based order processing simulation (class: `SoftwareOrderMatchSimulator`, NOT hardware). `is_hardware_fpga()` returns `false`. | No Xilinx synthesis; no real acceleration |
| **OCaml Portfolio** | ✅ Working | OCaml | Gradient descent Sharpe optimizer, VaR/CVaR computation | Not integrated with live pipeline |
| **Go Orchestrator** | ✅ Working | Go | Service registry, TCP health probes (100ms), hot-reload config, TLS, token-bucket rate limiting (1000 req/s), request ID middleware, structured JSON logging (`slog`), Prometheus, graceful shutdown, 14 unit tests | No order routing |
| **KDB+ Storage** | ✅ Working | Q | Tickerplant/RDB/HDB subscription protocols, sym enumeration schemas, Prometheus metrics, in-memory TTL query cache, and secure IPC adapter with password validation | Requires full KDB+ deployment |
| **Kernel Kill Switch** | ✅ Working | C (Linux) | GPIO kill switch v2.1.0 — port whitelist (SSH/22, health/8080, admin/9090 always pass; trading ports 5000–9100 blocked on activation) | Requires actual GPIO hardware |
| **Linear Signal Model** | ✅ Working | C++ | Weighted linear alpha signal: price momentum + OB imbalance + volume pressure + intraday. Named `LinearSignalModel` (not ONNX) | Not an ML model; no neural network / ONNX runtime |
| **Compliance Daemon** | ✅ Working | Rust | Standalone binary (`compliance-daemon`): spoofing detector + SHA-256 WORM audit log + Prometheus metrics + `/health` on `:9095` | No live SHM subscription yet (demo mode) |
| **C++ Options Pricing** | ✅ Working | C++ | Multi-threaded Monte Carlo simulation of European Call paths using thread-local `xoshiro256` random generators | Does not support path-dependent options yet |
| **Python Backtester** | ✅ Working | Python | Realistic backtester with per-trade commission fees (bps) and square-root market impact slippage (Almgren et al. 2005) | Does not simulate network jitter or queuing |
| **CI/CD Pipeline** | ✅ Working | GitHub Actions | 8 jobs: risk build+test+clippy, compliance build+test+clippy, go build+vet+test+race, cpp×2 (exec-core + pricing), python validation, rust-fmt check, go-fmt check | No hardware-in-loop tests |

---

## Architecture

```
                         HOT PATH (microsecond targets)
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

## Build & Run

### Prerequisites
- **Linux** (recommended) or **WSL2** on Windows
- `g++-12` or newer
- `rustc 1.78+` — install via [rustup.rs](https://rustup.rs)
- `Go 1.22+`
- `OCaml 5.1+` + `opam` (optional — portfolio optimizer only)
- `Python 3.11+` with `numpy`

```bash
# Build all services
make build

# Run individual components
./services/execution-core/build/matching_engine
ORCH_PORT=8080 ./build/orchestrator
./services/compliance/target/release/compliance-daemon --port 9095

# Run all tests
make test                # Rust (23 tests) + Go (14 tests) + lint
make test-rust           # Risk gate unit tests only
make test-compliance     # Compliance daemon tests
make test-go             # Go orchestrator tests (with -race)
make test-cpp            # C++ CTest (if built)
make test-lint           # Rust clippy

# Integration smoke test (Linux only)
make test-integration

# Check health endpoints
curl http://localhost:8080/health
curl http://localhost:8080/stats
curl http://localhost:8080/config
curl http://localhost:9092/metrics    # Risk gate Prometheus metrics
curl http://localhost:9095/health     # Compliance daemon
curl http://localhost:9095/metrics    # Compliance Prometheus metrics

# Hot-reload config without restart
curl -X POST -H "Content-Type: application/json" \
  -d '{"max_drawdown_limit":0.05,"max_order_rate":5000}' \
  http://localhost:8080/config
```

---

## Shared Memory IPC

| Path | Producer | Consumer | Capacity | Magic |
|---|---|---|---|---|
| `/robin_ingest_risk` | C++ Ingestion | Rust Risk Gate | 65,536 × 64B | `0x524f42494e484d5f` |
| `/robin_risk_match` | Rust Risk Gate | C++ Matching Engine | 65,536 × 64B | `0x524f42494e484d5f` |
| `/robin_match_storage` | C++ Matching Engine | KDB+ / Compliance | 65,536 × 64B | `0x524f42494e484d5f` |

All paths and constants are defined in [`services/shared/config.h`](services/shared/config.h) (C++) and [`services/risk-analytics/src/config.rs`](services/risk-analytics/src/config.rs) (Rust) — **single source of truth**.

---

## Service Ports

| Port | Service | Protocol | Endpoints |
|---|---|---|---|
| 8080 | Go Orchestrator | HTTP | `/health` `/stats` `/config` `/metrics` |
| 9091 | C++ Execution Core | TCP (health probe) | — |
| 9092 | Rust Risk Gate | HTTP | `/metrics` (Prometheus) |
| 9093 | Market Data | TCP (health probe) | — |
| 9094 | Portfolio Engine | TCP (health probe) | — |
| 9095 | Compliance Daemon | HTTP | `/health` `/metrics` |
| 5000 | Market Data Multicast | UDP | ITCH/OUCH feed |

---

## Risk Gate — Hard Blocks Reference

```
Order Received
     │
     ▼  [1] Kill Switch Active?           → Err(KillSwitchActive)
     ▼  [2] Circuit Breaker Tripped?      → Err(CircuitBreakerTripped)
     ▼  [3] qty > 1,000,000?              → Err(FatFinger)
     ▼  [4] price × qty > credit_limit?  → Err(CreditLimit)
     ▼  [5] Symbol on restricted list?   → Err(SymbolRestricted)
     ▼  [6] Duplicate order ID < 1ms?    → Err(DuplicateOrder)
     ▼  [7] Price outside ±5% collar?    → Err(PriceCollar)
     ▼  [8] Net position > 100,000?      → Err(PositionLimit)   [soft]
     ▼  [9] > 100 orders in last 1s?     → Err(VelocityLimit)   [soft]
     │
     ▼  Ok(Approved) → forward via SHM to Matching Engine
```

---

## Test Results

```
Rust risk-analytics:   23 / 23 tests PASS  ✅
Go gateway:            14 unit tests added  ✅
CI jobs:               8 jobs (all green on Linux)  ✅
```

---

## What Changed (Latest Release)

| # | File | Change |
|---|---|---|
| 1 | `services/kernel/gpio_kill.c` | Fixed ALL-traffic kill → port whitelist; SSH/mgmt never blocked |
| 2 | `services/risk-analytics/src/shm_bridge.rs` | Fixed `_shm_size` bug (Linux `ftruncate` now works) |
| 3 | `services/hardware-fpga/fpga_emulator.*` | Renamed `FPGAEmulator` → `SoftwareOrderMatchSimulator`; added `is_hardware_fpga()=false` |
| 4 | `services/ai-engine/onnx_inference.cpp` | Renamed to `LinearSignalModel`; removed fake ONNX file loading |
| 5 | `services/execution-core/src/order_book.hpp` | `MAX_PRICE_LEVELS` 64 → 256; overflow guard; `best_bid()` / `best_ask()` / `cancel_order()` |
| 6 | `services/risk-analytics/src/gate.rs` | Fixed velocity window off-by-one; added `CreditLimit` hard block; `rollback_position()`; 8 new tests |
| 7 | `services/risk-analytics/src/risk_gate_fast.rs` | `update_reference_price()` with `AtomicU32`; fixed Ask collar direction; 6 tests |
| 8 | `services/risk-analytics/src/metrics.rs` | **NEW** — Prometheus metrics HTTP on `:9092` |
| 9 | `services/risk-analytics/src/config.rs` | **NEW** — Rust mirror of shared IPC constants |
| 10 | `services/shared/config.h` | **NEW** — C/C++ shared IPC config (SHM paths, ports, risk limits) |
| 11 | `services/compliance/src/main.rs` | **NEW** — Standalone compliance daemon binary |
| 12 | `services/compliance/Cargo.toml` | Added `[[bin]]` entry for `compliance-daemon` |
| 13 | `services/gateway/orchestrator.go` | `slog` JSON logging; token-bucket rate limiter; request ID middleware; timeouts; graceful shutdown |
| 14 | `services/gateway/orchestrator_test.go` | **NEW** — 14 Go unit tests (HTTP endpoints, rate limiter, config hot-reload) |
| 15 | `Makefile` | Pinned `g++-12`; separate `test-rust` / `test-compliance` / `test-go` / `test-cpp` / `test-lint` / `test-integration` |
| 16 | `.github/workflows/ci.yml` | 8 jobs; pinned `CXX=g++-12`; added compliance, `go test -race`, `rust-fmt`, `go-fmt` |
| 17 | `scripts/integration_smoke_test.sh` | **NEW** — E2E pipeline smoke test |
| 18 | `.gitignore` | Added `*.q~`, `*.orig`, `*.bak`, `*.swp` |

---

## Key Limitations (Honest Assessment)

| Limitation | Impact | What's Needed to Fix |
|---|---|---|
| No DPDK kernel bypass | Kernel TCP/UDP adds ~10–30 μs jitter | Intel E810 NIC + DPDK 23.11 |
| No real FPGA synthesis | `std::memcpy` on CPU; no hardware speedup | Xilinx Alveo U50 + Vitis HLS 2023.2 |
| No production compliance | SEC 15c3-5 NOT certified | Compliance team + regulator engagement |
| No mTLS / HSM / Vault | No inter-service auth or key management | HashiCorp Vault + cert infra |
| No horizontal scaling | Single-node only | Raft consensus + multi-node SHM |
| No distributed tracing | No spans or trace IDs | Jaeger / OpenTelemetry |
| OCaml portfolio offline | Not connected to live pipeline | IPC bridge to risk gate feedback loop |
| KDB+ schema only | No TP/RDB/HDB tick plant | Full KDB+ deployment + sym enumeration |

---

## Compliance & Security Status

| Standard | Status | Notes |
|---|---|---|
| SEC Rule 15c3-5 | ❌ Design target only | Requires certified implementation + broker-dealer registration |
| MiFID II RTS 25 | ❌ Design target only | Requires hardware PTP clock sync + certified audit trail |
| FINRA Rule 3110 | ❌ Not implemented | Supervision system required |
| SOC 2 Type II | ❌ Not implemented | Requires 6-month independent audit period |
| TLS 1.2+ | ⚠️ Optional | Set `ORCH_TLS_CERT` + `ORCH_TLS_KEY` env vars |
| mTLS | ❌ Not implemented | Required for production inter-service communications |
| HSM / Vault | ❌ Not implemented | Required for key management |
| Audit logging | ✅ Prototype | SHA-256 chained WORM log (append-only, chain-verifiable) |
| Spoofing detection | ✅ Prototype | Pattern-based: large order + rapid cancel within configurable window |

---

## Project Structure

```
c:\Robin\
├── .github/workflows/ci.yml       8-job CI pipeline
├── Makefile                       Build + test targets (g++-12 pinned)
├── scripts/
│   └── integration_smoke_test.sh  E2E pipeline test (Linux)
├── config/                        DPDK, PTP, RT kernel configs
├── docs/                          ARCHITECTURE, SECURITY, COMPLIANCE, OpenAPI
├── services/
│   ├── shared/
│   │   └── config.h               IPC constants (SHM paths, ports, risk limits)
│   ├── execution-core/            C++20 matching engine (256 levels, lock-free)
│   ├── network-bridge/            ITCH/OUCH parser, DPDK stubs
│   ├── ingestion/                 UDP multicast → SHM ring
│   ├── hardware-fpga/             SoftwareOrderMatchSimulator (CPU sim, NOT FPGA)
│   ├── risk-analytics/
│   │   └── src/
│   │       ├── gate.rs            7+2 block risk gate, 8 tests
│   │       ├── risk_gate_fast.rs  Symbol restrict, price collar, 6 tests
│   │       ├── circuit_breaker.rs Daily drawdown halt
│   │       ├── metrics.rs         Prometheus exposition (:9092)
│   │       ├── shm_bridge.rs      POSIX SHM SPSC ring bridge
│   │       └── config.rs          Rust mirror of shared/config.h
│   ├── gateway/
│   │   ├── orchestrator.go        Health probes, hot-reload, rate limit, slog
│   │   └── orchestrator_test.go   14 unit tests
│   ├── compliance/
│   │   └── src/
│   │       ├── lib.rs             Spoofing detector + WORM logger
│   │       └── main.rs            Standalone daemon binary (:9095)
│   ├── portfolio/                 OCaml gradient descent optimizer
│   ├── strategy-engine/           Python backtester + R analytics
│   ├── pricing/                   C++ Monte Carlo options (xoshiro256)
│   ├── ai-engine/                 C++ LinearSignalModel (NOT neural net)
│   ├── kdb-storage/               Q/KDB+ tick schemas (schema only)
│   └── kernel/                    GPIO kill switch v2.1.0 (whitelist-aware)
└── frontend/                      Next.js + Tauri dashboard (UI prototype)
```
