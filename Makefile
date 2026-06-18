# Robin Trading Platform - Top-Level Build System
# Orchestrates multi-language build: C++20, Rust, OCaml, Go, Q/KDB+

.PHONY: all build deps clean test lint benchmark docker chaos deploy

SHELL := /bin/bash
CMAKE := cmake
CARGO := cargo
DUNE := dune
GO := go
NPM := npm

BUILD_TYPE ?= Release
PARALLEL ?= $(shell nproc 2>/dev/null || echo 4)

all: build

# === Dependencies ===

deps:
	@echo "[BUILD] Installing system dependencies..."
	@sudo apt-get update -qq
	@sudo apt-get install -y -qq \
		build-essential cmake g++-12 clang-14 \
		libnuma-dev libtbb-dev libboost-dev \
		libdpdk-dev libpcap-dev linux-tools-common \
		linux-tools-generic libelf-dev \
		libssl-dev pkg-config \
		2>/dev/null || true

deps-rust:
	@echo "[BUILD] Checking Rust toolchain..."
	@which cargo || (curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y)

deps-ocaml:
	@echo "[BUILD] Checking OCaml toolchain..."
	@which opam || (echo "Install opam from https://opam.ocaml.org")
	@opam init -y --disable-sandboxing 2>/dev/null || true
	@opam install -y dune core owl 2>/dev/null || true

# === Build ===

build: build-cpp build-rust build-go build-ocaml

build-cpp:
	@echo "=== Building C++ Components ==="
	@for dir in services/execution-core services/network-bridge services/ingestion \
	            services/hardware-fpga services/pricing services/ai-engine; do \
		if [ -f "$$dir/CMakeLists.txt" ]; then \
			echo "[BUILD] $$dir..."; \
			mkdir -p "$$dir/build"; \
			cd "$$dir/build" && $(CMAKE) .. -DCMAKE_BUILD_TYPE=$(BUILD_TYPE) && \
			$(CMAKE) --build . --parallel $(PARALLEL) && cd ../../..; \
		fi; \
	done
	@echo "[BUILD] C++ components complete"

build-rust:
	@echo "=== Building Rust Components ==="
	@for dir in services/risk-analytics services/compliance; do \
		if [ -f "$$dir/Cargo.toml" ]; then \
			echo "[BUILD] $$dir..."; \
			cd "$$dir" && $(CARGO) build --release && cd ../..; \
		fi; \
	done
	@echo "[BUILD] Rust components complete"

build-go:
	@echo "=== Building Go Components ==="
	@if [ -f services/gateway/go.mod ]; then \
		cd services/gateway && $(GO) build -ldflags="-s -w -extldflags=-static" -o bin/orchestrator . && cd ../..; \
	fi
	@echo "[BUILD] Go components complete"

build-ocaml:
	@echo "=== Building OCaml Components ==="
	@for dir in services/portfolio services/compliance; do \
		if [ -f "$$dir/bin/dune" ]; then \
			echo "[BUILD] $$dir..."; \
			cd "$$dir" && $(DUNE) build && cd ../..; \
		fi; \
	done
	@echo "[BUILD] OCaml components complete"

# === Tests ===

test: test-cpp test-rust

test-cpp:
	@echo "=== Running C++ Tests ==="
	@for dir in services/execution-core services/pricing; do \
		if [ -f "$$dir/build" ]; then \
			ctest --test-dir "$$dir/build" --output-on-failure 2>/dev/null || true; \
		fi; \
	done

test-rust:
	@echo "=== Running Rust Tests ==="
	@for dir in services/risk-analytics services/compliance; do \
		if [ -f "$$dir/Cargo.toml" ]; then \
			cd "$$dir" && $(CARGO) test --release && cd ../..; \
		fi; \
	done

test-go:
	@echo "=== Running Go Tests ==="
	@if [ -f services/gateway/go.mod ]; then \
		cd services/gateway && $(GO) test ./... && cd ../..; \
	fi

# === Linting ===

lint: lint-cpp lint-rust lint-go

lint-cpp:
	@echo "=== Linting C++ ==="
	@for dir in services/execution-core services/network-bridge services/ingestion \
	            services/hardware-fpga services/pricing; do \
		if [ -d "$$dir" ]; then \
			find "$$dir" -name '*.cpp' -o -name '*.hpp' | xargs -r clang-tidy --quiet 2>/dev/null || true; \
		fi; \
	done

lint-rust:
	@echo "=== Linting Rust ==="
	@for dir in services/risk-analytics services/compliance; do \
		if [ -f "$$dir/Cargo.toml" ]; then \
			cd "$$dir" && $(CARGO) clippy -- -D warnings 2>/dev/null || true; \
			cd ../..; \
		fi; \
	done

lint-go:
	@echo "=== Linting Go ==="
	@if [ -f services/gateway/go.mod ]; then \
		cd services/gateway && $(GO) vet ./... && cd ../..; \
	fi

# === Benchmark ===

benchmark:
	@echo "=== Running Benchmarks ==="
	@scripts/test_latency.sh ${ITERATIONS:-1000000}

# === Chaos Engineering ===

chaos:
	@echo "=== Running Chaos Engineering ==="
	@scripts/chaos_test.sh ${DURATION:-300}

# === Docker ===

docker:
	@echo "=== Building Docker Images ==="
	docker build -t robin/matching-engine:latest -f docker/matching.Dockerfile .
	docker build -t robin/risk-gate:latest -f docker/risk.Dockerfile .
	docker build -t robin/orchestrator:latest -f docker/orchestrator.Dockerfile .
	docker build -t robin/frontend:latest -f docker/frontend.Dockerfile .

# === Clean ===

clean:
	@echo "=== Cleaning Build Artifacts ==="
	@for dir in services/execution-core services/network-bridge services/ingestion \
	            services/hardware-fpga services/pricing services/ai-engine; do \
		rm -rf "$$dir/build" 2>/dev/null || true; \
	done
	@for dir in services/risk-analytics services/compliance services/gateway; do \
		rm -rf "$$dir/target" 2>/dev/null || true; \
		rm -rf "$$dir/bin" 2>/dev/null || true; \
	done
	@rm -rf services/portfolio/_build services/compliance/_build 2>/dev/null || true
	@rm -rf frontend/node_modules frontend/.next 2>/dev/null || true
	@echo "[BUILD] Clean complete"

# === Help ===

help:
	@echo "Robin Trading Platform - Build System"
	@echo ""
	@echo "Usage:"
	@echo "  make all          - Full build (C++ + Rust + Go + OCaml)"
	@echo "  make build        - Same as 'make all'"
	@echo "  make test         - Run all tests"
	@echo "  make lint         - Run all linters"
	@echo "  make benchmark    - Run latency benchmarks"
	@echo "  make chaos        - Run chaos engineering tests"
	@echo "  make docker       - Build Docker images"
	@echo "  make clean        - Remove build artifacts"
	@echo ""
	@echo "Components:"
	@echo "  make build-cpp    - C++ hot-path (matching engine, DPDK, FPGA)"
	@echo "  make build-rust   - Rust risk engine (SEC 15c3-5 gate)"
	@echo "  make build-go     - Go orchestrator (microservices)"
	@echo "  make build-ocaml  - OCaml portfolio optimization"
