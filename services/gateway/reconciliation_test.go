package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// ============================================================================
// Order State Machine Reconciliation Tests (Phase 3.5)
// ============================================================================

// newTestReconcileDB creates an in-memory SQLite DB with the full schema and
// registers the default symbol map entries the reconciliation path needs.
func newTestReconcileDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}

	schema, err := os.ReadFile("../../schema_sqlite.sql")
	if err != nil {
		schema, err = os.ReadFile("schema_sqlite.sql")
	}
	if err != nil {
		t.Fatalf("failed to read schema: %v", err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatalf("failed to exec schema: %v", err)
	}
	return db
}

func TestReconcileOrderState_RehydratesMissingOrders(t *testing.T) {
	db := newTestReconcileDB(t)
	defer db.Close()

	// Register the symbol map so reconciliation can resolve instrument ids.
	registerSymbol("BTC/USD", 1)

	// Insert a WORKING order that is NOT in the in-memory state machine.
	_, err := db.Exec(`
		INSERT INTO orders (cl_order_id, instrument_id, price, qty, side, status, account_id, client_id, strategy_id, created_at_ns, updated_at_ns)
		VALUES ('ORD-MISSING', 1, 64500000000, 100000000, 0, 'WORKING', 1, 1, 1, 1, 1)`)
	if err != nil {
		t.Fatalf("failed to insert order: %v", err)
	}

	sm := NewOrderStateMachine(nil)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	report, err := ReconcileOrderState(db, sm, logger)
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	if report.DbOpenOrders != 1 {
		t.Errorf("expected 1 open DB order, got %d", report.DbOpenOrders)
	}
	if len(report.Rehydrated) != 1 {
		t.Errorf("expected 1 rehydrated order, got %d: %v", len(report.Rehydrated), report.Rehydrated)
	}

	// The order must now be queryable from the state machine.
	o, ok := sm.GetOrder("ORD-MISSING")
	if !ok {
		t.Fatal("expected rehydrated order in state machine")
	}
	if o.Symbol != "BTC/USD" {
		t.Errorf("expected symbol BTC/USD, got %s", o.Symbol)
	}
	if o.State != OrderStateWorking {
		t.Errorf("expected WORKING state, got %s", o.State)
	}
	if o.Qty != 1.0 {
		t.Errorf("expected qty 1.0, got %f", o.Qty)
	}
}

func TestReconcileOrderState_SkipsTerminalAndMatched(t *testing.T) {
	db := newTestReconcileDB(t)
	defer db.Close()
	registerSymbol("BTC/USD", 1)

	// Terminal order — must NOT be rehydrated.
	_, err := db.Exec(`
		INSERT INTO orders (cl_order_id, instrument_id, price, qty, side, status, account_id, client_id, strategy_id, created_at_ns, updated_at_ns)
		VALUES ('ORD-FILLED', 1, 64500000000, 100000000, 0, 'FILLED', 1, 1, 1, 1, 1)`)
	if err != nil {
		t.Fatalf("failed to insert order: %v", err)
	}

	sm := NewOrderStateMachine(nil)
	// Register a matched order (present in both views).
	matched := &ManagedOrder{ClOrdID: "ORD-OPEN", Symbol: "BTC/USD", Qty: 1.0, LeavesQty: 1.0}
	if err := sm.Register(matched); err != nil {
		t.Fatalf("failed to register matched order: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO orders (cl_order_id, instrument_id, price, qty, side, status, account_id, client_id, strategy_id, created_at_ns, updated_at_ns)
		VALUES ('ORD-OPEN', 1, 64500000000, 100000000, 0, 'WORKING', 1, 1, 1, 1, 1)`)
	if err != nil {
		t.Fatalf("failed to insert order: %v", err)
	}

	report, err := ReconcileOrderState(db, sm, nil)
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	if len(report.Rehydrated) != 0 {
		t.Errorf("expected 0 rehydrated, got %v", report.Rehydrated)
	}
	if report.Matched != 1 {
		t.Errorf("expected 1 matched, got %d", report.Matched)
	}
	if _, ok := sm.GetOrder("ORD-FILLED"); ok {
		t.Error("terminal order should not be rehydrated into the state machine")
	}
}

func TestReconcileOrderState_DetectsOrphanedInMemory(t *testing.T) {
	db := newTestReconcileDB(t)
	defer db.Close()
	registerSymbol("BTC/USD", 1)

	sm := NewOrderStateMachine(nil)
	// In-memory order with NO corresponding DB row → orphaned.
	orphan := &ManagedOrder{ClOrdID: "ORD-ORPHAN", Symbol: "BTC/USD", Qty: 1.0, LeavesQty: 1.0}
	if err := sm.Register(orphan); err != nil {
		t.Fatalf("failed to register orphan: %v", err)
	}

	report, err := ReconcileOrderState(db, sm, nil)
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	if len(report.OrphanedInMemory) != 1 || report.OrphanedInMemory[0] != "ORD-ORPHAN" {
		t.Errorf("expected orphaned ORD-ORPHAN, got %v", report.OrphanedInMemory)
	}
}

func TestReconcileOrderState_NoDB(t *testing.T) {
	sm := NewOrderStateMachine(nil)
	report, err := ReconcileOrderState(nil, sm, nil)
	if err != nil {
		t.Fatalf("reconcile with nil db should not error: %v", err)
	}
	if len(report.Errors) == 0 {
		t.Error("expected an error note when no DB is configured")
	}
}

func TestDbStatusToLifecycleState(t *testing.T) {
	cases := []struct {
		in   string
		want OrderLifecycleState
	}{
		{"NEW", OrderStateNew},
		{"PENDING", OrderStatePending},
		{"WORKING", OrderStateWorking},
		{"PARTIAL", OrderStatePartialFill},
		{"FILLED", OrderStateFilled},
		{"CANCEL_PENDING", OrderStateCancelPending},
		{"CANCELED", OrderStateCanceled},
		{"REJECTED", OrderStateRejected},
		{"EXPIRED", OrderStateExpired},
		{"BOGUS", OrderStateNew},
	}
	for _, tc := range cases {
		if got := dbStatusToLifecycleState(tc.in); got != tc.want {
			t.Errorf("dbStatusToLifecycleState(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestSymbolNameForInstrument(t *testing.T) {
	registerSymbol("ETH/USD", 2)
	if got := symbolNameForInstrument(2); got != "ETH/USD" {
		t.Errorf("expected ETH/USD, got %s", got)
	}
	if got := symbolNameForInstrument(9999); got != "UNKNOWN" {
		t.Errorf("expected UNKNOWN, got %s", got)
	}
}

// silence unused import guard (fmt is used in helper context)
var _ = fmt.Sprintf
