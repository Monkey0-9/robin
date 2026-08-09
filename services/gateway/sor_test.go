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

func TestNbbo(t *testing.T) {
	quotes := []ExchangeQuote{
		{Exchange: "A", Bid: 100.0, Ask: 101.0},
		{Exchange: "B", Bid: 99.5, Ask: 100.5},
		{Exchange: "C", Bid: 100.5, Ask: 102.0},
	}
	bid, ask := nbbo(quotes)
	if bid != 100.5 {
		t.Errorf("expected NBBO bid 100.5, got %f", bid)
	}
	if ask != 100.5 {
		t.Errorf("expected NBBO ask 100.5, got %f", ask)
	}
}

func TestNbbo_EmptyQuotes(t *testing.T) {
	bid, ask := nbbo(nil)
	if bid != 0 || ask != 0 {
		t.Errorf("expected zero NBBO for empty quotes, got bid=%f ask=%f", bid, ask)
	}
}

func TestRouteOrder_Nbbo(t *testing.T) {
	midPrice := 64500.0

	buy := RouteOrder("BTC/USD", "BUY", midPrice, "AUTO")
	if buy.FillPrice != buy.NbboAsk {
		t.Errorf("expected fill to equal NBBO ask %f, got %f", buy.NbboAsk, buy.FillPrice)
	}
	if buy.PriceImprovementBps != 0 {
		t.Errorf("expected 0 price improvement when fill == NBBO, got %f", buy.PriceImprovementBps)
	}

	sell := RouteOrder("BTC/USD", "SELL", midPrice, "AUTO")
	if sell.FillPrice != sell.NbboBid {
		t.Errorf("expected fill to equal NBBO bid %f, got %f", sell.NbboBid, sell.FillPrice)
	}

	if buy.NbboBid <= 0 || buy.NbboAsk <= 0 {
		t.Errorf("expected positive NBBO, got bid=%f ask=%f", buy.NbboBid, buy.NbboAsk)
	}
	if buy.NbboAsk <= buy.NbboBid {
		t.Errorf("expected NBBO ask to exceed NBBO bid, got bid=%f ask=%f", buy.NbboBid, buy.NbboAsk)
	}
}
