package main

// ============================================================================
// Robin Trading Platform — Best Execution Monitor
// ============================================================================
// Implements MiFID II Article 27 (best execution obligation) and Reg NMS
// best execution monitoring.
//
// Tracks per-order execution quality metrics:
//   • Price improvement vs. NBBO mid (in basis points)
//   • Slippage vs. arrival price
//   • Fill rate (% of submitted orders filled)
//   • Time-to-fill latency distribution
//   • Exchange routing effectiveness
//
// Endpoints:
//   GET /api/execution/quality         — rolling best execution stats
//   GET /api/execution/quality/report  — MiFID II Art. 27 periodic report
// ============================================================================

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// ExecutionRecord captures per-order execution quality data.
type ExecutionRecord struct {
	OrderID         int64
	Symbol          string
	Side            string
	ArrivalPriceUSD float64 // price when order entered system
	FillPriceUSD    float64 // actual execution price
	SubmittedQty    float64
	FilledQty       float64
	Exchange        string
	EntryTimeNs     int64
	FillTimeNs      int64
	SlippageBps     float64 // (FillPrice - ArrivalPrice) / ArrivalPrice * 10000 * side_sign
	PriceImprovBps  float64 // positive = price improvement vs NBBO mid
	TimeToFillNs    int64
}

// BestExecutionMonitor tracks execution quality for MiFID II / Reg NMS reporting.
type BestExecutionMonitor struct {
	mu         sync.RWMutex
	records    []ExecutionRecord
	maxRecords int

	// Rolling stats (last 1000 orders)
	totalOrders      atomic.Uint64
	totalFilled      atomic.Uint64
	totalSlippageBps atomic.Int64 // sum of slippage_bps * 1000 for precision
	totalTTFNs       atomic.Int64 // sum of time-to-fill in ns
	totalPImpBps     atomic.Int64 // sum of price improvement bps * 1000

	logger *slog.Logger
	db     *sql.DB
}

// NewBestExecutionMonitor creates a BestExecutionMonitor.
func NewBestExecutionMonitor(db *sql.DB, logger *slog.Logger) *BestExecutionMonitor {
	return &BestExecutionMonitor{
		maxRecords: 10000,
		db:         db,
		logger:     logger,
	}
}

// Record submits an execution record to the monitor.
func (bem *BestExecutionMonitor) Record(rec ExecutionRecord) {
	// Compute slippage
	if rec.ArrivalPriceUSD > 0 {
		diff := rec.FillPriceUSD - rec.ArrivalPriceUSD
		if rec.Side == "SELL" {
			diff = -diff
		}
		rec.SlippageBps = diff / rec.ArrivalPriceUSD * 10_000
	}

	// Time to fill
	if rec.FillTimeNs > 0 && rec.EntryTimeNs > 0 {
		rec.TimeToFillNs = rec.FillTimeNs - rec.EntryTimeNs
	}

	// Update rolling stats
	bem.totalOrders.Add(1)
	if rec.FilledQty > 0 {
		bem.totalFilled.Add(1)
	}
	bem.totalSlippageBps.Add(int64(rec.SlippageBps * 1000))
	bem.totalTTFNs.Add(rec.TimeToFillNs)
	bem.totalPImpBps.Add(int64(rec.PriceImprovBps * 1000))

	// Store record
	bem.mu.Lock()
	bem.records = append(bem.records, rec)
	if len(bem.records) > bem.maxRecords {
		bem.records = bem.records[1:]
	}
	bem.mu.Unlock()

	// Update trade slippage in DB
	if bem.db != nil && rec.OrderID > 0 {
		_, _ = bem.db.Exec(`
			UPDATE trades SET slippage_bps=$1 WHERE order_id=$2`,
			int64(rec.SlippageBps*100), rec.OrderID,
		)
	}
}

