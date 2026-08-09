package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ============================================================================
// Bulk Order API Tests (Phase 3.7)
// ============================================================================

// TestBulkOrder_SubmitBatch uses the same mock matching engine as the replay
// tests: the engine responds FILLED to every order JSON line.
func TestBulkOrder_SubmitBatch(t *testing.T) {
	// The orchestrator forwards orders to the risk-gate entry port (9092);
	// the mock engine must listen there to satisfy SendOrderJSON.
	mockEngine := startMockMatchingEngine(t, PortRiskHealth)
	defer mockEngine.Close()

	// Phase 3.1 strict SOR only routes on live venue quotes — seed the NBBO
	// cache the same way the exchange feeds would in production.
	publish("BTC/USD", "Coinbase", 59_999.5, 60_002.0)
	publish("BTC/USD", "Binance", 59_998.0, 60_003.0)
	publish("ETH/USD", "Coinbase", 2_999.0, 3_001.0)
	publish("ETH/USD", "Binance", 2_998.0, 3_002.0)

	orch := NewOrchestrator()
	if err := orch.matchClient.Connect(); err != nil {
		t.Fatalf("failed to connect to mock engine: %v", err)
	}
	server := orch.setupHTTPServer(0)

	batch := []OrderRequest{
		{Symbol: "BTC/USD", Side: "BUY", Price: 60000 * 100000000, Qty: 1 * 100000000, OrderType: "LIMIT"},
		{Symbol: "ETH/USD", Side: "SELL", Price: 3000 * 100000000, Qty: 2 * 100000000, OrderType: "LIMIT"},
	}
	body, _ := json.Marshal(batch)
	req, _ := http.NewRequest("POST", "/api/orders/bulk", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+generateTestToken("trader"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d\nbody: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp["batch_size"].(float64) != 2 {
		t.Errorf("expected batch_size 2, got %v", resp["batch_size"])
	}
	if resp["accepted"].(float64) != 2 {
		t.Errorf("expected accepted 2, got %v", resp["accepted"])
	}

	results := resp["results"].([]interface{})
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	first := results[0].(map[string]interface{})
	if first["status"] != "FILLED" {
		t.Errorf("expected first order FILLED, got %v", first["status"])
	}
}

func TestBulkOrder_EmptyBatchRejected(t *testing.T) {
	orch := NewOrchestrator()
	server := orch.setupHTTPServer(0)

	req, _ := http.NewRequest("POST", "/api/orders/bulk", bytes.NewBuffer([]byte(`[]`)))
	req.Header.Set("Authorization", "Bearer "+generateTestToken("trader"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty batch, got %d", rr.Code)
	}
}

func TestBulkOrder_InvalidOrderRejectsWholeBatch(t *testing.T) {
	orch := NewOrchestrator()
	server := orch.setupHTTPServer(0)

	// Second order has an invalid side — the whole batch must be rejected.
	batch := []OrderRequest{
		{Symbol: "BTC/USD", Side: "BUY", Price: 100, Qty: 100},
		{Symbol: "BTC/USD", Side: "SIDEWAYS", Price: 100, Qty: 100},
	}
	body, _ := json.Marshal(batch)
	req, _ := http.NewRequest("POST", "/api/orders/bulk", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+generateTestToken("trader"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid order in batch, got %d", rr.Code)
	}
}

func TestBulkOrder_CircuitBreakerBlocksBatch(t *testing.T) {
	orch := NewOrchestrator()
	server := orch.setupHTTPServer(0)

	// Trip the global breaker — bulk submissions must be blocked.
	orch.circuitBreaker.Trip("TEST_TRIP", "test")
	defer orch.circuitBreaker.Reset("test_done", "test")

	batch := []OrderRequest{
		{Symbol: "BTC/USD", Side: "BUY", Price: 100, Qty: 100},
	}
	body, _ := json.Marshal(batch)
	req, _ := http.NewRequest("POST", "/api/orders/bulk", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+generateTestToken("trader"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409 when breaker tripped, got %d", rr.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["reason"] != "CIRCUIT_BREAKER_TRIPPED" {
		t.Errorf("expected CIRCUIT_BREAKER_TRIPPED reason, got %v", resp["reason"])
	}
}

func TestBulkOrder_ForbiddenForAdmin(t *testing.T) {
	orch := NewOrchestrator()
	server := orch.setupHTTPServer(0)

	req, _ := http.NewRequest("POST", "/api/orders/bulk", bytes.NewBuffer([]byte(`[{}]`)))
	req.Header.Set("Authorization", "Bearer "+generateTestToken("admin"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for admin bulk submit, got %d", rr.Code)
	}
}
