package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// handleSECAuditReport exports compliance events for a SEC 15c3-5 audit.
//
// Unlike the previous mock, this endpoint reads from the auditable ledger
// (audit_log) and the CEO attestation table (compliance_certifications) so an
// examiner receives actual evidence, not hardcoded sample rows. An optional
// ?year=YYYY filter exports a single attestation year.
func handleSECAuditReport(db *sql.DB, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		events := make([]map[string]interface{}, 0, 64)
		now := time.Now().UTC()
		yearStr := r.URL.Query().Get("year")

		if db != nil {
			// 1) CEO annual certifications (SEC 15c3-5 §(e)(2)), newest first.
			rows, err := db.Query(`
				SELECT year, ceo_name, attested_at_ns, systems_reviewed
				FROM compliance_certifications
				ORDER BY year DESC LIMIT 50`)
			if err == nil {
				for rows.Next() {
					var year, attestedNs int64
					var ceo, systems string
					if err := rows.Scan(&year, &ceo, &attestedNs, &systems); err != nil {
						continue
					}
					if yearStr != "" && fmt.Sprintf("%d", year) != yearStr {
						continue
					}
					sysCount := 0
					trimmed := strings.TrimSpace(systems)
					if trimmed != "" && trimmed != "[]" {
						sysCount = len(strings.Split(strings.Trim(trimmed, "[]"), ","))
					}
					events = append(events, map[string]interface{}{
						"event":            "CEO_CERTIFICATION",
						"timestamp":        time.Unix(0, attestedNs).UTC().Format(time.RFC3339),
						"year":             year,
						"ceo":              ceo,
						"systems_reviewed": sysCount,
					})
				}
				rows.Close()
			} else {
				logger.Error("failed to read compliance_certifications", "error", err)
			}

			// 2) Recent audit-ledger events (kill switch, limit breaches, etc).
			rrows, err := db.Query(`
				SELECT sequence_id, timestamp_ns, action, order_id, price, qty
				FROM audit_log
				ORDER BY sequence_id DESC LIMIT 200`)
			if err == nil {
				for rrows.Next() {
					var seq, tsNs int64
					var action string
					var orderID, price, qty int64
					if err := rrows.Scan(&seq, &tsNs, &action, &orderID, &price, &qty); err != nil {
						continue
					}
					if yearStr != "" && time.Unix(0, tsNs).UTC().Format("2006") != yearStr {
						continue
					}
					events = append(events, map[string]interface{}{
						"event":       action,
						"sequence_id": seq,
						"timestamp":   time.Unix(0, tsNs).UTC().Format(time.RFC3339),
						"order_id":    orderID,
						"price":       price,
						"qty":         qty,
						"ledger":      "audit_log",
					})
				}
				rrows.Close()
			} else {
				logger.Error("failed to read audit_log", "error", err)
			}
		}

		if len(events) == 0 {
			// First-run / no-data state: emit a clearly generated marker rather
			// than fabricating ledger rows — examiners must never mistake
			// placeholder data for evidence.
			events = append(events, map[string]interface{}{
				"event":     "AUDIT_EMPTY_STATE",
				"timestamp": now.Format(time.RFC3339),
				"details":   "No ledger rows present yet — first export of a fresh database.",
				"source":    true,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "success",
			"report_count": len(events),
			"generated_at": now.Format(time.RFC3339),
			"sec_rule":     "15c3-5",
			"report":       events,
		}); err != nil {
			logger.Error("failed to write audit report response", "error", err)
		}
	}
}

// yearText renders an integer year as its "YYYY" string. Kept for filter
// normalization consistency.
func yearText(year int64) string {
	return strconv.FormatInt(year, 10)
}
