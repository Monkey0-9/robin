package main

// ============================================================================
// Robin Gateway — Login Handler
// ============================================================================
// POST /api/auth/login   — returns short-lived JWT for the frontend
// POST /api/auth/refresh — (optional) re-issue token using valid unexpired token
//
// Users are stored in the SQLite users table with bcrypt password hashes.
// A default admin/admin seed is created on first run if the table is empty.
// ============================================================================

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// LoginRequest is the JSON body for POST /api/auth/login.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse is returned on successful authentication.
type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	Role      string `json:"role"`
	Sub       string `json:"sub"`
}

// ensureDefaultUsers seeds admin/admin if the users table has no rows.
// ONLY for local development. In production, use proper user management.
func ensureDefaultUsers(db *sql.DB, logger *slog.Logger) {
	if db == nil {
		return
	}

	// Create users table if not present
	db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'trader',
		created_at_ns INTEGER NOT NULL DEFAULT 0
	)`)

	var count int
	db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if count > 0 {
		return
	}

	// Seed admin user with password "admin"
	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if err != nil {
		logger.Error("failed to hash default admin password", "error", err)
		return
	}
	db.Exec(
		"INSERT INTO users (username, password_hash, role, created_at_ns) VALUES (?, ?, ?, ?)",
		"admin", string(hash), "admin", time.Now().UnixNano(),
	)

	// Also seed a trader user
	traderHash, err := bcrypt.GenerateFromPassword([]byte("trader"), bcrypt.DefaultCost)
	if err == nil {
		db.Exec(
			"INSERT INTO users (username, password_hash, role, created_at_ns) VALUES (?, ?, ?, ?)",
			"trader", string(traderHash), "trader", time.Now().UnixNano(),
		)
	}

	logger.Info("seeded default users: admin/admin (role=admin), trader/trader (role=trader) — CHANGE IN PRODUCTION")
}

// handleLogin handles POST /api/auth/login.
func handleLogin(db *sql.DB, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "username and password are required"})
			return
		}

		// Look up user
		var passwordHash, role string
		err := db.QueryRow(
			"SELECT password_hash, role FROM users WHERE username = ?",
			req.Username,
		).Scan(&passwordHash, &role)

		if err == sql.ErrNoRows {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid credentials"})
			return
		}
		if err != nil {
			logger.Error("login db query failed", "error", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "internal error"})
			return
		}

		// Verify password
		if bcryptErr := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); bcryptErr != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid credentials"})
			return
		}

		// Issue JWT — 8h expiry for prototype (15 min in production)
		expiry := 8 * time.Hour
		token, err := createJWT(req.Username, role, expiry)
		if err != nil {
			logger.Error("failed to create JWT", "error", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "token generation failed"})
			return
		}

		logger.Info("user logged in", "username", req.Username, "role", role)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(LoginResponse{
			Token:     token,
			ExpiresAt: time.Now().Add(expiry).Unix(),
			Role:      role,
			Sub:       req.Username,
		})
	}
}

// handleRefreshToken re-issues a fresh JWT if the current one is still valid.
func handleRefreshToken() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(contextKeyJWTClaims).(interface{ getMapClaims() map[string]interface{} })
		_ = claims
		_ = ok

		// Extract claims from context (set by jwtAuthMiddleware)
		rawClaims := r.Context().Value(contextKeyJWTClaims)
		if rawClaims == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "no valid token in context"})
			return
		}

		// Re-issue with same subject and role
		claimsMap, ok := rawClaims.(interface {
			GetSubject() (string, error)
		})
		_ = claimsMap
		_ = ok

		// Simple re-issue using jwt.MapClaims from context
		type mapClaimsGetter interface {
			AsMap() map[string]interface{}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		json.NewEncoder(w).Encode(map[string]string{"error": "refresh not yet implemented — re-login"})
	}
}
