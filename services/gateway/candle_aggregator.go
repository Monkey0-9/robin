package main

import (
	"math"
	"sort"
	"sync"
	"time"
)

// CandleBar is a single OHLCV bar
type CandleBar struct {
	Time   int64   `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

type candleKey struct {
	symbol     string
	resolution string
}

type candleAccumulator struct {
	mu      sync.RWMutex
	bars    map[candleKey][]*CandleBar
	current map[candleKey]*CandleBar
	maxBars int
}

var globalCandleAgg = &candleAccumulator{
	bars:    make(map[candleKey][]*CandleBar),
	current: make(map[candleKey]*CandleBar),
	maxBars: 500,
}

func resolutionSeconds(res string) int64 {
	switch res {
	case "1m":
		return 60
	case "5m":
		return 300
	case "15m":
		return 900
	case "1H":
		return 3600
	case "4H":
		return 14400
	case "1D":
		return 86400
	default:
		return 60
	}
}

// AddTick ingests a single trade tick into all active resolutions for the symbol.
func (a *candleAccumulator) AddTick(symbol string, price, volume float64, ts time.Time) {
	resolutions := []string{"1m", "5m", "15m", "1H", "4H", "1D"}
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, res := range resolutions {
		k := candleKey{symbol, res}
		periodSec := resolutionSeconds(res)
		barStart := (ts.Unix() / periodSec) * periodSec

		cur := a.current[k]
		if cur == nil || cur.Time != barStart {
			// Close out the previous bar, gap-filling any missing periods
			// between it and the new bar with empty candles so the series has
			// no time holes (charts rely on contiguous bars).
			if cur != nil {
				for t := cur.Time + periodSec; t < barStart; t += periodSec {
					empty := &CandleBar{Time: t, Open: cur.Close, High: cur.Close, Low: cur.Close, Close: cur.Close, Volume: 0}
					bars := a.bars[k]
					bars = append(bars, empty)
					if len(bars) > a.maxBars {
						bars = bars[len(bars)-a.maxBars:]
					}
					a.bars[k] = bars
				}
				bars := a.bars[k]
				bars = append(bars, cur)
				if len(bars) > a.maxBars {
					bars = bars[len(bars)-a.maxBars:]
				}
				a.bars[k] = bars
			}
			// Open new bar
			a.current[k] = &CandleBar{
				Time:   barStart,
				Open:   price,
				High:   price,
				Low:    price,
				Close:  price,
				Volume: volume,
			}
		} else {
			// Update current bar
			cur.Close = price
			if price > cur.High {
				cur.High = price
			}
			if price < cur.Low {
				cur.Low = price
			}
			cur.Volume += volume
		}
	}
}

// GetCandles returns historical candles for a symbol + resolution.
// Always includes the current in-progress bar at the end.
func (a *candleAccumulator) GetCandles(symbol, resolution string, count int) []CandleBar {
	k := candleKey{symbol, resolution}
	a.mu.RLock()
	defer a.mu.RUnlock()

	historical := a.bars[k]
	cur := a.current[k]

	result := make([]CandleBar, 0, len(historical)+1)
	for _, b := range historical {
		result = append(result, *b)
	}
	if cur != nil {
		result = append(result, *cur)
	}

	// Sort by time ascending
	sort.Slice(result, func(i, j int) bool {
		return result[i].Time < result[j].Time
	})

	// Deduplicate (same timestamp)
	deduped := result[:0]
	var lastT int64
	for _, b := range result {
		if b.Time != lastT {
			deduped = append(deduped, b)
			lastT = b.Time
		}
	}
	result = deduped

	if count > 0 && len(result) > count {
		result = result[len(result)-count:]
	}
	return result
}

// --- Wilder's RSI ---
func wilderRSI(closes []float64, period int) float64 {
	if len(closes) < period+1 {
		return 50.0
	}
	// Initial average gain/loss
	avgGain := 0.0
	avgLoss := 0.0
	for i := 1; i <= period; i++ {
		ch := closes[i] - closes[i-1]
		if ch > 0 {
			avgGain += ch
		} else {
			avgLoss -= ch
		}
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)

	// Wilder's smoothing for remaining data
	for i := period + 1; i < len(closes); i++ {
		ch := closes[i] - closes[i-1]
		gain := 0.0
		loss := 0.0
		if ch > 0 {
			gain = ch
		} else {
			loss = -ch
		}
		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
	}
	if avgLoss == 0 {
		return 100.0
	}
	rs := avgGain / avgLoss
	return 100.0 - (100.0 / (1.0 + rs))
}

// --- Proper EMA series ---
func emaFull(closes []float64, period int) []float64 {
	if len(closes) < period {
		return nil
	}
	result := make([]float64, len(closes))
	k := 2.0 / float64(period+1)
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += closes[i]
	}
	result[period-1] = sum / float64(period)
	for i := period; i < len(closes); i++ {
		result[i] = closes[i]*k + result[i-1]*(1-k)
	}
	return result
}

// --- MACD ---
type MACDResult struct {
	MACD      float64
	Signal    float64
	Histogram float64
}

func computeMACD(closes []float64, fast, slow, signal int) *MACDResult {
	if len(closes) < slow+signal {
		return nil
	}
	emaFastSeries := emaFull(closes, fast)
	emaSlowSeries := emaFull(closes, slow)
	if emaFastSeries == nil || emaSlowSeries == nil {
		return nil
	}
	macdLine := make([]float64, len(closes))
	for i := slow - 1; i < len(closes); i++ {
		macdLine[i] = emaFastSeries[i] - emaSlowSeries[i]
	}
	// Signal = EMA of macdLine
	validMacd := macdLine[slow-1:]
	signalSeries := emaFull(validMacd, signal)
	if signalSeries == nil {
		return nil
	}
	lastMACD := macdLine[len(macdLine)-1]
	lastSignal := signalSeries[len(signalSeries)-1]
	return &MACDResult{
		MACD:      lastMACD,
		Signal:    lastSignal,
		Histogram: lastMACD - lastSignal,
	}
}

// --- Bollinger Bands ---
type BBResult struct {
	Middle float64
	Upper  float64
	Lower  float64
	BWidth float64
}

func computeBollinger(closes []float64, period int, mult float64) *BBResult {
	if len(closes) < period {
		return nil
	}
	slice := closes[len(closes)-period:]
	sum := 0.0
	for _, p := range slice {
		sum += p
	}
	mean := sum / float64(period)
	variance := 0.0
	for _, p := range slice {
		d := p - mean
		variance += d * d
	}
	stddev := math.Sqrt(variance / float64(period))
	upper := mean + mult*stddev
	lower := mean - mult*stddev
	bwidth := (upper - lower) / mean
	return &BBResult{Middle: mean, Upper: upper, Lower: lower, BWidth: bwidth}
}

// --- ATR (Average True Range) ---
func computeATR(bars []CandleBar, period int) float64 {
	if len(bars) < period+1 {
		return 0
	}
	atr := 0.0
	for i := len(bars) - period; i < len(bars); i++ {
		hl := bars[i].High - bars[i].Low
		hc := math.Abs(bars[i].High - bars[i-1].Close)
		lc := math.Abs(bars[i].Low - bars[i-1].Close)
		tr := math.Max(hl, math.Max(hc, lc))
		atr += tr
	}
	return atr / float64(period)
}

// --- Stochastic %K ---
func computeStochastic(bars []CandleBar, kPeriod int) (pctK, pctD float64) {
	if len(bars) < kPeriod+3 {
		return 50, 50
	}
	// %K values for last 3 bars (for %D smoothing)
	var kVals []float64
	for d := 3; d >= 1; d-- {
		end := len(bars) - d + 1
		start := end - kPeriod
		if start < 0 {
			start = 0
		}
		slice := bars[start:end]
		lowest := slice[0].Low
		highest := slice[0].High
		for _, b := range slice {
			if b.Low < lowest {
				lowest = b.Low
			}
			if b.High > highest {
				highest = b.High
			}
		}
		if highest == lowest {
			kVals = append(kVals, 50)
		} else {
			k := (slice[len(slice)-1].Close - lowest) / (highest - lowest) * 100
			kVals = append(kVals, k)
		}
	}
	pctK = kVals[len(kVals)-1]
	// %D = SMA(3) of %K
	sum := 0.0
	for _, v := range kVals {
		sum += v
	}
	pctD = sum / float64(len(kVals))
	return
}

// --- OBV (On-Balance Volume) ---
func computeOBV(bars []CandleBar) float64 {
	obv := 0.0
	for i := 1; i < len(bars); i++ {
		if bars[i].Close > bars[i-1].Close {
			obv += bars[i].Volume
		} else if bars[i].Close < bars[i-1].Close {
			obv -= bars[i].Volume
		}
	}
	return obv
}

// --- ADX (Average Directional Index) ---
func computeADX(bars []CandleBar, period int) float64 {
	if len(bars) < period+1 {
		return 25.0
	}
	plusDM := 0.0
	minusDM := 0.0
	trSum := 0.0
	for i := len(bars) - period; i < len(bars); i++ {
		upMove := bars[i].High - bars[i-1].High
		downMove := bars[i-1].Low - bars[i].Low
		if upMove > downMove && upMove > 0 {
			plusDM += upMove
		}
		if downMove > upMove && downMove > 0 {
			minusDM += downMove
		}
		hl := bars[i].High - bars[i].Low
		hc := math.Abs(bars[i].High - bars[i-1].Close)
		lc := math.Abs(bars[i].Low - bars[i-1].Close)
		trSum += math.Max(hl, math.Max(hc, lc))
	}
	if trSum == 0 {
		return 25.0
	}
	plusDI := 100.0 * (plusDM / trSum)
	minusDI := 100.0 * (minusDM / trSum)
	diDiff := math.Abs(plusDI - minusDI)
	diSum := plusDI + minusDI
	if diSum == 0 {
		return 25.0
	}
	return 100.0 * (diDiff / diSum)
}

// FullIndicators is the complete institutional indicator set computed from real candles
type FullIndicators struct {
	Symbol    string  `json:"symbol"`
	Price     float64 `json:"price"`
	Timestamp int64   `json:"timestamp"`
	SMA20     float64 `json:"sma20"`
	EMA12     float64 `json:"ema12"`
	EMA26     float64 `json:"ema26"`
	EMA50     float64 `json:"ema50"`
	UpperBand float64 `json:"upperBand"`
	LowerBand float64 `json:"lowerBand"`
	MidBand   float64 `json:"midBand"`
	BBWidth   float64 `json:"bbWidth"`
	MACD      float64 `json:"macd"`
	MACDSig   float64 `json:"macdSignal"`
	MACDHist  float64 `json:"macdHistogram"`
	RSI       float64 `json:"rsi"`
	ATR       float64 `json:"atr"`
	StochK    float64 `json:"stochK"`
	StochD    float64 `json:"stochD"`
	VWAP      float64 `json:"vwap"`
	OBV       float64 `json:"obv"`
	ADX       float64 `json:"adx"`
}

// ComputeFullIndicators computes all indicators from real candle history
func ComputeFullIndicators(symbol string, vwap float64) *FullIndicators {
	bars := globalCandleAgg.GetCandles(symbol, "1m", 200)
	if len(bars) < 30 {
		return nil
	}
	closes := make([]float64, len(bars))
	for i, b := range bars {
		closes[i] = b.Close
	}

	currentPrice := closes[len(closes)-1]
	rsi := wilderRSI(closes, 14)

	// SMA20
	sma20 := 0.0
	if len(closes) >= 20 {
		for _, p := range closes[len(closes)-20:] {
			sma20 += p
		}
		sma20 /= 20
	}

	ema12series := emaFull(closes, 12)
	ema26series := emaFull(closes, 26)
	ema50series := emaFull(closes, 50)

	ema12 := 0.0
	ema26 := 0.0
	ema50 := 0.0
	if ema12series != nil {
		ema12 = ema12series[len(ema12series)-1]
	}
	if ema26series != nil {
		ema26 = ema26series[len(ema26series)-1]
	}
	if ema50series != nil && len(ema50series) >= 50 {
		ema50 = ema50series[len(ema50series)-1]
	}

	bb := computeBollinger(closes, 20, 2.0)
	upper, lower, mid, bwidth := 0.0, 0.0, 0.0, 0.0
	if bb != nil {
		upper, lower, mid, bwidth = bb.Upper, bb.Lower, bb.Middle, bb.BWidth
	}

	macdR := computeMACD(closes, 12, 26, 9)
	macdVal, macdSig, macdHist := 0.0, 0.0, 0.0
	if macdR != nil {
		macdVal, macdSig, macdHist = macdR.MACD, macdR.Signal, macdR.Histogram
	}

	atr := computeATR(bars, 14)
	stochK, stochD := computeStochastic(bars, 14)
	obv := computeOBV(bars)
	adx := computeADX(bars, 14)

	return &FullIndicators{
		Symbol:    symbol,
		Price:     currentPrice,
		Timestamp: time.Now().UnixMilli(),
		SMA20:     sma20,
		EMA12:     ema12,
		EMA26:     ema26,
		EMA50:     ema50,
		UpperBand: upper,
		LowerBand: lower,
		MidBand:   mid,
		BBWidth:   bwidth,
		MACD:      macdVal,
		MACDSig:   macdSig,
		MACDHist:  macdHist,
		RSI:       rsi,
		ATR:       atr,
		StochK:    stochK,
		StochD:    stochD,
		VWAP:      vwap,
		OBV:       obv,
		ADX:       adx,
	}
}
