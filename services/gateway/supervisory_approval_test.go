package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// TestSupervisoryApproval_GatesLargeOrderAndRoutesOnApprove verifies the
// FINRA 3110 gate: an order whose notional exceeds the configured threshold is
// held as SUPERVISORY_APPROVAL_REQUIRED (202) and not routed to the engine
// until a principal approves it via /api/supervisory/approve/{id}.
func TestSupervisoryApproval_GatesLargeOrderAndRoutesOnApprove(t *testing.T) {
	mockEngine := startMockMatchingEngine(t, PortRiskHealth)
	defer mockEngine.Close()

	publish("BTC/USD", "Coinbase", 59_999.5, 60_002.0)
	publish("BTC/USD", "Binance", 59_998.0, 60_003.0)

	t.Setenv("ROBIN_SUPERVISORY_THRESHOLD", "100") // force approval for any real order

	orch := NewOrchestrator()
	if err := orch.matchClient.Connect(); err != nil {
		t.Fatalf("failed to connect to mock engine: %v", err)
	}
	server := orch.setupHTTPServer(0)
	traderToken := generateTestToken("trader")
	adminToken := generateTestToken("admin")

	// Place a large order — notional ≈ 60000 * 100 = $6M > $100 threshold.
	bodyBytes, _ := json.Marshal(OrderRequest{
		Symbol: "BTC/USD", Side: "SELL", Price: 60000 * 100000000, Qty: 100 * 100000000, OrderType: "LIMIT",
	})
	req, _ := http.NewRequest("POST", "/order", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+traderToken)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202 SUPERVISORY_APPROVAL_REQUIRED, got %d: %s", rr.Code, rr.Body.String())
	}
	var held struct {
		Status     string  `json:"status"`
		ClOrdID    string  `json:"cl_ord_id"`
		ApprovalID int64   `json:"approval_id"`
		Notional   float64 `json:"notional"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &held); err != nil {
		t.Fatalf("bad hold response: %v", err)
	}
	if held.Status != "SUPERVISORY_APPROVAL_REQUIRED" || held.ApprovalID == 0 || held.ClOrdID == "" {
		t.Fatalf("unexpected hold response: %+v", held)
	}

	// The order is parked in the DB as PENDING_APPROVAL, not routed to engine.
	if orch.db != nil {
		var st string
		err := orch.db.QueryRow("SELECT status FROM orders WHERE cl_order_id = $1", held.ClOrdID).Scan(&st)
		if err != nil {
			t.Fatalf("db lookup failed: %v", err)
		}
		if st != "PENDING_APPROVAL" {
			t.Errorf("expected DB status PENDING_APPROVAL, got %s", st)
		}
	}

	// Pending list exposes the held approval.
	pendReq, _ := http.NewRequest("GET", "/api/supervisory/pending", nil)
	pendReq.Header.Set("Authorization", "Bearer "+adminToken)
	pendRR := httptest.NewRecorder()
	server.Handler.ServeHTTP(pendRR, pendReq)
	if pendRR.Code != http.StatusOK {
		t.Fatalf("pending list failed: %d", pendRR.Code)
	}
	var pend struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal(pendRR.Body.Bytes(), &pend)
	if pend.Count != 1 {
		t.Errorf("expected 1 pending approval, got %d", pend.Count)
	}

	// Principal approves: the order is released to the engine (returns APPROVED)
	// and the engine confirms it as FILLED.
	appReq, _ := http.NewRequest("POST",
		"/api/supervisory/approve/"+strconv.FormatInt(held.ApprovalID, 10),
		bytes.NewBufferString(`{"reason":"approved after review"}`))
	appReq.Header.Set("Authorization", "Bearer "+adminToken)
	appReq.Header.Set("Content-Type", "application/json")
	appRR := httptest.NewRecorder()
	server.Handler.ServeHTTP(appRR, appReq)
	if appRR.Code != http.StatusOK {
		t.Fatalf("expected 200 on approve, got %d: %s", appRR.Code, appRR.Body.String())
	}
	var appResp struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(appRR.Body.Bytes(), &appResp)
	if appResp.Status != "APPROVED" {
		t.Errorf("expected APPROVED response, got %s", appResp.Status)
	}

	if orch.db != nil {
		var st string
		if err := orch.db.QueryRow("SELECT status FROM orders WHERE cl_order_id = $1", held.ClOrdID).Scan(&st); err != nil {
			t.Fatalf("db lookup after approval failed: %v", err)
		}
		if st != "FILLED" {
			t.Errorf("expected engine-confirmed FILLED after approval, got %s", st)
		}
	}

	// No longer pending after approval.
	pendReq2, _ := http.NewRequest("GET", "/api/supervisory/pending", nil)
	pendReq2.Header.Set("Authorization", "Bearer "+adminToken)
	pendRR2 := httptest.NewRecorder()
	server.Handler.ServeHTTP(pendRR2, pendReq2)
	var pend2 struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal(pendRR2.Body.Bytes(), &pend2)
	if pend2.Count != 0 {
		t.Errorf("expected 0 pending approvals after approval, got %d", pend2.Count)
	}
}

// TestSupervisoryApproval_SmallOrderRoutesDirectly verifies orders below the
// threshold are auto-approved and flow straight to the engine with a 200.
func TestSupervisoryApproval_SmallOrderRoutesDirectly(t *testing.T) {
	mockEngine := startMockMatchingEngine(t, PortRiskHealth)
	defer mockEngine.Close()

	publish("BTC/USD", "Coinbase", 59_999.5, 60_002.0)
	publish("BTC/USD", "Binance", 59_998.0, 60_003.0)

	t.Setenv("ROBIN_SUPERVISORY_THRESHOLD", "1000000000") // effectively infinite

	orch := NewOrchestrator()
	if err := orch.matchClient.Connect(); err != nil {
		t.Fatalf("failed to connect to mock engine: %v", err)
	}
	server := orch.setupHTTPServer(0)

	bodyBytes, _ := json.Marshal(OrderRequest{
		Symbol: "BTC/USD", Side: "SELL", Price: 60000 * 100000000, Qty: 1 * 100000000, OrderType: "LIMIT",
	})
	req, _ := http.NewRequest("POST", "/order", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+generateTestToken("trader"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for below-threshold order, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["status"] != "FILLED" {
		t.Errorf("expected FILLED for direct-route order, got %v", resp["status"])
	}
}
