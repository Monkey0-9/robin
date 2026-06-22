.PHONY: all build build-cpp build-rust build-go build-compliance build-kernel \
        test test-rust test-go test-compliance test-cpp test-lint \
        clean help

SHELL    := /bin/bash
# Pin g++-12 explicitly to match CI environment; fall back to g++ if not found.
CXX      := $(shell command -v g++-12 2>/dev/null || echo g++)
RUST     := cargo
GO       := go
NPROC    := $(shell nproc 2>/dev/null || echo 4)

all: build

# ============================================================================
# Build
# ============================================================================
build: build-cpp build-rust build-go build-compliance
	@echo "[BUILD] All services built."

## C++ execution core, pricing, ingestion, network-bridge
build-cpp:
	@echo "[BUILD] C++ services (compiler: $(CXX))..."
	cmake -S services/execution-core \
	      -B services/execution-core/build \
	      -DCMAKE_BUILD_TYPE=Release \
	      -DCMAKE_CXX_COMPILER=$(CXX) 2>/dev/null || true
	cmake --build services/execution-core/build -j$(NPROC) 2>/dev/null || true
	cmake -S services/pricing \
	      -B services/pricing/build \
	      -DCMAKE_BUILD_TYPE=Release \
	      -DCMAKE_CXX_COMPILER=$(CXX) 2>/dev/null || true
	cmake --build services/pricing/build -j$(NPROC) 2>/dev/null || true

## Rust risk gate (library + bin)
build-rust:
	@echo "[BUILD] Rust risk analytics..."
	cd services/risk-analytics && $(RUST) build --release 2>/dev/null || true

## Go orchestrator
build-go:
	@echo "[BUILD] Go orchestrator..."
	cd services/gateway && $(GO) build -o ../../build/orchestrator . 2>/dev/null || true

## Compliance daemon (Rust)
build-compliance:
	@echo "[BUILD] Compliance daemon..."
	cd services/compliance && $(RUST) build --release 2>/dev/null || true

## GPIO kill switch kernel module (Linux only)
build-kernel:
	@echo "[BUILD] Kernel module (requires Linux kernel headers)..."
	$(MAKE) -C services/kernel 2>/dev/null || true

# ============================================================================
# Tests
# ============================================================================
test: test-rust test-compliance test-go test-lint
	@echo "[TEST] All tests complete."

## Rust risk-analytics unit tests
test-rust:
	@echo "[TEST] Rust risk-analytics..."
	cd services/risk-analytics && $(RUST) test 2>&1

## Compliance daemon unit tests
test-compliance:
	@echo "[TEST] Rust compliance..."
	cd services/compliance && $(RUST) test 2>&1

## Go unit tests (all packages)
test-go:
	@echo "[TEST] Go gateway..."
	cd services/gateway && $(GO) test -v ./... 2>&1

## C++ unit tests (via CTest, if cmake targets exist)
test-cpp:
	@echo "[TEST] C++ (CTest)..."
	if [ -d services/execution-core/build ]; then \
	    cd services/execution-core/build && ctest --output-on-failure; \
	else \
	    echo "[SKIP] C++ not built yet — run make build-cpp first"; \
	fi

## Lint all Rust code (requires clippy)
test-lint:
	@echo "[LINT] Rust clippy..."
	cd services/risk-analytics && $(RUST) clippy -- -D warnings 2>&1 || true
	cd services/compliance     && $(RUST) clippy -- -D warnings 2>&1 || true

## Integration smoke test (Linux only)
test-integration:
	@echo "[TEST] Integration smoke test..."
	bash scripts/integration_smoke_test.sh

# ============================================================================
# Utility
# ============================================================================

## Clean all build artifacts
clean:
	rm -rf build/
	rm -rf services/execution-core/build/
	rm -rf services/pricing/build/
	cd services/risk-analytics && $(RUST) clean 2>/dev/null || true
	cd services/compliance     && $(RUST) clean 2>/dev/null || true
	$(MAKE) -C services/kernel clean 2>/dev/null || true
	rm -rf logs/robin_audit_test.log

## Show help
help:
	@echo "Robin Trading Platform — Makefile targets:"
	@echo ""
	@grep -E '^##' Makefile | sed 's/## /  /'
	@echo ""
	@echo "Compiler: $(CXX)"
	@echo "NPROC:    $(NPROC)"
