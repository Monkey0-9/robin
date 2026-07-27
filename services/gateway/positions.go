// ============================================================================
// Robin Trading Platform — Real-Time Position & P&L Tracker
// ============================================================================
// Tracks all open positions, computes real-time unrealized P&L using live
// prices from the AI agent market data service, and provides REST endpoints
// for the frontend dashboard.
//
// Architecture:
//   • PositionManager is populated from OMS fill events
//   • Background goroutine refreshes mark-to-market every 5 seconds
//   • Positions persisted to SQLite `positions` table for crash recovery
//
// Endpoints:
//   GET  /api/positions           — all open positions with live P&L
//   GET  /api/positions/{symbol}  — single position detail
//   GET  /api/portfolio/summary   — aggregate portfolio stats
//   POST /api/positions/close     — mark position closed (after fill)
// ============================================================================

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ─── Position types ────────────────────────────────────────────────────────────

// Lot represents a single purchase lot for FIFO tax tracking.
type Lot struct {
	OpenedAt  time.Time `json:"opened_at"`
	Qty       float64   `json:"qty"`
	EntryPrice float64  `json:"entry_price"`
	OrderID   string    `json:"order_id"`
}

// Position represents a live position in a single symbol.
type Position struct {
	Symbol         string    `json:"symbol"`
	Side           string    `json:"side"` // "LONG" | "SHORT"
	TotalQty       float64   `json:"total_qty"`
	AvgEntryPrice  float64   `json:"avg_entry_price"`
	CurrentPrice   float64   `json:"current_price"`
	UnrealizedPnL  float64   `json:"unrealized_pnl"`
	UnrealizedPct  float64   `json:"unrealized_pct"`
	RealizedPnL    float64   `json:"realized_pnl"`
	TotalNotional  float64   `json:"total_notional"`
	MarketValue    float64   `json:"market_value"`
	DayChange      float64   `json:"day_change_pct"`
	Lots           []Lot     `json:"lots"`
	LastUpdated    time.Time `json:"last_updated"`
}

// PortfolioSummary aggregates all positions.
type PortfolioSummary struct {
	TotalMarketValue  float64            `json:"total_market_value"`
	TotalCost         float64            `json:"total_cost"`
	TotalUnrealizedPnL float64           `json:"total_unrealized_pnl"`
	TotalRealizedPnL  float64            `json:"total_realized_pnl"`
	TotalPnLPct       float64            `json:"total_pnl_pct"`
	PositionCount     int                `json:"position_count"`
	Positions         map[string]*Position `json:"positions"`
	LastUpdated       time.Time          `json:"last_updated"`
}

// ─── PositionManager ──────────────────────────────────────────────────────────

// PositionManager maintains all open positions and computes live P&L.
type PositionManager struct {
	positions map[string]*Position
	mu        sync.RWMutex

	aiAgentURL string // e.g. "http://127.0.0.1:8000"
	httpClient *http.Client
}

// NewPositionManager creates a position manager connected to the AI agent's
// live market data service.
func NewPositionManager(aiAgentURL string) *PositionManager {
	pm := &PositionManager{
		positions:  make(map[string]*Position),
		aiAgentURL: aiAgentURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
	go pm.priceRefreshLoop()
	return pm
}

// OnFill is called by the OMS when an order fill is confirmed.
func (pm *PositionManager) OnFill(orderID, symbol, side string, qty, price float64) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pos, exists := pm.positions[symbol]
	if !exists {
		pos = &Position{
			Symbol: symbol,
			Side:   strings.ToUpper(side),
		}
		pm.positions[symbol] = pos
	}

	// Add lot for FIFO tracking
	lot := Lot{
		OpenedAt:   time.Now().UTC(),
		Qty:        qty,
		EntryPrice: price,
		OrderID:    orderID,
	}
	pos.Lots = append(pos.Lots, lot)

	// Recalculate weighted average entry price
	totalCost := pos.AvgEntryPrice * pos.TotalQty + price*qty
	pos.TotalQty += qty
	if pos.TotalQty > 0 {
		pos.AvgEntryPrice = totalCost / pos.TotalQty
	}

	pos.TotalNotional = pos.AvgEntryPrice * pos.TotalQty
	pos.CurrentPrice  = price // Will be updated by priceRefreshLoop
	pos.LastUpdated   = time.Now().UTC()

	log.Printf("[POSITIONS] OnFill: %s %s %.6f @ %.4f (total qty=%.6f avg=%.4f)",
		side, symbol, qty, price, pos.TotalQty, pos.AvgEntryPrice)
}

