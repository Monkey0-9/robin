# Robin Quantitative Trading Platform — Testing & Verification Guide
**Document ID:** SPEC-TEST-202608-01  
**Classification:** Quality Assurance & Testing Guide  

---

## 1. Full Multi-Language Test Execution

Run the complete test suite across all languages using the master Makefile or individual runtime CLI commands:

```bash
# Run all tests via master Makefile
make test

# Individual subsystem execution:
# 1. Rust Risk Analytics (64 tests)
cd services/risk-analytics && cargo test --lib

# 2. Rust Compliance Suite (12 tests)
cd services/compliance && cargo test --lib

# 3. Go Gateway & OMS (31 tests)
cd services/gateway && go test -v ./...

# 4. Go End-to-End Integration (1 test)
cd tests/integration && go test -v .

# 5. Python AI Agent & Strategies (28 tests + components)
cd services/ai-agent && python -m pytest tests/test_robin.py
cd services/ai-agent && python test_components.py

# 6. Python Strategy Backtester
cd research/strategy-engine && python backtester.py

# 7. Next.js Trading Terminal Build
cd frontend && npm run build
```

---

## 2. Invariant & Property-Based Verification
* **Price Collars:** Validates bid orders above and ask orders below the 5% reference threshold are rejected.
* **Order Book FIFO:** Validates orders at the same price fill strictly in order of receipt timestamp.
* **Tamper-Evident WORM Chain:** Validates any mutation or byte modification to audit records is flagged immediately.
