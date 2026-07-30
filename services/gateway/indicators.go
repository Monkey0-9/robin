package main

import (
	"math"
	"sync"
)

type IndicatorEngine struct {
	mu     sync.Mutex
	prices map[string][]float64
}

var globalIndicators = &IndicatorEngine{
	prices: make(map[string][]float64),
}

func (ie *IndicatorEngine) AddPrice(symbol string, price float64) map[string]float64 {
	ie.mu.Lock()
	defer ie.mu.Unlock()

	history := ie.prices[symbol]
	history = append(history, price)
	if len(history) > 100 {
		history = history[len(history)-100:] // keep last 100
	}
	ie.prices[symbol] = history

	if len(history) < 20 {
		return nil
	}

	// Calculate SMA 20
	sum := 0.0
	for _, p := range history[len(history)-20:] {
		sum += p
	}
	sma20 := sum / 20.0

	// Calculate Bollinger Bands (20, 2)
	variance := 0.0
	for _, p := range history[len(history)-20:] {
		variance += math.Pow(p-sma20, 2)
	}
	stdDev := math.Sqrt(variance / 20.0)
	upperBand := sma20 + (2.0 * stdDev)
	lowerBand := sma20 - (2.0 * stdDev)

	// Calculate EMA 12 and EMA 26 for MACD
	ema12 := calculateEMA(history, 12)
	ema26 := calculateEMA(history, 26)
	macd := ema12 - ema26

	// Calculate RSI 14
	rsi := calculateRSI(history, 14)

	return map[string]float64{
		"sma20":     sma20,
		"upperBand": upperBand,
		"lowerBand": lowerBand,
		"macd":      macd,
		"rsi":       rsi,
	}
}

func calculateEMA(prices []float64, period int) float64 {
	if len(prices) < period {
		return prices[len(prices)-1]
	}
	k := 2.0 / float64(period+1)
	ema := prices[len(prices)-period]
	for i := len(prices) - period + 1; i < len(prices); i++ {
		ema = (prices[i] * k) + (ema * (1.0 - k))
	}
	return ema
}

func calculateRSI(prices []float64, period int) float64 {
	if len(prices) <= period {
		return 50.0
	}
	var gains, losses float64
	for i := len(prices) - period; i < len(prices); i++ {
		change := prices[i] - prices[i-1]
		if change > 0 {
			gains += change
		} else {
			losses -= change
		}
	}
	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)
	if avgLoss == 0 {
		return 100.0
	}
	rs := avgGain / avgLoss
	return 100.0 - (100.0 / (1.0 + rs))
}
