package main

// ============================================================================
// Order State Machine Reconciliation (Phase 3.5)
// ============================================================================
// The gateway keeps two views of order life-cycle state:
//   1. The in-memory OrderStateMachine (globalOrderSM) — fast path for the
//      blotter, WebSocket order updates, and OMS queries.
//   2. The SQLite orders table — durable, restart-surviving source of truth.
//
// These can diverge: a crash between engine-ack and state-machine transition,
// a gateway restart (in-memory map lost), or a manual DB edit. ReconcileOrderState
// rehydrates the in-memory machine from the durable table for any non-terminal
// order missing in memory, flags orphaned in-memory orders that no longer exist
// as open in the DB, and emits a structured report so operators can see exactly
// what was repaired.
// ============================================================================

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

// dbStatusToLifecycleState maps the SQLite orders.status values (NEW, PARTIAL,
// FILLED, CANCELED, REJECTED plus the gateway's WORKING / PENDING) onto the
// in-memory state machine vocabulary.
func dbStatusToLifecycleState(dbStatus string) OrderLifecycleState {
	switch dbStatus {
	case "NEW":
		return OrderStateNew
	case "PENDING":
		return OrderStatePending
	case "WORKING":
		return OrderStateWorking
	case "PARTIAL":
		return OrderStatePartialFill
	case "FILLED":
		return OrderStateFilled
	case "CANCEL_PENDING":
		return OrderStateCancelPending
	case "CANCELED":
		return OrderStateCanceled
	case "REJECTED":
		return OrderStateRejected
	case "EXPIRED":
		return OrderStateExpired
	default:
		return OrderStateNew
	}
}

// ReconciliationReport summarises one reconciliation pass.
type ReconciliationReport struct {
	ReconciledAtNs   int64    `json:"reconciled_at_ns"`
	DbOpenOrders     int      `json:"db_open_orders"`
	InMemoryOrders   int      `json:"in_memory_orders"`
	Rehydrated       []string `json:"rehydrated"`
	OrphanedInMemory []string `json:"orphaned_in_memory"`
	Matched          int      `json:"matched"`
	Errors           []string `json:"errors"`
}

// lastReconcileAtNs records when the most recent successful reconciliation ran.
var lastReconcileAtNs atomic.Int64

// ReconcileOrderState rehydrates the in-memory state machine from the durable
// orders table for all non-terminal orders and reports divergence. It is safe
// to call concurrently with live order traffic: the state machine is guarded
// by its own mutex, and missing orders are simply (re)registered and advanced
// to the state the DB says they are in.
func ReconcileOrderState(db *sql.DB, sm *OrderStateMachine, logger *slog.Logger) (*ReconciliationReport, error) {
	report := &ReconciliationReport{
		ReconciledAtNs: time.Now().UnixNano(),
	}

	if db == nil {
		report.Errors = append(report.Errors, "no database configured, reconciliation skipped")
		return report, nil
	}
	if sm == nil {
		report.Errors = append(report.Errors, "no in-memory state machine, reconciliation skipped")
		return report, nil
	}

	// Non-terminal orders are the only ones that can exist in one view and not
	// the other. Terminal orders (FILLED, CANCELED, REJECTED, EXPIRED) are
	// immutable and do not need to be present in the hot-path machine.
	rows, err := db.Query(`
		SELECT cl_order_id, instrument_id, price, qty, side, status, order_type,
		       account_id, client_id, strategy_id, exchange
		FROM orders
		WHERE status NOT IN ('FILLED', 'CANCELED', 'REJECTED', 'EXPIRED')`)
	if err != nil {
		return report, err
	}
	defer rows.Close()

	// Map of open orders known to the DB.
	dbOpen := make(map[string]struct{})
	for rows.Next() {
		var clOrdID, status, orderType, exchange string
		var instrumentID, price, qty, side, accountID, clientID, strategyID int64
		if err := rows.Scan(&clOrdID, &instrumentID, &price, &qty, &side,
			&status, &orderType, &accountID, &clientID, &strategyID, &exchange); err != nil {
			report.Errors = append(report.Errors, "scan error: "+err.Error())
			continue
		}
		report.DbOpenOrders++
		dbOpen[clOrdID] = struct{}{}

		// Already tracked in memory — nothing to do.
		if _, ok := sm.GetOrder(clOrdID); ok {
			report.Matched++
			continue
		}

		// Rehydrate the order into the state machine in the DB-reported state.
		sideStr := "BUY"
		if side == 1 {
			sideStr = "SELL"
		}
		if orderType == "" {
			orderType = "LIMIT"
		}
		managed := &ManagedOrder{
			ClOrdID:        clOrdID,
			Symbol:         symbolNameForInstrument(instrumentID),
			Side:           sideStr,
			OrderType:      orderType,
			Qty:            float64(qty) / 100000000.0,
			Price:          float64(price) / 100000000.0,
			LeavesQty:      float64(qty) / 100000000.0,
			State:          dbStatusToLifecycleState(status),
			CreatedAtNs:    time.Now().UnixNano(),
			UpdatedAtNs:    time.Now().UnixNano(),
			RoutedExchange: exchange,
		}
		if err := sm.Restore(managed); err != nil {
			report.Errors = append(report.Errors, "register "+clOrdID+": "+err.Error())
			continue
		}
		report.Rehydrated = append(report.Rehydrated, clOrdID)
		if logger != nil {
			logger.Info("order rehydrated into state machine from DB",
				"cl_ord_id", clOrdID, "status", status)
		}
	}
	if err := rows.Err(); err != nil {
		return report, err
	}

	// Detect in-memory orders that no longer exist as open orders in the DB
	// (e.g. the row was manually terminal-ed, or the DB is authoritative).
	for _, o := range sm.GetAllOrders() {
		if _, ok := dbOpen[o.ClOrdID]; !ok {
			report.OrphanedInMemory = append(report.OrphanedInMemory, o.ClOrdID)
		}
	}

	lastReconcileAtNs.Store(report.ReconciledAtNs)
	return report, nil
}

// symbolNameForInstrument is the inverse of getInstrumentID: it resolves an
// instrument id back to its symbol for display during reconciliation.
func symbolNameForInstrument(id int64) string {
	for sym, sid := range globalSymbolMapSnapshot() {
		if int64(sid) == id {
			return sym
		}
	}
	return "UNKNOWN"
}

// globalSymbolMapSnapshot returns a copy of the current symbol map.
func globalSymbolMapSnapshot() map[string]uint64 {
	symbolMapMu.RLock()
	defer symbolMapMu.RUnlock()
	out := make(map[string]uint64, len(symbolMap))
	for k, v := range symbolMap {
		out[k] = v
	}
	return out
}

// handleOrderReconcile exposes GET /api/orders/reconcile — an operator-triggered
// reconciliation pass (admin only) that reports what was repaired.
func handleOrderReconcile(db *sql.DB, sm *OrderStateMachine, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report, err := ReconcileOrderState(db, sm, logger)
		if err != nil {
			http.Error(w, `{"error":"reconciliation failed: `+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(report)
	}
}
