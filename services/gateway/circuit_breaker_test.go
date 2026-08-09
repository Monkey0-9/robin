package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Circuit Breaker Tests (Phase 3.6)
// ============================================================================

func TestCircuitBreaker_TripReset(t *testing.T) {
	cb := NewCircuitBreakerManager(nil, nil, nil)
	if cb.IsTripped() {
		t.Fatal("expected breaker to start untripped")
	}

	cb.Trip("TEST_REASON", "test")
	if !cb.IsTripped() {
		t.Fatal("expected breaker to be tripped after Trip")
	}
	status := cb.GetStatus()
	if status["reason"] != "TEST_REASON" {
		t.Errorf("expected reason TEST_REASON, got %v", status["reason"])
	}
	if status["source"] != "test" {
		t.Errorf("expected source test, got %v", status["source"])
	}
	if status["trip_count"].(uint64) != 1 {
		t.Errorf("expected trip_count 1, got %v", status["trip_count"])
	}

	cb.Reset("manual", "operator")
	if cb.IsTripped() {
		t.Fatal("expected breaker to be reset")
	}
	status = cb.GetStatus()
	if status["reset_count"].(uint64) != 1 {
		t.Errorf("expected reset_count 1, got %v", status["reset_count"])
	}
}

func TestCircuitBreaker_RepeatedTripIsIdempotent(t *testing.T) {
	cb := NewCircuitBreakerManager(nil, nil, nil)
	cb.Trip("R1", "s1")
	firstTrip := cb.GetStatus()["trip_time_ns"].(int64)
	time.Sleep(2 * time.Millisecond)
	cb.Trip("R2", "s2")

	// First trip time must be preserved on repeated trips.
	if cb.GetStatus()["trip_time_ns"].(int64) != firstTrip {
		t.Error("trip_time_ns changed on repeated trip")
	}
	if cb.GetStatus()["reason"] != "R1" {
		t.Errorf("reason changed on repeated trip, got %v", cb.GetStatus()["reason"])
	}
}

func TestCircuitBreaker_CheckDrawdown(t *testing.T) {
	cb := NewCircuitBreakerManager(nil, nil, nil)

	// Below the 10% limit — no trip.
	cb.CheckDrawdown(1000.0, 950.0, 0.10)
	if cb.IsTripped() {
		t.Fatal("expected no trip below the drawdown limit")
	}

	// At the 10% limit — trip.
	cb.CheckDrawdown(1000.0, 900.0, 0.10)
	if !cb.IsTripped() {
		t.Fatal("expected trip at the drawdown limit")
	}
	if !strings.Contains(cb.GetStatus()["reason"].(string), "DRAWDOWN") {
		t.Errorf("expected drawdown reason, got %v", cb.GetStatus()["reason"])
	}
}

func TestCircuitBreaker_CheckDrawdownZeroEquity(t *testing.T) {
	cb := NewCircuitBreakerManager(nil, nil, nil)
	cb.CheckDrawdown(0, 0, 0.10)
	if cb.IsTripped() {
		t.Fatal("expected no trip with zero equity")
	}
	cb.CheckDrawdown(1000.0, 0, 0.10)
	if cb.IsTripped() {
		t.Fatal("expected no trip with zero current equity")
	}
}

func TestCircuitBreaker_PollRiskEngineTrips(t *testing.T) {
	cb := NewCircuitBreakerManager(nil, nil, nil)

	// Fake risk engine metrics endpoint that reports one breaker trip.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("# TYPE robin_risk_circuit_breaker_trips_total counter\nrobin_risk_circuit_breaker_trips_total 3\n"))
	}))
	defer srv.Close()

	cb.PollRiskEngine(context.Background(), srv.URL)
	if !cb.IsTripped() {
		t.Fatal("expected breaker to trip after risk engine reports trips")
	}
	status := cb.GetStatus()
	if status["source"] != "risk_engine_metrics" {
		t.Errorf("expected source risk_engine_metrics, got %v", status["source"])
	}

	// A second poll with the same counter must not change trip time.
	before := status["trip_time_ns"].(int64)
	time.Sleep(2 * time.Millisecond)
	cb.PollRiskEngine(context.Background(), srv.URL)
	if cb.GetStatus()["trip_time_ns"].(int64) != before {
		t.Error("trip_time_ns changed on non-advancing poll")
	}
}

func TestCircuitBreaker_PollRiskEngineNoTrips(t *testing.T) {
	cb := NewCircuitBreakerManager(nil, nil, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("# TYPE robin_risk_circuit_breaker_trips_total counter\nrobin_risk_circuit_breaker_trips_total 0\n"))
	}))
	defer srv.Close()

	cb.PollRiskEngine(context.Background(), srv.URL)
	if cb.IsTripped() {
		t.Fatal("expected no trip when risk engine reports zero trips")
	}
}

func TestCircuitBreaker_PollRiskEngineUnreachable(t *testing.T) {
	cb := NewCircuitBreakerManager(nil, nil, nil)
	// No listener on this port — the client will fail quickly and must NOT trip.
	cb.PollRiskEngine(context.Background(), "http://127.0.0.1:1/metrics")
	if cb.IsTripped() {
		t.Fatal("expected no trip when risk engine is unreachable")
	}
}

func TestCircuitBreaker_StatusHandler(t *testing.T) {
	cb := NewCircuitBreakerManager(nil, nil, nil)
	cb.Trip("REASON", "test")

	handler := circuitBreakerStatusHandler(cb)
	req := httptest.NewRequest("GET", "/api/circuitbreaker/status", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var status map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if status["tripped"] != true {
		t.Errorf("expected tripped=true, got %v", status["tripped"])
	}
}

func TestCircuitBreaker_TripResetHandlers(t *testing.T) {
	cb := NewCircuitBreakerManager(nil, nil, nil)

	tripHandler := circuitBreakerTripHandler(cb)
	req := httptest.NewRequest("POST", "/api/circuitbreaker/trip", strings.NewReader(`{"reason":"ops"}`))
	w := httptest.NewRecorder()
	tripHandler(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 from trip, got %d", w.Code)
	}
	if !cb.IsTripped() {
		t.Fatal("expected breaker tripped via handler")
	}

	resetHandler := circuitBreakerResetHandler(cb)
	req = httptest.NewRequest("POST", "/api/circuitbreaker/reset", strings.NewReader(`{"reason":"all clear"}`))
	w = httptest.NewRecorder()
	resetHandler(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 from reset, got %d", w.Code)
	}
	if cb.IsTripped() {
		t.Fatal("expected breaker reset via handler")
	}
}
