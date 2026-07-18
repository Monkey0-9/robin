package main

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"math/rand"
	"strings"
	"time"
)

// Exchanges is the static list of 30 global exchanges supported by the SOR simulator
var Exchanges = []string{
	"Tradegate", "Xetra", "NYSE", "NASDAQ", "LSE",
	"Euronext Paris", "Euronext Amsterdam", "Euronext Brussels", "Euronext Dublin", "Euronext Lisbon",
	"Borsa Italiana", "SIX Swiss Exchange", "BME Madrid", "DirectEdge", "BATS",
	"Chi-X", "Tradeweb", "Instinet", "Liquidnet", "Turquoise",
	"Aquis Exchange", "Nasdaq Stockholm", "Nasdaq Copenhagen", "Nasdaq Helsinki", "Börse Frankfurt",
	"Börse Stuttgart", "Börse München", "Börse Düsseldorf", "Börse Hamburg", "Robin Pools",
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

// GenerateQuotes generates deterministic simulated quotes across 30 exchanges for a symbol
func GenerateQuotes(symbol string, midPrice float64) []ExchangeQuote {
	quotes := make([]ExchangeQuote, len(Exchanges))

	// Create a stable seed using symbol name
	h := sha256.New()
	h.Write([]byte(symbol))
	seedBase := binary.BigEndian.Uint64(h.Sum(nil)[:8])

	// Tick bucket updates every 200ms to allow visual simulation without excessive speed
	nowBucket := time.Now().UnixNano() / int64(200*time.Millisecond)

	for i, ex := range Exchanges {
		// Unique seed per exchange per time bucket
		exSeed := seedBase ^ uint64(i) ^ uint64(nowBucket)
		r := rand.New(rand.NewSource(int64(exSeed)))

		// 1bps of mid price
		bps := midPrice / 10000.0
		if bps < 0.0001 {
			bps = 0.0001
		}

		// Shift base price slightly per exchange (up to ±10 bps)
		shiftBps := (r.Float64() - 0.5) * 20.0
		exMid := midPrice + shiftBps*bps

		// Spread ranges from 1 bps to 5 bps
		spreadBps := 1.0 + r.Float64()*4.0
		spread := spreadBps * bps

		bid := exMid - spread/2.0
		ask := exMid + spread/2.0

		// Format decimals based on asset pricing
		if symbol != "EUR/USD" {
			bid = math.Round(bid*100) / 100
			ask = math.Round(ask*100) / 100
		} else {
			bid = math.Round(bid*10000) / 10000
			ask = math.Round(ask*10000) / 10000
		}

		// Size ranges from 0.1 to 10.0
		bidSize := math.Round((0.1+r.Float64()*9.9)*100) / 100
		askSize := math.Round((0.1+r.Float64()*9.9)*100) / 100

		quotes[i] = ExchangeQuote{
			Exchange: ex,
			Bid:      bid,
			Ask:      ask,
			BidSize:  bidSize,
			AskSize:  askSize,
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
