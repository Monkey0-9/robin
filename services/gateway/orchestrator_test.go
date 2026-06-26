package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ============================================================================
// Orchestrator Tests
// ============================================================================

func newTestOrch() *Orchestrator {
	return NewOrchestrator()
}

func TestNewOrchestrator(t *testing.T) {
	orch := newTestOrch()
	if orch == nil {
		t.Fatal("NewOrchestrator returned nil")
	}
	if orch.services == nil {
		t.Fatal("services map is nil")
	}
}

func TestRegisterService(t *testing.T) {
	orch := newTestOrch()
	orch.RegisterService("TestSvc", "127.0.0.1:9999")
	svcs := orch.GetServices()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "TestSvc" {
		t.Errorf("expected name TestSvc, got %s", svcs[0].Name)
	}
	if svcs[0].Status != StatusUnknown {
		t.Errorf("expected StatusUnknown on registration, got %v", svcs[0].Status)
	}
}

func TestHotReloadConfig_Valid(t *testing.T) {
	orch := newTestOrch()
	cfg := `{"max_drawdown_limit":0.05,"max_order_rate":5000}`
	err := orch.HotReloadConfig([]byte(cfg))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	loaded := orch.GetConfig()
	if loaded.MaxDrawdownLimit != 0.05 {
		t.Errorf("expected MaxDrawdownLimit=0.05, got %f", loaded.MaxDrawdownLimit)
	}
	if loaded.MaxOrderRate != 5000 {
		t.Errorf("expected MaxOrderRate=5000, got %d", loaded.MaxOrderRate)
	}
}

func TestHotReloadConfig_Invalid(t *testing.T) {
	orch := newTestOrch()
	err := orch.HotReloadConfig([]byte("{invalid json}"))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestGetConfig_DefaultValues(t *testing.T) {
	os.Remove("config_state.json") // Ensure we load defaults, not persisted state from other tests
	orch := newTestOrch()
	cfg := orch.GetConfig()
	if cfg.MaxDrawdownLimit != 0.10 {
		t.Errorf("expected default drawdown 0.10, got %f", cfg.MaxDrawdownLimit)
	}
	if cfg.MaxOrderRate != 10000 {
		t.Errorf("expected default order rate 10000, got %d", cfg.MaxOrderRate)
	}
}

// ============================================================================
// HTTP Handler Tests
// ============================================================================

func TestHealthEndpoint(t *testing.T) {
	orch := newTestOrch()
	srv := orch.setupHTTPServer(0) // port 0 — not started, just for handler
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v\nbody: %s", err, w.Body.String())
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", resp["status"])
	}
}

func TestServicesEndpoint(t *testing.T) {
	orch := newTestOrch()
	orch.RegisterService("Alpha", "127.0.0.1:9001")
	orch.RegisterService("Beta",  "127.0.0.1:9002")
	srv := orch.setupHTTPServer(0)
	req := httptest.NewRequest("GET", "/services", nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var svcs []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &svcs); err != nil {
		t.Fatalf("invalid JSON: %v\nbody: %s", err, w.Body.String())
	}
	if len(svcs) != 2 {
		t.Errorf("expected 2 services, got %d", len(svcs))
	}
}

func TestConfigGetEndpoint(t *testing.T) {
	orch := newTestOrch()
	srv := orch.setupHTTPServer(0)
	req := httptest.NewRequest("GET", "/config", nil)
	req.Header.Set("Authorization", "Bearer "+generateTestToken("admin"))
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := cfg["max_drawdown_limit"]; !ok {
		t.Error("expected max_drawdown_limit in config response")
	}
}

func TestConfigPostEndpoint(t *testing.T) {
	testToken := generateTestToken("admin")

	orch := newTestOrch()
	srv := orch.setupHTTPServer(0)
	body := `{"max_drawdown_limit":0.15,"max_order_rate":2000}`
	req := httptest.NewRequest("POST", "/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d\nbody: %s", w.Code, w.Body.String())
	}
	cfg := orch.GetConfig()
	if cfg.MaxDrawdownLimit != 0.15 {
		t.Errorf("config not updated: expected 0.15, got %f", cfg.MaxDrawdownLimit)
	}
}


func TestConfigPostEndpoint_Unauthorized(t *testing.T) {
	orch := newTestOrch()
	srv := orch.setupHTTPServer(0)
	body := `{"max_drawdown_limit":0.15,"max_order_rate":2000}`
	req := httptest.NewRequest("POST", "/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d\nbody: %s", w.Code, w.Body.String())
	}
}

