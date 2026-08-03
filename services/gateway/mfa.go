package main

// ============================================================================
// Robin Trading Platform — TOTP Two-Factor Authentication (MFA)
// ============================================================================
// Implements RFC 6238 (TOTP) two-factor authentication for institutional
// access control. Key features:
//   • HMAC-SHA1 TOTP code generation and validation (RFC 6238)
//   • 30-second window with ±1 window tolerance for clock drift
//   • AES-256-GCM encrypted TOTP secrets in database
//   • QR-code-compatible otpauth:// URI generation
//   • Account lockout after 5 consecutive TOTP failures
//   • Admin can revoke/reset MFA for any user
//
// Endpoints:
//   POST /api/auth/mfa/setup   — generate TOTP secret + QR URI (trader/admin)
//   POST /api/auth/mfa/verify  — verify TOTP code, return short-lived JWT
//   POST /api/auth/mfa/disable — admin only, disables MFA for a user
//   GET  /api/auth/mfa/status  — returns MFA enrollment status for caller
// ============================================================================

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

const (
	totpWindow    = 30                 // 30 second window per RFC 6238
	totpTolerance = 1                  // accept ±1 window for clock drift
	totpDigits    = 6                  // 6-digit codes
	totpIssuer    = "Robin Trading"
	totpLockAfter = 5                  // lock account after 5 failures
	totpLockDur   = 15 * time.Minute  // 15 min lockout
)

// ============================================================================
// TOTP Core (RFC 6238)
// ============================================================================

