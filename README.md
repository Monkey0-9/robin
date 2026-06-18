# Robin Trading Platform

Ultra-low latency quantitative trading system.

## Architecture

```
 ┌────────────────────────────────────────────────────────────────────┐
 │                        HOT PATH (<500ns)                          │
 │                                                                    │
 │  [Network] ──DPDK──> [Shared Memory Ring Buffer 1]                  │
 │   (C++ DPDK)         (mmap, cache-aligned, Cap'n Proto)             │
 │                           │                                         │
 │                           v                                         │
 │                      [Risk Gate]                                    │
 │                       (Rust)                                        │
 │                       - Hard blocks: <100ns                        │
 │                       - Soft blocks: <100ns                        │
 │                       - bumpalo allocator (zero-heap)              │
 │                           │                                         │
 │                           v                                         │
 │                      [Shared Memory Ring Buffer 2]                  │
 │                           │                                         │
 │                           v                                         │
 │                      [Matching Engine]                              │
 │                       (OCaml)                                       │
 │                       - Bigarray order book (off-heap)             │
 │                       - Custom GC tuning (8MB minor heap)          │
 │                       - Branchless pattern matching                │
 │                           │                                         │
 │                           v                                         │
 │                      [Shared Memory Ring Buffer 3]                  │
 └────────────────────────────────────────────────────────────────────┘
         │
         v
 ┌────────────────────────────────────────────────────────────────────┐
 │                        WARM PATH (1-10ms)                          │
 │                                                                    │
 │  [KDB+ Tick DB] ← Aeron IPC ← Shared Memory Ring Buffer 3         │
 │   (Post-trade capture, NOT hot path)                              │
 │                                                                    │
 │  [OCaml Portfolio Optimization]  (Jane Street Core style)          │
 │  [Go Orchestrator]              (Health checks, config mgmt)      │
 └────────────────────────────────────────────────────────────────────┘
         │
         v
 ┌────────────────────────────────────────────────────────────────────┐
 │                        COLD PATH (offline/batch)                   │
 │                                                                    │
 │  [Python]  ML research, backtesting (PyTorch)                      │
 │  [R]       Statistical analysis, regulatory reporting              │
 │  [ONNX]    Pre-computed alpha signals (updated every 1-10ms)      │
 │  [KDB+ HDB] Historical partitioned database                        │
 └────────────────────────────────────────────────────────────────────┘
```

## Language Boundaries (Zero FFI on Hot Path)

All IPC between hot-path services uses **shared memory (mmap)** with lock-free SPSC queues — NOT FFI calls:

