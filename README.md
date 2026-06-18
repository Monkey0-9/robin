# Robin Trading Platform

Production-grade, ultra-low latency quantitative trading system.

## Architecture

```
[Market Data Feeds] --> [DPDK/ef_vi Kernel Bypass] --> [Lock-Free SPSC Queue]
                                                             |
                                                             v
                                                     [C++20 Order Matching]
                                                        (AVX-512 FPGA)
                                                             |
                                                             v
                                                     [Rust Risk Gate]
                                                    (SEC 15c3-5 Hard Blocks)
                                                             |
                                                             v
                                                     [KDB+ Tick DB] [Aeron IPC]
```

## Performance Targets

| Metric | Target | Current |
|--------|--------|---------|
| Tick-to-Trade (p50) | <200ns | [MEASURED] |
| Tick-to-Trade (p99) | <500ns | [MEASURED] |
| Risk Gate (hard blocks) | <100ns | [MEASURED] |
| Market Data RX | <50ns | [MEASURED] |
| Throughput | 1M+ orders/sec | [MEASURED] |
| Availability | 99.999% | [MEASURED] |

## Stack

| Layer | Language | Technology |
|-------|----------|------------|
| Order Matching | C++20 | Lock-free SPSC, AVX-512, FPGA (Alveo) |
| Network RX | C++20 | DPDK / Solarflare ef_vi |
| Risk Gate | Rust | Zero-allocation, shared memory IPC |
| IPF | C++ | Shared memory (mmap) |
| Network | C++ | Aeron UDP multicast |
| Portfolio Opt | OCaml | Functional, pre-computed signals |
| Tick DB | Q/KDB+ | Partitioned, compressed |
| Analytics | R | GARCH, VaR, regulatory reports |
| Orchestration | Go | Health checks, hot-reload config |
| ML Research | Python | PyTorch, backtesting |
| UI | TypeScript/Rust | Next.js, Tauri, WebGL2, WASM |

## Build & Run

```bash
# Full build
make all

# Run latency benchmark
make benchmark

# Run chaos engineering
make chaos

# Start services (production)
sudo ./scripts/start_native.sh
```

## Hardware Requirements

- **CPU**: Intel Xeon Scalable 8480+ (Sapphire Rapids) or AMD EPYC 9654
- **NIC**: Intel E810-CQDA2 (100GbE) or Solarflare XtremeScale X2522
- **FPGA**: Xilinx Alveo U50 (8GB HBM2) or U200
- **PTP**: Oscilloquartz OSA 5430 with atomic clock holdover
- **Memory**: 512GB DDR5-4800, NUMA-aware
- **Storage**: NVMe Samsung PM1733 3.84TB

## Compliance

- SEC Rule 15c3-5: Pre-trade risk controls (hard + soft blocks)
- MiFID II RTS 25: Clock synchronization (<100ns to UTC)
- SEC CAT: Automated reporting via R pipeline
- SOC 2 Type II: Vendor due diligence
- ISO 27001: Information security management

## Directory Structure

```
services/
├── execution-core/     # C++20 matching engine, lock-free queues
├── network-bridge/     # DPDK/Solarflare kernel bypass
├── ingestion/          # ITCH/OUCH parser
├── hardware-fpga/      # Xilinx Alveo (Vitis HLS kernel)
├── risk-analytics/     # Rust SEC 15c3-5 risk gate
├── gateway/            # Go orchestrator
├── compliance/         # OCaml + Rust compliance rules
├── portfolio/          # OCaml portfolio optimization
├── strategy-engine/    # Python backtesting, R analytics
├── pricing/            # Monte Carlo (C++)
├── ai-engine/          # ONNX inference (C++)
├── kdb-storage/        # Q/KDB+ tick database
└── kernel/             # GPIO kill switch kernel module
```
