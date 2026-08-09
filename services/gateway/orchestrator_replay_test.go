package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var testPrivateKey *rsa.PrivateKey

func init() {
	os.Setenv("ROBIN_MASTER_KEY", "test-master-key-for-unit-tests-only-32bytes!")
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	testPrivateKey = priv
	jwtAuth = JWTConfig{
		PublicKey:  &priv.PublicKey,
		PrivateKey: priv,
	}
}

func generateTestToken(role string) string {
	claims := jwt.MapClaims{
		"sub":  "test-user",
		"role": role,
		"iss":  "robin-gateway",
		"aud":  "robin-services",
		"exp":  time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tokenString, _ := token.SignedString(testPrivateKey)
	return tokenString
}

// startMockMatchingEngine starts a minimal TCP server that responds to order JSON
// with a valid FILLED response for integration tests.
func startMockMatchingEngine(t *testing.T, port int) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("failed to start mock engine: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				scanner := bufio.NewScanner(c)
				for scanner.Scan() {
					line := scanner.Text()
					if line == "health" {
						c.Write([]byte("{\"status\":\"ok\"}\n"))
						continue
					}
					// Parse any order JSON and respond FILLED
					resp := `{"order_id":1,"instrument_id":1,"fill_price":6500000000000,"fill_qty":1500000000,"status":"FILLED","success":true,"error":""}`
					if line != "" {
						c.Write([]byte(resp + "\n"))
					}
				}
			}(conn)
		}
	}()
	return ln
}

func TestDeterministicReplay(t *testing.T) {
	// Start mock risk gate on the port the orchestrator connects to (PortRiskHealth = 9092)
	// The orchestrator sends orders to the risk gate, which forwards to the matching engine.
	mockEngine := startMockMatchingEngine(t, PortRiskHealth)
	defer mockEngine.Close()

	orch := NewOrchestrator()
	// Connect mock risk gate
	if err := orch.matchClient.Connect(); err != nil {
		t.Fatalf("failed to connect to mock engine: %v", err)
	}
	server := orch.setupHTTPServer(0)

	traderToken := generateTestToken("trader")
	adminToken := generateTestToken("admin")

	// Price and qty use fixed-point int64 (scaled by 1e8) for deterministic arithmetic
	sendOrder := func(symbol, side, ordType string, price, qty int64, token string) (int, map[string]interface{}) {
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

	// 1. Send an order as a trader (fixed-point: price*1e8, qty*1e8).
	// The engine response is returned synchronously (Bug #7 fix), so the
	// client sees the actual fill status rather than a premature "WORKING".
	code, resp := sendOrder("BTC/USD", "BUY", "LIMIT", 60000*100000000, 15*100000000, traderToken)
	if code != http.StatusOK {
		t.Errorf("expected 200 OK, got code %d", code)
	}
	if resp["status"] != "FILLED" {
		t.Errorf("expected engine status FILLED to be returned synchronously, got %v", resp["status"])
	}
	if fp, ok := resp["fill_price"].(float64); !ok || fp <= 0 {
		t.Errorf("expected a fill price to be returned, got %v", resp["fill_price"])
	}

	// 2. Send an order as an admin (should be forbidden)
	code2, resp2 := sendOrder("ETH/USD", "SELL", "MARKET", 3000*100000000, 10*100000000, adminToken)
	if code2 != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", code2)
	}
	if errStr, ok := resp2["error"].(string); !ok || errStr != "forbidden: insufficient permissions" {
		t.Errorf("expected forbidden error, got %v", resp2["error"])
	}

	// Give async routing time to complete
	time.Sleep(100 * time.Millisecond)

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
