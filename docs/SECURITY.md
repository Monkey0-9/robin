# Security Architecture

## Encryption

| Layer | Protocol | Implementation |
|-------|----------|---------------|
| External API | TLS 1.3 | Mutual TLS (mTLS) with certificate pinning |
| Internal mesh | WireGuard | Point-to-point encrypted tunnels |
| At rest | AES-256-GCM | LUKS full-disk encryption |
| Key storage | HSM | Thales Luna 7 (FIPS 140-2 Level 3) |

## Secrets Management

- **Vault**: HashiCorp Vault for dynamic secrets, PKI, database credentials
- **Rotation**: Automatic every 24 hours for API keys, 90 days for TLS certs
- **Audit**: All secret access logged, immutable audit trail

## Authentication & Authorization

- **API**: JWT with RS256 signatures (HSM-signed)
- **Trader**: OAuth 2.0 + WebAuthn hardware keys (YubiKey)
- **Admin**: mTLS with client certificates + MFA
- **Service-to-service**: SPIFFE/SPIRE workload identity

## Supply Chain Security

- **SBOM**: SPDX 2.3 for all dependencies (generated via `cargo cyclonedx`)
- **Scanning**: Trivy, cargo audit, opam security (weekly)
- **Build**: Reproducible builds via Nix flakes
- **Signing**: GPG-signed commits + container image signing (cosign)

## Network Security

- **Segmentation**: DMZ (external), Execution Zone (hot path), Analytics Zone, Management Zone
- **Firewall**: nftables with default-deny, only allowlist traffic
- **IDS/IPS**: Suricata for network-level threat detection
- **DDoS**: Cloudflare Magic Transit + rate limiting per IP

## Incident Response

| Level | Responder | Response Time | Escalation |
|-------|-----------|---------------|------------|
| L1 | Trader | <30s | L2 if unresolved |
| L2 | Risk Team | <2min | L3 if >5min |
| L3 | CTO | <5min | L4 if >15min |
| L4 | CEO | <15min | L5 if >30min |
| L5 | Regulatory | <1hr | FINRA/SEC notification |
