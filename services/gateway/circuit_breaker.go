package main

// ============================================================================
// Circuit Breaker Integration (Phase 3.6)
// ============================================================================
// The gateway mirrors the risk engine's daily-drawdown circuit breaker so a
// trip halts order entry *at the gateway* — before an order even reaches the
// risk gate or matching engine. It has three trip sources:
//
//   1. Local drawdown evaluation from the orchestrator's peak/current equity
//      (updated every second, compared against MaxDrawdownLimit).
//   2. Polling the risk engine's Prometheus endpoint (/metrics on
//      ROBIN_RISK_METRICS_URL) for robin_risk_circuit_breaker_trips_total —
//      any increase trips the gateway breaker with the same reason.
//   3. A manual operator trip/reset via the REST API.
//
// Trip/reset events are broadcast on the WebSocket, persisted to the kill
// switch log for audit, and exposed as a Prometheus gauge.
// ============================================================================

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// CircuitBreakerActive is a Prometheus gauge: 1 when the gateway circuit
// breaker is tripped, 0 otherwise.
var CircuitBreakerActive = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "robin_gateway_circuit_breaker_active",
	Help: "Gateway circuit breaker state (1=tripped, 0=normal)",
})

// CircuitBreakerManager tracks the gateway-side circuit breaker state.
type CircuitBreakerManager struct {
	tripped       atomic.Bool
	reason        atomic.Value // string
	source        atomic.Value // string
	tripTimeNs    atomic.Int64
	tripCount     atomic.Uint64
	resetCount    atomic.Uint64
	lastRiskTrips atomic.Uint64

	db     *sql.DB
	logger *slog.Logger
	wsHub  *WebSocketHub
}

// globalCircuitBreaker is the process-wide circuit breaker used by the order path.
var globalCircuitBreaker *CircuitBreakerManager

// NewCircuitBreakerManager creates a breaker with no trip state.
func NewCircuitBreakerManager(db *sql.DB, logger *slog.Logger, wsHub *WebSocketHub) *CircuitBreakerManager {
	cb := &CircuitBreakerManager{
		db:     db,
		logger: logger,
		wsHub:  wsHub,
	}
	cb.reason.Store("")
	cb.source.Store("")
	return cb
}

// InitCircuitBreaker initializes the global breaker. Must be called from
// NewOrchestrator before any order traffic.
func InitCircuitBreaker(db *sql.DB, logger *slog.Logger, wsHub *WebSocketHub) *CircuitBreakerManager {
	if globalCircuitBreaker == nil {
		globalCircuitBreaker = NewCircuitBreakerManager(db, logger, wsHub)
	}
	return globalCircuitBreaker
}

// IsTripped is a lock-free hot-path check suitable for every order.
func (cb *CircuitBreakerManager) IsTripped() bool {
	return cb.tripped.Load()
}

// Trip activates the breaker with the given reason and source. Idempotent:
// repeated trips keep the first trip time and increment the counter once.
func (cb *CircuitBreakerManager) Trip(reason, source string) {
	if cb == nil {
		return
	}
	first := cb.tripped.CompareAndSwap(false, true)
	if first {
		cb.reason.Store(reason)
		cb.source.Store(source)
		cb.tripTimeNs.Store(time.Now().UnixNano())
		cb.tripCount.Add(1)
		CircuitBreakerActive.Set(1)
		if cb.logger != nil {
			cb.logger.Error("[CIRCUIT BREAKER] TRIPPED", "reason", reason, "source", source)
		}
		cb.broadcast("TRIP", reason, source)
		cb.persist("TRIP", reason, source)
	} else {
		cb.tripCount.Add(1)
	}
}

// Reset clears the breaker. Only the manual path uses this; drawdown-derived
// trips must be reset deliberately by an operator (never automatically).
func (cb *CircuitBreakerManager) Reset(reason, source string) {
	if cb == nil {
		return
	}
	if cb.tripped.CompareAndSwap(true, false) {
		prevReason, _ := cb.reason.Load().(string)
		cb.reason.Store("")
		cb.source.Store("")
		cb.tripTimeNs.Store(0)
		cb.resetCount.Add(1)
		CircuitBreakerActive.Set(0)
		if cb.logger != nil {
			cb.logger.Warn("[CIRCUIT BREAKER] RESET", "prev_reason", prevReason, "source", source)
		}
		cb.broadcast("RESET", reason, source)
		cb.persist("RESET", reason, source)
	}
}

// CheckDrawdown evaluates peak/current equity against the configured daily
// drawdown limit (fraction, e.g. 0.10 = 10%). Trips when the limit is met.
func (cb *CircuitBreakerManager) CheckDrawdown(peakEquity, currentEquity, limit float64) {
	if cb == nil || limit <= 0 {
		return
	}
	if cb.IsTripped() {
		return
	}
	if peakEquity <= 0 || currentEquity <= 0 {
		return
	}
	drawdown := (peakEquity - currentEquity) / peakEquity
	if drawdown >= limit {
		cb.Trip(fmt.Sprintf("DAILY_DRAWDOWN_LIMIT_EXCEEDED (%.2f%% >= %.2f%%)",
			drawdown*100, limit*100), "local_drawdown")
	}
}

