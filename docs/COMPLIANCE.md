# Compliance Framework (Design Specification — NOT Implemented)

> [!WARNING]
> **None of the compliance features described below are currently implemented, validated, or certified for production use.**
> - SEC Rule 15c3-5: Design specification only
> - MiFID II RTS 25: Not configured
> - FINRA Rule 3110: Not implemented
> - SOC 2 Type II: Design targets only

## SEC Rule 15c3-5 (Market Access Rule) — Unimplemented

### Hard Blocks
- Kill Switch Status: GPIO kernel module exists but integration with risk gate is not tested
- Circuit Breaker: Basic drawdown check implemented in Rust (non-atomic counters)
- Fat-Finger Protection: Order size limits implemented
- Price Collar: ±X% from last trade implemented
- Credit Limit: Not implemented
- Symbol Restriction: Fixed-size restricted list implemented
- Duplicate Detection: Direct-mapped array with timestamp window

### Soft Blocks
- Position Limits: Per-symbol net position limits implemented
- Velocity Limits: Order rate limits with sliding window implemented
- Concentration Limits: Not implemented
- Strategy Filters: Not implemented

### CEO Annual Certification — UNIMPLEMENTED

### Third-Party Independence — UNIMPLEMENTED

## MiFID II RTS 25 (Clock Synchronization) — UNCONFIGURED

## FINRA Rule 3110 (Supervision) — UNIMPLEMENTED

## SOC 2 Type II — UNIMPLEMENTED
