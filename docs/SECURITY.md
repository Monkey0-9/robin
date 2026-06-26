# Security Architecture (Current State)

> [!WARNING]
> **This is a research prototype. Advanced enterprise-grade security features (HSM hardware keys, WireGuard tunnels) are not implemented.**

## Current Security State

| Feature | Status | Notes |
|---------|--------|-------|
| Authentication | IMPLEMENTED | Enforces strict JWT validation on gateway and token validation on WebSocket |
| Authorization | IMPLEMENTED | RBAC (trader and admin roles) in gateway router |
| Encryption (in transit) | PARTIAL | TLS supported in Go gateway; internal nodes use plain TCP |
| Encryption (at rest) | NOT IMPLEMENTED | Raw files and standard log files |
| Secrets Management | PARTIAL | Vault/AWS template stubs; loads keys from environment variables |
| Audit Logging | IMPLEMENTED | SHA-256 chained tamper-evident logging in compliance service |
| Input Validation | IMPLEMENTED | Full bounds/restricted checks in risk gate |
| Rate Limiting | IMPLEMENTED | Token-bucket on gateway API + velocity check in risk gate |
| Supply Chain Security | NOT IMPLEMENTED | No SBOM, no dependency scanning |
