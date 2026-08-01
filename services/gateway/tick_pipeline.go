package main

import (
	"sync"
)

// VolumeStats accumulates trade-derived volume analytics per symbol.
type VolumeStats struct {
	TotalVolume float64
	TotalValue  float64 // price * size
	CVD         float64
}

// globalVolStats holds session volume analytics shared across all feeds.
var globalVolStats = struct {
	sync.Mutex
	stats map[string]*VolumeStats
}{stats: make(map[string]*VolumeStats)}

// ingestTrade routes a normalized tick through the shared downstream pipeline:
// volume analytics (VWAP/CVD), candle aggregation, persistence, and WS broadcasts.
func ingestTrade(hub *WebSocketHub, tick NormalizedTick) {
	if tick.Size <= 0 || tick.Price <= 0 {
		return
	}

	takerSide := tick.TakerSide()

	var totalVolume, vwap, cvd float64

	globalVolStats.Lock()
	vs, ok := globalVolStats.stats[tick.Symbol]
	if !ok {
		vs = &VolumeStats{}
		globalVolStats.stats[tick.Symbol] = vs
	}
	vs.TotalVolume += tick.Size
	vs.TotalValue += tick.Price * tick.Size
	if takerSide == "buy" {
		vs.CVD += tick.Size
	} else if takerSide == "sell" {
		vs.CVD -= tick.Size
	}
	totalVolume = vs.TotalVolume
	cvd = vs.CVD
	if vs.TotalVolume > 0 {
		vwap = vs.TotalValue / vs.TotalVolume
	}
	globalVolStats.Unlock()

	// Feed the tick into the candle aggregator (all resolutions)
	globalCandleAgg.AddTick(tick.Symbol, tick.Price, tick.Size, tick.Timestamp)

	// Persist the trade
	if globalTickLogger != nil {
		globalTickLogger.LogTrade(tick.Symbol, tick.TradeID, takerSide, tick.Price, tick.Size, tick.Timestamp)
	}

	// Compute full institutional indicators from real candle history
	if fullInds := ComputeFullIndicators(tick.Symbol, vwap); fullInds != nil {
		hub.BroadcastJSON(map[string]interface{}{
			"type": "indicators",
			"data": fullInds,
		})
	}

	// Broadcast volume stats (trade-derived VWAP + CVD)
	hub.BroadcastJSON(map[string]interface{}{
		"type": "volume_stats",
		"data": map[string]interface{}{
			"symbol": tick.Symbol,
			"volume": totalVolume,
			"vwap":   vwap,
			"cvd":    cvd,
		},
	})
}
