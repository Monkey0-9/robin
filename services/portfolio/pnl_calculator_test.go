package portfolio

import (
	"math"
	"testing"
)

func TestExecuteTradeFIFO(t *testing.T) {
	pm := NewPortfolioManager(100000.0)

	// Buy 10 @ 100, then 10 @ 110
	if _, err := pm.ExecuteTrade(Trade{Symbol: "AAPL", Side: "BUY", Qty: 10, Price: 100}); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.ExecuteTrade(Trade{Symbol: "AAPL", Side: "BUY", Qty: 10, Price: 110}); err != nil {
		t.Fatal(err)
	}

	pos, _ := pm.GetPosition("AAPL")
	if pos.Size != 20 {
		t.Fatalf("size = %v, want 20", pos.Size)
	}
	if math.Abs(pos.AvgEntryPrice-105.0) > 1e-9 {
		t.Fatalf("avg entry = %v, want 105", pos.AvgEntryPrice)
	}

	// Sell 15 @ 120 — FIFO closes 10@100 and 5@110
	realized, err := pm.ExecuteTrade(Trade{Symbol: "AAPL", Side: "SELL", Qty: 15, Price: 120})
	if err != nil {
		t.Fatal(err)
	}
	// Realized = 10*(120-100) + 5*(120-110) = 200 + 50 = 250
	if math.Abs(realized-250.0) > 1e-9 {
		t.Fatalf("realized = %v, want 250", realized)
	}

	pos, _ = pm.GetPosition("AAPL")
	if pos.Size != 5 {
		t.Fatalf("size = %v, want 5", pos.Size)
	}
	if math.Abs(pos.RealizedPnL-250.0) > 1e-9 {
		t.Fatalf("pos realized pnl = %v, want 250", pos.RealizedPnL)
	}
	// Remaining lot is 5 @ 110
	if len(pos.lots) != 1 || math.Abs(pos.lots[0].Price-110.0) > 1e-9 {
		t.Fatalf("remaining lot = %+v, want single 5@110", pos.lots)
	}

	// Mark to market at 120
	pm.UpdateMarketPrice("AAPL", 120)
	pos, _ = pm.GetPosition("AAPL")
	if math.Abs(pos.UnrealizedPnL-50.0) > 1e-9 {
		t.Fatalf("unrealized = %v, want 50", pos.UnrealizedPnL)
	}

	// Cash: 100000 - 1000 - 1100 + 1800 = 99700
	if math.Abs(pm.GetCash()-99700.0) > 1e-6 {
		t.Fatalf("cash = %v, want 99700", pm.GetCash())
	}
	// Equity = cash + unrealized = 99700 + 50
	if math.Abs(pm.GetTotalEquity()-99750.0) > 1e-6 {
		t.Fatalf("equity = %v, want 99750", pm.GetTotalEquity())
	}
}

func TestExecuteTradeInsufficientPosition(t *testing.T) {
	pm := NewPortfolioManager(10000.0)
	pm.ExecuteTrade(Trade{Symbol: "TSLA", Side: "BUY", Qty: 5, Price: 200})
	if _, err := pm.ExecuteTrade(Trade{Symbol: "TSLA", Side: "SELL", Qty: 10, Price: 210}); err == nil {
		t.Fatal("expected error on oversell")
	}
}
