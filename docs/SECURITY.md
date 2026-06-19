# Security Architecture (Design Specification — NOT Implemented)

> [!WARNING]
> **None of the security features described below are currently implemented.**
> - No TLS, mTLS, or WireGuard
> - No HSM or HashiCorp Vault
> - No authentication or authorization
> - No secrets management
> - No encryption at rest or in transit

## Current Security State

| Feature | Status | Notes |
|---------|--------|-------|
| Authentication | NOT IMPLEMENTED | No JWT, mTLS, or API keys |
| Authorization | NOT IMPLEMENTED | No RBAC, no role-based access |
| Encryption (in transit) | NOT IMPLEMENTED | No TLS, no WireGuard |
| Encryption (at rest) | NOT IMPLEMENTED | No LUKS, no AES |
| Secrets Management | NOT IMPLEMENTED | No Vault, no KMS, hardcoded paths |
| Audit Logging | NOT IMPLEMENTED | println! only, no tamper-evident logs |
| Input Validation | PARTIAL | Basic order field checks exist |
| Rate Limiting | PARTIAL | Velocity check in risk gate only |
| Supply Chain Security | NOT IMPLEMENTED | No SBOM, no dependency scanning |

All security features described in this document are aspirational design targets.
