---
title: CEO Annual Certification - SEC Rule 15c3-5
firm: Robin Trading Systems LLC
filing_date: [DATE]
---

# CERTIFICATION OF PRE-TRADE RISK CONTROL SYSTEMS

## SEC Rule 15c3-5 - Annual Certification

I, **[CEO Name]**, Chief Executive Officer of **Robin Trading Systems LLC**
(the "Firm"), hereby certify that the pre-trade risk control systems described
herein are designed to establish, maintain, and enforce direct and exclusive
control over the Firm's proprietary and customer orders, as required by
SEC Rule 15c3-5(a) and (b).

---

## 1. System Design Review

I have reviewed the design, testing, and operation of these systems with:

- **[CRO Name]**, Chief Risk Officer
- **[CTO Name]**, Chief Technology Officer
- **[COO Name]**, Chief Operating Officer

### 1.1 Hard Blocks (Non-Configurable)

| # | Control | Implementation | Status |
|---|---------|---------------|--------|
| 1 | Hardware Kill Switch | GPIO pin 18 + kernel module | OPERATIONAL |
| 2 | Circuit Breaker | Rust RiskCircuitBreaker | OPERATIONAL |
| 3 | Order Size Limits | Rust PreTradeRiskEvaluator | OPERATIONAL |
| 4 | Price Collars | Rust PreTradeRiskEvaluator | OPERATIONAL |
| 5 | Credit Limits | Rust RiskGate | OPERATIONAL |
| 6 | Restricted Symbols | Rust PreTradeRiskEvaluator | OPERATIONAL |
| 7 | Duplicate Detection | Rust RiskGate | OPERATIONAL |
| 8 | Order Rate Limiting | Rust RiskGate | OPERATIONAL |

### 1.2 Soft Blocks (Configurable)

| # | Control | Default | Current | Last Updated |
|---|---------|---------|---------|-------------|
| 1 | Position Limits | 10,000 | [CURRENT] | [DATE] |
| 2 | Velocity Limits | 10,000/s | [CURRENT] | [DATE] |
| 3 | Concentration Limits | 5% | [CURRENT] | [DATE] |
| 4 | Max Drawdown | 10% | [CURRENT] | [DATE] |
| 5 | Leverage Limit | 2.0x | [CURRENT] | [DATE] |

## 2. Third-Party Risk Management

Third-party vendors and their due diligence status:

| Vendor | Product | Due Diligence | SOC 2 | Expiration |
|--------|---------|--------------|-------|------------|
| KX Systems | KDB+ | Completed | Type II | [DATE] |
| Xilinx (AMD) | Alveo FPGA | Completed | N/A | [DATE] |
| HashiCorp | Vault | Completed | Type II | [DATE] |
| Intel | DPDK | Source audit | N/A | [DATE] |

## 3. Testing & Validation Summary

- **Unit Test Coverage**: >90% (verified [DATE])
- **Integration Tests**: End-to-end latency validated [DATE]
- **90-Day Latency Monitoring**: [START_DATE] to [END_DATE]
- **Chaos Engineering**: Final report [DATE]
- **Penetration Testing**: Completed [DATE] by [FIRM]
- **Red Team Exercise**: Completed [DATE] by [FIRM]

### 3.1 Latency Validation (90-Day Window)

| Metric | Target | Measured | Status |
|--------|--------|----------|--------|
| p50 | <200ns | [MEASURED] | [PASS/FAIL] |
| p99 | <500ns | [MEASURED] | [PASS/FAIL] |
| p99.9 | <1µs | [MEASURED] | [PASS/FAIL] |
| p99.99 | <10µs | [MEASURED] | [PASS/FAIL] |
| Max | <100µs | [MEASURED] | [PASS/FAIL] |
| Jitter | <10ns | [MEASURED] | [PASS/FAIL] |

## 4. Supervision & Surveillance

- Post-trade surveillance system: **OPERATIONAL**
- Real-time alerts to surveillance team: **ENABLED**
- Automated escalation to CRO: **ENABLED**
- Regulatory reporting (CAT/OATS): **AUTOMATED**

## 5. Attestation

I attest, under penalty of perjury, that:

(a) The pre-trade risk control systems provide **direct and exclusive control**
over the Firm's orders at all times.

(b) No order can reach the market without passing through all hard block
controls.

(c) All controls were tested and verified working within the [90-DAY] period
ending [END_DATE].

(d) Third-party vendors have been subject to due diligence review.

(e) The Chief Risk Officer and I have reviewed and approved all configurable
risk parameters in the last [30] days.

(f) Incident response procedures are documented and tested quarterly.

---

**Firm**: Robin Trading Systems LLC
**CRD#**: 394028
**SEC#**: 8-74820

**Date**: _______________________

**Signature**: _______________________
**Name**: [CEO NAME]
**Title**: Chief Executive Officer

**Witnessed By**:

**Signature**: _______________________
**Name**: [CRO NAME]
**Title**: Chief Risk Officer

**Signature**: _______________________
**Name**: [CTO NAME]
**Title**: Chief Technology Officer
