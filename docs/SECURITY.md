# System Security Profile & Guardrails

This document outlines the security controls, authentication rules, and hardware security configurations for the `quantum-terminal` platform.

## 1. Authentication & API Protection
* **HMAC & JWT Authentication:** All external API gateway connections require signed JSON Web Tokens (JWT) verified at high speed via Boost.Asio.
* **Token Bucket Rate Limiting:** Enforced via Redis lua script buckets (`token_bucket.lua`) at the gateway layer, preventing DDoS and resource starvation.

---

## 2. Hardware Security Modules (HSM)
* **PKCS#11 HSM Bridge:** All signing keys and private certificates for exchange routing are stored inside physical Hardware Security Modules (HSMs).
* **SHM Isolation:** Shared memory frames (`shm_bridge.rs`) are locked into RAM using `mlock` to prevent swap-to-disk exposure.

---

## 3. Kernel Jitter & Thread Protections
* Memory boundaries are isolated using network namespaces (`colo_setup.sh`).
* User space memory mapped segments are strictly restricted to the `trading` group via `/etc/security/limits.d/99-realtime.conf`.
