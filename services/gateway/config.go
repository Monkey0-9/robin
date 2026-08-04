package main

// ============================================================================
// Robin Trading Platform — Shared Go Configuration (AUTO-GENERATED)
// ============================================================================

// shm_paths
const (
	SHMIngestToRisk   = "/robin_ingest_risk"
	SHMRiskToMatch    = "/robin_risk_match"
	SHMMatchToStorage = "/robin_match_storage"
)

// shm_constants
const (
	SHMCapacity = 65536
	SHMMsgSize  = 64
	SHMMagic    = 0x524f42494e484d5f
	SHMVersion  = 1
)

// ports
const (
	PortOrchestrator    = 8080
	PortExecutionHealth = 9091
	PortRiskHealth      = 9092
	PortMarketData      = 9093
	PortPortfolio       = 9094
	PortCompliance      = 9095
)

// risk_limits
const (
	PositionLimit   = 100000
	CreditLimit     = 10000000000
	MaxOrdersPerSec = 100
	PriceCollarBPS  = 500
	DrawdownLimit   = 0.10
)

// market_data
const (
	McastGroup = "233.0.0.1"
	McastPort  = 5000
)

// audit_paths
const (
	AuditLogPath    = "/var/log/robin/audit.log"
	AuditLogPathDev = "logs/audit.log"
)

// messages
const (
	MsgOrderNew     = 0x01
	MsgOrderCancel  = 0x02
	MsgOrderReplace = 0x03
	MsgTrade        = 0x10
	MsgHeartbeat    = 0xFF
)
