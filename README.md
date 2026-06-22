# Robin Trading Platform

Research prototype for ultra-low latency quantitative trading concepts.

> [!WARNING]
> **DISCLAIMER: This is a research prototype / proof-of-concept. It is NOT production-ready and must NOT be used for live trading.**
> - No active regulatory compliance (SEC 15c3-5, MiFID II, FINRA 3110 are unimplemented design targets)
> - No production security (TLS, mTLS, HSM, Vault are unimplemented)
> - FPGA acceleration is a software simulation (memcpy-based, not synthesized hardware)
> - All latency figures are aspirational targets, not measured results
> - Many components are placeholders, stubs, or incomplete

## Current State

| Component | Status | Notes |
|-----------|--------|-------|
| C++ Matching Engine | ✅ Fixed | Price-time priority, no heap alloc on hot path (placement new), embedded per-level queues |
| Rust Risk Gate | ✅ Fixed | Zero-allocation hot path, pre-trade checks, duplicate detection, velocity limits. 12/12 tests pass |
| Network Ingestion | ⚠️ Prototype | Socket-based UDP multicast + ITCH parser works; no DPDK kernel bypass |
| FPGA Emulator | ⚠️ Software Sim | memcpy-based simulation, not actual Xilinx hardware |
| OCaml Portfolio | ✅ Functional | Gradient descent optimizer, valid VaR/Sharpe computation |
| Go Orchestrator | ✅ Functional | HTTP server with health checks, service registry, config hot-reload, TLS support |
| KDB+ Storage | ⚠️ Basic Schema | Table definitions only, no production TP/RDB/HDB architecture |
| Kernel Module | ✅ Fixed | GPIO kill switch updated to descriptor-based API (gpiod_to_irq), no deprecated calls |
| AI/ONNX Inference | ✅ Implemented | Cold-path ONNX inference stub with model loading and signal computation |
| Compliance Module | ✅ Implemented | Spoofing detection, WORM audit logging with SHA-256 chain verification |
| Security | ✅ Partial | TLS 1.2+ support in orchestrator (cert/key configurable). mTLS, HSM, Vault still design targets |

## Architecture

```
 ┌─────────────────────────────────────────────────────┐
 │                     HOT PATH                         │
 │                                                      │
 │ [Network Ingestion] (C++, UDP multicast)            │
 │         ↓                                           │
 │ [Shared Memory Ring Buffer] (mmap SPSC)             │
 │         ↓                                           │
 │ [Risk Gate] (Rust - pre-trade checks)               │
 │         ↓                                           │
 │ [Shared Memory Ring Buffer] (mmap SPSC)             │
 │         ↓                                           │
 │ [Matching Engine] (C++ - order matching)            │
 └─────────────────────────────────────────────────────┘
         ↓
 ┌─────────────────────────────────────────────────────┐
 │                    WARM PATH                         │
 │                                                      │
 │ [KDB+ Tick DB] (Q - post-trade analytics)           │
 │ [OCaml Portfolio] (Gradient descent optimization)   │
 │ [Go Orchestrator] (Health checks, config mgmt)      │
 └─────────────────────────────────────────────────────┘
         ↓
 ┌─────────────────────────────────────────────────────┐
 │                    COLD PATH                         │
 │                                                      │
 │ [Python] Backtesting, model validation               │
 │ [R] Statistical analysis, regulatory reporting       │
 └─────────────────────────────────────────────────────┘
```

## Build & Run

```bash
# Prerequisites: g++-12+, rustc 1.78+, Go 1.22+, OCaml 5.1+, Python 3.11+
make build-cpp    # Build C++ matching engine + pricing
make build-rust   # Build Rust risk gate
make build-go     # Build Go orchestrator
make build        # Build all available services

# Run individual components
./services/execution-core/build/matching_engine
./services/gateway/orchestrator
```

## Key Limitations

- No real DPDK kernel bypass (requires Intel E810 hardware + DPDK 23.11+)
- No actual FPGA synthesis (requires Xilinx Alveo U50 + Vitis HLS)
- No production compliance (SEC 15c3-5, MiFID II RTS 25 are unimplemented)
- No security (TLS, mTLS, HSM, Vault are design targets only)
- No horizontal scaling or failover
- No observability stack (logging, metrics, tracing are absent)
- No integration tests (components never tested together end-to-end)

## Compliance & Security Status

- **SEC Rule 15c3-5**: Design specification only, NOT implemented
- **MiFID II RTS 25**: Clock sync targets are speculative, NOT configured
- **FINRA Rule 3110**: NOT implemented
- **SOC 2 Type II**: NOT implemented
- **TLS 1.3 / mTLS**: NOT implemented
- **HSM / Vault**: NOT implemented
- **Authentication / Authorization**: NOT implemented

## Project Structure

```
services/
├── execution-core/        C++20 matching engine, lock-free SPSC queues
├── network-bridge/        DPDK kernel bypass stubs (placeholder)
├── ingestion/             UDP multicast ITCH/OUCH parser
├── hardware-fpga/         Xilinx Alveo software simulation (Vitis HLS kernel code)
├── risk-analytics/        Rust pre-trade risk gate
├── gateway/               Go orchestrator with HTTP API
├── compliance/            EMPTY - compliance module shell
├── portfolio/             OCaml portfolio optimization
├── strategy-engine/       Python backtesting, R analytics
├── pricing/               Monte Carlo options pricing (C++)
├── ai-engine/             ML model validation (Python), ONNX stub (C++)
├── kdb-storage/           Q/KDB+ tick database schemas
└── kernel/                GPIO kill switch kernel module
```
