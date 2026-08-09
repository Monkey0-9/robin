package main

import (
	"testing"
	"time"
)

func TestAddTick_GapFill(t *testing.T) {
	a := &candleAccumulator{
		bars:    make(map[candleKey][]*CandleBar),
		current: make(map[candleKey]*CandleBar),
		maxBars: 500,
	}

	base := time.Unix(1000, 0)
	a.AddTick("BTC/USD", 100.0, 1.0, base)                     // 1m bar at 960
	a.AddTick("BTC/USD", 101.0, 2.0, base.Add(15*time.Second)) // same bar (1015)
	a.AddTick("BTC/USD", 105.0, 1.0, base.Add(150*time.Second)) // jumps to bar 1140

	candles := a.GetCandles("BTC/USD", "1m", 0)
	if len(candles) != 4 {
		t.Fatalf("expected 4 candles (closed, 2 gap-filled, current), got %d", len(candles))
	}

	// Gap-filled candles must be zero-volume carry-forward bars
	for _, gap := range candles[1:3] {
		if gap.Volume != 0 {
			t.Errorf("expected zero volume on gap candle, got %f", gap.Volume)
		}
		if gap.Open != 101.0 || gap.Close != 101.0 || gap.High != 101.0 || gap.Low != 101.0 {
			t.Errorf("expected carry-forward prices (101.0), got %+v", gap)
		}
	}

	// Bar series must be contiguous with no time holes
	for i := 1; i < len(candles); i++ {
		if candles[i].Time-candles[i-1].Time != 60 {
			t.Errorf("bar series not contiguous: %d -> %d", candles[i-1].Time, candles[i].Time)
		}
	}
}

func TestAddTick_NoGapWhenContiguous(t *testing.T) {
	a := &candleAccumulator{
		bars:    make(map[candleKey][]*CandleBar),
		current: make(map[candleKey]*CandleBar),
		maxBars: 500,
	}

	base := time.Unix(1000, 0)
	for i := 0; i < 3; i++ {
		a.AddTick("BTC/USD", 100.0, 1.0, base.Add(time.Duration(i)*60*time.Second))
	}

	candles := a.GetCandles("BTC/USD", "1m", 0)
	if len(candles) != 3 {
		t.Fatalf("expected exactly 3 bars for contiguous ticks, got %d", len(candles))
	}
	for _, c := range candles {
		if c.Volume == 0 {
			t.Errorf("did not expect gap-fill volume for contiguous series: %+v", c)
		}
	}
}
