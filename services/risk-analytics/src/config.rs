// ============================================================================
// Robin Trading Platform — Shared Configuration (AUTO-GENERATED)
// ============================================================================

/// shm_paths
pub const SHM_INGEST_TO_RISK: &str = "/robin_ingest_risk";
pub const SHM_RISK_TO_MATCH: &str = "/robin_risk_match";
pub const SHM_MATCH_TO_STORAGE: &str = "/robin_match_storage";

/// shm_constants
pub const SHM_CAPACITY: usize = 65536;
pub const SHM_MSG_SIZE: usize = 64;
pub const SHM_MAGIC: u64 = 0x524f42494e484d5f;
pub const SHM_VERSION: u32 = 1;

/// ports
pub const PORT_ORCHESTRATOR: u16 = 8080;
pub const PORT_EXECUTION_HEALTH: u16 = 9091;
pub const PORT_RISK_HEALTH: u16 = 9092;
pub const PORT_MARKET_DATA: u16 = 9093;
pub const PORT_PORTFOLIO: u16 = 9094;
pub const PORT_COMPLIANCE: u16 = 9095;

/// risk_limits
pub const POSITION_LIMIT: i64 = 100000;
pub const CREDIT_LIMIT: u64 = 10000000000;
pub const MAX_ORDERS_PER_SEC: usize = 100;
pub const PRICE_COLLAR_BPS: u32 = 500;
pub const DRAWDOWN_LIMIT: f64 = 0.1;

/// market_data
pub const MCAST_GROUP: &str = "233.0.0.1";
pub const MCAST_PORT: u16 = 5000;

/// audit_paths
pub const AUDIT_LOG_PATH: &str = "/var/log/robin/audit.log";
pub const AUDIT_LOG_PATH_DEV: &str = "logs/audit.log";

/// messages
pub const MSG_ORDER_NEW: u8 = 0x01;
pub const MSG_ORDER_CANCEL: u8 = 0x02;
pub const MSG_ORDER_REPLACE: u8 = 0x03;
pub const MSG_TRADE: u8 = 0x10;
pub const MSG_HEARTBEAT: u8 = 0xFF;
