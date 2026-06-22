# Robin Trading Platform

Research prototype for ultra-low latency quantitative trading concepts.

> [!WARNING]
> **DISCLAIMER: This is a research prototype / proof-of-concept. It is NOT production-ready and MUST NOT be used for live trading.**
> - No active regulatory compliance (SEC 15c3-5, MiFID II, FINRA 3110 are design targets only — not certified)
> - No production security (TLS is configurable in the gateway; mTLS, HSM, Vault are unimplemented)
> - FPGA acceleration is a CPU software simulation (`std::memcpy` on DRAM, not synthesized Xilinx hardware)
> - All sub-microsecond latency figures are aspirational targets based on similar HFT systems in literature, not measured results from this codebase
> - Many components are prototypes, stubs, or incomplete

---

## Current Implementation Status

| Component | Status | Language | What It Does | Known Gaps |
|---|---|---|---|---|
| C++ Matching Engine | ✅ Working | C++20 | Price-time priority order book (256 levels), lock-free SPSC queues, no heap alloc on hot path | No real market data feed, timestamp hardcoded |
| Rust Risk Gate | ✅ Working | Rust | 7 hard blocks, 2 soft blocks, 8 unit tests, zero-alloc hot path, velocity window fixed | No persistence across restarts |
| Rust Risk Metrics | ✅ Working | Rust | Prometheus text metrics over TCP (:9092) | No histogram buckets (avg/max only) |
| Network Ingestion | ✅ Working | C++ | UDP multicast socket + ITCH/OUCH protocol parser, SHM ring push | No DPDK kernel bypass |
| FPGA Simulator | ⚠️ Software Sim | C++ | CPU-based order processing simulation (NOT hardware) | Not an FPGA; no Xilinx synthesis |
| OCaml Portfolio | ✅ Working | OCaml | Gradient descent Sharpe optimizer, VaR/CVaR/Sharpe computation | No live market feed integration |
| Go Orchestrator | ✅ Working | Go | Service registry, TCP health probes, hot-reload config, TLS, rate limiting (1000/s), structured logging (slog JSON), Prometheus, 14 unit tests | No order routing |
| KDB+ Storage | ⚠️ Schema Only | Q | Table schemas for trade/quote/order | No TP/RDB/HDB architecture, no sym enum |
| Kernel Module | ✅ Working | C (Linux kernel) | GPIO kill switch with port whitelist (SSH/mgmt preserved, trading ports blocked) | Requires actual GPIO hardware |
| Linear Signal Model | ✅ Working | C++ | Weighted linear alpha signal (price momentum, OB imbalance, volume, intraday) | Not an ML model; no ONNX runtime |
| Compliance | ✅ Working | Rust | Spoofing detector + SHA-256 WORM audit logger + compliance daemon binary (:9095) | No live order event subscription yet |
| CI/CD | ✅ Working | GitHub Actions | Build+test all services, clippy, rustfmt, gofmt, race detector | No hardware-in-loop tests |

---

## Architecture

```
                         HOT PATH (microsecond targets)
┌─────────────────────────────────────────────────────────────────┐
│  [UDP Multicast] ──ITCH/OUCH parse──► [CPPIngestionPipeline]    │
│                                              │                   │
│                              /dev/shm/robin_ingest_risk          │
│                              (POSIX SHM SPSC ring — 65536 slots) │
│                                              │                   │
│                                              ▼                   │
│                                   [Rust RiskGate]                │
│                          7 Hard Blocks + 2 Soft Blocks           │
│                    Kill switch → Circuit breaker → Fat finger     │
│                    Credit limit → Restricted list → Duplicate     │
│                    Price collar → Position limit → Velocity       │
│                                              │                   │
│                              /dev/shm/robin_risk_match            │
│                                              │                   │
│                                              ▼                   │
│                               [C++ MatchingEngine]               │
│                       Price-time priority, 256 levels/side        │
│                       Lock-free SPSC in/out queues               │
└─────────────────────────────────────────────────────────────────┘
                              │
         ┌────────────────────┼─────────────────────┐
         ▼                    ▼                      ▼
  WARM PATH                COLD PATH            OBSERVABILITY
  ──────────               ──────────           ─────────────
  KDB+ Tick DB             Python Backtester    Go Orchestrator
  OCaml Portfolio          R Risk Analytics     :8080 /health /stats
  (gradient descent)       Compliance Daemon    /config /metrics
                           SHA-256 audit log    Rust metrics :9092
                           (WORM append-only)   Compliance :9095
```

