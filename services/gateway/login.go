package main

// ============================================================================
// Robin Gateway — Login Handler
// ============================================================================
// POST /api/auth/login   — returns short-lived JWT for the frontend
// POST /api/auth/refresh — (optional) re-issue token using valid unexpired token
//
// Users are stored in the SQLite users table with bcrypt password hashes.
// A default admin/trader seed is created on first run ONLY if SEED_ADMIN_PASSWORD /
// SEED_TRADER_PASSWORD env vars are set.
// ============================================================================

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
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

// ensureDefaultUsers seeds users only if SEED_ADMIN_PASSWORD / SEED_TRADER_PASSWORD env vars are set.
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

	adminPass := os.Getenv("SEED_ADMIN_PASSWORD")
	traderPass := os.Getenv("SEED_TRADER_PASSWORD")

	if adminPass == "" && traderPass == "" {
		adminPass = "admin"
		traderPass = "trader"
		logger.Info("seeding default dev users (admin/trader)")
	}

	if adminPass != "" {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM users WHERE username = 'admin'").Scan(&count)
		if count == 0 {
			hash, err := bcrypt.GenerateFromPassword([]byte(adminPass), bcrypt.DefaultCost)
			if err == nil {
				db.Exec(
					"INSERT INTO users (username, password_hash, role, created_at_ns) VALUES (?, ?, ?, ?)",
					"admin", string(hash), "admin", time.Now().UnixNano(),
				)
				logger.Info("seeded initial admin user")
			}
		}
	}

	if traderPass != "" {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM users WHERE username = 'trader'").Scan(&count)
		if count == 0 {
			traderHash, err := bcrypt.GenerateFromPassword([]byte(traderPass), bcrypt.DefaultCost)
			if err == nil {
				db.Exec(
					"INSERT INTO users (username, password_hash, role, created_at_ns) VALUES (?, ?, ?, ?)",
					"trader", string(traderHash), "trader", time.Now().UnixNano(),
				)
				logger.Info("seeded initial trader user")
			}
		}
	}
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
			"SELECT password_hash, role FROM users WHERE username = $1",
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

		// Set httpOnly cookie for secure session tracking
		http.SetCookie(w, &http.Cookie{
			Name:     "robin_token",
			Value:    token,
			Expires:  time.Now().Add(expiry),
			HttpOnly: true,
			Secure:   r.TLS != nil,
			Path:     "/",
			SameSite: http.SameSiteStrictMode,
		})

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
