# Security Policy

## Supported Versions

This is a research prototype. There are no production versions.

| Version | Status |
|---------|--------|
| main    | Research only — do NOT deploy in production |

## Reporting a Vulnerability

If you find a security issue in this research codebase, please open a private GitHub Security Advisory or email the maintainer directly.

Do NOT file public issues for security vulnerabilities.

## Known Security Limitations

This prototype is explicitly **not** production-ready. Known limitations:

### Authentication
- The Go gateway has a Bearer token middleware stub (`jwtAuthMiddleware`). It validates token presence but does **not** verify JWT signatures. Production must integrate a proper JWKS/JWT library (e.g., `golang-jwt/jwt`).
- The KDB+ HTTP gateway (`http_gateway.q`) uses a static bearer token from the `ROBIN_KDB_API_TOKEN` environment variable. Production must use mTLS or an OAuth2 service.
- The WebSocket bridge has no authentication.

### Transport Security
- TLS is configurable in the Go gateway (`ORCH_TLS_CERT`, `ORCH_TLS_KEY`) but requires external certificate provisioning.
- The frontend communicates with the gateway over plain HTTP by default.

### Secrets Management
- No secrets are hardcoded. All credentials use environment variables.
- Production must use a secrets manager (HashiCorp Vault, AWS Secrets Manager, or GCP Secret Manager).

### Regulatory Compliance
- SEC 15c3-5, MiFID II, and FINRA 3110 are design targets, not certified implementations.
- The SEC CAT reporting function in `risk_analytics.R` requires `ROBIN_FIRM_ID` and `ROBIN_CRD_NUM` to be set.

### FPGA / Hardware
- The "FPGA" is a software simulation (`SoftwareOrderMatchSimulator`). `is_hardware_fpga()` always returns `false`.

### Network
- No DPDK kernel bypass is implemented. The ingestion pipeline uses standard POSIX UDP sockets.
- No firewall rules are provided.

## Environment Variables (Required for production)

| Variable | Service | Purpose |
|---|---|---|
| `ROBIN_KDB_API_TOKEN` | KDB+ HTTP Gateway | Bearer token for HTTP auth |
| `ROBIN_KDB_IPC_PW` | KDB+ HTTP Gateway | IPC connection password |
| `ROBIN_FIRM_ID` | R Risk Analytics | SEC CAT FirmID (never hardcode) |
| `ROBIN_CRD_NUM` | R Risk Analytics | SEC CAT CRD number (never hardcode) |
| `ORCH_PORT` | Go Gateway | HTTP listen port (default 8080) |
| `ORCH_TLS_CERT` | Go Gateway | Path to TLS certificate |
| `ORCH_TLS_KEY` | Go Gateway | Path to TLS private key |
