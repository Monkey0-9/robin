# Regulatory Compliance & Audit Specifications

This document outlines the automated systems, rules, and procedures implemented to satisfy SEC, FINRA, and MiFID II requirements.

## 1. Pre-Trade Risk Gate Controls (SEC 15c3-5)
The pre-trade risk engine (`services/risk-analytics/src/gate.rs`) validates the following metrics before forwarding any order to the market:
* **Fat-Finger Checks:** Maximum size (50,000 shares) and maximum value ($1M) are verified inline.
* **Price Collar Checks:** Bids and asks are matched against a reference price band (5% deviation limit).

---

## 2. Market Abuse Monitoring
* **Spoofing & Layering Detection:** Real-time analysis of the order flow (`spoofing_detector.rs`) logs warnings if large orders are entered and canceled rapidly within 1000ms to impact prices.
* **Wash Trade Prevention:** Cross-account transactions are prevented at the order book level using Unique Client Identification keys.

---

## 3. CAT (Consolidated Audit Trail) & OATS Reporting
* **WORM Log Serialization:** Every order state transition is logged in a Write-Once-Read-Many format (`services/compliance/audit_logger.rs`) with SHA256 chain hashes.
* **Timestamp Precision:** Timestamps are synchronized down to single-digit nanoseconds using GPSDO PTP clocks (`ptp_sync.sh`).
* **Daily Export:** Log files are bundled and uploaded to FINRA CAT endpoints nightly (T+1).
