package main

import (
	"math"
	"strings"
)

// Exchanges is the static list of global exchanges supported by the SOR
var Exchanges = []string{
	"Coinbase", "Binance", "Kraken",
}

// ExchangeQuote represents a simulated quote from a specific exchange
type ExchangeQuote struct {
	Exchange string  `json:"exchange"`
	Bid      float64 `json:"bid"`
	Ask      float64 `json:"ask"`
	BidSize  float64 `json:"bid_size"`
	AskSize  float64 `json:"ask_size"`
}

// RoutingResult holds execution metadata for the routed order
type RoutingResult struct {
	RoutedExchange      string  `json:"routed_exchange"`
	FillPrice           float64 `json:"fill_price"`
	ExchangesSearched   int     `json:"exchanges_searched"`
	PriceImprovementBps float64 `json:"price_improvement_bps"`
	AverageMarketPrice  float64 `json:"average_market_price"`
}

// GenerateQuotes returns the current market price mapped across available exchanges
func GenerateQuotes(symbol string, midPrice float64) []ExchangeQuote {
	quotes := make([]ExchangeQuote, len(Exchanges))
	
	// Default spread (1 bps) since we don't have L2 books cached per-exchange in Go yet
	bps := midPrice / 10000.0
	if bps < 0.0001 {
		bps = 0.0001
	}

	for i, ex := range Exchanges {
		bid := midPrice - bps/2.0
		ask := midPrice + bps/2.0

		// Format decimals based on asset pricing
		if symbol != "EUR/USD" {
			bid = math.Round(bid*100) / 100
			ask = math.Round(ask*100) / 100
		} else {
			bid = math.Round(bid*10000) / 10000
			ask = math.Round(ask*10000) / 10000
		}

		quotes[i] = ExchangeQuote{
			Exchange: ex,
			Bid:      bid,
			Ask:      ask,
			BidSize:  1.0,
			AskSize:  1.0,
		}
	}
	return quotes
}

// RouteOrder selects the best price or directly routes an order to a preferred exchange
func RouteOrder(symbol string, side string, midPrice float64, preferredExchange string) RoutingResult {
	quotes := GenerateQuotes(symbol, midPrice)
	pref := strings.TrimSpace(strings.ToUpper(preferredExchange))

	// Direct routing to a specific exchange
	if pref != "" && pref != "AUTO" {
		for _, q := range quotes {
			normEx := strings.ReplaceAll(strings.ToUpper(q.Exchange), " ", "")
			normPref := strings.ReplaceAll(pref, " ", "")
			if normEx == normPref {
				var fill float64
				if side == "BUY" {
					fill = q.Ask
				} else {
					fill = q.Bid
				}

				// Compute average across all exchanges for comparison
				var sumPrice float64
				for _, oq := range quotes {
					if side == "BUY" {
						sumPrice += oq.Ask
					} else {
						sumPrice += oq.Bid
					}
				}
				avgPrice := sumPrice / float64(len(quotes))

				var improvementBps float64
				if side == "BUY" {
					improvementBps = ((avgPrice - fill) / avgPrice) * 10000.0
				} else {
					improvementBps = ((fill - avgPrice) / avgPrice) * 10000.0
				}

				return RoutingResult{
					RoutedExchange:      q.Exchange,
					FillPrice:           fill,
					ExchangesSearched:   1,
					PriceImprovementBps: math.Round(improvementBps*100) / 100,
					AverageMarketPrice:  avgPrice,
				}
			}
		}
	}

	// Smart Order Routing (Best Price Selection)
	var bestIdx = 0
	var sumPrice float64

	for i, q := range quotes {
		if side == "BUY" {
			sumPrice += q.Ask
			if q.Ask < quotes[bestIdx].Ask {
				bestIdx = i
			}
		} else {
			sumPrice += q.Bid
			if q.Bid > quotes[bestIdx].Bid {
				bestIdx = i
			}
		}
	}

	avgPrice := sumPrice / float64(len(quotes))
	bestQuote := quotes[bestIdx]

	var fillPrice float64
	if side == "BUY" {
		fillPrice = bestQuote.Ask
	} else {
		fillPrice = bestQuote.Bid
	}

	var improvementBps float64
	if side == "BUY" {
		improvementBps = ((avgPrice - fillPrice) / avgPrice) * 10000.0
	} else {
		improvementBps = ((fillPrice - avgPrice) / avgPrice) * 10000.0
	}

	if improvementBps < 0 {
		improvementBps = 0 // Safeguard
	}

	return RoutingResult{
		RoutedExchange:      bestQuote.Exchange,
		FillPrice:           fillPrice,
		ExchangesSearched:   len(Exchanges),
		PriceImprovementBps: math.Round(improvementBps*100) / 100,
		AverageMarketPrice:  avgPrice,
	}
}
