# Architecture (Current State)

This document describes the CURRENT architecture of the Robin trading platform prototype, not an aspirational target.

## Overview

The platform follows a pipeline architecture with three tiers: hot path (<1ms target), warm path (1-10ms), and cold path (offline/batch).

## Hot Path Components

### 1. Network Ingestion (C++)
**Location**: `services/ingestion/cpp_ingestion.cpp`
**Current**: UDP multicast socket receiving ITCH/OUCH protocol messages. Parses add order, executed, and cancel messages. Forwards parsed orders via shared memory ring buffer.
**Limitations**: No DPDK kernel bypass (requires Intel E810 hardware). Uses standard Linux sockets with ~5-10μs latency.

### 2. Shared Memory IPC (C++/Rust)
**Location**: `services/shared/ipc/lockfree_ring_buffer.hpp`, `services/risk-analytics/src/shm_bridge.rs`
**Current**: Lock-free SPSC (Single Producer, Single Consumer) ring buffers over mmap shared memory. Cache-line aligned atomic indexes. Two independent implementations exist (C++ and Rust) that need unification.
**Limitations**: `shm_bridge.rs` uses `write_volatile`/`read_volatile` instead of proper atomic operations. No memory barrier guarantees between write and index update.

### 3. Risk Gate (Rust)
**Location**: `services/risk-analytics/src/gate.rs`
**Current**: Pre-trade compliance checks including kill switch status, circuit breaker, duplicate detection (fixed-size direct-mapped array), fat-finger protection, price collar, position limits, velocity rate limiting.
**Limitations**: Non-atomic counters in `risk_gate_fast.rs`, `current_drawdown` in `circuit_breaker.rs` is non-atomic (data race), floating-point on hot path.

### 4. Matching Engine (C++)
**Location**: `services/execution-core/src/matching_engine.cpp`
**Current**: Lock-free matching thread with SPSC input/output queues. Supports LIMIT, MARKET, IOC, FOK order types. Basic FIFO matching within price levels.
**Limitations**: Uses `std::vector` (heap allocation) for order queues. No price-time priority (FIFO only). `get_or_create_book()` heap-allocates under lock.

## Warm Path Components

### 5. KDB+ Tick Database (Q)
**Location**: `services/kdb-storage/tick_db.q`
**Current**: Basic trade and quote table schemas. No sym enumeration, no partitioning config, no TP/RDB/HDB gateway architecture.

### 6. OCaml Portfolio Optimizer
**Location**: `services/portfolio/portfolio_opt.ml`
**Current**: Gradient descent optimizer with simplex projection for maximum Sharpe ratio portfolio. VaR calculation with covariance matrix.

### 7. Go Orchestrator
**Location**: `services/gateway/orchestrator.go`
**Current**: HTTP server with health checks, service registry, hot-reloadable config, metrics endpoint.

## Cold Path Components

### 8. Python Backtester
**Location**: `services/strategy-engine/backtester.py`
**Current**: Basic vectorized backtesting with buy/sell signals, Sharpe ratio, max drawdown calculation.

### 9. R Analytics
**Location**: `services/strategy-engine/risk_analytics.R`
**Current**: GARCH volatility modeling, VaR calculation, SEC CAT report generation stub.

## Hardware Abstraction

### FPGA Emulator
**Location**: `services/hardware-fpga/fpga_emulator.cpp`
**Current**: Software simulation of FPGA order matching using std::memcpy on regular DRAM. This is NOT hardware acceleration.
**Vitis HLS**: `services/hardware-fpga/vitis_hls_order_match.cpp` contains HLS kernel code that is syntactically valid but has synthesis issues (variable-bound UNROLL pragma).

### GPIO Kill Switch
**Location**: `services/kernel/gpio_kill.c`
**Current**: Linux kernel module that monitors a GPIO pin and blocks all outgoing IPv4 traffic via netfilter when triggered.

## IPC Architecture

```
┌──────────────┐     mmap SPSC      ┌──────────────┐     mmap SPSC      ┌──────────────┐
│  Ingestion   │ ────────────────>  │  Risk Gate    │ ────────────────>  │   Matching    │
│  (C++ UDP)   │     ring buf       │  (Rust)       │     ring buf       │   Engine      │
└──────────────┘                    └──────────────┘                    └──────────────┘
```

## Known Issues

See PERFORMANCE.md for actual (not target) latency measurements.
See SECURITY.md for security limitations.
See COMPLIANCE.md for compliance status.
