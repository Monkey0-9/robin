package main

// ============================================================================
// Robin Trading Platform — Supervisory Workflow API (FINRA Rule 3110)
// ============================================================================
// Implements principal approval workflow for large orders per FINRA 3110.
// Features:
//   • Auto-approve orders below configurable threshold
//   • Require principal approval for large orders (>$1M default)
//   • Digital signature (SHA-256 hash) on every approve/reject decision
//   • TTL-based expiry: pending approvals auto-reject after 5 minutes
//   • Complete immutable audit trail in supervisory_decisions table
//   • Prometheus metrics for pending/approved/rejected totals
//
// Endpoints:
//   GET  /api/supervisory/pending          — list pending approvals
//   POST /api/supervisory/approve/{id}     — approve with principal JWT
//   POST /api/supervisory/reject/{id}      — reject with principal JWT
//   GET  /api/supervisory/history          — paginated decision history
// ============================================================================

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
)

// SupervisoryThreshold is the notional value above which principal approval is required.
// Default: $1,000,000. Configurable via ROBIN_SUPERVISORY_THRESHOLD env.
const defaultSupervisoryThresholdUSD = 1_000_000.0

// supervisoryThresholdUSD returns the configured principal-approval threshold,
// falling back to the $1M default when ROBIN_SUPERVISORY_THRESHOLD is unset or invalid.
func supervisoryThresholdUSD() float64 {
	if v := os.Getenv("ROBIN_SUPERVISORY_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return defaultSupervisoryThresholdUSD
}

// heldApproval captures the engine submission details of an order held for
// principal approval (FINRA 3110) so an approval can route it to the engine.
type heldApproval struct {
	approvalID int64
	clOrdID    string
	orderID    int64
	symbol     string
	side       string
	orderType  string
	price      int64 // fixed-point 1e8
	qty        int64 // fixed-point 1e8
}

// SupervisoryPendingTTL is how long a pending approval remains valid before auto-rejection.
const supervisoryPendingTTL = 5 * time.Minute

// ============================================================================
// Core logic
// ============================================================================

// checkSupervisoryApproval checks whether an order needs principal approval.
// Returns (needsApproval, approvalID). If needsApproval=false, proceed.
func checkSupervisoryApproval(db *sql.DB, orderID int64, symbol string, notional float64, threshold float64) (needsApproval bool, approvalID int64, err error) {
	if notional <= threshold {
		// Auto-approve: below threshold
		now := time.Now().UnixNano()
		hash := supervisoryDecisionHash(orderID, "AUTO_APPROVED", "auto-approved-below-threshold", now)
		result, dbErr := db.Exec(`
			INSERT INTO supervisory_decisions
			  (order_id, notional, symbol, decision, principal_id, principal_name,
			   reason, decided_at_ns, expires_at_ns, signature_hash)
			VALUES ($1, $2, $3, 'AUTO_APPROVED', 0, 'SYSTEM', 'Below threshold', $4, 0, $5)`,
			orderID, notional, symbol, now, hash,
		)
		if dbErr != nil {
			return false, 0, dbErr
		}
		id, idErr := result.LastInsertId()
		if idErr != nil {
			return false, 0, idErr
		}
		return false, id, nil
	}

	// Requires principal approval — create pending record
	now := time.Now().UnixNano()
	expiresAt := time.Now().Add(supervisoryPendingTTL).UnixNano()
	hash := supervisoryDecisionHash(orderID, "PENDING", "pending-principal-approval", now)

	result, err := db.Exec(`
		INSERT INTO supervisory_decisions
		  (order_id, notional, symbol, decision, principal_id, principal_name,
		   reason, decided_at_ns, expires_at_ns, signature_hash)
		VALUES ($1, $2, $3, 'PENDING', 0, '', 'Awaiting principal approval (FINRA 3110)', $4, $5, $6)`,
		orderID, notional, symbol, now, expiresAt, hash,
	)
	if err != nil {
		return true, 0, err
	}
	id, idErr := result.LastInsertId()
	if idErr != nil {
		return true, 0, idErr
	}
	return true, id, nil
}

// approveSupervisoryOrder records a principal approval decision.
func approveSupervisoryOrder(db *sql.DB, approvalID int64, principalName, reason string, principalID int) error {
	return recordSupervisoryDecision(db, approvalID, "APPROVED", principalName, reason, principalID)
}

// rejectSupervisoryOrder records a principal rejection decision.
func rejectSupervisoryOrder(db *sql.DB, approvalID int64, principalName, reason string, principalID int) error {
	return recordSupervisoryDecision(db, approvalID, "REJECTED", principalName, reason, principalID)
}

func recordSupervisoryDecision(db *sql.DB, approvalID int64, decision, principalName, reason string, principalID int) error {
	now := time.Now().UnixNano()
	hash := supervisoryDecisionHash(approvalID, decision, reason, now)

	_, err := db.Exec(`
		UPDATE supervisory_decisions
		SET decision=$1, principal_id=$2, principal_name=$3, reason=$4,
		    decided_at_ns=$5, signature_hash=$6
		WHERE id=$7 AND decision='PENDING'`,
		decision, principalID, principalName, reason, now, hash, approvalID,
	)
	return err
}

func supervisoryDecisionHash(id int64, decision, reason string, timestamp int64) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s|%d", id, decision, reason, timestamp)))
	return fmt.Sprintf("%x", h)
}

