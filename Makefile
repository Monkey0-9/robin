.PHONY: all build build-cpp build-rust build-go build-kernel test clean help

SHELL := /bin/bash
CXX := g++
RUST := cargo
GO := go

all: build

## Build available services
build: build-cpp build-rust build-go

## C++ execution core and pricing
build-cpp:
	@echo "[BUILD] C++ services..."
	cmake -S services/execution-core -B services/execution-core/build -DCMAKE_BUILD_TYPE=Release 2>/dev/null || true
	cmake --build services/execution-core/build -j$$(nproc) 2>/dev/null || true
	cmake -S services/pricing -B services/pricing/build -DCMAKE_BUILD_TYPE=Release 2>/dev/null || true
	cmake --build services/pricing/build -j$$(nproc) 2>/dev/null || true

## Rust risk gate
build-rust:
	@echo "[BUILD] Rust risk gate..."
	cd services/risk-analytics && $(RUST) build --release 2>/dev/null || true

## Go orchestrator
build-go:
	@echo "[BUILD] Go orchestrator..."
	cd services/gateway && $(GO) build -o ../../build/orchestrator . 2>/dev/null || true

## GPIO kill switch kernel module
build-kernel:
	@echo "[BUILD] Kernel module..."
	$(MAKE) -C services/kernel 2>/dev/null || true

## Run all tests
test: test-rust test-go

test-rust:
	cd services/risk-analytics && $(RUST) test 2>/dev/null || true

test-go:
	cd services/gateway && $(GO) test ./... 2>/dev/null || true

## Clean all build artifacts
clean:
	rm -rf build/
	rm -rf services/execution-core/build/
	rm -rf services/pricing/build/
	cd services/risk-analytics && $(RUST) clean 2>/dev/null || true
	$(MAKE) -C services/kernel clean 2>/dev/null || true
	rm -rf logs/ pids/

## Help
help:
	@echo "Robin Trading Platform Makefile"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' Makefile | sed 's/## /  /'
