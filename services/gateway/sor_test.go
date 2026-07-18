package main

import (
	"testing"
)

func TestGenerateQuotes(t *testing.T) {
	quotes := GenerateQuotes("BTC/USD", 64500.0)
	if len(quotes) != len(Exchanges) {
		t.Fatalf("expected %d quotes, got %d", len(Exchanges), len(quotes))
	}

	for _, q := range quotes {
		if q.Exchange == "" {
			t.Error("expected non-empty exchange name")
		}
		if q.Bid <= 0 || q.Ask <= 0 {
			t.Errorf("expected positive bid/ask prices, got Bid=%f, Ask=%f", q.Bid, q.Ask)
		}
		if q.Ask <= q.Bid {
			t.Errorf("expected ask to be greater than bid, got Bid=%f, Ask=%f", q.Bid, q.Ask)
		}
		if q.BidSize <= 0 || q.AskSize <= 0 {
			t.Errorf("expected positive sizes, got BidSize=%f, AskSize=%f", q.BidSize, q.AskSize)
		}
	}
}

func TestRouteOrder_AutoBuy(t *testing.T) {
	midPrice := 64500.0
	symbol := "BTC/USD"

	quotes := GenerateQuotes(symbol, midPrice)
	// Find absolute lowest ask in quotes
	lowestAsk := quotes[0].Ask
	for _, q := range quotes {
		if q.Ask < lowestAsk {
			lowestAsk = q.Ask
		}
	}

	res := RouteOrder(symbol, "BUY", midPrice, "AUTO")
	if res.ExchangesSearched != len(Exchanges) {
		t.Errorf("expected to search %d exchanges, searched %d", len(Exchanges), res.ExchangesSearched)
	}
	if res.FillPrice != lowestAsk {
		t.Errorf("expected fill price to be the lowest ask %f, got %f", lowestAsk, res.FillPrice)
	}
	if res.PriceImprovementBps < 0 {
		t.Errorf("expected non-negative price improvement, got %f", res.PriceImprovementBps)
	}
}

func TestRouteOrder_AutoSell(t *testing.T) {
	midPrice := 64500.0
	symbol := "BTC/USD"

	quotes := GenerateQuotes(symbol, midPrice)
	// Find absolute highest bid in quotes
	highestBid := quotes[0].Bid
	for _, q := range quotes {
		if q.Bid > highestBid {
			highestBid = q.Bid
		}
	}

	res := RouteOrder(symbol, "SELL", midPrice, "AUTO")
	if res.ExchangesSearched != len(Exchanges) {
		t.Errorf("expected to search %d exchanges, searched %d", len(Exchanges), res.ExchangesSearched)
	}
	if res.FillPrice != highestBid {
		t.Errorf("expected fill price to be the highest bid %f, got %f", highestBid, res.FillPrice)
	}
}

func TestRouteOrder_DirectRouting(t *testing.T) {
	midPrice := 64500.0
	symbol := "BTC/USD"

	res := RouteOrder(symbol, "BUY", midPrice, "NYSE")
	if res.RoutedExchange != "NYSE" {
		t.Errorf("expected routed exchange to be NYSE, got %s", res.RoutedExchange)
	}
	if res.ExchangesSearched != 1 {
		t.Errorf("expected exchanges searched to be 1 for direct routing, got %d", res.ExchangesSearched)
	}

	// Verify case insensitivity and space ignoring
	res2 := RouteOrder(symbol, "BUY", midPrice, "EuronextParis")
	if res2.RoutedExchange != "Euronext Paris" {
		t.Errorf("expected routed exchange to be Euronext Paris, got %s", res2.RoutedExchange)
	}
}
