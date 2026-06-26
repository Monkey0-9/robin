// ============================================================================
// Robin Trading Platform — Shared IPC Configuration (AUTO-GENERATED)
// ============================================================================
#pragma once

// shm_paths
#define ROBIN_SHM_INGEST_TO_RISK    "/robin_ingest_risk"
#define ROBIN_SHM_RISK_TO_MATCH     "/robin_risk_match"
#define ROBIN_SHM_MATCH_TO_STORAGE  "/robin_match_storage"

// shm_constants
#define ROBIN_SHM_CAPACITY          65536u
#define ROBIN_SHM_MSG_SIZE          64u
#define ROBIN_SHM_MAGIC             0x524f42494e484d5fULL
#define ROBIN_SHM_VERSION           1u

// ports
#define ROBIN_PORT_ORCHESTRATOR     8080
#define ROBIN_PORT_EXECUTION_HEALTH 9091
#define ROBIN_PORT_RISK_HEALTH      9092
#define ROBIN_PORT_MARKET_DATA      9093
#define ROBIN_PORT_PORTFOLIO        9094
#define ROBIN_PORT_COMPLIANCE       9095

// risk_limits
#define ROBIN_POSITION_LIMIT        100000LL
#define ROBIN_CREDIT_LIMIT          10000000000ULL
#define ROBIN_MAX_ORDERS_PER_SEC    100u
#define ROBIN_PRICE_COLLAR_BPS      500u
#define ROBIN_DRAWDOWN_LIMIT        0.1

// market_data
#define ROBIN_MCAST_GROUP           "233.0.0.1"
#define ROBIN_MCAST_PORT            5000

// audit_paths
#define ROBIN_AUDIT_LOG_PATH        "/var/log/robin/audit.log"
#define ROBIN_AUDIT_LOG_PATH_DEV    "logs/audit.log"

// messages
#define ROBIN_MSG_ORDER_NEW         0x01u
#define ROBIN_MSG_ORDER_CANCEL      0x02u
#define ROBIN_MSG_ORDER_REPLACE     0x03u
#define ROBIN_MSG_TRADE             0x10u
#define ROBIN_MSG_HEARTBEAT         0xFFu
