# Performance Measurement (Current State)

> [!WARNING]
> All latency figures in this document are target specifications, not measured results. The prototype does not currently achieve sub-microsecond latencies.

## Current Limitations

| Metric | Target | Achieved | Notes |
|--------|--------|----------|-------|
| Matching Engine | <300ns | ~1-10μs | std::vector heap allocation on hot path |
| Risk Gate | <200ns | ~1-5μs | HashMap/linear scan, no SIMD |
| IPC (mmap SPSC) | <50ns | ~100-500ns | volatile instead of atomic in Rust bridge |
| End-to-End | <740ns | N/A | No integration tests measure this |
| Throughput | 1M/s | ~100K (est.) | Single-threaded mock only |

## Benchmarking Infrastructure

**Location**: `services/execution-core/src/latency_benchmark.hpp`
**Current**: TSC-based latency measurement with percentile calculation. Uses RDTSCP for timing and sorts samples for percentile extraction.

**Known Issues**:
- Heap allocation in constructor (`new uint64_t[max_samples_]`)
- `std::vector` and `std::sort()` on benchmark path
- No calibration against PTP wall-clock
- No handling of TSC skew across cores
- No HdrHistogram implementation despite claims

## Hardware Test Environment

**Current**: Development workstation (no specialized hardware configured)
**Target**: Intel Xeon 8480+ with Intel E810-CQDA2 100GbE, PTP grandmaster, PREEMPT_RT kernel — NOT AVAILABLE

## Kernel Boot Parameters (Target)

```
isolcpus=2-15 nohz_full=2-15 rcu_nocbs=2-15
hugepagesz=1G hugepages=1024
intel_idle.max_cstate=0 processor.max_cstate=0
intel_pstate=disable
```
These are NOT currently configured on any machine.

## 90-Day Validation Plan

Not started. No continuous latency measurement infrastructure exists.