---

## Build & Run

### Prerequisites
- Linux (recommended) or WSL2 on Windows
- `g++-12` or newer
- `rustc 1.78+` (install via [rustup](https://rustup.rs))
- `Go 1.22+`
- `OCaml 5.1+` + `opam` (optional — portfolio only)
- `Python 3.11+` with `numpy`

```bash
# Build all services
make build

# Run individual components
./services/execution-core/build/matching_engine
ORCH_PORT=8080 ./build/orchestrator
./services/compliance/target/release/compliance-daemon --port 9095

# Run all tests
make test

# Run integration smoke test (Linux only)
make test-integration

# Check health
curl http://localhost:8080/health
curl http://localhost:8080/stats
curl http://localhost:9095/metrics
```

---

## Shared Memory IPC

| Path | Producer | Consumer | Capacity |
|---|---|---|---|
| `/robin_ingest_risk` | C++ Ingestion | Rust Risk Gate | 65,536 messages |
| `/robin_risk_match` | Rust Risk Gate | C++ Matching Engine | 65,536 messages |
| `/robin_match_storage` | C++ Matching Engine | KDB+ / Compliance | 65,536 messages |

Message size: 64 bytes per slot. Magic: `0x524f42494e484d5f` ("ROBINHM_").

---

## Key Limitations (Honest Assessment)

| Limitation | Impact | Fix Required |
|---|---|---|
| No DPDK kernel bypass | Kernel TCP/UDP adds ~10-30μs jitter | Intel E810 NIC + DPDK 23.11 |
| No real FPGA synthesis | Software sim; no speedup over matching engine | Xilinx Alveo U50 + Vitis HLS |
| No production compliance | SEC 15c3-5 NOT certified | Full compliance team + regulator engagement |
| No security hardening | No mTLS, no HSM, no JWT | Vault + cert infrastructure |
| No horizontal scaling | Single-node only | Raft consensus, multi-node SHM |
| No observability stack | No traces, histogram latency buckets only | Jaeger / OpenTelemetry |
| No integration tests (full) | Components partially tested | Real market data replay harness |
| OCaml portfolio not integrated | Offline optimizer only | IPC to risk gate feedback loop |

---

## Compliance & Security Status

| Standard | Status | Notes |
|---|---|---|
| SEC Rule 15c3-5 | ❌ Design target only | Would require certified implementation + broker-dealer registration |
| MiFID II RTS 25 | ❌ Design target only | Requires hardware PTP clock synchronization + audit trail |
| FINRA Rule 3110 | ❌ Not implemented | Supervision system required |
| SOC 2 Type II | ❌ Not implemented | Requires 6-month audit period |
| TLS 1.2+ | ⚠️ Optional | Go orchestrator supports TLS — set ORCH_TLS_CERT + ORCH_TLS_KEY |
| mTLS | ❌ Not implemented | Required for production inter-service comms |
| HSM / Vault | ❌ Not implemented | Required for key management |
| Audit logging | ✅ Prototype | SHA-256 chained WORM log in compliance daemon |
| Spoofing detection | ✅ Prototype | Pattern-based (large order + rapid cancel within window) |

---

## Project Structure

```
services/
├── execution-core/     C++20 matching engine (price-time priority, 256 levels, lock-free)
├── network-bridge/     ITCH/OUCH parser (AVX-512 + fallback), DPDK stubs
├── ingestion/          UDP multicast ITCH parser → SHM ring
├── hardware-fpga/      CPU simulation of FPGA order processing interface (NOT real hardware)
├── risk-analytics/     Rust pre-trade risk gate, metrics, shared config
├── gateway/            Go orchestrator: health probes, hot-reload, rate limiting, TLS
├── compliance/         Rust spoofing detector + WORM audit logger + daemon binary
├── portfolio/          OCaml gradient descent optimizer, VaR/CVaR, signal generation
├── strategy-engine/    Python backtester, R statistical analytics
├── pricing/            C++ Monte Carlo options pricing (xoshiro256 RNG)
├── ai-engine/          C++ linear signal model (NOT neural network / ONNX)
├── kdb-storage/        Q/KDB+ tick database schemas (schema only, no TP/RDB/HDB)
├── kernel/             Linux kernel module: GPIO kill switch (whitelist-aware)
└── shared/             IPC config (config.h, config.rs), lock-free ring buffer
```