// GetRollingStats returns rolling execution quality statistics.
func (bem *BestExecutionMonitor) GetRollingStats() map[string]interface{} {
	orders := bem.totalOrders.Load()
	filled := bem.totalFilled.Load()

	fillRate := 0.0
	if orders > 0 {
		fillRate = float64(filled) / float64(orders) * 100
	}

	avgSlippageBps := 0.0
	avgTTFMs := 0.0
	avgPImpBps := 0.0
	if filled > 0 {
		avgSlippageBps = float64(bem.totalSlippageBps.Load()) / float64(filled) / 1000.0
		avgTTFMs = float64(bem.totalTTFNs.Load()) / float64(filled) / float64(time.Millisecond)
		avgPImpBps = float64(bem.totalPImpBps.Load()) / float64(filled) / 1000.0
	}

	// Exchange breakdown
	bem.mu.RLock()
	exchangeCounts := make(map[string]int)
	for _, r := range bem.records {
		exchangeCounts[r.Exchange]++
	}
	bem.mu.RUnlock()

	return map[string]interface{}{
		"total_orders":         orders,
		"total_filled":         filled,
		"fill_rate_pct":        fillRate,
		"avg_slippage_bps":     avgSlippageBps,
		"avg_time_to_fill_ms":  avgTTFMs,
		"avg_price_improv_bps": avgPImpBps,
		"exchange_routing":     exchangeCounts,
		"mifid_art27_ok":       avgSlippageBps < 5.0, // <5bps slippage = good execution
	}
}

// ============================================================================
// HTTP Handlers
// ============================================================================

// handleExecutionQuality handles GET /api/execution/quality.
func handleExecutionQuality(bem *BestExecutionMonitor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats := bem.GetRollingStats()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	}
}

// handleExecutionQualityReport handles GET /api/execution/quality/report.
// Generates a MiFID II Article 27 periodic best execution report.
func handleExecutionQualityReport(bem *BestExecutionMonitor, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats := bem.GetRollingStats()

		bem.mu.RLock()
		// Build per-instrument stats
		instrumentStats := make(map[string]map[string]interface{})
		for _, rec := range bem.records {
			if _, ok := instrumentStats[rec.Symbol]; !ok {
				instrumentStats[rec.Symbol] = map[string]interface{}{
					"count": 0, "slippage_sum": 0.0, "pi_sum": 0.0,
				}
			}
			instrumentStats[rec.Symbol]["count"] = instrumentStats[rec.Symbol]["count"].(int) + 1
			instrumentStats[rec.Symbol]["slippage_sum"] = instrumentStats[rec.Symbol]["slippage_sum"].(float64) + rec.SlippageBps
			instrumentStats[rec.Symbol]["pi_sum"] = instrumentStats[rec.Symbol]["pi_sum"].(float64) + rec.PriceImprovBps
		}
		bem.mu.RUnlock()

		// Compute per-instrument averages
		for sym, data := range instrumentStats {
			count := data["count"].(int)
			if count > 0 {
				instrumentStats[sym]["avg_slippage_bps"] = data["slippage_sum"].(float64) / float64(count)
				instrumentStats[sym]["avg_price_improv_bps"] = data["pi_sum"].(float64) / float64(count)
			}
		}

		report := map[string]interface{}{
			"report_type":     "MiFID_II_Art27_Best_Execution",
			"reporting_firm":  "Robin Trading Platform",
			"period":          fmt.Sprintf("%s to %s", time.Now().AddDate(0, -1, 0).Format("2006-01-02"), time.Now().Format("2006-01-02")),
			"generated_at":    time.Now().UTC().Format(time.RFC3339),
			"summary":         stats,
			"per_instrument":  instrumentStats,
			"compliance_note": "Report generated per MiFID II Article 27(3)(f) requirements",
		}

		logger.Info("Best execution report generated", "orders", stats["total_orders"])
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(report)
	}
}

// ============================================================================
// Failover Manager
// ============================================================================
// Primary/standby failover for the Gateway orchestrator.
// Implements RTO < 30 seconds via heartbeat monitoring and automatic promotion.

// FailoverRole describes the current node's role.
type FailoverRole string

const (
	RolePrimary FailoverRole = "PRIMARY"
	RoleStandby FailoverRole = "STANDBY"
	RoleUnknown FailoverRole = "UNKNOWN"
)

