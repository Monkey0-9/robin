// ============================================================================
// Live market-data feed into the risk engine (Phase 2 wiring).
//
// The Rust risk daemon maintains live reference prices, the Reg SHO (Rule 201)
// short-sale circuit breaker, the EWMA correlation monitor, and unrealized P&L
// mark for every open position. Those engines only fire when the daemon is fed
// price ticks — this forwarder streams the gateway's live NBBO/best prices to
// the daemon's control-plane TCP port (9092) so the risk checks run against
// real market data instead of sitting idle.
//
// Protocol (see services/risk-analytics/src/main.rs):
//
//	{"cmd":"previous_close","instrument_id":N,"price":TICKS}
//	{"cmd":"quote","instrument_id":N,"price":TICKS}
//
// Prices are transmitted in 1e8 tick scale, matching the risk Order struct.
// ============================================================================
package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

// RiskInstrumentID maps a market-data symbol to the risk daemon's instrument
// slot. Must stay in sync with the reference prices seeded in the daemon.
func RiskInstrumentID(symbol string) (uint32, bool) {
	switch symbol {
	case "BTC/USD", "BTC/USDT":
		return 1, true
	case "ETH/USD", "ETH/USDT":
		return 2, true
	case "AAPL":
		return 3, true
	case "EUR/USD":
		return 4, true
	case "SOL/USD", "SOL/USDT":
		return 5, true
	}
	return 0, false
}

// usdTicks converts a USD price into the risk engine's fixed-point 1e8 scale.
func usdTicks(price float64) uint64 {
	if price <= 0 {
		return 0
	}
	return uint64(price * 100_000_000)
}

// RiskFeedWriter is a small connection wrapper around the risk daemon TCP port.
type RiskFeedWriter struct {
	addr   string
	mu     sync.Mutex
	conn   net.Conn
	broker *bufio.Writer
}

func NewRiskFeedWriter(addr string) *RiskFeedWriter {
	return &RiskFeedWriter{addr: addr}
}

// Send writes one JSON command line to the risk daemon. On a stale/broken
// connection the writer transparently disconnects and retries once on a fresh
// socket so a single daemon restart never drops a control message.
func (r *RiskFeedWriter) Send(line string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeLocked(line) == nil {
		return nil
	}
	if r.conn != nil {
		r.conn.Close()
		r.conn = nil
		r.broker = nil
	}
	return r.writeLocked(line)
}

func (r *RiskFeedWriter) writeLocked(line string) error {
	if r.conn == nil {
		conn, err := net.DialTimeout("tcp", r.addr, 2*time.Second)
		if err != nil {
			return fmt.Errorf("risk feed dial: %w", err)
		}
		r.conn = conn
		r.broker = bufio.NewWriter(conn)
	}
	if _, err := r.broker.WriteString(line + "\n"); err != nil {
		return fmt.Errorf("risk feed write: %w", err)
	}
	return r.broker.Flush()
}

// StartRiskMarketFeed streams live prices and Reg SHO reference closes to the
// risk daemon. It is idempotent-safe and always runs best-effort: if the risk
// daemon is offline, prices are skipped and retried on the next tick, so an
// unhealthy risk feed never takes down order entry.
func StartRiskMarketFeed(ctx context.Context) {
	addr := os.Getenv("ROBIN_RISK_ADDR")
	if addr == "" {
		addr = "127.0.0.1:9092"
	}
	interval := time.Second * 2
	if v := os.Getenv("ROBIN_RISK_FEED_INTERVAL_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			interval = time.Duration(ms) * time.Millisecond
		}
	}

	rw := NewRiskFeedWriter(addr)
	seeded := make(map[uint32]bool)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Push live best prices from live market data on each tick. The daemon's
	// price collar and Reg SHO breaker arm against the seeded previous close.
	push := func() {
		for symbol, price := range globalMarketData.GetAllPrices() {
			inst, ok := RiskInstrumentID(symbol)
			if !ok || price <= 0 {
				continue
			}
			ticks := usdTicks(price)
			if ticks == 0 {
				continue
			}
			if !seeded[inst] {
				// First observation seeds the Reg SHO (Rule 201)
				// previous-close reference for the session.
				seed := fmt.Sprintf(`{"cmd":"previous_close","instrument_id":%d,"price":%d}`, inst, ticks)
				if err := rw.Send(seed); err == nil {
					seeded[inst] = true
				}
			}
			quote := fmt.Sprintf(`{"cmd":"quote","instrument_id":%d,"price":%d}`, inst, ticks)
			if err := rw.Send(quote); err != nil {
				slog.Debug("risk feed push failed (will retry)", "symbol", symbol, "error", err)
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("risk market feed stopped")
			return
		case <-ticker.C:
			push()
		}
	}
}
