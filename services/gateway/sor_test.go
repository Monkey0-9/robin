package main

import (
	"testing"
	"time"
)

func TestNbbo_ComputesNationalBestBidAsk(t *testing.T) {
	quotes := []ExchangeQuote{
		{Exchange: "Coinbase", Bid: 100.0, Ask: 101.0},
		{Exchange: "Binance", Bid: 99.5, Ask: 100.5},
		{Exchange: "Kraken", Bid: 100.5, Ask: 102.0},
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

func freshNBBO(t *testing.T) {
	t.Helper()
	globalNBBO = &NBBOCache{
		quotes: make(map[string]map[string]VenueQuote),
		lastUp: make(map[string]int64),
	}
}

func publish(symbol, venue string, bid, ask float64) {
	globalNBBO.Publish(symbol, venue, bid, ask, 1.0, 1.0)
}

func TestRouteOrder_UsesLiveNBBOWhenAvailable(t *testing.T) {
	freshNBBO(t)
	publish("BTC/USD", "Coinbase", 64495.0, 64505.0)
	publish("BTC/USD", "Binance", 64490.0, 64510.0)

	res, ok := RouteOrder("BTC/USD", "BUY", 64500.0, "AUTO")
	if !ok {
		t.Error("expected true ok status")
	}
	if res.FillPrice != 64505.0 {
		t.Errorf("expected fill price to match live NBBO ask 64505.0, got %f", res.FillPrice)
	}
	if res.RoutedExchange != "Coinbase" {
		t.Errorf("expected RoutedExchange Coinbase (best ask venue), got %s", res.RoutedExchange)
	}
	if res.IsSimulated {
		t.Error("expected IsSimulated to be false when routing on live quotes")
	}
}

func TestRouteOrder_SellUsesBestBidVenue(t *testing.T) {
	freshNBBO(t)
	publish("ETH/USD", "Binance", 3400.0, 3402.0)
	publish("ETH/USD", "Kraken", 3399.0, 3401.0)

	res, ok := RouteOrder("ETH/USD", "SELL", 3400.0, "AUTO")
	if !ok {
		t.Fatal("expected true ok status")
	}
	if res.FillPrice != 3400.0 {
		t.Errorf("expected fill price to match live NBBO bid 3400.0, got %f", res.FillPrice)
	}
	if res.RoutedExchange != "Binance" {
		t.Errorf("expected RoutedExchange Binance (best bid venue), got %s", res.RoutedExchange)
	}
}

func TestRouteOrder_PreferredExchangeHonoredAtNBBO(t *testing.T) {
	freshNBBO(t)
	publish("BTC/USD", "Coinbase", 64495.0, 64505.0)
	publish("BTC/USD", "NYSE", 64495.0, 64505.0) // quotes at the NBBO
	publish("BTC/USD", "KRK", 64490.0, 64520.0)

	res, ok := RouteOrder("BTC/USD", "BUY", 64500.0, "NYSE")
	if !ok {
		t.Fatal("expected true ok status")
	}
	if res.RoutedExchange != "NYSE" {
		t.Errorf("expected preferred NYSE to be used, got %s", res.RoutedExchange)
	}
}

func TestRouteOrder_ReturnsFalseWhenNoLiveQuotes(t *testing.T) {
	freshNBBO(t)
	_, ok := RouteOrder("BTC/USD", "BUY", 64500.0, "AUTO")
	if ok {
		t.Error("expected false ok status when no live quotes available")
	}
}

func TestRouteOrder_IgnoresStaleQuotes(t *testing.T) {
	freshNBBO(t)
	publish("BTC/USD", "Coinbase", 64000.0, 64010.0)
	// Artificially age the cache entry beyond the 10s live window.
	globalNBBO.mu.Lock()
	globalNBBO.lastUp["BTC/USD"] = time.Now().UnixMilli() - 30_000
	globalNBBO.mu.Unlock()

	_, ok := RouteOrder("BTC/USD", "BUY", 64000.0, "AUTO")
	if ok {
		t.Error("expected stale quotes to be treated as no live market data")
	}
}
