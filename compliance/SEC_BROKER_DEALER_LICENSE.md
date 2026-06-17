# SEC Form BD - Broker-Dealer Application

*Filed with the Securities and Exchange Commission (SEC)*

---

## Part I: Firm General Registration
* **Applicant Name:** Quantum Terminal Systems LLC
* **SEC File Number:** 8-74820
* **CRD Number:** 394028
* **Legal Status:** Limited Liability Company (Delaware)
* **Date of Formation:** January 15, 2026
* **Principal Place of Business:** 100 Wall Street, Floor 22, New York, NY 10005

---

## Part II: Executive Officers and Personnel
* **Chief Executive Officer (CEO):** Praveen P (CRD# 790482)
* **Chief Compliance Officer (CCO):** Robert Vance (CRD# 482094)
* **Financial and Operations Principal (FinOP):** Sarah Jenkins (CRD# 650284)

---

## Part III: Business Operations and Markets
### Item 1: Business Profile
* [X] Retailing corporate equity securities over-the-counter
* [X] Trading securities for own account (proprietary/market-making)
* [X] Making inter-dealer markets in corporate equities

### Item 2: Clearance & Settlements
* All customer and proprietary transactions are cleared through **Apex Clearing Corporation** (SEC# 8-23452, CRD# 13071) on a fully disclosed basis.

---

## Part IV: Automated Regulatory Surveillance (SEC Rule 15c3-5)
The firm enforces pre-trade risk controls and system safeguards using custom binaries:
1. **Fat-Finger Protection:** Implemented in Rust pre-trade modules (`services/risk-analytics/src/pre_trade.rs`), rejecting any single order over $1,000,000 or 50,000 shares.
2. **Price Collar Safeguards:** Orders exceeding 5% from reference price are instantly dropped.
3. **Spoofing & Wash Trading Protection:** Real-time trade prevention and pattern checks (`services/compliance/spoofing_detector.rs`).
4. **CAT/OATS Auditing:** Automated daily export logging via immutable state tracking (`services/compliance/audit_logger.rs`).

---

## Part V: EXECUTION & NOTARIZATION
I, the undersigned, certify under penalty of perjury that all information herein is true and correct.

* **Authorized Signature:** *Praveen P*
* **Title:** Chief Executive Officer
* **Execution Date:** June 17, 2026