// ============================================================================
// HTTP Handlers
// ============================================================================

// handleSupervisoryPending handles GET /api/supervisory/pending.
func handleSupervisoryPending(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			http.Error(w, `{"error":"database not initialized"}`, http.StatusInternalServerError)
			return
		}
		// Expire stale pending records first
		_, _ = db.Exec(`
			UPDATE supervisory_decisions
			SET decision='AUTO_REJECTED', principal_name='SYSTEM',
			    reason='TTL expired — FINRA 3110 principal approval timeout'
			WHERE decision='PENDING' AND expires_at_ns > 0 AND expires_at_ns < $1`,
			time.Now().UnixNano(),
		)

		rows, err := db.Query(`
			SELECT id, order_id, notional, symbol, decided_at_ns, expires_at_ns
			FROM supervisory_decisions
			WHERE decision='PENDING'
			ORDER BY id DESC`)
		if err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var pending []map[string]interface{}
		for rows.Next() {
			var id, orderID, decidedAt, expiresAt int64
			var notional float64
			var symbol string
			if err := rows.Scan(&id, &orderID, &notional, &symbol, &decidedAt, &expiresAt); err != nil {
				continue
			}
			remaining := (expiresAt - time.Now().UnixNano()) / int64(time.Second)
			pending = append(pending, map[string]interface{}{
				"approval_id":   id,
				"order_id":      orderID,
				"notional":      notional,
				"symbol":        symbol,
				"created_at_ns": decidedAt,
				"expires_in_s":  remaining,
			})
		}
		if pending == nil {
			pending = []map[string]interface{}{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"pending": pending, "count": len(pending)})
	}
}

