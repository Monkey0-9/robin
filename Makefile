# ============================================================================
# Robin Quantitative Trading Platform — Master Makefile
# ============================================================================
# Institutional-grade build, test, lint, and deployment targets.
# Supports Linux (Ubuntu 22.04/24.04), WSL2, and Containerized environments.
# ============================================================================

.PHONY: all build build-cpp build-rust build-go build-frontend \
        test test-cpp test-rust test-go test-python test-frontend \
        benchmark lint fmt docker deploy clean help

# Default target
all: build test

# ── Build Targets ────────────────────────────────────────────────────────────

## Build all binaries and frontend assets
build: build-cpp build-rust build-go build-frontend

## Build C++20 matching engine and benchmarks
build-cpp:
	@echo "=== Building C++ Execution Core ==="
	mkdir -p services/execution-core/build
	cd services/execution-core/build && cmake .. -DCMAKE_BUILD_TYPE=Release && cmake --build . -j$$(nproc || echo 4)

## Build Rust risk analytics and compliance services
build-rust:
	@echo "=== Building Rust Risk Analytics & Compliance ==="
	cd services/risk-analytics && cargo build --release
	cd services/compliance && cargo build --release

## Build Go gateway and portfolio services
build-go:
	@echo "=== Building Go Gateway & Portfolio Services ==="
	cd services/gateway && go build -v -o ../../bin/robin-gateway .
	cd services/portfolio && go build -v -o ../../bin/robin-portfolio .

## Build Next.js trading terminal frontend
build-frontend:
	@echo "=== Building Next.js Frontend ==="
	cd frontend && npm install && npm run build

# ── Testing Targets ──────────────────────────────────────────────────────────

## Run complete test suite across all languages
test: test-cpp test-rust test-go test-python test-frontend

## Run C++ matching engine benchmarks and tests
test-cpp:
	@echo "=== Testing C++ Execution Core ==="
	cd services/execution-core/build && ctest --output-on-failure || ./order_book_benchmark

## Run Rust unit and integration tests
test-rust:
	@echo "=== Testing Rust Risk Analytics & Compliance ==="
	cd services/risk-analytics && cargo test --lib -- --nocapture
	cd services/compliance && cargo test --lib -- --nocapture

## Run Go unit tests
test-go:
	@echo "=== Testing Go Services ==="
	cd services/gateway && go test -v -timeout 60s ./...
	cd services/portfolio && go test -v ./...

## Run Python quantitative engine and strategy tests
test-python:
	@echo "=== Testing Python AI Agent & Backtesters ==="
	cd services/ai-agent && python test_components.py
	cd research/strategy-engine && python backtester.py

## Run frontend tests
test-frontend:
	@echo "=== Testing Frontend ==="
	cd frontend && npm test -- --run

# ── Benchmarks & Diagnostics ─────────────────────────────────────────────────

## Run system-wide throughput, latency, and compliance benchmarks
benchmark:
	@echo "=== Executing Performance Benchmarks ==="
	chmod +x scripts/benchmark.sh
	./scripts/benchmark.sh

# ── Formatting & Linting ─────────────────────────────────────────────────────

## Run linters across all codebases
lint:
	@echo "=== Running Linters ==="
	cd services/risk-analytics && cargo clippy -- -D warnings
	cd services/compliance && cargo clippy -- -D warnings
	cd services/gateway && go vet ./...
	cd frontend && npm run lint

## Auto-format all source code
fmt:
	@echo "=== Formatting Source Files ==="
	find services/execution-core/src -name "*.cpp" -o -name "*.hpp" | xargs clang-format -i 2>/dev/null || true
	cd services/risk-analytics && cargo fmt
	cd services/compliance && cargo fmt
	cd services/gateway && gofmt -w .
	cd services/portfolio && gofmt -w .

# ── Docker & Deployment ──────────────────────────────────────────────────────

## Build and launch production Docker Compose stack (all 12 services)
docker:
	docker compose -f infra/docker-compose.prod.yml build

## Deploy production stack
deploy:
	docker compose -f infra/docker-compose.prod.yml up -d

## Tear down production stack
down:
	docker compose -f infra/docker-compose.prod.yml down

# ── Cleanup ──────────────────────────────────────────────────────────────────

## Clean all build artifacts
clean:
	rm -rf services/execution-core/build bin/
	cd services/risk-analytics && cargo clean
	cd services/compliance && cargo clean
	rm -rf frontend/.next frontend/out
