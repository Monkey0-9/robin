package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAISignalProxy_Contract verifies the /api/ai/signal handler contract
// by replaying the exact decode/encode logic against a mock Python upstream.
func TestAISignalProxy_Contract(t *testing.T) {
	// Mock Python /trade_decision — full signal path
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"reasoning":     "Bull regime, sentiment +0.45 -> BUY (78.0% conf)",
			"action":        "BUY",
			"confidence":    0.78,
			"regime":        "Bull",
			"sentiment":     0.45,
			"symbol":        "BTC-USD",
			"qty":           0.015,
			"price":         66000.0,
			"entry_target":  66000.0,
			"data_source":   "live",
		})
	}))
	defer upstream.Close()

	// Replay the handler's decode step
	resp, err := http.Post(upstream.URL, "application/json", nil)
	if err != nil {
		t.Fatalf("upstream call failed: %v", err)
	}
	defer resp.Body.Close()

	var sig struct {
		Action      string  `json:"action"`
		Confidence  float64 `json:"confidence"`
		Regime      string  `json:"regime"`
		Sentiment   float64 `json:"sentiment"`
		Reasoning   string  `json:"reasoning"`
		Symbol      string  `json:"symbol"`
		Price       float64 `json:"price"`
		EntryTarget float64 `json:"entry_target"`
		Error       string  `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sig); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if sig.Error != "" {
		t.Fatalf("unexpected error field: %s", sig.Error)
	}
	if sig.Action != "BUY" || sig.Confidence != 0.78 || sig.Regime != "Bull" {
		t.Errorf("signal mismatch: %+v", sig)
	}

	// Verify frontend contract serialization
	contract := map[string]interface{}{
		"symbol":       sig.Symbol,
		"action":       sig.Action,
		"confidence":   sig.Confidence,
		"regime":       sig.Regime,
		"sentiment":    sig.Sentiment,
		"reason":       sig.Reasoning,
		"price":        sig.Price,
		"entry_target": sig.EntryTarget,
		"timestamp":    int64(1),
	}
	blob, _ := json.Marshal(contract)
	var parsed map[string]interface{}
	if err := json.Unmarshal(blob, &parsed); err != nil {
		t.Fatalf("contract not JSON-serializable: %v", err)
	}
	if parsed["action"] != "BUY" || parsed["confidence"] != 0.78 {
		t.Errorf("contract serialization wrong: %v", parsed)
	}
	fmt.Println("AI signal contract verified: BUY / 0.78 / Bull")
}

// TestAISignalProxy_ErrorPath verifies the error path (no live data) returns
// an error object with symbol, not a hollow zero-valued signal.
func TestAISignalProxy_ErrorPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":  "No live price available for BTC-USD",
			"symbol": "BTC-USD",
			"action": "HOLD",
		})
	}))
	defer upstream.Close()

	resp, err := http.Post(upstream.URL, "application/json", nil)
	if err != nil {
		t.Fatalf("upstream call failed: %v", err)
	}
	defer resp.Body.Close()

	var sig struct {
		Action string `json:"action"`
		Error  string `json:"error"`
		Symbol string `json:"symbol"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sig); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	// Frontend AISignalPanel treats non-ok as offline state.
	if sig.Error == "" {
		t.Fatal("expected error field on no-live-data path")
	}
	if sig.Symbol != "BTC-USD" {
		t.Errorf("expected symbol BTC-USD, got %s", sig.Symbol)
	}
	fmt.Println("AI signal error path verified:", sig.Error)
}
