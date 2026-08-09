package main

import (
	"testing"
)

func TestCheckPositionLimit_LongBreach(t *testing.T) {
	pm := NewPositionManager("http://127.0.0.1:8000")
	pm.OnFill("EXEC-1", "BTC/USD", "BUY", 10.0, 60000.0)

	// Buying 200 more would push net to 210 > 200 limit.
	if err := pm.checkPositionLimit("BTC/USD", "BUY", 200.0, 200.0); err == nil {
		t.Error("expected position limit breach for oversized BUY")
	}
	// Buying 100 more keeps net at 110 <= 200.
	if err := pm.checkPositionLimit("BTC/USD", "BUY", 100.0, 200.0); err != nil {
		t.Errorf("did not expect breach for in-limit BUY: %v", err)
	}
}

func TestCheckPositionLimit_ShortBreach(t *testing.T) {
	pm := NewPositionManager("http://127.0.0.1:8000")
	pm.OnFill("EXEC-1", "BTC/USD", "SELL", 50.0, 60000.0) // short -50

	// Selling 160 more -> net -210, exceeding the 200 abs limit.
	if err := pm.checkPositionLimit("BTC/USD", "SELL", 160.0, 200.0); err == nil {
		t.Error("expected position limit breach for oversized SELL")
	}
	// Selling 100 more -> net -150, within limit.
	if err := pm.checkPositionLimit("BTC/USD", "SELL", 100.0, 200.0); err != nil {
		t.Errorf("did not expect breach for in-limit SELL: %v", err)
	}
}

func TestCheckPositionLimit_ClosingReducesRisk(t *testing.T) {
	pm := NewPositionManager("http://127.0.0.1:8000")
	pm.OnFill("EXEC-1", "BTC/USD", "BUY", 150.0, 100.0) // net +150

	// A SELL of 100 reduces the long to +50: never aggravates the limit.
	if err := pm.checkPositionLimit("BTC/USD", "SELL", 100.0, 200.0); err != nil {
		t.Errorf("closing trade should not breach: %v", err)
	}
}

func TestRecordAccountFill(t *testing.T) {
	pm := NewPositionManager("http://127.0.0.1:8000")
	pm.OnFill("EXEC-BUY", "BTC/USD", "BUY", 2.0, 100.0)
	pm.RecordAccountFill(42, "BTC/USD", "BUY", 2.0, 100.0)
	pm.RecordAccountFill(42, "BTC/USD", "SELL", 1.0, 120.0)

	accounts := pm.GetAccountPnL()
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
	acc := accounts[0]
	if acc.AccountID != 42 {
		t.Errorf("expected account 42, got %d", acc.AccountID)
	}
	if acc.TradeCount != 2 {
		t.Errorf("expected 2 trades, got %d", acc.TradeCount)
	}
	if acc.Fees < 0 {
		t.Errorf("fees should be non-negative, got %f", acc.Fees)
	}
}
