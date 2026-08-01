package portfolio

import (
	"fmt"
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
	lots          []Lot // FIFO cost basis lots
}

type Lot struct {
	Qty    float64
	Price  float64
}

type Trade struct {
	Symbol string
	Side   string // "BUY" or "SELL"
	Qty    float64
	Price  float64
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

// ExecuteTrade processes a fill against FIFO lots and updates realized P&L.
// BUY adds a new lot; SELL/SHORT closes against the oldest lots first.
func (p *PortfolioManager) ExecuteTrade(t Trade) (realized float64, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if t.Qty <= 0 {
		return 0, fmt.Errorf("trade qty must be positive")
	}

	pos, exists := p.positions[t.Symbol]
	if !exists {
		side := "LONG"
		if t.Side == "SELL" {
			side = "SHORT"
		}
		pos = &Position{Symbol: t.Symbol, Side: side}
		p.positions[t.Symbol] = pos
	}

	switch t.Side {
	case "BUY":
		pos.lots = append(pos.lots, Lot{Qty: t.Qty, Price: t.Price})
		pos.Size += t.Qty
		pos.Side = "LONG"
		// Recompute weighted average entry price across all lots
		totalQty := 0.0
		totalCost := 0.0
		for _, l := range pos.lots {
			totalQty += l.Qty
			totalCost += l.Qty * l.Price
		}
		if totalQty > 0 {
			pos.AvgEntryPrice = totalCost / totalQty
		}
		p.cash -= t.Qty * t.Price

	case "SELL":
		if pos.Size < t.Qty {
			return 0, fmt.Errorf("insufficient position: have %.6f, sell %.6f", pos.Size, t.Qty)
		}
		remaining := t.Qty
		realized = 0.0
		// FIFO: close oldest lots first
		kept := make([]Lot, 0, len(pos.lots))
		for _, l := range pos.lots {
			if remaining <= 0 {
				kept = append(kept, l)
				continue
			}
			closeQty := l.Qty
			if closeQty > remaining {
				closeQty = remaining
			}
			realized += (t.Price - l.Price) * closeQty
			remaining -= closeQty
			if l.Qty > closeQty {
				kept = append(kept, Lot{Qty: l.Qty - closeQty, Price: l.Price})
			}
		}
		pos.lots = kept
		pos.Size -= t.Qty
		pos.RealizedPnL += realized
		p.cash += t.Qty * t.Price
		// Recompute avg entry from remaining lots (partial FIFO close)
		totalQty := 0.0
		totalCost := 0.0
		for _, l := range pos.lots {
			totalQty += l.Qty
			totalCost += l.Qty * l.Price
		}
		if totalQty > 0 {
			pos.AvgEntryPrice = totalCost / totalQty
		}
		if pos.Size == 0 {
			pos.Side = "LONG"
		}

	default:
		return 0, fmt.Errorf("unsupported side: %s", t.Side)
	}

	p.recalculateTotals()
	return realized, nil
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

func (p *PortfolioManager) GetCash() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cash
}

func (p *PortfolioManager) GetPosition(symbol string) (*Position, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	pos, ok := p.positions[symbol]
	return pos, ok
}