// PollRiskEngine fetches the risk engine's Prometheus metrics and trips if the
// circuit breaker trip counter has advanced since the last poll.
func (cb *CircuitBreakerManager) PollRiskEngine(ctx context.Context, metricsURL string) {
	if cb == nil || metricsURL == "" {
		return
	}
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsURL, nil)
	if err != nil {
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		// Risk engine unreachable is not itself a breaker trip — but log it.
		if cb.logger != nil {
			cb.logger.Debug("circuit breaker risk poll failed", "url", metricsURL, "error", err)
		}
		return
	}
	defer resp.Body.Close()

	trips := uint64(0)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "robin_risk_circuit_breaker_trips_total ") {
			if n, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "robin_risk_circuit_breaker_trips_total ")), 10, 64); err == nil {
				trips = n
			}
		}
	}

	prev := cb.lastRiskTrips.Load()
	if trips > prev {
		cb.lastRiskTrips.Store(trips)
		cb.Trip("RISK_ENGINE_CIRCUIT_BREAKER_TRIPPED", "risk_engine_metrics")
	} else {
		// Keep the last seen count monotonic even if the risk engine restarts.
		if trips > 0 {
			cb.lastRiskTrips.Store(trips)
		}
	}
}

// StartRiskPolling begins periodic polling of the risk engine metrics endpoint.
// The URL comes from ROBIN_RISK_METRICS_URL (default http://127.0.0.1:9096/metrics).
func (cb *CircuitBreakerManager) StartRiskPolling(ctx context.Context) {
	if cb == nil {
		return
	}
	url := os.Getenv("ROBIN_RISK_METRICS_URL")
	if url == "" {
		url = "http://127.0.0.1:9096/metrics"
	}
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		cb.PollRiskEngine(ctx, url)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cb.PollRiskEngine(ctx, url)
			}
		}
	}()
}

// GetStatus returns a JSON-serializable snapshot of breaker state.
func (cb *CircuitBreakerManager) GetStatus() map[string]interface{} {
	if cb == nil {
		return map[string]interface{}{"tripped": false, "reason": "", "source": ""}
	}
	reason, _ := cb.reason.Load().(string)
	source, _ := cb.source.Load().(string)
	return map[string]interface{}{
		"tripped":      cb.tripped.Load(),
		"reason":       reason,
		"source":       source,
		"trip_time_ns": cb.tripTimeNs.Load(),
		"trip_count":   cb.tripCount.Load(),
		"reset_count":  cb.resetCount.Load(),
	}
}

func (cb *CircuitBreakerManager) broadcast(action, reason, source string) {
	if cb.wsHub != nil {
		cb.wsHub.BroadcastJSON(map[string]interface{}{
			"type":       "CIRCUIT_BREAKER",
			"action":     action,
			"reason":     reason,
			"source":     source,
			"time_ns":    time.Now().UnixNano(),
			"trip_count": cb.tripCount.Load(),
		})
	}
}

func (cb *CircuitBreakerManager) persist(action, reason, source string) {
	if cb.db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := cb.db.ExecContext(ctx, `
		INSERT INTO kill_switch_log
		  (level, target_id, action, reason, tripped_by, secondary_approver,
		   tripped_at_ns, reset_at_ns, chain_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		"CIRCUIT_BREAKER", "", action, reason, source, "",
		time.Now().UnixNano(), 0, "",
	)
	if err != nil && cb.logger != nil {
		cb.logger.Error("failed to persist circuit breaker event", "error", err)
	}
}

// ============================================================================
// HTTP Handlers
// ============================================================================

// circuitBreakerStatusHandler handles GET /api/circuitbreaker/status.
func circuitBreakerStatusHandler(cb *CircuitBreakerManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cb.GetStatus())
	}
}

// circuitBreakerTripHandler handles POST /api/circuitbreaker/trip (admin).
func circuitBreakerTripHandler(cb *CircuitBreakerManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Reason == "" {
			body.Reason = "manual_operator_trip"
		}
		admin := adminFromContext(r)
		cb.Trip(body.Reason, "manual_operator:"+admin)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cb.GetStatus())
	}
}

// circuitBreakerResetHandler handles POST /api/circuitbreaker/reset (admin).
func circuitBreakerResetHandler(cb *CircuitBreakerManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Reason == "" {
			body.Reason = "manual_operator_reset"
		}
		admin := adminFromContext(r)
		cb.Reset(body.Reason, "manual_operator:"+admin)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cb.GetStatus())
	}
}
