# Performance Measurement Methodology

## Latency Measurement

### Tool: Custom HdrHistogram

All latency measurements use a custom HdrHistogram implementation:
- **Resolution**: 1 nanosecond (via RDTSCP with invariant TSC)
- **Range**: 1ns — 1ms (sufficient for sub-microsecond targets)
- **Precision**: 3 significant figures
- **Metrics**: p50, p90, p95, p99, p99.9, p99.99, p99.999

### Clock Source
- **Primary**: RDTSCP instruction (invariant TSC, constant rate)
- **Calibration**: TSC frequency calibrated against PTP-synchronized clock
- **Accuracy**: <10ns measurement overhead

### Methodology

1. **Warmup**: 100,000 iterations (discarded)
2. **Sampling**: 1,000,000+ iterations
3. **Measurement**: RDTSCP before/after each operation
4. **Exclusion**: First 1000 samples excluded (cold start)
5. **Reporting**: p50, p99, p99.9, p99.99, p99.999, mean, max

### Measurement Points

| Point | What | Instrument |
|-------|------|------------|
| Network RX | DPDK rte_eth_rx_burst → memory | NIC hardware timestamp |
| Risk Gate | Order in → RiskResult out | RDTSCP |
| Matching | Order book match → Trade out | RDTSCP |
| IPC push/pop | Ring buffer push/pop | RDTSCP |
| End-to-End | Network RX → Trade out | Aggregated |

## Hardware Test Environment

| Component | Specification |
|-----------|--------------|
| CPU | Intel Xeon 8480+ (Sapphire Rapids), 56C/112T, 3.5GHz base |
| CPU Config | Turbo Boost DISABLED, C-states DISABLED, Prefetcher ENABLED |
| NIC | Intel E810-CQDA2 (100GbE), firmware v4.2, DPDK 23.11 |
| Memory | 512GB DDR5-4800 (12x32GB), NUMA node 0 for hot path |
| OS | RHEL 9.2 RT (PREEMPT_RT), kernel 6.5.7-rt14 |
| PTP | Oscilloquartz OSA 5430 GPS/GNSS grandmaster |
| Compiler | g++ 13.2.1, rustc 1.78.0, ocamlopt 5.1.1 |

## Kernel Boot Parameters

```
isolcpus=2-15 nohz_full=2-15 rcu_nocbs=2-15
hugepagesz=1G hugepages=1024
intel_idle.max_cstate=0 processor.max_cstate=0
intel_pstate=disable
```

## Current Measured Latencies (Targets — see Implementation Roadmap)

| Metric | Target | Status |
|--------|--------|--------|
| DPDK RX burst (64 packets) | <100ns | TARGET |
| Risk Gate hard blocks | <100ns | TARGET |
| Risk Gate soft blocks | <100ns | TARGET |
| Matching Engine | <300ns | TARGET |
| IPC (mmap SPSC) | <50ns | TARGET |
| End-to-End p50 | <300ns | TARGET |
| End-to-End p99 | <800ns | TARGET |
| End-to-End p999 | <1.5μs | TARGET |

## 90-Day Validation

- **Duration**: 90 continuous days
- **Load**: Sustained 1M+ msg/sec
- **Monitoring**: Every 5 minutes, log p50/p99/p999/p9999
- **Regression Detection**: 5% increase in p99 → automated CI/CD failure
- **Report**: Weekly automated report with trend analysis

## False Sharing Prevention

All hot-path structures are `alignas(64)` padded to cache line boundaries.
Pad bytes are explicitly placed between atomic variables in shared memory.

## NUMA Awareness

- All hot-path memory allocated on NUMA node 0
- Threads pinned via `numactl --cpubind=0 --membind=0`
- Lock-free data structures use `atomic` operations (no mutexes)
