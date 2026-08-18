package main

import (
	"math"
	"sort"
	"strings"
)

// ============================================================================
// Smart Order Router (SOR) & Multi-Venue Optimization Engine
// services/gateway/sor.go
// ============================================================================
// Implements best-execution routing across global venues with:
//   1. Consolidated NBBO evaluation across all quoting venues.
//   2. Net execution cost optimization = Price ± Fee/Rebate + Latency Penalty.
//   3. Multi-venue order splitting when quantity exceeds top-of-book depth.
//   4. Specific venue preference override with price-improvement verification.
// ============================================================================

// Exchanges is the static list of global exchanges supported by the SOR
var Exchanges = []string{
	"Coinbase", "Binance", "Kraken", "NYSE", "NASDAQ", "Euronext Paris", "LSE",
}

// VenueFeeModel defines maker/taker fee rates (in basis points) and expected latency (ms)
type VenueFeeModel struct {
	TakerFeeBps float64
	MakerRebate float64
	LatencyMs   float64
}

// ExchangeQuote is an alias to VenueQuote for backwards compatibility
type ExchangeQuote = VenueQuote

var VenueProfiles = map[string]VenueFeeModel{
	"Coinbase":       {TakerFeeBps: 5.0, MakerRebate: 1.5, LatencyMs: 12.0},
	"Binance":        {TakerFeeBps: 4.0, MakerRebate: 2.0, LatencyMs: 8.0},
	"Kraken":         {TakerFeeBps: 6.0, MakerRebate: 1.0, LatencyMs: 15.0},
	"NYSE":           {TakerFeeBps: 3.0, MakerRebate: 2.5, LatencyMs: 2.0},
	"NASDAQ":         {TakerFeeBps: 3.0, MakerRebate: 2.7, LatencyMs: 1.8},
	"Euronext Paris": {TakerFeeBps: 3.5, MakerRebate: 2.0, LatencyMs: 18.0},
	"LSE":            {TakerFeeBps: 3.2, MakerRebate: 2.2, LatencyMs: 16.0},
}

// VenueSplitAllocation represents a partial fill allocated to a single venue
type VenueSplitAllocation struct {
	Venue    string  `json:"venue"`
	AllocQty float64 `json:"alloc_qty"`
	Price    float64 `json:"price"`
	FeeBps   float64 `json:"fee_bps"`
}

// RoutingResult holds execution metadata for the routed order
type RoutingResult struct {
	RoutedExchange      string                 `json:"routed_exchange"`
	FillPrice           float64                `json:"fill_price"`
	ExchangesSearched   int                    `json:"exchanges_searched"`
	PriceImprovementBps float64                `json:"price_improvement_bps"`
	AverageMarketPrice  float64                `json:"average_market_price"`
	NbboBid             float64                `json:"nbbo_bid"`
	NbboAsk             float64                `json:"nbbo_ask"`
	IsSimulated         bool                   `json:"is_simulated"`
	Splits              []VenueSplitAllocation `json:"splits,omitempty"`
}