func TestStatsEndpoint(t *testing.T) {
	orch := newTestOrch()
	orch.RecordOrder()
	orch.RecordOrder()
	orch.RecordTrade()
	orch.RecordLatency(1500)
	srv := orch.setupHTTPServer(0)
	req := httptest.NewRequest("GET", "/stats", nil)
	req.Header.Set("Authorization", "Bearer "+generateTestToken("admin"))
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var stats map[string]uint64
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if stats["orders"] != 2 {
		t.Errorf("expected orders=2, got %d", stats["orders"])
	}
	if stats["trades"] != 1 {
		t.Errorf("expected trades=1, got %d", stats["trades"])
	}
	if stats["avg_lat_ns"] != 1500 {
		t.Errorf("expected avg_lat_ns=1500, got %d", stats["avg_lat_ns"])
	}
}

// ============================================================================
// Rate Limiter Tests
// ============================================================================

func TestTokenBucket_AllowsUpToRate(t *testing.T) {
	tb := newTokenBucket(10) // 10 req/s
	allowed := 0
	for i := 0; i < 10; i++ {
		if tb.Allow() {
			allowed++
		}
	}
	if allowed != 10 {
		t.Errorf("expected 10 allowed, got %d", allowed)
	}
	// 11th should be denied
	if tb.Allow() {
		t.Error("expected 11th request to be denied")
	}
}

func TestTokenBucket_RefillsOverTime(t *testing.T) {
	tb := newTokenBucket(100) // 100 req/s
	// Drain all tokens
	for tb.Allow() {
	}
	time.Sleep(50 * time.Millisecond) // 50ms → ~5 tokens refilled at 100/s
	allowed := 0
	for tb.Allow() {
		allowed++
	}
	if allowed < 3 || allowed > 8 {
		t.Errorf("expected ~5 tokens refilled, got %d", allowed)
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	calls := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	})
	limited := rateLimitMiddleware(2, handler) // 2 req/s

	// First 2 requests should pass
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		limited.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}
	// 3rd should be rate-limited
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	limited.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 on 3rd request, got %d", w.Code)
	}
}

// ============================================================================
// ServiceStatus Tests
// ============================================================================

func TestServiceStatusString(t *testing.T) {
	cases := []struct {
		s    ServiceStatus
		want string
	}{
		{StatusActive,   "ACTIVE"},
		{StatusDegraded, "DEGRADED"},
		{StatusFailed,   "FAILED"},
		{StatusUnknown,  "UNKNOWN"},
	}
	for _, tc := range cases {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("status %d: expected %s, got %s", tc.s, tc.want, got)
		}
	}
}

func TestServiceStatusMarshalJSON(t *testing.T) {
	b, err := StatusActive.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"ACTIVE"` {
		t.Errorf("expected \"ACTIVE\", got %s", string(b))
	}
}

func TestJWTAuthMiddleware_JWTVerification(t *testing.T) {
	// Set up keys
	hmacSecret := []byte("my-test-secret-key-123456789")
	authenticator := &jwtAuthenticator{
		hmacKey:   hmacSecret,
		useHMAC:   true,
		issuer:    "robin-gateway",
		audience:  "robin-services",
	}

	// Create a valid signed token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": "robin-gateway",
		"aud": "robin-services",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	validTokenStr, err := token.SignedString(hmacSecret)
	if err != nil {
		t.Fatal(err)
	}

	// Verify valid token succeeds
	claims, err := authenticator.verify(validTokenStr)
	if err != nil {
		t.Errorf("expected validation to succeed, got error: %v", err)
	}
	if claims["iss"] != "robin-gateway" {
		t.Errorf("expected issuer robin-gateway, got %v", claims["iss"])
	}

	// Verify tampered token fails
	tamperedTokenStr := validTokenStr + "extra"
	_, err = authenticator.verify(tamperedTokenStr)
	if err == nil {
		t.Error("expected validation to fail for tampered token, got nil")
	}

	// Verify token signed with different key fails
	wrongSecret := []byte("wrong-secret-key-987654321")
	wrongToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": "robin-gateway",
		"aud": "robin-services",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	wrongTokenStr, err := wrongToken.SignedString(wrongSecret)
	if err != nil {
		t.Fatal(err)
	}
	_, err = authenticator.verify(wrongTokenStr)
	if err == nil {
		t.Error("expected validation to fail for token with wrong secret, got nil")
	}
}
