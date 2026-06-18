# Regulatory Compliance & Audit Specifications

This document outlines the automated systems, rules, and procedures implemented to satisfy SEC, FINRA, and MiFID II requirements.

## 1. Pre-Trade Risk Gate Controls (SEC 15c3-5)

Pre-trade checks are written directly in C++ (`services/execution-core/src/risk_engine.hpp`) for sub-microsecond inline execution:

* **Fat-Finger Checks:** Maximum order size (50,000 shares) and maximum order value ($1,000,000) are hard-blocked at the CPU socket layer.
* **Price Collar Checks:** Buy and sell orders are validated against the reference NBO/NBB price. Orders deviating by more than 5% are dropped.
* **Direct & Exclusive Control**: Limits are configured securely and can only be modified by CCO and authorized Risk Desk operators.
* **Compliance Templates**: Detailed design and CEO verification workflows are defined in:
  * [SEC 15c3-5 Compliance Template](file:///c:/Robin/compliance/SEC_15c3_5_COMPLIANCE_TEMPLATE.md)
  * [CEO Certification Template](file:///c:/Robin/compliance/CEO_CERTIFICATION_TEMPLATE.md)

---

## 2. Market Abuse Monitoring

* **Spoofing & Layering Detection**: Real-time analysis of order book changes.
* **Wash Trade Prevention**: Prevent self-matching using Unique Client Identification keys.

---

## 3. CAT (Consolidated Audit Trail) & OATS Reporting

* **WORM Log Serialization**: Every order state transition is logged in a Write-Once-Read-Many format with SHA256 hashes.
* **Timestamp Precision**: synchronized to under 10ns using PTP Grandmaster clocks (`ptp_sync.sh`).
* **Daily Export**: Bundled and uploaded to FINRA CAT endpoints nightly (T+1).

