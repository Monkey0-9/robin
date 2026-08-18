# Robin Quantitative Trading Platform — Security & Zero-Trust Model
**Document ID:** SPEC-SEC-202608-01  
**Classification:** Institutional Security Architecture  

---

## 1. Zero-Trust Principles
1. **Zero Hardcoded Secrets:** All credentials, broker keys, and private keys are dynamically leased from HashiCorp Vault at runtime.
2. **mTLS Everywhere:** Internal service-to-service communication is secured via mutual TLS 1.3 with automated certificate rotation.
3. **Role-Based Access Control (RBAC):** Strict separation of privileges across TRADER, RISK_MANAGER, COMPLIANCE_OFFICER, and SUPERVISORY_PRINCIPAL.

---

## 2. Secrets Management & Vault PKI
* **Vault AppRole Auth:** Services authenticate using role IDs and secret IDs injected at container launch.
* **Transit Engine HMAC:** Sensitive audit records are signed using HSM/Transit cryptographic keyrings.
* **Lease Auto-Renewal:** Background goroutines refresh tokens prior to TTL expiry, preventing service outages.

---

## 3. Defense-in-Depth & Fail-Closed Behavior
* **Emergency Kernel Kill Switch:** Linux kernel netfilter module immediately drops all trading packets on ports 5000-9100 while keeping SSH/management channels open.
* **WORM Audit Chains:** SHA-256 tamper-evident append-only ledger logs all orders, cancels, fills, and administrative overrides.
