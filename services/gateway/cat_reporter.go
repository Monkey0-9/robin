package main

// ============================================================================
// Robin Trading Platform — CAT / MiFID II Transaction Reporting
// ============================================================================
// Implements Consolidated Audit Trail (CAT) and MiFID II RTS 22 transaction
// reporting:
//
//   CAT (FINRA/SEC): Complete order lifecycle reporting with FDID, RFID,
//   ManTA fields. Batches completed events for regulatory submission.
//
//   MiFID II RTS 22: Transaction reporting for EU-regulated instruments.
//   Fields: algorithm ID, decision maker, trading venue (MIC), buyer/seller.
//
// Endpoints:
//   POST /api/compliance/cat/record        — record a CAT event
//   GET  /api/compliance/cat/export        — export pending CAT batch (JSON)
//   POST /api/compliance/cat/submit        — mark batch as submitted
//   GET  /api/compliance/mifid/export      — export MiFID II records
// ============================================================================

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// CATEventType represents CAT order lifecycle event types.
type CATEventType string

const (
	CATEventNew    CATEventType = "NEW"
	CATEventRoute  CATEventType = "ROUTE"
	CATEventFill   CATEventType = "FILL"
	CATEventCancel CATEventType = "CANCEL"
	CATEventModify CATEventType = "MODIFY"
)

// defaultFDID is the Firm Designated ID for Robin. In production, register with FINRA CAT.
const defaultFDID = "ROBIN-FIRM-001"
const defaultRFID = "ROBIN-REG-001"

// ============================================================================
// CAT Reporting
// ============================================================================

// recordCATEvent records an order lifecycle event in the cat_reports table.
func recordCATEvent(db *sql.DB, orderID int64, eventType CATEventType, exchange string) error {
	if db == nil {
		return nil
	}
	now := time.Now().UnixNano()
	_, err := db.Exec(`
		INSERT INTO cat_reports
		  (order_id, cat_event_type, event_timestamp_ns, fdid, rfid,
		   reporting_party, exchange, status)
		VALUES ($1, $2, $3, $4, $5, 'ROBIN', $6, 'PENDING')`,
		orderID, string(eventType), now, defaultFDID, defaultRFID, exchange,
	)
	return err
}

// recordMiFIDReport records a MiFID II RTS 22 transaction report.
func recordMiFIDReport(db *sql.DB, orderID int64, algoID, decisionMaker, tradingVenue string) error {
	if db == nil {
		return nil
	}
	transRef := fmt.Sprintf("ROBIN-%d-%d", orderID, time.Now().UnixNano())
	_, err := db.Exec(`
		INSERT INTO mifid_reports
		  (order_id, transaction_ref, trading_venue, algo_id,
		   decision_maker_id, client_id_scheme, report_status)
		VALUES ($1, $2, $3, $4, $5, 'CONCAT', 'PENDING')`,
		orderID, transRef, tradingVenue, algoID, decisionMaker,
	)
	return err
}

// ============================================================================
// HTTP Handlers
// ============================================================================

// handleCATExport handles GET /api/compliance/cat/export.
// Returns all pending CAT events as a JSON batch ready for submission.
func handleCATExport(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			http.Error(w, `{"error":"database not available"}`, http.StatusInternalServerError)
			return
		}

		rows, err := db.Query(`
			SELECT c.id, c.order_id, c.cat_event_type, c.event_timestamp_ns,
			       c.fdid, c.rfid, c.exchange, c.status,
			       o.instrument_id, o.price, o.qty, o.side
			FROM cat_reports c
			JOIN orders o ON c.order_id = o.id
			WHERE c.status='PENDING'
			ORDER BY c.event_timestamp_ns`)
		if err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var events []map[string]interface{}
		for rows.Next() {
			var id, orderID, tsNs, instID, price, qty int64
			var eventType, fdid, rfid, exchange, status string
			var side int
			if err := rows.Scan(&id, &orderID, &eventType, &tsNs,
				&fdid, &rfid, &exchange, &status,
				&instID, &price, &qty, &side); err != nil {
				continue
			}
			sideStr := "BUY"
			if side == 1 {
				sideStr = "SELL"
			}
			events = append(events, map[string]interface{}{
				"cat_event_id":       id,
				"order_id":           orderID,
				"event_type":         eventType,
				"event_timestamp_ns": tsNs,
				"fdid":               fdid,
				"rfid":               rfid,
				"exchange":           exchange,
				"instrument_id":      instID,
				"price_units":        price,
				"qty_units":          qty,
				"side":               sideStr,
				"reporting_party":    "ROBIN",
			})
		}
		if events == nil {
			events = []map[string]interface{}{}
		}

		batchID := fmt.Sprintf("CAT-BATCH-%d", time.Now().UnixNano())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"batch_id":   batchID,
			"events":     events,
			"count":      len(events),
			"export_time": time.Now().UTC().Format(time.RFC3339),
			"format":     "CAT-JSON-v1",
		})
	}
}

