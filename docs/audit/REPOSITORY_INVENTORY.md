# Robin Quantitative Trading Platform — Comprehensive Repository Inventory

**Document ID:** AUDIT-INV-202608-01  
**Generated:** 2026-08-18  
**Classification:** Institutional Architecture Audit  

---

## 1. Executive Metrics

| Dimension | Count | Details |
| :--- | :--- | :--- |
| **Total Source Files** | 184 | Spanning C++20, Rust, Go, Python, OCaml, Q/KDB+, C, TypeScript, SQL |
| **Total Languages** | 9 | C++20, Rust (2021 edition), Go 1.22, Python 3.11, OCaml 4.14, Q, C99, TypeScript 5.0, SQL |
| **Active Services** | 12 | Matching Engine, Risk Analytics, Compliance, Gateway, Ingestion, Pricing, Portfolio, KDB+ Tickplant, AI Agent, Swarm, Frontend, Kernel Module |
| **Container Images** | 8 | Production Docker Compose definitions (`infra/docker-compose.prod.yml`) |
| **Total Unit/Integration Tests** | 137+ | Rust (76), Go (34), Python (40), C++ benchmarks & property tests |

---

## 2. Directory Structure & Component Taxonomy

```text
c:\Robin\
├── .github/workflows/          # CI/CD pipelines (15 jobs: build, test, fuzz, pentest, SBOM)
├── config/                     # Configuration templates, SHM layouts, and YAML profiles
├── deploy/                     # Deployment scripts, systemd unit files, and cloud manifests
├── docs/                       # Architectural specifications, runbooks, and audit trails
│   ├── architecture/           # Canonical system specifications and hot/warm/cold path contracts
│   └── audit/                  # Acceptance matrices, inventory logs, and institutional audits
├── frontend/                   # Next.js 16 (Turbopack) institutional trading terminal
│   ├── src/components/         # L2/L3 order books, depth charts, blotters, risk gauges
│   └── src/store/              # Zustand terminal store with real-time WebSocket subscriptions
├── infra/                      # Orchestration & telemetry infrastructure
│   ├── docker-compose.prod.yml # Production 12-service stack with rtprio=99 and IPC_LOCK
│   ├── grafana/dashboards/     # Sub-microsecond latency, VaR, and compliance Grafana JSONs
│   ├── prometheus/             # Alerting rules (alerts.yml) and scraping configs
│   └── tls/                    # mTLS certificates, CA authority, and Vault PKI bindings
├── protos/                     # Protocol Buffer definitions for gRPC and binary serialization
├── research/                   # Quantitative research, alpha modeling, and historical backtesting
│   ├── ai-engine/              # Microstructure feature engineering & ONNX inference
│   └── strategy-engine/        # Backtester, corporate actions, correlation, and R analytics
├── scripts/                    # Hermetic build, benchmarking, chaos engineering, and pentesting
├── services/                   # Polyglot core services
│   ├── ai-agent/               # Python multi-agent LLM macro/sentiment & Kelly position sizing
│   ├── compliance/             # Rust SEC 15c3-5, MiFID II RTS 22/25, CAT XML, and WORM logger
│   ├── execution-core/         # C++20 price-time matching engine, FIX/OUCH codecs, NUMA pools
│   ├── gateway/                # Go OMS/SOR gateway with Vault secrets and async WAL
│   ├── ingestion/              # C++ DPDK 23.11 kernel-bypass & zero-copy ITCH/XDP parsers
│   ├── kdb-storage/            # KDB+/Q Tickplant (TP), RDB, HDB, and REST query bridge
│   ├── kernel/                 # Linux kernel module GPIO netfilter emergency kill switch
│   ├── market-data/            # Consolidated feed multiplexer and candle aggregators
│   ├── portfolio/              # Go & OCaml portfolio optimizer and Sharpe gradient descent
│   ├── pricing/                # C++ & CUDA GPU options pricing (cuRAND Monte Carlo / Greeks)
│   ├── risk-analytics/         # Rust 16-shard lock-free pre-trade risk gate & SIMD VaR/CVaR
│   └── shared/                 # Single-source-of-truth C/Rust/Go headers and IPC constants
└── tests/                      # End-to-end integration, failure injection, and load tests
```

---

## 3. Communication, IPC & Port Allocation Map

| Service | Port / IPC Channel | Protocol | Transport | Latency Target |
| :--- | :--- | :--- | :--- | :--- |
| **Ingestion Engine** | UDP Multicast | NASDAQ ITCH 5.0 / NYSE XDP | DPDK PMD Kernel Bypass | <2 μs |
| **Ingest → Risk Bridge** | `/robin_ingest_risk` | 64-byte aligned SPSC ring | POSIX SHM (Release/Acquire) | <100 ns |
| **Risk Analytics Gate** | `/robin_risk_match` | SPSC lock-free ring | POSIX SHM (Hugepages) | <500 ns |
| **Matching Engine** | `/robin_match_storage` | SPSC lock-free ring | POSIX SHM (Hugepages) | <2 μs |
| **Go Gateway / OMS** | `8080` (HTTP) / `8081` (WS) | REST JSON / WebSockets | TCP / TLS 1.3 | <1 ms |
| **KDB+ Tickerplant** | `5000` (QIPC) / `5001` (HTTP) | QIPC / REST JSON | TCP | <1 ms |
| **Risk Metrics Daemon** | `9090` (Prometheus) | Prometheus Text / OTLP | TCP | <5 ms |
| **Compliance Daemon** | `9095` (HTTP) | JSON Status / WORM Log | TCP / File WAL | <10 ms |
| **Next.js Trading Terminal** | `3000` (HTTP) | HTTP / React SSR | TCP | <20 ms |
| **Kernel Kill Switch** | GPIO Pin / Netfilter Hook | Kernel IRQ / Drop Rule | Hardware PCIe | <100 ns |

---

## 4. Build & Verification Systems

* **Make:** Master [`Makefile`](file:///c:/Robin/Makefile) with `build`, `test`, `benchmark`, `lint`, `fmt`, `docker`, and `deploy` targets.
* **CMake:** [`services/execution-core/CMakeLists.txt`](file:///c:/Robin/services/execution-core/CMakeLists.txt) targeting C++20 with `-O3 -mavx2 -lnuma -lpthread`.
* **Cargo:** Rust 2021 workspaces in `services/risk-analytics` and `services/compliance`.
* **Go Toolchain:** Go 1.22 modules in `services/gateway`, `services/portfolio`, and `tests/integration`.
* **Python/Pytest:** Python 3.11 virtual environment with `pytest`, `numpy`, `pandas`, `scipy`, and `torch`.
* **Node.js/Next.js:** Next.js 16.2.12 with Turbopack bundler and TypeScript 5.0.
