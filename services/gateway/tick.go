package main

import (
	"strings"
	"time"
)

// NormalizedTick is the canonical tick structure for all venues (Binance, Alpaca, Coinbase, OANDA)
type NormalizedTick struct {
	Symbol    string    `json:"symbol"`
	Venue     string    `json:"venue"` // "binance", "alpaca", "coinbase", "oanda"
	Price     float64   `json:"price"`
	Size      float64   `json:"size"`
	Side      string    `json:"side"` // taker/aggressor side: "buy" | "sell"
	Timestamp time.Time `json:"timestamp"`
	TradeID   string    `json:"trade_id,omitempty"`
}

// TakerSide returns the aggressor side of the tick, normalizing to "buy"/"sell".
func (t NormalizedTick) TakerSide() string {
	s := strings.ToLower(t.Side)
	if s == "buy" || s == "sell" {
		return s
	}
	if s == "b" || s == "long" {
		return "buy"
	}
	if s == "s" || s == "short" {
		return "sell"
	}
	return "buy"
}
