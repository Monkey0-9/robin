# Connection Pooling Guidelines

Effective connection pooling is critical for the low-latency performance and high throughput of the Robin Platform. This document outlines the strategies and configurations across our major dependencies.

## 1. Database Connections (KDB+ / PostgreSQL)

### PostgreSQL (Compliance / Relational Storage)
- **Library**: Services should use robust connection poolers (e.g., `pgxpool` in Go or SQLAlchemy connection pooling in Python).
- **Max Open Connections**: Tuned according to the service's concurrent thread/goroutine count. Typically bounded between `20` and `50` per instance.
- **Max Idle Connections**: Set appropriately to prevent establishing new connections on traffic spikes (e.g., `10`).
- **Connection Max Lifetime**: Connections should be recycled (e.g., `1 hour`) to handle database firewall timeouts gracefully.

### KDB+ (Tick Data / Fast Storage)
- **Library**: `kdb+` q clients.
- **Strategy**: Keep persistent TCP connections for live publishing (TP/RDB). Reconnecting repeatedly adds unacceptable latency in HFT systems.
- **Multiplexing**: For historical queries (HDB), use a load balancer or dispatcher process rather than deep connection pools.

## 2. Inter-Service Communication (gRPC & TCP)

### Matching Engine (C++ / FPGA) TCP Bridge
- The Gateway maintains a single, highly optimized, non-blocking TCP socket to the matching engine.
- Connection is persistent with zero-allocation buffers for order routing.

### Internal gRPC Services
- **Keep-Alive**: Enable gRPC keep-alive pings to prevent intermediate load balancers from dropping idle connections.
- **Connection Reuse**: Channels must be reused across requests. Do not create a new gRPC channel per request.

## 3. Best Practices
- **Always close/release connections**: Use `defer` in Go or `with` blocks in Python to ensure connections return to the pool.
- **Timeout configurations**: Every connection attempt and query must have an explicit timeout context. Default to `< 1s` for DB and `< 50ms` for inter-service RPCs.