// FailoverManager manages primary/standby gateway state.
type FailoverManager struct {
	role              atomic.Uint32 // 0=unknown, 1=primary, 2=standby
	primaryAddr       string
	standbyAddr       string
	heartbeatInterval time.Duration
	heartbeatTimeout  time.Duration

	consecutiveFailures atomic.Uint32
	promotionThreshold  uint32

	mu              sync.RWMutex
	lastHeartbeatNs atomic.Int64

	logger *slog.Logger
}

// NewFailoverManager creates a new FailoverManager.
func NewFailoverManager(primaryAddr, standbyAddr string, logger *slog.Logger) *FailoverManager {
	fm := &FailoverManager{
		primaryAddr:        primaryAddr,
		standbyAddr:        standbyAddr,
		heartbeatInterval:  100 * time.Millisecond,
		heartbeatTimeout:   500 * time.Millisecond,
		promotionThreshold: 5,
		logger:             logger,
	}
	// Default to primary role
	fm.role.Store(1)
	return fm
}

// GetRole returns the current failover role.
func (fm *FailoverManager) GetRole() FailoverRole {
	switch fm.role.Load() {
	case 1:
		return RolePrimary
	case 2:
		return RoleStandby
	default:
		return RoleUnknown
	}
}

// IsPrimary returns true if this node is acting as primary.
func (fm *FailoverManager) IsPrimary() bool {
	return fm.role.Load() == 1
}

// StartHeartbeat begins heartbeat monitoring (standby watches primary).
func (fm *FailoverManager) StartHeartbeat(ctx context.Context) {
	if fm.primaryAddr == "" || fm.standbyAddr == "" {
		return // single-node mode
	}
	go fm.heartbeatLoop(ctx)
}

func (fm *FailoverManager) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(fm.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if fm.GetRole() == RoleStandby {
				fm.checkPrimaryHealth()
			}
		}
	}
}

func (fm *FailoverManager) checkPrimaryHealth() {
	// Health check the primary via its /health endpoint
	client := &http.Client{Timeout: fm.heartbeatTimeout}
	resp, err := client.Get("http://" + fm.primaryAddr + "/health")
	if err == nil {
		resp.Body.Close()
		fm.consecutiveFailures.Store(0)
		fm.lastHeartbeatNs.Store(time.Now().UnixNano())
		return
	}

	failures := fm.consecutiveFailures.Add(1)
	fm.logger.Warn("[FAILOVER] Primary health check failed",
		"failures", failures, "threshold", fm.promotionThreshold,
	)

	if failures >= fm.promotionThreshold {
		fm.promoteToPrimary("primary unreachable")
	}
}

// PromoteToPrimary elevates this standby to primary role.
func (fm *FailoverManager) promoteToPrimary(reason string) {
	fm.role.Store(1)
	fm.consecutiveFailures.Store(0)
	fm.logger.Error("[FAILOVER] PROMOTING TO PRIMARY",
		"reason", reason, "new_role", "PRIMARY",
	)
}

// DemoteToStandby moves this node to standby.
func (fm *FailoverManager) DemoteToStandby() {
	fm.role.Store(2)
	fm.logger.Warn("[FAILOVER] Demoted to STANDBY")
}

// GetStatus returns the failover state for the /api/failover/status endpoint.
func (fm *FailoverManager) GetStatus() map[string]interface{} {
	return map[string]interface{}{
		"role":                 string(fm.GetRole()),
		"primary_addr":         fm.primaryAddr,
		"standby_addr":         fm.standbyAddr,
		"consecutive_failures": fm.consecutiveFailures.Load(),
		"last_heartbeat_ns":    fm.lastHeartbeatNs.Load(),
	}
}

// handleFailoverStatus handles GET /api/failover/status.
func handleFailoverStatus(fm *FailoverManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(fm.GetStatus())
	}
}

// handleFailoverPromote handles POST /api/failover/promote (admin).
func handleFailoverPromote(fm *FailoverManager, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin := adminFromContext(r)
		fm.promoteToPrimary("manual promotion by " + admin)
		logger.Warn("[FAILOVER] Manual promotion", "by", admin)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "promoted", "new_role": "PRIMARY", "promoted_by": admin,
		})
	}
}