// nbbo computes the National Best Bid/Offer across all quoted venues.
func nbbo(quotes []VenueQuote) (bid, ask float64) {
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
func RouteOrder(symbol string, side string, midPrice float64, preferredExchange string) (RoutingResult, bool) {
	if nbboBid, nbboAsk, bestAskVenue, ok := globalNBBO.BestBidAsk(symbol); ok {
		return routeOnLiveQuotes(symbol, side, nbboBid, nbboAsk, bestAskVenue, preferredExchange), true
	}
	return RoutingResult{}, false
}

// RouteOrderWithSplit performs cost-optimal multi-venue order routing for large orders
func RouteOrderWithSplit(symbol string, side string, totalQty float64, preferredExchange string) (RoutingResult, bool) {
	quotes := globalNBBO.Venues(symbol)
	if len(quotes) == 0 {
		return RoutingResult{}, false
	}

	nbboBid, nbboAsk := nbbo(quotes)
	if nbboBid <= 0 || nbboAsk <= 0 {
		return RoutingResult{}, false
	}

	mid := (nbboBid + nbboAsk) / 2.0
	isBuy := strings.ToUpper(side) == "BUY"

	type ScoredQuote struct {
		Quote    VenueQuote
		NetPrice float64
		AvailQty float64
	}

	var scored []ScoredQuote
	for _, q := range quotes {
		profile, exists := VenueProfiles[q.Exchange]
		if !exists {
			profile = VenueFeeModel{TakerFeeBps: 5.0, LatencyMs: 10.0}
		}

		feeMultiplier := 1.0 + (profile.TakerFeeBps / 10000.0)
		latencyCost := (profile.LatencyMs / 1000.0) * 0.0001 * mid

		var rawPrice, availQty, netPrice float64
		if isBuy {
			rawPrice = q.Ask
			availQty = q.AskSize
			netPrice = (rawPrice * feeMultiplier) + latencyCost
		} else {
			rawPrice = q.Bid
			availQty = q.BidSize
			netPrice = (rawPrice * (1.0 - profile.TakerFeeBps/10000.0)) - latencyCost
		}

		if rawPrice > 0 && availQty > 0 {
			scored = append(scored, ScoredQuote{
				Quote:    q,
				NetPrice: netPrice,
				AvailQty: availQty,
			})
		}
	}

	// Sort: ascending for BUY (cheapest first), descending for SELL (highest first)
	sort.Slice(scored, func(i, j int) bool {
		if isBuy {
			return scored[i].NetPrice < scored[j].NetPrice
		}
		return scored[i].NetPrice > scored[j].NetPrice
	})

	if len(scored) == 0 {
		return routeOnLiveQuotes(symbol, side, nbboBid, nbboAsk, quotes[0].Exchange, preferredExchange), true
	}

	remaining := totalQty
	if remaining <= 0 {
		remaining = 1.0
	}

	var splits []VenueSplitAllocation
	var totalWeightedPrice float64
	var totalFilled float64

	for _, s := range scored {
		if remaining <= 0 {
			break
		}
		fillQty := math.Min(remaining, s.AvailQty)
		if fillQty <= 0 {
			continue
		}

		var fillPx float64
		if isBuy {
			fillPx = s.Quote.Ask
		} else {
			fillPx = s.Quote.Bid
		}

		profile := VenueProfiles[s.Quote.Exchange]
		splits = append(splits, VenueSplitAllocation{
			Venue:    s.Quote.Exchange,
			AllocQty: fillQty,
			Price:    fillPx,
			FeeBps:   profile.TakerFeeBps,
		})

		totalWeightedPrice += fillPx * fillQty
		totalFilled += fillQty
		remaining -= fillQty
	}

	avgFillPrice := totalWeightedPrice / math.Max(totalFilled, 1.0)

	var improvementBps float64
	if mid > 0 {
		if isBuy {
			improvementBps = math.Max(0.0, ((mid-avgFillPrice)/mid)*10000.0)
		} else {
			improvementBps = math.Max(0.0, ((avgFillPrice-mid)/mid)*10000.0)
		}
	}

	primaryVenue := scored[0].Quote.Exchange
	if len(splits) > 0 {
		primaryVenue = splits[0].Venue
	}

	return RoutingResult{
		RoutedExchange:      primaryVenue,
		FillPrice:           avgFillPrice,
		ExchangesSearched:   len(quotes),
		PriceImprovementBps: math.Round(improvementBps*100) / 100,
		AverageMarketPrice:  mid,
		NbboBid:             nbboBid,
		NbboAsk:             nbboAsk,
		IsSimulated:         false,
		Splits:              splits,
	}, true
}

// routeOnLiveQuotes routes a single order to the best quoting venue
func routeOnLiveQuotes(symbol, side string, nbboBid, nbboAsk float64, bestAskVenue, preferredExchange string) RoutingResult {
	venue := bestAskVenue
	if side != "BUY" {
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
