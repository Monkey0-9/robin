package main

import (
	"math"
	"strings"
)

// Exchanges is the static list of global exchanges supported by the SOR
var Exchanges = []string{
	"Coinbase", "Binance", "Kraken", "NYSE", "Euronext Paris",
}

// ExchangeQuote represents a simulated quote from a specific exchange
type ExchangeQuote struct {
	Exchange    string  `json:"exchange"`
	Bid         float64 `json:"bid"`
	Ask         float64 `json:"ask"`
	BidSize     float64 `json:"bid_size"`
	AskSize     float64 `json:"ask_size"`
	IsSimulated bool    `json:"is_simulated"`
}

// RoutingResult holds execution metadata for the routed order
type RoutingResult struct {
	RoutedExchange      string  `json:"routed_exchange"`
	FillPrice           float64 `json:"fill_price"`
	ExchangesSearched   int     `json:"exchanges_searched"`
	PriceImprovementBps float64 `json:"price_improvement_bps"`
	AverageMarketPrice  float64 `json:"average_market_price"`
	NbboBid             float64 `json:"nbbo_bid"`
	NbboAsk             float64 `json:"nbbo_ask"`
	IsSimulated         bool    `json:"is_simulated"`
}

// nbbo computes the National Best Bid/Offer across all quoted exchanges.
// NBBO bid is the highest bid; NBBO offer is the lowest ask.
func nbbo(quotes []ExchangeQuote) (bid, ask float64) {
	bid = math.Inf(-1)
	ask = math.Inf(1)
	for _, q := range quotes {
		if q.Bid > bid {
			bid = q.Bid
		}
		if q.Ask < ask {
			ask = q.Ask
		}
	}
	if math.IsInf(bid, -1) {
		bid = 0
	}
	if math.IsInf(ask, 1) {
		ask = 0
	}
	return bid, ask
}

// RouteOrder selects the best price or directly routes an order to a preferred exchange.
// It strictly uses live venue quotes from the NBBO cache.
func RouteOrder(symbol string, side string, midPrice float64, preferredExchange string) (RoutingResult, bool) {
	if nbboBid, nbboAsk, bestAskVenue, ok := globalNBBO.BestBidAsk(symbol); ok {
		return routeOnLiveQuotes(symbol, side, nbboBid, nbboAsk, bestAskVenue, preferredExchange), true
	}
	// No live quotes available; refuse to route on synthetic data.
	return RoutingResult{}, false
}

// routeOnLiveQuotes routes a BUY to the venue posting the national best ask and
// a SELL to the venue posting the national best bid. Price improvement is
// measured against the mid of the consolidated NBBO.
func routeOnLiveQuotes(symbol, side string, nbboBid, nbboAsk float64, bestAskVenue, preferredExchange string) RoutingResult {
	venue := bestAskVenue
	if side != "BUY" {
		// For sells the best venue is the one with the highest bid.
		quotes := globalNBBO.Venues(symbol)
		bestBid := math.Inf(-1)
		for _, q := range quotes {
			if q.Bid > bestBid {
				bestBid = q.Bid
				venue = q.Exchange
			}
		}
	}

	var fill float64
	if side == "BUY" {
		fill = nbboAsk
	} else {
		fill = nbboBid
	}
	mid := (nbboBid + nbboAsk) / 2
	var improvementBps float64
	if mid > 0 {
		if side == "BUY" {
			improvementBps = ((mid - fill) / mid) * 10000.0
		} else {
			improvementBps = ((fill - mid) / mid) * 10000.0
		}
	}
	if improvementBps < 0 {
		improvementBps = 0
	}

	exchanges := globalNBBO.Venues(symbol)

	// Honor a specific preferred exchange: prefer it when it quotes at the NBBO.
	if strings.TrimSpace(strings.ToUpper(preferredExchange)) != "" &&
		strings.ToUpper(preferredExchange) != "AUTO" {
		pref := strings.ReplaceAll(strings.ToUpper(preferredExchange), " ", "")
		for _, q := range exchanges {
			if strings.ReplaceAll(strings.ToUpper(q.Exchange), " ", "") == pref {
				var pfill float64
				if side == "BUY" {
					pfill = q.Ask
				} else {
					pfill = q.Bid
				}
				// Only use the preferred venue if it actually improves the price.
				if (side == "BUY" && pfill <= nbboAsk) || (side != "BUY" && pfill >= nbboBid) {
					venue = q.Exchange
					fill = pfill
				}
			}
		}
	}

	return RoutingResult{
		RoutedExchange:      venue,
		FillPrice:           fill,
		ExchangesSearched:   len(exchanges),
		PriceImprovementBps: math.Round(improvementBps*100) / 100,
		AverageMarketPrice:  mid,
		NbboBid:             nbboBid,
		NbboAsk:             nbboAsk,
		IsSimulated:         false,
	}
}
