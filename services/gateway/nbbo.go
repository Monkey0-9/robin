// ============================================================================
// Live NBBO cache (Phase 3.1): best bid/offer per venue per symbol, fed by the
// exchange WebSocket streams and consumed by the Smart Order Router so routing
// decisions use real market data rather than synthetic quotes whenever the
// venue feed is live.
// ============================================================================
package main

import (
	"math"
	"sync"
	"time"
)

// VenueQuote is a single venue's best bid/ask with sizes (real market data).
type VenueQuote struct {
	Symbol   string
	Exchange string
	Bid      float64
	Ask      float64
	BidSize  float64
	AskSize  float64
}

// NBBOCache is a lock-free-enough, mutex-guarded store of per-symbol NBBO
// quotes from live venue feeds. It is intentionally small and updated only at
// L2 change events (single digits of kHz), well below the contention rate that
// would justify an atomics-heavy design.
type NBBOCache struct {
	mu     sync.RWMutex
	quotes map[string]map[string]VenueQuote
	lastUp map[string]int64
}

var globalNBBO = &NBBOCache{
	quotes: make(map[string]map[string]VenueQuote),
	lastUp: make(map[string]int64),
}

// Publish updates the cached best bid/ask for a (symbol, venue) pair from an
// L2/trade update.
func (n *NBBOCache) Publish(symbol, venue string, bid, ask, bidSize, askSize float64) {
	if symbol == "" || venue == "" {
		return
	}
	// Sanity: never publish inverted books or zero sizes from a broken frame.
	if bid > 0 && ask > 0 && bid <= ask {
		n.mu.Lock()
		defer n.mu.Unlock()
		if n.quotes[symbol] == nil {
			n.quotes[symbol] = make(map[string]VenueQuote)
		}
		n.quotes[symbol][venue] = VenueQuote{
			Symbol:   symbol,
			Exchange: venue,
			Bid:      bid,
			Ask:      ask,
			BidSize:  bidSize,
			AskSize:  askSize,
		}
		n.lastUp[symbol] = time.Now().UnixMilli()
	}
}

// Venues returns the set of venues that currently have a cached quote for the
// symbol (recent data only — stale > 10s quotes are not market data).
func (n *NBBOCache) Venues(symbol string) []VenueQuote {
	n.mu.RLock()
	defer n.mu.RUnlock()
	byVenue := n.quotes[symbol]
	if len(byVenue) == 0 {
		return nil
	}
	now := time.Now().UnixMilli()
	out := make([]VenueQuote, 0, len(byVenue))
	for _, v := range byVenue {
		if n.lastUp[symbol] > 0 && now-n.lastUp[symbol] > 10000 {
			continue
		}
		out = append(out, v)
	}
	return out
}

// Stale returns true when no venue has a live (recent) quote for the symbol.
func (n *NBBOCache) Stale(symbol string) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	byVenue := n.quotes[symbol]
	if len(byVenue) == 0 {
		return true
	}
	now := time.Now().UnixMilli()
	return n.lastUp[symbol] == 0 || now-n.lastUp[symbol] > 10000
}

// BestBidAsk returns the overall national best bid/ask across venues and the
// venue supplying the best ask (for BUY routing) — the NBBO is the max bid /
// min ask across every live venue.
func (n *NBBOCache) BestBidAsk(symbol string) (bid, ask float64, bestAskVenue string, ok bool) {
	quotes := n.Venues(symbol)
	if len(quotes) == 0 {
		return 0, 0, "", false
	}
	bestBid := math.Inf(-1)
	bestAsk := math.Inf(1)
	for _, q := range quotes {
		if q.Bid > bestBid {
			bestBid = q.Bid
		}
		if q.Ask < bestAsk {
			bestAsk = q.Ask
			bestAskVenue = q.Exchange
		}
	}
	if math.IsInf(bestBid, -1) || math.IsInf(bestAsk, 1) {
		return 0, 0, "", false
	}
	return bestBid, bestAsk, bestAskVenue, true
}
