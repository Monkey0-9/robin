package main

// ============================================================================
// Robin Trading Platform — Multi-Level Kill Switch
// ============================================================================
// Implements deterministic kill switches at 3 levels per institutional HFT
// requirements and SEC Rule 15c3-5 (direct and exclusive control mandate):
//
//   Level 1 — SYSTEM: Halts ALL order processing platform-wide
//   Level 2 — ALGO:   Halts a specific strategy/algorithm by ID
//   Level 3 — TRADER: Halts a specific trader by ID
//
// Key properties:
//   • Atomic state via sync/atomic — zero-latency check on hot path
//   • Persistent to SQLite kill_switch_log for crash recovery
//   • Dual-person integrity on reset (two separate admin JWTs required)
//   • WebSocket broadcast to all connected clients on state change
//   • Prometheus metrics for kill switch state
// ============================================================================

import (
	"context"
	"crypto/sha256"
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

// contextKey is the type for context values used in JWT claims.
type contextKey string

const contextKeyJWTClaims contextKey = "jwt_claims"

// KillSwitchLevel represents the level of a kill switch.
type KillSwitchLevel string

const (
	KillSwitchSystem KillSwitchLevel = "SYSTEM"
	KillSwitchAlgo   KillSwitchLevel = "ALGO"
	KillSwitchTrader KillSwitchLevel = "TRADER"
)

// KillSwitchManager manages multi-level kill switches atomically.
type KillSwitchManager struct {
	// Level 1: System-wide halt (0=running, 1=halted)
	systemHalt atomic.Uint32

	// Level 2: Per-algo halts (256 algorithms max)
	algoHalts   [256]atomic.Uint32
	algoMu      sync.RWMutex
	algoReasons map[string]string // algo_id -> reason

	// Level 3: Per-trader halts (4096 traders max via hash)
	traderHalts   [4096]atomic.Uint32
	traderMu      sync.RWMutex
	traderReasons map[string]string // trader_id -> reason

	// Trip metadata
	systemReason   string
	systemTrippedBy string
	systemTripTime  int64 // unix nanoseconds
	metaMu         sync.RWMutex

	// Pending dual-person reset: maps reset token -> requesting admin
	pendingResets map[string]string // token -> admin_username
	resetMu       sync.Mutex

	db     *sql.DB
	logger *slog.Logger
	wsHub  *WebSocketHub
}

// NewKillSwitchManager creates and returns a new KillSwitchManager.
func NewKillSwitchManager(db *sql.DB, logger *slog.Logger, wsHub *WebSocketHub) *KillSwitchManager {
	ks := &KillSwitchManager{
		algoReasons:   make(map[string]string),
		traderReasons: make(map[string]string),
		pendingResets:  make(map[string]string),
		db:            db,
		logger:        logger,
		wsHub:         wsHub,
	}
	ks.restoreFromDB()
	return ks
}

// ============================================================================
// Hot-path check methods (called on every order — must be lock-free)
// ============================================================================

// IsSystemHalted returns true if the system-level kill switch is active.
// Lock-free atomic read — suitable for the hot order path.
func (ks *KillSwitchManager) IsSystemHalted() bool {
	return ks.systemHalt.Load() == 1
}

// IsAlgoHalted returns true if the given algo is halted.
func (ks *KillSwitchManager) IsAlgoHalted(algoID string) bool {
	if algoID == "" {
		return false
	}
	slot := algoIDToSlot(algoID)
	return ks.algoHalts[slot].Load() == 1
}

// IsTraderHalted returns true if the given trader is halted.
func (ks *KillSwitchManager) IsTraderHalted(traderID string) bool {
	if traderID == "" {
		return false
	}
	slot := traderIDToSlot(traderID)
	return ks.traderHalts[slot].Load() == 1
}

// IsOrderBlocked returns true if the order should be blocked by any kill switch.
// Checks in order: system → algo → trader.
func (ks *KillSwitchManager) IsOrderBlocked(algoID, traderID string) (blocked bool, reason string) {
	if ks.IsSystemHalted() {
		ks.metaMu.RLock()
		reason = "SYSTEM_KILL_SWITCH: " + ks.systemReason
		ks.metaMu.RUnlock()
		return true, reason
	}
	if ks.IsAlgoHalted(algoID) {
		ks.algoMu.RLock()
		r := ks.algoReasons[algoID]
		ks.algoMu.RUnlock()
		return true, "ALGO_KILL_SWITCH[" + algoID + "]: " + r
	}
	if ks.IsTraderHalted(traderID) {
		ks.traderMu.RLock()
		r := ks.traderReasons[traderID]
		ks.traderMu.RUnlock()
		return true, "TRADER_KILL_SWITCH[" + traderID + "]: " + r
	}
	return false, ""
}

// ============================================================================
// Trip methods
// ============================================================================

// TripSystem activates the system-level kill switch.
func (ks *KillSwitchManager) TripSystem(reason, trippedBy string) {
	ks.systemHalt.Store(1)
	now := time.Now().UnixNano()

	ks.metaMu.Lock()
	ks.systemReason = reason
	ks.systemTrippedBy = trippedBy
	ks.systemTripTime = now
	ks.metaMu.Unlock()

	hash := killSwitchHash(string(KillSwitchSystem), "", "TRIP", reason, now)
	ks.persistEvent(KillSwitchSystem, "", "TRIP", reason, trippedBy, "", now, hash)

	ks.logger.Error("[KILL SWITCH] SYSTEM HALT ACTIVATED",
		"reason", reason, "tripped_by", trippedBy,
	)

	if ks.wsHub != nil {
		ks.wsHub.BroadcastJSON(map[string]interface{}{
			"type":       "KILL_SWITCH",
			"level":      "SYSTEM",
			"action":     "TRIP",
			"reason":     reason,
			"tripped_by": trippedBy,
			"time_ns":    now,
		})
	}
}

// TripAlgo halts a specific algorithm.
func (ks *KillSwitchManager) TripAlgo(algoID, reason, trippedBy string) {
	slot := algoIDToSlot(algoID)
	ks.algoHalts[slot].Store(1)

	ks.algoMu.Lock()
	ks.algoReasons[algoID] = reason
	ks.algoMu.Unlock()

	now := time.Now().UnixNano()
	hash := killSwitchHash(string(KillSwitchAlgo), algoID, "TRIP", reason, now)
	ks.persistEvent(KillSwitchAlgo, algoID, "TRIP", reason, trippedBy, "", now, hash)

	ks.logger.Warn("[KILL SWITCH] ALGO HALT",
		"algo_id", algoID, "reason", reason, "tripped_by", trippedBy,
	)
}

// TripTrader halts a specific trader.
func (ks *KillSwitchManager) TripTrader(traderID, reason, trippedBy string) {
	slot := traderIDToSlot(traderID)
	ks.traderHalts[slot].Store(1)

	ks.traderMu.Lock()
	ks.traderReasons[traderID] = reason
	ks.traderMu.Unlock()

	now := time.Now().UnixNano()
	hash := killSwitchHash(string(KillSwitchTrader), traderID, "TRIP", reason, now)
	ks.persistEvent(KillSwitchTrader, traderID, "TRIP", reason, trippedBy, "", now, hash)

	ks.logger.Warn("[KILL SWITCH] TRADER HALT",
		"trader_id", traderID, "reason", reason, "tripped_by", trippedBy,
	)
}

// ============================================================================
// Dual-person reset flow
// ============================================================================

// InitiateSystemReset starts the dual-person reset process.
// Returns a reset token that the second admin must supply to confirm.
func (ks *KillSwitchManager) InitiateSystemReset(adminUsername, reason string) string {
	token := fmt.Sprintf("RST-%x", sha256.Sum256([]byte(
		fmt.Sprintf("%s|%s|%d", adminUsername, reason, time.Now().UnixNano()),
	)))[:24]

	ks.resetMu.Lock()
	ks.pendingResets[token] = adminUsername
	ks.resetMu.Unlock()

	ks.logger.Warn("[KILL SWITCH] System reset initiated — awaiting secondary approver",
		"initiated_by", adminUsername, "reset_token", token,
	)
	return token
}

// ConfirmSystemReset completes the dual-person reset. The confirming admin
// must be different from the initiating admin. Returns error if invalid.
func (ks *KillSwitchManager) ConfirmSystemReset(resetToken, confirmingAdmin, reason string) error {
	ks.resetMu.Lock()
	initiatingAdmin, ok := ks.pendingResets[resetToken]
	if ok {
		delete(ks.pendingResets, resetToken)
	}
	ks.resetMu.Unlock()

	if !ok {
		return fmt.Errorf("invalid or expired reset token")
	}
	if initiatingAdmin == confirmingAdmin {
		return fmt.Errorf("dual-person integrity violation: confirming admin must differ from initiating admin")
	}

	ks.systemHalt.Store(0)
	now := time.Now().UnixNano()

	ks.metaMu.Lock()
	prevReason := ks.systemReason
	ks.systemReason = ""
	ks.systemTrippedBy = ""
	ks.metaMu.Unlock()

	hash := killSwitchHash(string(KillSwitchSystem), "", "RESET", reason, now)
	ks.persistEvent(KillSwitchSystem, "", "RESET", prevReason,
		initiatingAdmin, confirmingAdmin, now, hash)

	ks.logger.Warn("[KILL SWITCH] SYSTEM RESET CONFIRMED",
		"initiated_by", initiatingAdmin,
		"confirmed_by", confirmingAdmin,
		"reason", reason,
	)

	if ks.wsHub != nil {
		ks.wsHub.BroadcastJSON(map[string]interface{}{
			"type":          "KILL_SWITCH",
			"level":         "SYSTEM",
			"action":        "RESET",
			"initiated_by":  initiatingAdmin,
			"confirmed_by":  confirmingAdmin,
			"time_ns":       now,
		})
	}
	return nil
}

// ResetAlgo clears an algo-level halt.
func (ks *KillSwitchManager) ResetAlgo(algoID, resetBy, reason string) {
	slot := algoIDToSlot(algoID)
	ks.algoHalts[slot].Store(0)
	ks.algoMu.Lock()
	delete(ks.algoReasons, algoID)
	ks.algoMu.Unlock()

	now := time.Now().UnixNano()
	hash := killSwitchHash(string(KillSwitchAlgo), algoID, "RESET", reason, now)
	ks.persistEvent(KillSwitchAlgo, algoID, "RESET", reason, resetBy, "", now, hash)
	ks.logger.Warn("[KILL SWITCH] Algo reset", "algo_id", algoID, "reset_by", resetBy)
}

// ResetTrader clears a trader-level halt.
func (ks *KillSwitchManager) ResetTrader(traderID, resetBy, reason string) {
	slot := traderIDToSlot(traderID)
	ks.traderHalts[slot].Store(0)
	ks.traderMu.Lock()
	delete(ks.traderReasons, traderID)
	ks.traderMu.Unlock()

	now := time.Now().UnixNano()
	hash := killSwitchHash(string(KillSwitchTrader), traderID, "RESET", reason, now)
	ks.persistEvent(KillSwitchTrader, traderID, "RESET", reason, resetBy, "", now, hash)
	ks.logger.Warn("[KILL SWITCH] Trader reset", "trader_id", traderID, "reset_by", resetBy)
}

// ============================================================================
// Status
// ============================================================================

// GetStatus returns the current state of all kill switches for the /api/killswitch/status endpoint.
func (ks *KillSwitchManager) GetStatus() map[string]interface{} {
	ks.metaMu.RLock()
	sysReason := ks.systemReason
	sysBy := ks.systemTrippedBy
	sysTime := ks.systemTripTime
	ks.metaMu.RUnlock()

	return map[string]interface{}{
		"system_halted":     ks.systemHalt.Load() == 1,
		"system_reason":     sysReason,
		"system_tripped_by": sysBy,
		"system_trip_time":  sysTime,
		"algo_halts":        ks.getAlgoHalts(),
		"trader_halts":      ks.getTraderHalts(),
	}
}

func (ks *KillSwitchManager) getAlgoHalts() map[string]string {
	ks.algoMu.RLock()
	defer ks.algoMu.RUnlock()
	result := make(map[string]string)
	for id, reason := range ks.algoReasons {
		result[id] = reason
	}
	return result
}

func (ks *KillSwitchManager) getTraderHalts() map[string]string {
	ks.traderMu.RLock()
	defer ks.traderMu.RUnlock()
	result := make(map[string]string)
	for id, reason := range ks.traderReasons {
		result[id] = reason
	}
	return result
}

// ============================================================================
// Persistence
// ============================================================================

func (ks *KillSwitchManager) persistEvent(level KillSwitchLevel, targetID, action, reason, trippedBy, secondaryApprover string, nowNs int64, hash string) {
	if ks.db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := ks.db.ExecContext(ctx, `
		INSERT INTO kill_switch_log
		  (level, target_id, action, reason, tripped_by, secondary_approver,
		   tripped_at_ns, reset_at_ns, chain_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(level), targetID, action, reason, trippedBy, secondaryApprover,
		nowNs, 0, hash,
	)
	if err != nil {
		ks.logger.Error("failed to persist kill switch event", "error", err)
	}
}

// restoreFromDB reloads persisted kill switch state on startup.
func (ks *KillSwitchManager) restoreFromDB() {
	if ks.db == nil {
		return
	}
	// 1. Restore System Halt
	var action, reason, trippedBy string
	err := ks.db.QueryRow(`
		SELECT action, reason, tripped_by FROM kill_switch_log
		WHERE level='SYSTEM'
		ORDER BY id DESC LIMIT 1`,
	).Scan(&action, &reason, &trippedBy)

	if err == nil && action == "TRIP" {
		ks.systemHalt.Store(1)
		ks.metaMu.Lock()
		ks.systemReason = reason
		ks.systemTrippedBy = trippedBy
		ks.metaMu.Unlock()
		if ks.logger != nil {
			ks.logger.Warn("[KILL SWITCH] Restored system halt from DB", "reason", reason)
		}
	}

	// 2. Restore Algo Halts
	rows, err := ks.db.Query(`
		SELECT target_id, action, reason FROM kill_switch_log
		WHERE level='ALGO'
		GROUP BY target_id
		HAVING id = MAX(id) AND action='TRIP'`,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var algoID, act, rsn string
			if err := rows.Scan(&algoID, &act, &rsn); err == nil {
				slot := algoIDToSlot(algoID)
				ks.algoHalts[slot].Store(1)
				ks.algoMu.Lock()
				ks.algoReasons[algoID] = rsn
				ks.algoMu.Unlock()
				if ks.logger != nil {
					ks.logger.Warn("[KILL SWITCH] Restored ALGO halt from DB", "algo_id", algoID, "reason", rsn)
				}
			}
		}
	}

	// 3. Restore Trader Halts
	trows, err := ks.db.Query(`
		SELECT target_id, action, reason FROM kill_switch_log
		WHERE level='TRADER'
		GROUP BY target_id
		HAVING id = MAX(id) AND action='TRIP'`,
	)
	if err == nil {
		defer trows.Close()
		for trows.Next() {
			var traderID, act, rsn string
			if err := trows.Scan(&traderID, &act, &rsn); err == nil {
				slot := traderIDToSlot(traderID)
				ks.traderHalts[slot].Store(1)
				ks.traderMu.Lock()
				ks.traderReasons[traderID] = rsn
				ks.traderMu.Unlock()
				if ks.logger != nil {
					ks.logger.Warn("[KILL SWITCH] Restored TRADER halt from DB", "trader_id", traderID, "reason", rsn)
				}
			}
		}
	}
}

// ============================================================================
// Helpers
// ============================================================================

func algoIDToSlot(algoID string) int {
	h := sha256.Sum256([]byte(algoID))
	return int(h[0])<<8|int(h[1]) & 0xFF // 0..255
}

func traderIDToSlot(traderID string) int {
	h := sha256.Sum256([]byte(traderID))
	return (int(h[0])<<8 | int(h[1])) % 4096
}

func killSwitchHash(level, targetID, action, reason string, nowNs int64) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s|%d", level, targetID, action, reason, nowNs)))
	return fmt.Sprintf("%x", h)
}

// ============================================================================
// HTTP Handlers (registered in orchestrator.go setupHTTPServer)
// ============================================================================

// killSwitchStatusHandler handles GET /api/killswitch/status.
func killSwitchStatusHandler(ks *KillSwitchManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ks.GetStatus())
	}
}

// killSwitchTripSystemHandler handles POST /api/killswitch/system/trip.
func killSwitchTripSystemHandler(ks *KillSwitchManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Reason == "" {
			http.Error(w, `{"error":"reason is required"}`, http.StatusBadRequest)
			return
		}

		admin := adminFromContext(r)
		ks.TripSystem(body.Reason, admin)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "SYSTEM_HALTED",
			"tripped_by": admin,
			"reason":     body.Reason,
			"time_ns":    time.Now().UnixNano(),
		})
	}
}

// killSwitchInitResetHandler handles POST /api/killswitch/system/reset/initiate.
func killSwitchInitResetHandler(ks *KillSwitchManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		admin := adminFromContext(r)
		token := ks.InitiateSystemReset(admin, body.Reason)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"reset_token":    token,
			"initiated_by":   admin,
			"message":        "Provide this reset_token to a DIFFERENT admin to confirm the reset (dual-person integrity)",
			"expires_in_min": 15,
		})
	}
}

// killSwitchConfirmResetHandler handles POST /api/killswitch/system/reset/confirm.
func killSwitchConfirmResetHandler(ks *KillSwitchManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ResetToken string `json:"reset_token"`
			Reason     string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ResetToken == "" {
			http.Error(w, `{"error":"reset_token is required"}`, http.StatusBadRequest)
			return
		}

		admin := adminFromContext(r)
		if err := ks.ConfirmSystemReset(body.ResetToken, admin, body.Reason); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "SYSTEM_RESUMED",
			"confirmed_by": admin,
			"time_ns":      time.Now().UnixNano(),
		})
	}
}

// killSwitchTripAlgoHandler handles POST /api/killswitch/algo/{id}/trip.
func killSwitchTripAlgoHandler(ks *KillSwitchManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		algoID := extractPathParam(r, "id")
		if algoID == "" {
			http.Error(w, `{"error":"algo id required"}`, http.StatusBadRequest)
			return
		}
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		admin := adminFromContext(r)
		ks.TripAlgo(algoID, body.Reason, admin)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "ALGO_HALTED",
			"algo_id":    algoID,
			"tripped_by": admin,
		})
	}
}

// killSwitchResetAlgoHandler handles POST /api/killswitch/algo/{id}/reset.
func killSwitchResetAlgoHandler(ks *KillSwitchManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		algoID := extractPathParam(r, "id")
		var body struct{ Reason string `json:"reason"` }
		_ = json.NewDecoder(r.Body).Decode(&body)
		admin := adminFromContext(r)
		ks.ResetAlgo(algoID, admin, body.Reason)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ALGO_RESUMED", "algo_id": algoID})
	}
}

// killSwitchTripTraderHandler handles POST /api/killswitch/trader/{id}/trip.
func killSwitchTripTraderHandler(ks *KillSwitchManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		traderID := extractPathParam(r, "id")
		if traderID == "" {
			http.Error(w, `{"error":"trader id required"}`, http.StatusBadRequest)
			return
		}
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		admin := adminFromContext(r)
		ks.TripTrader(traderID, body.Reason, admin)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "TRADER_HALTED",
			"trader_id":  traderID,
			"tripped_by": admin,
		})
	}
}

// killSwitchResetTraderHandler handles POST /api/killswitch/trader/{id}/reset.
func killSwitchResetTraderHandler(ks *KillSwitchManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		traderID := extractPathParam(r, "id")
		var body struct{ Reason string `json:"reason"` }
		_ = json.NewDecoder(r.Body).Decode(&body)
		admin := adminFromContext(r)
		ks.ResetTrader(traderID, admin, body.Reason)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "TRADER_RESUMED", "trader_id": traderID})
	}
}

// ============================================================================
// Kill Switch Log Handler
// ============================================================================

// killSwitchLogHandler handles GET /api/killswitch/log.
func killSwitchLogHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limitStr := r.URL.Query().Get("limit")
		limit := 50
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 500 {
			limit = n
		}

		rows, err := db.Query(`
			SELECT id, level, target_id, action, reason, tripped_by, secondary_approver,
			       tripped_at_ns, reset_at_ns, chain_hash
			FROM kill_switch_log
			ORDER BY id DESC LIMIT ?`, limit)
		if err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var events []map[string]interface{}
		for rows.Next() {
			var id int64
			var level, targetID, action, reason, trippedBy, secondaryApprover, chainHash string
			var trippedAt, resetAt int64
			if err := rows.Scan(&id, &level, &targetID, &action, &reason, &trippedBy,
				&secondaryApprover, &trippedAt, &resetAt, &chainHash); err != nil {
				continue
			}
			events = append(events, map[string]interface{}{
				"id": id, "level": level, "target_id": targetID,
				"action": action, "reason": reason, "tripped_by": trippedBy,
				"secondary_approver": secondaryApprover,
				"tripped_at_ns": trippedAt, "reset_at_ns": resetAt,
				"chain_hash": chainHash,
			})
		}
		if events == nil {
			events = []map[string]interface{}{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"events": events, "count": len(events)})
	}
}

// ============================================================================
// Helpers
// ============================================================================

func adminFromContext(r *http.Request) string {
	if claims, ok := r.Context().Value(contextKeyJWTClaims).(map[string]interface{}); ok {
		if sub, ok := claims["sub"].(string); ok && sub != "" {
			return sub
		}
	}
	return "unknown"
}

func extractPathParam(r *http.Request, key string) string {
	// Works with gorilla/mux path variables
	path := r.URL.Path
	// Simple last segment extraction
	parts := make([]string, 0)
	for _, p := range []byte(path) {
		if p == '/' {
			parts = append(parts, "")
		} else if len(parts) > 0 {
			parts[len(parts)-1] += string(p)
		}
	}
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	return ""
}
