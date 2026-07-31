package portfolio

import (
	"sync"
)

type Position struct {
	Symbol        string
	Side          string // "LONG" or "SHORT"
	Size          float64
	AvgEntryPrice float64
	MarketPrice   float64
	UnrealizedPnL float64
	RealizedPnL   float64
}

type PortfolioManager struct {
	mu          sync.RWMutex
	positions   map[string]*Position
	cash        float64
	marginUsed  float64
	totalEquity float64
}

func NewPortfolioManager(initialCash float64) *PortfolioManager {
	return &PortfolioManager{
		positions:   make(map[string]*Position),
		cash:        initialCash,
		totalEquity: initialCash,
	}
}

func (p *PortfolioManager) UpdateMarketPrice(symbol string, price float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	pos, exists := p.positions[symbol]
	if !exists {
		return
	}

	pos.MarketPrice = price
	if pos.Side == "LONG" {
		pos.UnrealizedPnL = (price - pos.AvgEntryPrice) * pos.Size
	} else if pos.Side == "SHORT" {
		pos.UnrealizedPnL = (pos.AvgEntryPrice - price) * pos.Size
	}

	p.recalculateTotals()
}

func (p *PortfolioManager) recalculateTotals() {
	var totalUnrealized float64 = 0
	for _, pos := range p.positions {
		totalUnrealized += pos.UnrealizedPnL
	}
	p.totalEquity = p.cash + totalUnrealized
}

func (p *PortfolioManager) GetTotalEquity() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.totalEquity
}
