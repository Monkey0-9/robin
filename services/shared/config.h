// ============================================================================
// Robin Trading Platform — Shared IPC Configuration
// ============================================================================
// This header defines ALL inter-process communication (IPC) constants shared
// across the C++, Rust, and Go services.
//
// CRITICAL: Any change here must be mirrored in:
//   - services/risk-analytics/src/config.rs  (Rust)
//   - services/gateway/config.go              (Go, if added)
//
// The SHM ring buffer layout:
//   [ShmHeader (64 bytes)] [ShmMessage × SHM_CAPACITY (64 bytes each)]
//
// Pipeline data flow:
//   [Ingestion] ──SHM_INGEST_TO_RISK──► [Risk Gate] ──SHM_RISK_TO_MATCH──► [Matching Engine]
//                                                                                   │
//                                                           ┌─────────────────────┘
//                                                           ▼
//                                        [Compliance Daemon] ──Audit Log──► (WORM file)
//                                        [KDB+ / Storage]   ──Tick DB──►   (HDB/RDB)
// ============================================================================

#pragma once

// Shared memory path constants (POSIX shm_open names, Linux only)
#define ROBIN_SHM_INGEST_TO_RISK    "/robin_ingest_risk"
#define ROBIN_SHM_RISK_TO_MATCH     "/robin_risk_match"
#define ROBIN_SHM_MATCH_TO_STORAGE  "/robin_match_storage"

// SHM ring buffer capacity (must be power of 2)
#define ROBIN_SHM_CAPACITY          65536u
#define ROBIN_SHM_MSG_SIZE          64u    // bytes per message (ShmMessage struct)
#define ROBIN_SHM_MAGIC             0x524f42494e484d5fULL  // "ROBINHM_"
#define ROBIN_SHM_VERSION           1u

// Service health-check TCP ports
#define ROBIN_PORT_ORCHESTRATOR     8080
#define ROBIN_PORT_EXECUTION_HEALTH 9091
#define ROBIN_PORT_RISK_HEALTH      9092
#define ROBIN_PORT_MARKET_DATA      9093
#define ROBIN_PORT_PORTFOLIO        9094
#define ROBIN_PORT_COMPLIANCE       9095

// Market data multicast group + port (UDP)
#define ROBIN_MCAST_GROUP           "233.0.0.1"
#define ROBIN_MCAST_PORT            5000

// Default position limit per instrument (net shares)
#define ROBIN_POSITION_LIMIT        100000LL

// Default credit limit (price_units × qty)
#define ROBIN_CREDIT_LIMIT          10000000000ULL  // 10 billion

// Velocity: max orders per second
#define ROBIN_MAX_ORDERS_PER_SEC    100u

// Price collar: ±5% from last trade
#define ROBIN_PRICE_COLLAR_BPS      500u

// Audit log path (WORM — append-only, never truncate)
#define ROBIN_AUDIT_LOG_PATH        "/var/log/robin/audit.log"
#define ROBIN_AUDIT_LOG_PATH_DEV    "logs/audit.log"

// Circuit breaker: trip on 10% daily drawdown
#define ROBIN_DRAWDOWN_LIMIT        0.10

// ============================================================================
// Message type codes (msg_type field in ShmMessage)
// ============================================================================
#define ROBIN_MSG_ORDER_NEW         0x01u
#define ROBIN_MSG_ORDER_CANCEL      0x02u
#define ROBIN_MSG_ORDER_REPLACE     0x03u
#define ROBIN_MSG_TRADE             0x10u
#define ROBIN_MSG_HEARTBEAT         0xFFu
