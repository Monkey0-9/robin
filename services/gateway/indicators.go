package main

// indicators.go — Legacy bridge: IndicatorEngine using price-only history.
// All real indicator math is now in candle_aggregator.go (ComputeFullIndicators).
// This file keeps the IndicatorEngine type alive so existing callers compile.

import (
	"sync"
)

type IndicatorEngine struct {
	mu     sync.Mutex
	prices map[string][]float64
}

var globalIndicators = &IndicatorEngine{
	prices: make(map[string][]float64),
}

// AddPrice adds a tick price and computes indicators from candle_aggregator.
// Returns nil when real candle data is not yet available (< 30 bars).
func (ie *IndicatorEngine) AddPrice(symbol string, price float64) map[string]float64 {
	ie.mu.Lock()
	history := ie.prices[symbol]
	history = append(history, price)
	if len(history) > 300 {
		history = history[len(history)-300:]
	}
	ie.prices[symbol] = history
	ie.mu.Unlock()

	// Use the institutional-grade computations from candle_aggregator.go
	// which operate on real OHLCV bars, not just price ticks.
	inds := ComputeFullIndicators(symbol, 0)
	if inds == nil {
		return nil
	}
	return map[string]float64{
		"sma20":     inds.SMA20,
		"upperBand": inds.UpperBand,
		"lowerBand": inds.LowerBand,
		"macd":      inds.MACD,
		"rsi":       inds.RSI,
		"ema12":     inds.EMA12,
		"ema26":     inds.EMA26,
		"ema50":     inds.EMA50,
		"atr":       inds.ATR,
		"stochK":    inds.StochK,
		"stochD":    inds.StochD,
	}
}
