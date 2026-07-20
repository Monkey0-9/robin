package main

// ============================================================================
// Robin Trading Platform — Post-Trade Surveillance Engine
// ============================================================================
// Implements real-time post-trade surveillance per FINRA Rule 4560 and
// MiFID II Article 16 surveillance obligations.
//
// Detects the following manipulative patterns:
//   1. WASH_TRADE         — same client buys + sells same instrument ≤60s apart
//   2. LAYERING           — ≥5 orders same side + price, then rapid cancel (>80%)
//   3. MARKING_THE_CLOSE  — large order in final 5 min of primary session
//   4. MOMENTUM_IGNITION  — burst of orders followed by price-favorable cancel
//   5. SPOOFING           — large order placed + cancelled before fill
//
// Architecture:
//   • Async goroutine consuming from an in-process trade event channel
//   • Non-blocking: surveillance never slows the critical order path
//   • Alerts written to surveillance_alerts table with evidence JSON
//   • Prometheus counters for each alert type
//   • GET  /api/surveillance/alerts   — list unreviewed alerts
//   • POST /api/surveillance/review   — mark alert reviewed
//   • GET  /api/surveillance/status   — engine stats
// ============================================================================

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// TradeEvent is sent to the surveillance engine for each completed trade/order.
type TradeEvent struct {
	EventType  string  // "NEW", "FILL", "CANCEL"
	OrderID    int64
	ClientID   int64
	Symbol     string
	InstrumentID int64
	Side       string  // "BUY" or "SELL"
	Price      float64
	Qty        float64
	TimestampNs int64
}

// SurveillanceEngine processes trade events and detects manipulation patterns.
type SurveillanceEngine struct {
	events chan TradeEvent
	db     *sql.DB
	logger *slog.Logger

	// Pattern detection state
	mu            sync.Mutex
	clientOrders  map[int64][]TradeEvent          // clientID -> recent orders
	clientFills   map[int64][]TradeEvent           // clientID -> recent fills
	priceLevels   map[string][]TradeEvent          // "clientID:symbol:price:side" -> orders
	cancelCounts  map[int64]int                    // orderID -> cancel count

	// Metrics
	alertsTotal   atomic.Uint64
	eventsTotal   atomic.Uint64
}

// Alert type constants
const (
	AlertWashTrade        = "WASH_TRADE"
	AlertLayering         = "LAYERING"
	AlertMarkingTheClose  = "MARKING_THE_CLOSE"
	AlertMomentumIgnition = "MOMENTUM_IGNITION"
	AlertSpoofing         = "SPOOFING"
)

const (
	washTradeWindowNs     = 60 * int64(time.Second)  // 60 second window
	layeringMinOrders     = 5                          // 5+ orders at same level
	layeringCancelThresh  = 0.80                       // 80%+ cancel rate
	markingCloseWindowMin = 5                          // last 5 minutes of session
	eventBufferSize       = 10000                      // async event channel buffer
	maxClientHistory      = 500                        // orders per client kept in memory
)

// NewSurveillanceEngine creates and returns a new SurveillanceEngine.
func NewSurveillanceEngine(db *sql.DB, logger *slog.Logger) *SurveillanceEngine {
	return &SurveillanceEngine{
		events:       make(chan TradeEvent, eventBufferSize),
		db:           db,
		logger:       logger,
		clientOrders: make(map[int64][]TradeEvent),
		clientFills:  make(map[int64][]TradeEvent),
		priceLevels:  make(map[string][]TradeEvent),
		cancelCounts: make(map[int64]int),
	}
}

// Submit sends a trade event to the surveillance engine (non-blocking).
func (se *SurveillanceEngine) Submit(event TradeEvent) {
	select {
	case se.events <- event:
	default:
		// Drop if buffer full — surveillance must not block trading
		se.logger.Warn("[SURVEILLANCE] event buffer full, dropping event", "order_id", event.OrderID)
	}
}

// Start begins the background surveillance processing goroutine.
func (se *SurveillanceEngine) Start(ctx context.Context) {
	go se.processLoop(ctx)
	go se.cleanupLoop(ctx)
}