// handleCATSubmit handles POST /api/compliance/cat/submit.
// Marks a batch of CAT events as submitted to the CAT system.
func handleCATSubmit(db *sql.DB, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			BatchID  string  `json:"batch_id"`
			EventIDs []int64 `json:"event_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}

		if db == nil || len(body.EventIDs) == 0 {
			http.Error(w, `{"error":"event_ids required"}`, http.StatusBadRequest)
			return
		}

		now := time.Now().UnixNano()
		submittedCount := 0
		for _, id := range body.EventIDs {
			result, err := db.Exec(`
				UPDATE cat_reports SET status='SUBMITTED', batch_id=$1, submitted_at_ns=$2
				WHERE id=$3`, body.BatchID, now, id)
			if err == nil {
				if n, _ := result.RowsAffected(); n > 0 {
					submittedCount++
				}
			}
		}

		logger.Info("CAT batch submitted",
			"batch_id", body.BatchID,
			"events_submitted", submittedCount,
		)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":            "submitted",
			"batch_id":          body.BatchID,
			"events_submitted":  submittedCount,
			"submitted_at_ns":   now,
		})
	}
}

// handleMiFIDExport handles GET /api/compliance/mifid/export.
// Returns pending MiFID II RTS 22 transaction reports.
func handleMiFIDExport(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			http.Error(w, `{"error":"database not available"}`, http.StatusInternalServerError)
			return
		}

		rows, err := db.Query(`
			SELECT m.id, m.order_id, m.transaction_ref, m.trading_venue,
			       m.algo_id, m.decision_maker_id, m.report_status,
			       o.price, o.qty, o.side, o.created_at_ns
			FROM mifid_reports m
			JOIN orders o ON m.order_id = o.id
			WHERE m.report_status='PENDING'
			ORDER BY o.created_at_ns`)
		if err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var reports []map[string]interface{}
		for rows.Next() {
			var id, orderID, price, qty, createdAt int64
			var transRef, venue, algoID, decisionMaker, status string
			var side int
			if err := rows.Scan(&id, &orderID, &transRef, &venue,
				&algoID, &decisionMaker, &status,
				&price, &qty, &side, &createdAt); err != nil {
				continue
			}
			sideStr := "BUY"
			if side == 1 {
				sideStr = "SELL"
			}
			reports = append(reports, map[string]interface{}{
				"report_id":        id,
				"order_id":         orderID,
				"transaction_ref":  transRef,
				"trading_venue":    venue,
				"algo_id":          algoID,
				"decision_maker":   decisionMaker,
				"side":             sideStr,
				"price_units":      price,
				"qty_units":        qty,
				"transaction_time": createdAt,
				"status":           status,
			})
		}
		if reports == nil {
			reports = []map[string]interface{}{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"reports":     reports,
			"count":       len(reports),
			"format":      "MiFID-II-RTS22-v1",
			"export_time": time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// handleCATStatus handles GET /api/compliance/cat/status.
func handleCATStatus(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var pending, submitted, accepted, rejected int64
		if db != nil {
			if err := db.QueryRow(`SELECT COUNT(*) FROM cat_reports WHERE status='PENDING'`).Scan(&pending); err != nil {
				pending = -1
			}
			if err := db.QueryRow(`SELECT COUNT(*) FROM cat_reports WHERE status='SUBMITTED'`).Scan(&submitted); err != nil {
				submitted = -1
			}
			if err := db.QueryRow(`SELECT COUNT(*) FROM cat_reports WHERE status='ACCEPTED'`).Scan(&accepted); err != nil {
				accepted = -1
			}
			if err := db.QueryRow(`SELECT COUNT(*) FROM cat_reports WHERE status='REJECTED'`).Scan(&rejected); err != nil {
				rejected = -1
			}
		}

		var mifidPending, mifidSubmitted int64
		if db != nil {
			if err := db.QueryRow(`SELECT COUNT(*) FROM mifid_reports WHERE report_status='PENDING'`).Scan(&mifidPending); err != nil {
				mifidPending = -1
			}
			if err := db.QueryRow(`SELECT COUNT(*) FROM mifid_reports WHERE report_status='SUBMITTED'`).Scan(&mifidSubmitted); err != nil {
				mifidSubmitted = -1
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"cat": map[string]int64{
				"pending": pending, "submitted": submitted,
				"accepted": accepted, "rejected": rejected,
			},
			"mifid": map[string]int64{
				"pending": mifidPending, "submitted": mifidSubmitted,
			},
		})
	}
}

var _ = recordCATEvent
var _ = recordMiFIDReport