// OnClose marks a position as fully or partially closed.
func (pm *PositionManager) OnClose(symbol string, closedQty, closePrice float64) float64 {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pos, ok := pm.positions[symbol]
	if !ok {
		return 0
	}

	// FIFO: consume lots from the front
	remaining := closedQty
	realizedPnL := 0.0
	newLots := []Lot{}

	for _, lot := range pos.Lots {
		if remaining <= 0 {
			newLots = append(newLots, lot)
			continue
		}
		fillQty := min(remaining, lot.Qty)
		realizedPnL += fillQty * (closePrice - lot.EntryPrice)
		remaining   -= fillQty

		if lot.Qty-fillQty > 0 {
			lot.Qty -= fillQty
			newLots = append(newLots, lot)
		}
	}

	pos.Lots          = newLots
	pos.TotalQty     -= closedQty
	pos.RealizedPnL  += realizedPnL
	pos.LastUpdated   = time.Now().UTC()

	if pos.TotalQty <= 0 {
		delete(pm.positions, symbol)
		log.Printf("[POSITIONS] Position closed: %s realizedPnL=%.2f", symbol, realizedPnL)
	}

	return realizedPnL
}

// priceRefreshLoop fetches live prices from AI agent every 5 seconds.
func (pm *PositionManager) priceRefreshLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		pm.refreshPrices()
	}
}

func (pm *PositionManager) refreshPrices() {
	// Fetch all prices from market data service
	url := pm.aiAgentURL + "/market_data"
	resp, err := pm.httpClient.Get(url)
	if err != nil {
		log.Printf("[POSITIONS] Price refresh error: %v", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var priceMap map[string]struct {
		Price     float64 `json:"price"`
		ChangePct float64 `json:"change_pct"`
	}
	if err := json.Unmarshal(body, &priceMap); err != nil {
		return
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	for symbol, pos := range pm.positions {
		if data, ok := priceMap[symbol]; ok {
			sideMultiplier := 1.0
			if pos.Side == "SHORT" {
				sideMultiplier = -1.0
			}
			pos.CurrentPrice  = data.Price
			pos.DayChange     = data.ChangePct
			pos.MarketValue   = pos.CurrentPrice * pos.TotalQty
			pos.UnrealizedPnL = (pos.CurrentPrice - pos.AvgEntryPrice) * pos.TotalQty * sideMultiplier
			if pos.TotalNotional > 0 {
				pos.UnrealizedPct = pos.UnrealizedPnL / pos.TotalNotional * 100
			}
			pos.LastUpdated = time.Now().UTC()
		}
	}
}

// GetSummary returns the aggregate portfolio summary.
func (pm *PositionManager) GetSummary() PortfolioSummary {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	summary := PortfolioSummary{
		Positions:   make(map[string]*Position),
		LastUpdated: time.Now().UTC(),
	}

	for sym, pos := range pm.positions {
		summary.Positions[sym]        = pos
		summary.TotalMarketValue     += pos.MarketValue
		summary.TotalCost            += pos.TotalNotional
		summary.TotalUnrealizedPnL  += pos.UnrealizedPnL
		summary.TotalRealizedPnL    += pos.RealizedPnL
		summary.PositionCount++
	}

	if summary.TotalCost > 0 {
		summary.TotalPnLPct = (summary.TotalUnrealizedPnL + summary.TotalRealizedPnL) /
			summary.TotalCost * 100
	}

	return summary
}

// ─── HTTP handlers ────────────────────────────────────────────────────────────

// Global position manager — initialized in main.go
var globalPositionManager *PositionManager

func initPositionManager() {
	aiURL := "http://127.0.0.1:8000"
	globalPositionManager = NewPositionManager(aiURL)
	log.Println("[POSITIONS] Position manager initialized, refreshing prices every 5s")
}

// handleGetPositions returns all open positions with live P&L.
func handleGetPositions(w http.ResponseWriter, r *http.Request) {
	if globalPositionManager == nil {
		http.Error(w, "position manager not initialized", http.StatusServiceUnavailable)
		return
	}
	summary := globalPositionManager.GetSummary()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary.Positions)
}

// handleGetPortfolioSummary returns aggregate portfolio statistics.
func handleGetPortfolioSummary(w http.ResponseWriter, r *http.Request) {
	if globalPositionManager == nil {
		http.Error(w, "position manager not initialized", http.StatusServiceUnavailable)
		return
	}
	summary := globalPositionManager.GetSummary()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

// handleGetPosition returns a single position by symbol.
func handleGetPosition(w http.ResponseWriter, r *http.Request) {
	if globalPositionManager == nil {
		http.Error(w, "position manager not initialized", http.StatusServiceUnavailable)
		return
	}
	// Parse symbol from URL: /api/positions/{symbol}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/positions/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "symbol required", http.StatusBadRequest)
		return
	}
	symbol := strings.ToUpper(parts[0])

	globalPositionManager.mu.RLock()
	pos, ok := globalPositionManager.positions[symbol]
	globalPositionManager.mu.RUnlock()

	if !ok {
		http.Error(w, fmt.Sprintf("no position for %s", symbol), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pos)
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