func (se *SurveillanceEngine) processLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-se.events:
			se.eventsTotal.Add(1)
			se.processEvent(event)
		}
	}
}

func (se *SurveillanceEngine) processEvent(event TradeEvent) {
	se.mu.Lock()
	defer se.mu.Unlock()

	// Store in client history
	se.clientOrders[event.ClientID] = append(se.clientOrders[event.ClientID], event)
	if len(se.clientOrders[event.ClientID]) > maxClientHistory {
		se.clientOrders[event.ClientID] = se.clientOrders[event.ClientID][1:]
	}

	switch event.EventType {
	case "NEW":
		se.checkLayering(event)
		se.checkMarkingTheClose(event)
		se.checkMomentumIgnition(event)
	case "FILL":
		se.clientFills[event.ClientID] = append(se.clientFills[event.ClientID], event)
		se.checkWashTrade(event)
	case "CANCEL":
		se.cancelCounts[event.OrderID]++
		se.checkSpoofing(event)
	}
}

// ============================================================================
// Pattern detectors
// ============================================================================

// checkWashTrade detects wash trading: same client buys + sells within 60s.
func (se *SurveillanceEngine) checkWashTrade(fillEvent TradeEvent) {
	fills := se.clientFills[fillEvent.ClientID]
	for _, prev := range fills {
		if prev.OrderID == fillEvent.OrderID {
			continue
		}
		// Same instrument, opposite side, within window
		if prev.Symbol == fillEvent.Symbol &&
			prev.Side != fillEvent.Side &&
			absDiffNs(prev.TimestampNs, fillEvent.TimestampNs) <= washTradeWindowNs {

			evidence := map[string]interface{}{
				"order_a": prev.OrderID, "side_a": prev.Side,
				"order_b": fillEvent.OrderID, "side_b": fillEvent.Side,
				"symbol": fillEvent.Symbol, "window_s": 60,
				"time_diff_ms": absDiffNs(prev.TimestampNs, fillEvent.TimestampNs) / int64(time.Millisecond),
			}
			se.raiseAlert(AlertWashTrade, fillEvent.ClientID,
				[]int64{prev.OrderID, fillEvent.OrderID}, evidence,
			)
			return
		}
	}
}

// checkLayering detects layering: many orders at same price + mass cancel.
func (se *SurveillanceEngine) checkLayering(newEvent TradeEvent) {
	key := fmt.Sprintf("%d:%s:%.2f:%s", newEvent.ClientID, newEvent.Symbol, newEvent.Price, newEvent.Side)
	se.priceLevels[key] = append(se.priceLevels[key], newEvent)
	levels := se.priceLevels[key]

	if len(levels) < layeringMinOrders {
		return
	}

	// Count cancels at this price level
	cancelCount := 0
	var orderIDs []int64
	for _, ev := range levels {
		orderIDs = append(orderIDs, ev.OrderID)
		if c := se.cancelCounts[ev.OrderID]; c > 0 {
			cancelCount++
		}
	}

	cancelRate := float64(cancelCount) / float64(len(levels))
	if cancelRate >= layeringCancelThresh {
		evidence := map[string]interface{}{
			"symbol": newEvent.Symbol, "side": newEvent.Side,
			"price": newEvent.Price, "order_count": len(levels),
			"cancel_count": cancelCount, "cancel_rate_pct": cancelRate * 100,
		}
		se.raiseAlert(AlertLayering, newEvent.ClientID, orderIDs, evidence)
		// Reset to avoid repeated alerts
		se.priceLevels[key] = nil
	}
}

// checkMarkingTheClose detects large orders near market close (last 5 minutes).
func (se *SurveillanceEngine) checkMarkingTheClose(event TradeEvent) {
	// Primary US session close: 16:00 ET = 21:00 UTC
	t := time.Unix(0, event.TimestampNs).UTC()
	closeHour, closeMin := 21, 0
	sessionCloseUTC := time.Date(t.Year(), t.Month(), t.Day(), closeHour, closeMin, 0, 0, time.UTC)
	windowStart := sessionCloseUTC.Add(-time.Duration(markingCloseWindowMin) * time.Minute)

	if t.After(windowStart) && t.Before(sessionCloseUTC) {
		// Large order threshold: > $50,000 notional
		notional := event.Price * event.Qty
		if notional > 50_000 {
			evidence := map[string]interface{}{
				"symbol": event.Symbol, "side": event.Side,
				"price": event.Price, "qty": event.Qty,
				"notional": notional,
				"minutes_to_close": sessionCloseUTC.Sub(t).Minutes(),
			}
			se.raiseAlert(AlertMarkingTheClose, event.ClientID, []int64{event.OrderID}, evidence)
		}
	}
}

