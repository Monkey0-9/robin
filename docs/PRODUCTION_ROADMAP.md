# Implementation Roadmap (Aspirational)

> [!WARNING]
> This roadmap outlines future steps required to transition from research prototype to production system. **Several prototype phases are fully implemented in the current platform.**

## Phase 1: Hot Path Foundation (Weeks 1-4)

- [x] Implement shared memory SPSC ring buffer (mmap, cache-line aligned)
- [x] Build Rust risk gate with lock-free data structures (pre-allocated, no HashMap)
- [x] Build C++ matching engine with price-time priority (not FIFO)
- [ ] Implement real network ingestion (UDP multicast, replace mock data)
- [ ] Integration tests for all hot path components

## Phase 2: Network & FPGA (Weeks 5-8)

- [ ] Deploy DPDK with Intel E810-CQDA2 (requires hardware)
- [ ] Implement PTP synchronization (linuxptp, hardware timestamps)
- [ ] Configure CPU isolation (isolcpus, nohz_full, rcu_nocbs)
- [ ] NUMA-aware memory allocation
- [ ] Actual FPGA synthesis for Xilinx Alveo (requires hardware + Vitis HLS)

## Phase 3: Compliance & Risk (Weeks 9-12)

- [ ] Build SEC 15c3-5 hard/soft block architecture (production-grade)
- [x] Implement immutable audit logging (SHA-256 chained, tamper-evident)
- [ ] Implement CEO certification workflow
- [ ] 90-day continuous latency validation framework
- [ ] Post-trade surveillance integration

## Phase 4: Analytics & Warm Path (Weeks 13-16)

- [ ] Production KDB+ schema (sym enum, par.txt, TP/RDB/HDB gateway)
- [ ] Real ONNX inference integration (cold path)
- [ ] Production security implementation (TLS, Vault, HSM)
- [x] Go orchestrator service discovery and health checks

## Phase 5: Security & Hardening (Weeks 17-20)

- [ ] HashiCorp Vault for secrets management
- [ ] TLS 1.3 / WireGuard for encryption
- [ ] HSM (Thales Luna 7) for key storage (requires hardware)
- [ ] SBOM generation (SPDX/CycloneDX)
- [ ] Dependency scanning (cargo audit, etc.)
- [ ] Penetration testing

## Phase 6: Chaos Engineering & Validation (Weeks 21-24)

- [ ] Chaos testing (network latency, packet loss, memory pressure)
- [ ] Disaster recovery (RPO <1s, RTO <5min)
- [ ] Independent third-party audit
- [ ] SOC 2 Type II certification preparation
