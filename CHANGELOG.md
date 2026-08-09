# Changelog

All notable changes to the Robin Platform will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.4.0] - 2026-08-09

### Added

- **Gateway — Phase 3 (Smart Order Routing, Reconciliation, Circuit Breaker, Bulk Orders)**:
  - **Real SOR across live venues (3.1)**: added `services/gateway/nbbo.go` live NBBO cache (per-venue best bid/ask with staleness window) fed by the Coinbase, Binance, and Kraken WebSocket streams; `RouteOrder` now routes on live national best bid/offer (`routeOnLiveQuotes`) and only falls back to synthetic quotes while feeds warm up. NBBO aggregation, multi-venue routing, and stale-feed exclusion covered by tests in `sor_test.go`.
  - **Order state reconciliation (3.5)**: new `services/gateway/reconciliation.go` + `OrderStateMachine.Restore` rehydrate the in-memory order state machine from the durable SQLite `orders` table after restart or divergence; reports matched / rehydrated / orphaned orders via `GET /api/orders/reconcile` (admin).
  - **Circuit breaker integration (3.6)**: new `services/gateway/circuit_breaker.go` mirrors the risk engine's daily-drawdown breaker — trips from local peak/current equity against `max_drawdown_limit`, polls the risk engine's Prometheus endpoint for `robin_risk_circuit_breaker_trips_total`, and provides manual admin trip/reset. Tripped breaker blocks `/order` and `/api/orders/bulk` entry with `CIRCUIT_BREAKER_TRIPPED`. WebSocket broadcast, `kill_switch_log` persistence, and `robin_gateway_circuit_breaker_active` gauge included.
  - **Bulk order API (3.7)**: `POST /api/orders/bulk` submits up to 500 orders with up-front validation (atomic rejection of an invalid batch), single-transaction DB persistence, and per-order engine results; covered by tests in `bulk_order_test.go`.
  - **Per-account P&L (3.6)**: `PositionManager.RecordAccountFill` / `GetAccountPnL` and `GET /api/positions/accounts`.
  - **Hot-reload atomic rollback (3.3)**: `atomicWriteFile` (temp file + fsync + atomic rename + dir sync) used by `persistConfig`; `HotReloadConfig` rolls back in-memory state on persistence failure.
  - **Symbol map from DB (3.4)**: `loadSymbolsFromDB` / `persistSymbolToDB` backed by the new `instruments` reference table so symbol→id mappings survive restarts.

## [1.3.0] - 2026-08-03

### Added

- **CEO Demo Integration & Real-Time Feeds**:
  - Integrated live Coinbase WebSocket feed ingestion for real-time market data streaming.
  - Implemented `lightweight-charts` visual components in terminal dashboard for low-latency market charting.
  - Expanded Rust Risk & Compliance Engine metrics and RBAC role optimization across gateway endpoints.
- **API CORS & Preflight Hardening**:
  - Implemented full CORS middleware and explicit preflight `OPTIONS` handling in Go Gateway API (`services/gateway/main.go`).
  - Synchronized `Authorization` header propagation with JWT bearer tokens across Next.js frontend requests.

### Fixed

- **Multi-Service Build & Compilation Stability**:
  - Resolved Go Gateway compilation and handler wiring issues (`oms.go`, `main.go`).
  - Fixed Rust build errors and type definitions across `risk-analytics` and `compliance` services (`gpio_kill_switch.rs`, `gate.rs`).
  - Fixed C++ Execution Engine compilation and linking errors in `network-bridge` (`dpdk_ingest.cpp`, `CMakeLists.txt`).

---

## [1.2.0] - 2026-07-20

### Added
- **AI Agent Platform & Model Suite (Phases 7-12)**:
  - Implemented multi-agent trading system in `services/ai-agent/agents.py` with specialized agents: Orchestrator, Data Engine, Macro Analyst, Model Trainer, Portfolio Optimizer, Model Risk, Strategy Engine, Position Sizer, and Live Feed.
  - Added pre-packaged 100-year historical backtesting Parquet datasets for AAPL, BTC_USD, ETH_USD, EUR_USD, and TSLA.
  - Developed historical backtesting engine with realistic fees and slippage models (`services/ai-agent/backtester.py`, `services/ai-agent/strategy_engine.py`).
  - Added secure vault credentials handling in `services/ai-agent/secret_vault.py`.
- **Advanced Execution & Gateway Controls**:
  - Implemented a Best Price Smart Order Router (SOR) in `services/gateway/best_execution.go`.
  - Added Consolidated Audit Trail (CAT) reporting framework in `services/gateway/cat_reporter.go`.
  - Developed Compliance Certification flow (`services/gateway/compliance_certification.go`) and Post-Trade Surveillance monitoring (`services/gateway/post_trade_surveillance.go`).
  - Added Supervisory API (`services/gateway/supervisory_api.go`) and Multi-Factor Authentication (MFA) handlers (`services/gateway/mfa.go`).
  - Integrated high-resolution Time Synchronization check (`services/gateway/time_sync.go`).
  - Built Kernel-level Kill Switch dashboard & logic (`services/gateway/kill_switch.go`).
  - Added database migrations to ensure table schema coherence on SQLite start (`schema_sqlite.sql`).
- **C++ Execution Core Enhancements**:
  - Created C++ Strategy Engine (`strategy_engine.hpp`) and live feed components (`live_feed.cpp`) in `services/execution-core`.
  - Added `strategy_benchmark.cpp` to benchmark execution path latency under trading simulations.
- **Security Remediation & mTLS**:
  - Upgraded JWT signature verification from HS256 to asymmetric RS256 with key pair generation.
  - Added automated RSA key generation (`scripts/generate_rsa_keys.sh`), mTLS setup (`scripts/setup_mtls.sh`), and periodic key rotation (`scripts/rotate_keys.sh`).
  - Implemented data backup utilities (`scripts/backup.sh`) and model downloaders (`scripts/download_models.sh`).
- **Observability & Frontend Terminal**:
  - Added Prometheus alerting rules (`config/alerting_rules.yml`) and Grafana dashboard (`config/grafana_dashboard.json`).
  - Added React frontend panels for the terminal: `AIAutonomousPanel` and `ComplianceDashboard` for real-time visualization of autonomous model signals and compliance checks.
- **Onboarding Guides**:
  - Added `docs/AI_ONBOARDING.md` to guide developers and incoming AI models on platform capabilities, API keys, backtests, and pipeline mechanics.

### Fixed
- Fixed double-scaling bug of price and quantity fields (scaled to 1e8) in Rust risk-analytics daemon.
- Resolved MSVC compiler warnings and Clippy lints across Rust and C++ components.
- Fixed `pytest.ini` exclusions to bypass symlinks that caused OS collection errors.
- Corrected mTLS certificate loading paths and execution paths in `scripts/smoke_test.ps1`.

---

## [1.1.0] - 2026-06-30

### Added
- Comprehensive `CHANGELOG.md` file.
- `CONTRIBUTING.md` file detailing guidelines for team collaboration.
- Connection pooling documentation.
- Sample configuration file (`config/robin_config.example.yaml`) for quick developer setup.
- Health check endpoints (`/ready`, `/live`) across Gateway and AI Agent services for better orchestration and Kubernetes support.
- Graceful shutdown lifecycle hooks in the AI Agent (FastAPI).

### Changed
- Configured rate limiter tuning applied to the `/order` and `/config` endpoints in the Gateway to prevent overload during traffic spikes.