// checkMomentumIgnition detects bursts of orders followed by price-favorable cancels.
func (se *SurveillanceEngine) checkMomentumIgnition(event TradeEvent) {
	history := se.clientOrders[event.ClientID]
	if len(history) < 10 {
		return
	}

	// Count orders in last 2 seconds
	cutoff := event.TimestampNs - 2*int64(time.Second)
	burstCount := 0
	for _, h := range history {
		if h.TimestampNs > cutoff && h.Symbol == event.Symbol {
			burstCount++
		}
	}

	if burstCount >= 10 {
		// High-frequency burst on same symbol — potential momentum ignition
		evidence := map[string]interface{}{
			"symbol": event.Symbol, "burst_count": burstCount,
			"window_s": 2, "side": event.Side,
		}
		se.raiseAlert(AlertMomentumIgnition, event.ClientID, []int64{event.OrderID}, evidence)
	}
}

// checkSpoofing detects large orders cancelled before fill.
func (se *SurveillanceEngine) checkSpoofing(cancelEvent TradeEvent) {
	// Large orders (> $100,000 notional) cancelled without any fill are suspicious
	notional := cancelEvent.Price * cancelEvent.Qty
	if notional > 100_000 {
		// Check if this order had any fills
		for _, fill := range se.clientFills[cancelEvent.ClientID] {
			if fill.OrderID == cancelEvent.OrderID {
				return // Had a fill — not spoofing
			}
		}
		evidence := map[string]interface{}{
			"symbol": cancelEvent.Symbol, "side": cancelEvent.Side,
			"price": cancelEvent.Price, "qty": cancelEvent.Qty,
			"notional": notional, "had_fill": false,
		}
		se.raiseAlert(AlertSpoofing, cancelEvent.ClientID, []int64{cancelEvent.OrderID}, evidence)
	}
}

// ============================================================================
// Alert recording
// ============================================================================

func (se *SurveillanceEngine) raiseAlert(alertType string, clientID int64, orderIDs []int64, evidence map[string]interface{}) {
	orderIDsJSON, _ := json.Marshal(orderIDs)
	evidenceJSON, _ := json.Marshal(evidence)

	now := time.Now().UnixNano()
	if se.db != nil {
		_, err := se.db.Exec(`
			INSERT INTO surveillance_alerts
			  (alert_type, client_id, order_ids, detected_at_ns, evidence_json, status)
			VALUES (?, ?, ?, ?, ?, 'UNREVIEWED')`,
			alertType, clientID, string(orderIDsJSON), now, string(evidenceJSON),
		)
		if err != nil {
			se.logger.Error("[SURVEILLANCE] Failed to write alert", "type", alertType, "error", err)
		}
	}

	se.alertsTotal.Add(1)
	se.logger.Warn("[SURVEILLANCE] ALERT RAISED",
		"type", alertType,
		"client_id", clientID,
		"order_ids", orderIDs,
		"evidence", string(evidenceJSON),
	)
}

// cleanupLoop removes stale state to prevent memory growth.
func (se *SurveillanceEngine) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			se.mu.Lock()
			cutoff := time.Now().Add(-1 * time.Hour).UnixNano()
			for clientID, orders := range se.clientOrders {
				filtered := orders[:0]
				for _, o := range orders {
					if o.TimestampNs > cutoff {
						filtered = append(filtered, o)
					}
				}
				se.clientOrders[clientID] = filtered
			}
			for clientID, fills := range se.clientFills {
				filtered := fills[:0]
				for _, f := range fills {
					if f.TimestampNs > cutoff {
						filtered = append(filtered, f)
					}
				}
				se.clientFills[clientID] = filtered
			}
			se.mu.Unlock()
		}
	}
}

