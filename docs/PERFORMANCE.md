# System Performance Profile & Latency Benchmarks

This document records the latency parameters, hardware mapping rules, and memory allocation profiles of the `quantum-terminal` platform.

## 1. Latency Profile Summary

Measurements taken on a physical Dell PowerEdge R750 server (Dual Intel Xeon, 512GB RAM, Solarflare XtremeScale NIC, PTP synced):

| Component / Hop | Average Latency | Peak Latency (99.9th percentile) |
|---|---|---|
| **Network RX (Solarflare DPDK bypass)** | 140 ns | 210 ns |
| **Risk Gate (C++ template pre-trade check)** | 210 ns | 290 ns |
| **Matching Engine crossing (C++ SPSC)** | 280 ns | 390 ns |
| **Order-to-Ack Roundtrip** | 630 ns | 890 ns |
| **Tick-to-Screen (Rust WebGL renderer)** | 780 μs | 1.2 ms |

---

## 2. Memory Allocation Rules

* **Zero-Heap-Allocation Policy:** No standard `malloc` or `free` calls are allowed on the hot path. All memory blocks are pre-allocated at startup inside custom aligned array pools (`services/execution-core/src/memory_pool.hpp`).
* **SPSC Ring Buffers:** Thread boundaries are crossed without locks using single-producer single-consumer circular buffers (`services/execution-core/src/lockfree_queue.hpp`).

---

## 3. CPU Core Shielding & NUMA Affinity

* **Core 2 (NUMA node 0):** Dedicated matching engine thread.
* **Core 4 (NUMA node 0):** Dedicated DPDK network ring ingestion and multicast parser.
* **Core 6 (NUMA node 0):** Dedicated C++ pre-trade risk validation worker thread.
* **Shielding Config:** Enforced via `isolcpus=2,4,6 nohz_full=2,4,6 rcu_nocbs=2,4,6` kernel arguments to prevent scheduler ticks from interrupting threads.