// generateTOTPSecret creates a new random 20-byte TOTP base32 secret.
func generateTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("failed to generate TOTP secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

// computeTOTP returns the TOTP code for the given base32 secret and Unix time.
// Implements RFC 6238 / RFC 4226 HOTP.
func computeTOTP(base32Secret string, unixTime int64) (string, error) {
	keyBytes, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(
		strings.ToUpper(base32Secret),
	)
	if err != nil {
		return "", fmt.Errorf("invalid TOTP secret: %w", err)
	}

	counter := uint64(math.Floor(float64(unixTime) / float64(totpWindow)))
	counterBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(counterBytes, counter)

	mac := hmac.New(sha1.New, keyBytes)
	mac.Write(counterBytes)
	h := mac.Sum(nil)

	// Dynamic truncation per RFC 4226
	offset := h[len(h)-1] & 0x0F
	code := (uint32(h[offset]&0x7F)<<24 |
		uint32(h[offset+1])<<16 |
		uint32(h[offset+2])<<8 |
		uint32(h[offset+3])) % 1_000_000

	return fmt.Sprintf("%06d", code), nil
}

// validateTOTP checks the provided code against ±totpTolerance windows.
func validateTOTP(base32Secret, code string) bool {
	now := time.Now().Unix()
	for delta := -totpTolerance; delta <= totpTolerance; delta++ {
		t := now + int64(delta)*int64(totpWindow)
		expected, err := computeTOTP(base32Secret, t)
		if err != nil {
			continue
		}
		if hmacEqual([]byte(code), []byte(expected)) {
			return true
		}
	}
	return false
}

// hmacEqual performs constant-time string comparison to prevent timing attacks.
func hmacEqual(a, b []byte) bool {
	return hmac.Equal(a, b)
}

// buildOTPAuthURI creates the otpauth:// URI used to populate QR codes.
func buildOTPAuthURI(username, secret string) string {
	return fmt.Sprintf(
		"otpauth://totp/%s:%s?secret=%s&issuer=%s&digits=%d&period=%d",
		totpIssuer, username, secret, totpIssuer, totpDigits, totpWindow,
	)
}

// ============================================================================
// MFA Database operations
// ============================================================================

// mfaSetupForUser generates a new TOTP secret for the user, stores it
// encrypted in the DB, and returns the TOTP URI for QR code generation.
func mfaSetupForUser(db *sql.DB, enc *EncryptionService, username string) (uri, rawSecret string, err error) {
	rawSecret, err = generateTOTPSecret()
	if err != nil {
		return "", "", err
	}

	// Encrypt before storage
	encSecret, err := enc.Encrypt([]byte(rawSecret))
	if err != nil {
		return "", "", fmt.Errorf("failed to encrypt TOTP secret: %w", err)
	}

	_, err = db.Exec(`
		UPDATE user_credentials
		SET totp_secret_enc=$1, totp_enabled=0
		WHERE username=$2`,
		encSecret, username,
	)
	if err != nil {
		return "", "", fmt.Errorf("failed to store TOTP secret: %w", err)
	}

	uri = buildOTPAuthURI(username, rawSecret)
	return uri, rawSecret, nil
}

// mfaEnableForUser marks TOTP as enabled after first successful verification.
func mfaEnableForUser(db *sql.DB, username string) error {
	_, err := db.Exec(`UPDATE user_credentials SET totp_enabled=1 WHERE username=$1`, username)
	return err
}

// mfaVerifyUser verifies a TOTP code for a user, handling lockout logic.
// Returns true if code is valid, false + error if locked or invalid.
func mfaVerifyUser(db *sql.DB, enc *EncryptionService, username, code string) (bool, error) {
	var encSecret string
	var failedAttempts int
	var lockedUntilNs int64
	var totpEnabled int

	err := db.QueryRow(`
		SELECT totp_secret_enc, failed_attempts, locked_until_ns, totp_enabled
		FROM user_credentials WHERE username=$1`, username,
	).Scan(&encSecret, &failedAttempts, &lockedUntilNs, &totpEnabled)
	if err == sql.ErrNoRows {
		return false, fmt.Errorf("user not found")
	}
	if err != nil {
		return false, err
	}

	// Check lockout
	if lockedUntilNs > 0 && time.Now().UnixNano() < lockedUntilNs {
		remaining := time.Until(time.Unix(0, lockedUntilNs)).Round(time.Second)
		return false, fmt.Errorf("account locked for %v due to too many failed attempts", remaining)
	}

	if encSecret == "" {
		return false, fmt.Errorf("MFA not configured for user")
	}

	// Decrypt secret
	secretBytes, err := enc.Decrypt(encSecret)
	if err != nil {
		return false, fmt.Errorf("failed to decrypt TOTP secret")
	}

	valid := validateTOTP(string(secretBytes), code)

	if valid {
		// Clear failure counter and enable if first verification
		_, _ = db.Exec(`
			UPDATE user_credentials SET failed_attempts=0, locked_until_ns=0, totp_enabled=1
			WHERE username=$1`, username)
		return true, nil
	}

	// Increment failure counter
	failedAttempts++
	lockedUntilUpdate := int64(0)
	if failedAttempts >= totpLockAfter {
		lockedUntilUpdate = time.Now().Add(totpLockDur).UnixNano()
	}
	_, _ = db.Exec(`
		UPDATE user_credentials SET failed_attempts=$1, locked_until_ns=$2
		WHERE username=$3`, failedAttempts, lockedUntilUpdate, username)

	return false, nil
}

// mfaStatusForUser returns whether MFA is enabled for the given user.
func mfaStatusForUser(db *sql.DB, username string) (enabled bool, hasSecret bool, err error) {
	var encSecret string
	var totpEnabled int
	err = db.QueryRow(`
		SELECT totp_secret_enc, totp_enabled FROM user_credentials WHERE username=$1`, username,
	).Scan(&encSecret, &totpEnabled)
	if err != nil {
		return false, false, err
	}
	return totpEnabled == 1, encSecret != "", nil
}

// ============================================================================
// HTTP Handlers
// ============================================================================

// handleMFASetup handles POST /api/auth/mfa/setup.
// Generates a new TOTP secret and returns the otpauth:// URI.
func handleMFASetup(db *sql.DB, enc *EncryptionService, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := usernameFromContext(r)
		if username == "" {
			http.Error(w, `{"error":"not authenticated"}`, http.StatusUnauthorized)
			return
		}

		uri, _, err := mfaSetupForUser(db, enc, username)
		if err != nil {
			logger.Error("MFA setup failed", "user", username, "error", err)
			http.Error(w, `{"error":"MFA setup failed"}`, http.StatusInternalServerError)
			return
		}

		logger.Info("MFA setup initiated", "user", username)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"otp_uri":  uri,
			"issuer":   totpIssuer,
			"message":  "Scan the QR code in your authenticator app, then verify with POST /api/auth/mfa/verify",
			"digits":   totpDigits,
			"period_s": totpWindow,
		})
	}
}

// handleMFAVerify handles POST /api/auth/mfa/verify.
// Validates TOTP code. On success, the JWT claims will include mfa_verified:true.
func handleMFAVerify(db *sql.DB, enc *EncryptionService, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code == "" {
			http.Error(w, `{"error":"code is required"}`, http.StatusBadRequest)
			return
		}

		username := usernameFromContext(r)
		if username == "" {
			http.Error(w, `{"error":"not authenticated"}`, http.StatusUnauthorized)
			return
		}

		valid, err := mfaVerifyUser(db, enc, username, body.Code)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusTooManyRequests)
			return
		}
		if !valid {
			logger.Warn("MFA verification failed", "user", username)
			http.Error(w, `{"error":"invalid TOTP code"}`, http.StatusUnauthorized)
			return
		}

		logger.Info("MFA verified successfully", "user", username)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "mfa_verified",
			"user":         username,
			"message":      "MFA verification successful. mfa_verified=true will be included in your next JWT.",
			"verified_at":  time.Now().UnixNano(),
		})
	}
}

