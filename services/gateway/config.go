package main

// ============================================================================
// Robin Trading Platform — Shared Go Configuration
// ============================================================================
// Constants mirrored from services/shared/config.h and
// services/risk-analytics/src/config.rs.
//
// CRITICAL: Keep in sync with those files.

// Shared memory path constants (POSIX shm_open names, Linux only)
const (
	SHMIngestToRisk   = "/robin_ingest_risk"
	SHMRiskToMatch    = "/robin_risk_match"
	SHMMatchToStorage = "/robin_match_storage"
)

// SHM ring buffer capacity (must be power of 2)
const (
	SHMCapacity = 65536
	SHMMsgSize  = 64
	SHMMagic    = 0x524f42494e484d5f // "ROBINHM_"
	SHMVersion  = 1
)

// Service TCP health-check ports
const (
	PortOrchestrator     = 8080
	PortExecutionHealth  = 9091
	PortRiskHealth       = 9092
	PortMarketData       = 9093
	PortPortfolio        = 9094
	PortCompliance       = 9095
)

// Risk defaults
const (
	PositionLimit     = 100000
	CreditLimit       = 10000000000
	MaxOrdersPerSec   = 100
	PriceCollarBPS    = 500
	DrawdownLimit     = 0.10
)

// Market data multicast group + port (UDP)
const (
	McastGroup = "233.0.0.1"
	McastPort  = 5000
)

// Audit log paths
const (
	AuditLogPath    = "/var/log/robin/audit.log"
	AuditLogPathDev = "logs/audit.log"
)

// Message type codes (msg_type field in ShmMessage)
const (
	MsgOrderNew     = 0x01
	MsgOrderCancel  = 0x02
	MsgOrderReplace = 0x03
	MsgTrade        = 0x10
	MsgHeartbeat    = 0xFF
)
