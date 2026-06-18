.PHONY: all build deps test lint benchmark chaos docker clean help

SHELL := /bin/bash

# Architecture targets
CXX := g++
RUST := cargo
OCAML := dune
GO := go
NPM := npm

# NUMA and benchmark config
NUMA := numactl --cpubind=0 --membind=0

all: build

## Build all hot-path services
build: build-cpp build-rust build-ocaml build-go build-kernel

## C++ hot path (DPDK network, matching engine, FPGA simulation)
build-cpp:
	@echo "[BUILD] C++ hot path..."
	cmake -S services/execution-core -B services/execution-core/build -DCMAKE_BUILD_TYPE=Release
	cmake --build services/execution-core/build -j$$(nproc)
	cmake -S services/network-bridge -B services/network-bridge/build -DCMAKE_BUILD_TYPE=Release
	cmake --build services/network-bridge/build -j$$(nproc)
	cmake -S services/ingestion -B services/ingestion/build -DCMAKE_BUILD_TYPE=Release
	cmake --build services/ingestion/build -j$$(nproc)
	cmake -S services/hardware-fpga -B services/hardware-fpga/build -DCMAKE_BUILD_TYPE=Release
	cmake --build services/hardware-fpga/build -j$$(nproc)
	cmake -S services/pricing -B services/pricing/build -DCMAKE_BUILD_TYPE=Release
	cmake --build services/pricing/build -j$$(nproc)
	cmake -S services/ai-engine -B services/ai-engine/build -DCMAKE_BUILD_TYPE=Release
	cmake --build services/ai-engine/build -j$$(nproc)

## Rust risk gate
build-rust:
	@echo "[BUILD] Rust risk gate..."
	cd services/risk-analytics && $(RUST) build --release
	cd services/compliance && $(RUST) build --release

## OCaml matching engine & portfolio
build-ocaml:
	@echo "[BUILD] OCaml services..."
	cd services/portfolio && $(OCAML) build

## GPIO kill switch kernel module
build-kernel:
	@echo "[BUILD] Kernel module..."
	$(MAKE) -C services/kernel

## Go orchestrator
build-go:
	@echo "[BUILD] Go orchestrator..."
	cd services/gateway && $(GO) build -o ../../build/orchestrator .

## Run HdrHistogram latency benchmark
benchmark:
	@echo "[BENCH] Running HdrHistogram latency benchmark..."
	@mkdir -p logs/latency
	$(NUMA) ./build/order_book_benchmark 1000000 100000 | tee logs/latency/matching_bench.log
	@echo "[BENCH] Results in logs/latency/"

## Run all tests
test: test-rust test-ocaml test-go

test-rust:
	cd services/risk-analytics && $(RUST) test --release
	cd services/compliance && $(RUST) test --release

test-ocaml:
	cd services/portfolio && $(OCAML) test

test-go:
	cd services/gateway && $(GO) test ./...

## Lint all code
lint: lint-rust lint-go

lint-rust:
	cd services/risk-analytics && $(RUST) clippy -- -D warnings
	cd services/compliance && $(RUST) clippy -- -D warnings

lint-go:
	cd services/gateway && $(GO) vet ./...

## Chaos engineering tests
chaos:
	@echo "[CHAOS] Running chaos engineering tests..."
	scripts/chaos_test.sh

## Docker build
docker:
	docker compose build

## Clean all build artifacts
clean:
	rm -rf build/
	rm -rf services/execution-core/build/
	rm -rf services/network-bridge/build/
	rm -rf services/ingestion/build/
	rm -rf services/hardware-fpga/build/
	rm -rf services/pricing/build/
	rm -rf services/ai-engine/build/
	cd services/risk-analytics && $(RUST) clean
	cd services/compliance && $(RUST) clean
	cd services/portfolio && $(OCAML) clean
	$(MAKE) -C services/kernel clean
	rm -rf logs/ pids/

## Help
help:
	@echo "Robin Trading Platform Makefile"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' Makefile | sed 's/## /  /'