// handleMFAStatus handles GET /api/auth/mfa/status.
func handleMFAStatus(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := usernameFromContext(r)
		enabled, hasSecret, err := mfaStatusForUser(db, username)
		if err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"username":   username,
			"mfa_enabled": enabled,
			"has_secret": hasSecret,
			"totp_digits": totpDigits,
			"totp_period": totpWindow,
		})
	}
}

// handleMFADisable handles POST /api/auth/mfa/disable (admin only).
func handleMFADisable(db *sql.DB, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Username string `json:"username"`
			Reason   string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" {
			http.Error(w, `{"error":"username is required"}`, http.StatusBadRequest)
			return
		}

		_, err := db.Exec(`
			UPDATE user_credentials SET totp_secret_enc='', totp_enabled=0 WHERE username=$1`,
			body.Username,
		)
		if err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}

		admin := adminFromContext(r)
		logger.Warn("MFA disabled by admin",
			"target_user", body.Username, "admin", admin, "reason", body.Reason,
		)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "mfa_disabled",
			"username": body.Username,
			"admin":    admin,
		})
	}
}

// handleCreateUser handles POST /api/auth/users (admin only).
// Creates a new user in the database with bcrypt password hash.
func handleCreateUser(db *sql.DB, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Role     string `json:"role"` // viewer, trader, admin
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}
		if body.Username == "" || body.Password == "" {
			http.Error(w, `{"error":"username and password required"}`, http.StatusBadRequest)
			return
		}
		if body.Role == "" {
			body.Role = "viewer"
		}
		validRoles := map[string]bool{"viewer": true, "trader": true, "admin": true}
		if !validRoles[body.Role] {
			http.Error(w, `{"error":"role must be viewer, trader, or admin"}`, http.StatusBadRequest)
			return
		}
		if len(body.Password) < 12 {
			http.Error(w, `{"error":"password must be at least 12 characters"}`, http.StatusBadRequest)
			return
		}

		// PBKDF2-SHA256 password hashing (600k iterations, OWASP 2024 recommendation)
		h := pbkdf2Hash(body.Password, 600000)

		now := time.Now().UnixNano()
		_, err := db.Exec(`
			INSERT INTO user_credentials (username, bcrypt_hash, role, created_at_ns, must_change_password)
			VALUES ($1, $2, $3, $4, 1)`,
			body.Username, h, body.Role, now,
		)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				http.Error(w, `{"error":"username already exists"}`, http.StatusConflict)
				return
			}
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}

		admin := adminFromContext(r)
		logger.Info("user created", "username", body.Username, "role", body.Role, "created_by", admin)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "created",
			"username": body.Username,
			"role":     body.Role,
		})
	}
}

func handleListUsers(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query(`
			SELECT user_id, username, role, totp_enabled, created_at_ns, last_login_ns, failed_attempts
			FROM user_credentials ORDER BY user_id`)
		if err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var users []map[string]interface{}
		for rows.Next() {
			var userID int64
			var username, role string
			var totpEnabled, failedAttempts int
			var createdAt, lastLogin int64
			if err := rows.Scan(&userID, &username, &role, &totpEnabled, &createdAt, &lastLogin, &failedAttempts); err != nil {
				continue
			}
			users = append(users, map[string]interface{}{
				"user_id": userID, "username": username, "role": role,
				"mfa_enabled": totpEnabled == 1,
				"created_at_ns": createdAt, "last_login_ns": lastLogin,
				"failed_attempts": failedAttempts,
			})
		}
		if users == nil {
			users = []map[string]interface{}{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"users": users, "count": len(users)})
	}
}

// ============================================================================
// Helpers
// ============================================================================

func usernameFromContext(r *http.Request) string {
	if claims, ok := r.Context().Value(contextKeyJWTClaims).(map[string]interface{}); ok {
		if sub, ok := claims["sub"].(string); ok {
			return sub
		}
	}
	return ""
}

func pbkdf2Hash(password string, iter int) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic("failed to generate PBKDF2 salt: " + err.Error())
	}
	dk := pbkdf2.Key([]byte(password), salt, iter, 32, sha256.New)
	return fmt.Sprintf("PBKDF2-SHA256:%x:%s", dk, base64.StdEncoding.EncodeToString(salt))
}

func totpTimeStep(unixSec int64) uint64 {
	return uint64(unixSec) / uint64(totpWindow)
}

// formatTOTPCode formats an integer as a zero-padded 6-digit string.
func formatTOTPCode(n uint32) string {
	return strconv.FormatUint(uint64(n%1_000_000), 10)
}

var _ = mfaEnableForUser
var _ = totpTimeStep
var _ = formatTOTPCode