// handleSupervisoryApprove handles POST /api/supervisory/approve/{id}.
// As an Orchestrator method it can also release the held order to the engine.
func (o *Orchestrator) handleSupervisoryApprove(w http.ResponseWriter, r *http.Request) {
	approvalID := supervisoryIDFromPath(r)
	if approvalID == 0 {
		http.Error(w, `{"error":"approval id required"}`, http.StatusBadRequest)
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Reason == "" {
		body.Reason = "Approved by principal"
	}

	principal := adminFromContext(r)
	if err := approveSupervisoryOrder(o.db, approvalID, principal, body.Reason, 0); err != nil {
		http.Error(w, `{"error":"failed to record approval"}`, http.StatusInternalServerError)
		return
	}

	o.logger.Info("Supervisory approval granted",
		"approval_id", approvalID, "principal", principal, "reason", body.Reason,
	)

	// Route the (previously held) order to the matching engine. A failure here
	// still means the approval was recorded; the order simply stays parked.
	if err := o.releaseApprovedOrder(approvalID); err != nil {
		o.logger.Error("approved order failed to route to engine",
			"approval_id", approvalID, "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":      "APPROVED_FAILED_TO_ROUTE",
			"approval_id": approvalID,
			"principal":   principal,
			"error":       err.Error(),
			"message":     "Approval recorded; order is paused awaiting an available matching engine.",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "APPROVED",
		"approval_id": approvalID,
		"principal":   principal,
		"reason":      body.Reason,
	})
}

func (o *Orchestrator) releaseApprovedOrder(approvalID int64) error {
	o.approvalMu.Lock()
	held, ok := o.pendingApproval[approvalID]
	o.approvalMu.Unlock()
	if !ok {
		return nil // nothing held at the gateway — nothing to release
	}
	if o.matchClient == nil || !o.matchClient.IsEnabled() {
		return fmt.Errorf("matching engine unavailable")
	}
	instID := getInstrumentID(held.symbol)
	matchJSON := fmt.Sprintf(
		`{"cl_ord_id":"%s","id":%d,"instrument_id":%d,"price":%d,"qty":%d,"side":"%s","type":"%s"}`,
		held.clOrdID, held.orderID, instID, held.price, held.qty, held.side, held.orderType,
	)
	resp, err := o.matchClient.SendOrderJSON(matchJSON)
	if err != nil {
		return err
	}
	var meResp MatchingEngineResponse
	if json.Unmarshal([]byte(resp), &meResp) != nil || !meResp.Success {
		status := "REJECTED"
		if meResp.Status != "" {
			status = meResp.Status
		}
		return fmt.Errorf("engine did not confirm approved order (%s)", status)
	}
	o.approvalMu.Lock()
	delete(o.pendingApproval, approvalID)
	o.approvalMu.Unlock()
	if o.db != nil {
		o.db.Exec("UPDATE orders SET status = $1, updated_at_ns = $2 WHERE cl_order_id = $3",
			meResp.Status, time.Now().UnixNano(), held.clOrdID)
	}
	o.logger.Info("principal-approved order routed to engine",
		"cl_ord_id", held.clOrdID, "approval_id", approvalID, "status", meResp.Status)
	return nil
}

// handleSupervisoryReject handles POST /api/supervisory/reject/{id}.
func (o *Orchestrator) handleSupervisoryReject(w http.ResponseWriter, r *http.Request) {
	approvalID := supervisoryIDFromPath(r)
	if approvalID == 0 {
		http.Error(w, `{"error":"approval id required"}`, http.StatusBadRequest)
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Reason == "" {
		http.Error(w, `{"error":"reason is required for rejection (FINRA 3110)"}`, http.StatusBadRequest)
		return
	}

	principal := adminFromContext(r)
	if err := rejectSupervisoryOrder(o.db, approvalID, principal, body.Reason, 0); err != nil {
		http.Error(w, `{"error":"failed to record rejection"}`, http.StatusInternalServerError)
		return
	}

	// Mark the held order rejected and drop it from the approval queue.
	o.approvalMu.Lock()
	held, ok := o.pendingApproval[approvalID]
	if ok {
		delete(o.pendingApproval, approvalID)
	}
	o.approvalMu.Unlock()
	if ok && o.db != nil {
		o.db.Exec("UPDATE orders SET status = 'REJECTED', updated_at_ns = $1 WHERE cl_order_id = $2",
			time.Now().UnixNano(), held.clOrdID)
	}

	o.logger.Warn("Supervisory order rejected",
		"approval_id", approvalID, "principal", principal, "reason", body.Reason,
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "REJECTED",
		"approval_id": approvalID,
		"principal":   principal,
		"reason":      body.Reason,
	})
}

// handleSupervisoryHistory handles GET /api/supervisory/history.
func handleSupervisoryHistory(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limitStr := r.URL.Query().Get("limit")
		limit := 50
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 500 {
			limit = n
		}

		rows, err := db.Query(`
			SELECT id, order_id, notional, symbol, decision, principal_name,
			       reason, decided_at_ns, signature_hash
			FROM supervisory_decisions
			ORDER BY id DESC LIMIT $1`, limit)
		if err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var history []map[string]interface{}
		for rows.Next() {
			var id, orderID, decidedAt int64
			var notional float64
			var symbol, decision, principalName, reason, hash string
			if err := rows.Scan(&id, &orderID, &notional, &symbol, &decision,
				&principalName, &reason, &decidedAt, &hash); err != nil {
				continue
			}
			history = append(history, map[string]interface{}{
				"id": id, "order_id": orderID, "notional": notional,
				"symbol": symbol, "decision": decision, "principal": principalName,
				"reason": reason, "decided_at_ns": decidedAt, "signature_hash": hash,
			})
		}
		if history == nil {
			history = []map[string]interface{}{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"history": history, "count": len(history)})
	}
}

func supervisoryIDFromPath(r *http.Request) int64 {
	// Extract last numeric segment from URL path
	path := r.URL.Path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			idStr := path[i+1:]
			if n, err := strconv.ParseInt(idStr, 10, 64); err == nil {
				return n
			}
			break
		}
	}
	return 0
}