| Boundary | Mechanism | Latency |
|----------|-----------|---------|
| C++ Network → Rust Risk | mmap ring buffer (Cap'n Proto) | <50ns |
| Rust Risk → OCaml Match | mmap ring buffer (Cap'n Proto) | <50ns |
| OCaml Match → KDB+ | Aeron IPC | <100ns |

## Performance Targets

| Metric | Target | Measurement Method |
|--------|--------|-------------------|
| Network RX (DPDK burst) | <100ns | RDTSCP + hardware timestamps |
| Risk Gate (hard blocks) | <100ns | HdrHistogram (p50/p99/p999) |
| Risk Gate (soft blocks) | <100ns | HdrHistogram (p50/p99/p999) |
| Matching Engine | <300ns | HdrHistogram (p50/p99/p999) |
| IPC (mmap SPSC) | <50ns | RDTSCP before/after push/pop |
| End-to-End (p50) | <300ns | Aggregated HdrHistogram |
| End-to-End (p99) | <800ns | Aggregated HdrHistogram |
| End-to-End (p999) | <1.5μs | Aggregated HdrHistogram |
| Throughput | 1M+ orders/sec | Sustained 24-hour load test |
| Availability | 99.999% | 5.26 min/year downtime |

**IMPORTANT**: All latency figures are **targets** measured under controlled conditions (specified hardware, sustained 1M msg/sec load). See `docs/PERFORMANCE.md` for full methodology, hardware specifications, and 90-day validation results.

## Stack

| Layer | Language | Technology | Notes |
|-------|----------|------------|-------|
| Network RX | C++20 | DPDK 23.11+ | Intel E810-CQDA2 (100GbE) |
| Risk Gate | Rust | crossbeam SPSC, bumpalo | Zero-heap after init |
| Matching Engine | OCaml | Bigarray, custom GC | Off-heap order book |
| FPGA Accel | Vitis HLS | Xilinx Alveo U50 (SIMULATION) | Software simulation for dev |
| Portfolio Opt | OCaml | Pre-computed signals | Warm path only |
| Tick DB | Q/KDB+ | Aeron IPC subscriber | Post-trade analytics |
| Orchestration | Go | gorilla/mux, Prometheus | Health checks, config |
| Analytics | R | GARCH, VaR | Regulatory reports |
| ML Research | Python | PyTorch | Cold path (offline) |
| UI | TypeScript/Rust | Next.js, Tauri, WebGL2 | Trader dashboard |

## Build & Run

```bash
# Full build (Linux with DPDK, Rust, OCaml toolchains)
make all

# Run latency benchmark with HdrHistogram
make benchmark

# Run HdrHistogram-based latency validation (1M iterations)
./build/order_book_benchmark 1000000 100000

# Run chaos engineering tests
make chaos

# Start services (production)
sudo ./scripts/start_native.sh
```

## Latency Measurement Methodology

- **Tool**: Custom HdrHistogram implementation (nanosecond resolution)
- **Metrics**: p50, p90, p95, p99, p99.9, p99.99, p99.999, mean, max
- **Clock**: RDTSCP with invariant TSC, calibrated against PTP
- **Hardware Timestamps**: NIC-assisted (DPDK `rte_eth_read_clock`)
- **Warmup**: 100k iterations before measurement
- **Sample Size**: 1M+ iterations per run
- **Sustained Load**: 1M+ msg/sec for 24 hours
- **Regression Detection**: Automated CI/CD (5% p99 increase = FAIL)

### Hardware Specifications (Benchmark Environment)

| Component | Specification |
|-----------|--------------|
| CPU | Intel Xeon 8480+ (Sapphire Rapids), 56C/112T |
| CPU Freq | 3.5GHz base, Turbo Boost DISABLED |
| NIC | Intel E810-CQDA2 (100GbE), firmware v4.2 |
| Memory | 512GB DDR5-4800 (12x32GB), NUMA node 0 |
| OS | RHEL 9.2 RT (PREEMPT_RT), kernel 6.5.7-rt14 |
| PTP | Oscilloquartz OSA 5430, <100ns to UTC |
| Compiler | g++ 13.2.1, rustc 1.78.0, ocamlopt 5.1.1 |

## Shared Memory IPC (mmap Ring Buffer)

```cpp
// services/shared/ipc/lockfree_ring_buffer.hpp
// Cache-line aligned SPSC ring buffer over mmap
// Used for all inter-service communication on hot path
```

### Protocol: Cap'n Proto (Zero-Copy Serialization)

All hot-path messages use Cap'n Proto for zero-copy serialization/deserialization. Schema files in `services/shared/schemas/`.

## FPGA Acceleration

The FPGA emulator in `services/hardware-fpga/` is a **software simulation layer** for development and testing. Production deployment requires actual Xilinx Alveo U50 hardware with Vitis HLS kernel synthesis.

## Compliance

- **SEC Rule 15c3-5**: Complete hard/soft block architecture
  - Hard blocks (immutable): Order size limits, price collars, credit limits, symbol restrictions, duplicate detection
  - Soft blocks (configurable): Position limits, velocity limits, concentration limits
  - CEO annual certification workflow (DocuSign integration)
  - Third-party independence verification
  - Immutable audit log (SHA-256 chained)
- **MiFID II RTS 25**: PTP clock synchronization (<100ns to UTC)
- **FINRA Rule 3110**: Immediate execution reporting
- **SOC 2 Type II**: Security, availability, confidentiality

## Hot Path Language Strategy

| Language | Role | GC? | Heap Allocation? |
|----------|------|-----|------------------|
| C++ | Network RX (DPDK) | No | Placement new, object pools |
| Rust | Risk Gate | No | bumpalo arena, pre-allocated |
| OCaml | Matching Engine | Yes (tuned) | Bigarray (off-heap) |

**Key Principle**: Zero FFI calls on hot path. Zero heap allocation after initialization. All IPC via shared memory (mmap).

## Directory Structure

```
services/
├── execution-core/        # C++20 matching engine, lock-free queues
│   └── bench/             # HdrHistogram latency benchmarks
├── network-bridge/        # DPDK kernel bypass (Intel E810)
├── ingestion/             # ITCH/OUCH parser (AVX-512 SIMD)
├── hardware-fpga/         # Xilinx Alveo simulation (Vitis HLS kernel)
├── risk-analytics/        # Rust SEC 15c3-5 risk gate
├── gateway/               # Go orchestrator
├── compliance/            # OCaml + Rust compliance rules
├── portfolio/             # OCaml portfolio optimization
├── strategy-engine/       # Python backtesting, R analytics
├── pricing/               # Monte Carlo options (C++)
├── ai-engine/             # ONNX inference (C++, COLD path only)
├── kdb-storage/          # Q/KDB+ tick database (WARM path)
└── kernel/               # GPIO kill switch kernel module
```