// ============================================================================
// HTTP Handlers
// ============================================================================

// handleSurveillanceAlerts handles GET /api/surveillance/alerts.
func handleSurveillanceAlerts(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		statusFilter := r.URL.Query().Get("status")
		if statusFilter == "" {
			statusFilter = "UNREVIEWED"
		}
		limitStr := r.URL.Query().Get("limit")
		limit := 50
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 500 {
			limit = n
		}

		rows, err := db.Query(`
			SELECT id, alert_type, client_id, order_ids, detected_at_ns,
			       evidence_json, status
			FROM surveillance_alerts
			WHERE status=?
			ORDER BY detected_at_ns DESC LIMIT ?`,
			statusFilter, limit)
		if err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var alerts []map[string]interface{}
		for rows.Next() {
			var id, clientID, detectedAt int64
			var alertType, orderIDs, evidenceJSON, status string
			if err := rows.Scan(&id, &alertType, &clientID, &orderIDs,
				&detectedAt, &evidenceJSON, &status); err != nil {
				continue
			}
			var evidence interface{}
			json.Unmarshal([]byte(evidenceJSON), &evidence)
			alerts = append(alerts, map[string]interface{}{
				"id": id, "alert_type": alertType, "client_id": clientID,
				"order_ids": orderIDs, "detected_at_ns": detectedAt,
				"evidence": evidence, "status": status,
			})
		}
		if alerts == nil {
			alerts = []map[string]interface{}{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"alerts": alerts, "count": len(alerts), "status_filter": statusFilter,
		})
	}
}

// handleSurveillanceReview handles POST /api/surveillance/review/{id}.
func handleSurveillanceReview(db *sql.DB, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		alertID := supervisoryIDFromPath(r) // reuse path extractor
		var body struct {
			Status string `json:"status"` // REVIEWED_CLEAR, ESCALATED, REPORTED
			Notes  string `json:"notes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || alertID == 0 {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}

		validStatuses := map[string]bool{
			"REVIEWED_CLEAR": true, "ESCALATED": true, "REPORTED": true,
		}
		if !validStatuses[body.Status] {
			http.Error(w, `{"error":"status must be REVIEWED_CLEAR, ESCALATED, or REPORTED"}`, http.StatusBadRequest)
			return
		}

		reviewer := adminFromContext(r)
		now := time.Now().UnixNano()
		_, err := db.Exec(`
			UPDATE surveillance_alerts
			SET status=?, reviewer_id=0, reviewed_at_ns=?, review_notes=?
			WHERE id=?`,
			body.Status, now, body.Notes+"|reviewed_by:"+reviewer, alertID,
		)
		if err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}

		logger.Info("[SURVEILLANCE] Alert reviewed",
			"alert_id", alertID, "status", body.Status, "reviewer", reviewer,
		)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": body.Status, "alert_id": alertID, "reviewer": reviewer,
		})
	}
}

// handleSurveillanceStatus handles GET /api/surveillance/status.
func handleSurveillanceStatus(db *sql.DB, se *SurveillanceEngine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var unreviewed, escalated, reported int64
		if db != nil {
			db.QueryRow(`SELECT COUNT(*) FROM surveillance_alerts WHERE status='UNREVIEWED'`).Scan(&unreviewed)
			db.QueryRow(`SELECT COUNT(*) FROM surveillance_alerts WHERE status='ESCALATED'`).Scan(&escalated)
			db.QueryRow(`SELECT COUNT(*) FROM surveillance_alerts WHERE status='REPORTED'`).Scan(&reported)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"engine_running":  true,
			"events_processed": se.eventsTotal.Load(),
			"alerts_total":    se.alertsTotal.Load(),
			"alerts": map[string]int64{
				"unreviewed": unreviewed,
				"escalated":  escalated,
				"reported":   reported,
			},
			"buffer_size": eventBufferSize,
		})
	}
}

// ============================================================================
// Helpers
// ============================================================================

func absDiffNs(a, b int64) int64 {
	d := a - b
	if d < 0 {
		return -d
	}
	return d
}
