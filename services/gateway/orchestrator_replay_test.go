package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func generateTestToken(role string) string {
	jwtAuth.useHMAC = true
	jwtAuth.hmacKey = []byte("test-secret")
	jwtAuth.publicKey = nil

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  "test-user",
		"role": role,
		"iss":  "robin-gateway",
		"aud":  "robin-services",
		"exp":  time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := token.SignedString(jwtAuth.hmacKey)
	return tokenString
}

func TestDeterministicReplay(t *testing.T) {
	orch := NewOrchestrator()
	orch.matchClient = nil // Force simulated fills
	server := orch.setupHTTPServer(0)
	
	traderToken := generateTestToken("trader")
	adminToken := generateTestToken("admin")

	sendOrder := func(symbol, side, ordType string, price, qty float64, token string) (int, map[string]interface{}) {
		reqBody := OrderRequest{
			Symbol:    symbol,
			Side:      side,
			Price:     price,
			Qty:       qty,
			OrderType: ordType,
		}
		bodyBytes, _ := json.Marshal(reqBody)
		req, _ := http.NewRequest("POST", "/order", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		server.Handler.ServeHTTP(rr, req)

		var resp map[string]interface{}
		json.Unmarshal(rr.Body.Bytes(), &resp)
		return rr.Code, resp
	}

	// 1. Send an order as a trader
	code, resp := sendOrder("BTC/USD", "BUY", "LIMIT", 60000.0, 1.5, traderToken)
	if code != http.StatusOK || resp["status"] != "FILLED" {
		t.Errorf("expected OK and FILLED, got code %d, status %v", code, resp["status"])
	}

	// 2. Send an order as an admin (should be forbidden)
	code2, resp2 := sendOrder("ETH/USD", "SELL", "MARKET", 3000.0, 10.0, adminToken)
	if code2 != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", code2)
	}
	if errStr, ok := resp2["error"].(string); !ok || errStr != "forbidden: insufficient permissions" {
		t.Errorf("expected forbidden error, got %v", resp2["error"])
	}

	// 3. Check stats as admin
	req, _ := http.NewRequest("GET", "/stats", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 OK for stats, got %d", rr.Code)
	}

	var stats map[string]uint64
	json.Unmarshal(rr.Body.Bytes(), &stats)

	if stats["orders"] != 1 {
		// Note: The forbidden order doesn't reach the handler logic so it isn't counted in orch.orderCount
		t.Errorf("expected 1 order attempted, got %d", stats["orders"])
	}
	if stats["trades"] != 1 {
		t.Errorf("expected 1 trade filled, got %d", stats["trades"])
	}
}
