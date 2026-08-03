package main

import (
    "sync"
    "time"
)

type Position struct {
    Symbol        string
    Side          string
    Size          float64
    AvgEntryPrice float64
    MarketPrice   float64
    UnrealizedPnL float64
    RealizedPnL   float64
    Timestamp     time.Time
}

type Portfolio struct {
    mu         sync.RWMutex
    positions  map[string]*Position
    cash       float64
    totalEquity float64
    dayPnL     float64
}

func (p *Portfolio) UpdatePrice(symbol string, price float64) {
    p.mu.Lock()
    defer p.mu.Unlock()
    
    pos, ok := p.positions[symbol]
    if !ok { return }
    
    pos.MarketPrice = price
    if pos.Side == "LONG" {
        pos.UnrealizedPnL = (price - pos.AvgEntryPrice) * pos.Size
    } else {
        pos.UnrealizedPnL = (pos.AvgEntryPrice - price) * pos.Size
    }
    
    p.recalculate()
}

func (p *Portfolio) recalculate() {
    p.totalEquity = p.cash
    p.dayPnL = 0
    for _, pos := range p.positions {
        p.totalEquity += pos.UnrealizedPnL
        p.dayPnL += pos.UnrealizedPnL
    }
}

func (p *Portfolio) GetPositions() []Position {
    p.mu.RLock()
    defer p.mu.RUnlock()
    result := make([]Position, 0, len(p.positions))
    for _, pos := range p.positions {
        result = append(result, *pos)
    }
    return result
}
