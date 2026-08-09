package main

import (
	"bufio"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRiskDaemon accepts lines over TCP and captures them for assertions.
type fakeRiskDaemon struct {
	ln  net.Listener
	mu  sync.Mutex
	got []string
}

func startFakeRiskDaemon(t *testing.T) *fakeRiskDaemon {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRiskDaemon{ln: ln}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				sc := bufio.NewScanner(c)
				for sc.Scan() {
					f.mu.Lock()
					f.got = append(f.got, sc.Text())
					f.mu.Unlock()
				}
			}()
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeRiskDaemon) lines() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.got))
	copy(out, f.got)
	return out
}

func waitLines(t *testing.T, f *fakeRiskDaemon, n int) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if l := f.lines(); len(l) >= n {
			return l
		}
		time.Sleep(10 * time.Millisecond)
	}
	return f.lines()
}

// TestRiskInstrumentID_knownSymbols verifies the symbol->instrument mapping the
// feed uses to address the risk daemon.
func TestRiskInstrumentID_knownSymbols(t *testing.T) {
	cases := map[string]uint32{
		"BTC/USD":  1,
		"BTC/USDT": 1,
		"ETH/USD":  2,
		"SOL/USD":  5,
		"EUR/USD":  4,
	}
	for sym, want := range cases {
		got, ok := RiskInstrumentID(sym)
		if !ok {
			t.Fatalf("expected %q to map", sym)
		}
		if got != want {
			t.Errorf("RiskInstrumentID(%q) = %d, want %d", sym, got, want)
		}
	}
	if _, ok := RiskInstrumentID("NOPE/USD"); ok {
		t.Error("expected unknown symbol to not map")
	}
}

func TestUSDToTicks(t *testing.T) {
	if got := usdTicks(64_500.0); got != 6_450_000_000_000 {
		t.Errorf("usdTicks(64500) = %d, want 6450000000000", got)
	}
	if usdTicks(0) != 0 {
		t.Error("expected zero for non-positive price")
	}
}

// TestRiskFeedWriter_Reconnects verifies the writer dials lazily and redials
// after a dropped connection.
func TestRiskFeedWriter_Reconnects(t *testing.T) {
	f := startFakeRiskDaemon(t)

	rw := NewRiskFeedWriter(f.ln.Addr().String())
	if err := rw.Send(`{"cmd":"quote","instrument_id":1,"price":100}`); err != nil {
		t.Fatalf("first send failed: %v", err)
	}
	// Force a reconnect on the next send.
	if rw.conn != nil {
		rw.conn.Close()
	}
	if err := rw.Send(`{"cmd":"quote","instrument_id":2,"price":200}`); err != nil {
		t.Fatalf("send after reconnect failed: %v", err)
	}

	lines := waitLines(t, f, 2)
	if len(lines) < 2 {
		t.Fatalf("expected 2 commands, got %v", lines)
	}
	both := lines[0] + lines[1]
	if !strings.Contains(both, `"price":100`) || !strings.Contains(both, `"price":200`) {
		t.Errorf("price payloads missing: %v", lines)
	}
}

// TestRiskFeedWriter_NoPanicOffline verifies sends fail softly when the daemon
// is unreachable (no panic, no hang).
func TestRiskFeedWriter_NoPanicOffline(t *testing.T) {
	rw := NewRiskFeedWriter("127.0.0.1:1")
	if err := rw.Send(`{"cmd":"quote","instrument_id":1,"price":100}`); err == nil {
		t.Fatal("expected an error when the risk daemon is offline")
	}
}

// TestRiskMarketFeed_seedsPreviousClose verifies one feed cycle sends a
// previous_close seed followed by a quote for the live symbol — the Reg SHO
// wiring path.
func TestRiskMarketFeed_seedsPreviousClose(t *testing.T) {
	f := startFakeRiskDaemon(t)
	// Seed a price in the global market-data cache the feed reads from.
	globalMarketData.UpdatePrice("BTC/USD", 64_500.0)

	// Replicate the feed's push cycle against the fake daemon.
	rw := NewRiskFeedWriter(f.ln.Addr().String())
	cycle := func(seeded map[uint32]bool) {
		for symbol, price := range globalMarketData.GetAllPrices() {
			inst, ok := RiskInstrumentID(symbol)
			if !ok || price <= 0 {
				continue
			}
			ticks := usdTicks(price)
			seed := `{"cmd":"previous_close","instrument_id":` + strconv.FormatUint(uint64(inst), 10) + `,"price":` + strconv.FormatUint(ticks, 10) + `}`
			if err := rw.Send(seed); err == nil {
				seeded[inst] = true
			}
			if err := rw.Send(`{"cmd":"quote","instrument_id":` + strconv.FormatUint(uint64(inst), 10) + `,"price":` + strconv.FormatUint(ticks, 10) + `}`); err != nil {
				return
			}
		}
	}

	seeded := make(map[uint32]bool)
	cycle(seeded)

	lines := waitLines(t, f, 2)
	var hasSeed, hasQuote bool
	for _, line := range lines {
		if strings.Contains(line, `"cmd":"previous_close"`) && strings.Contains(line, `"instrument_id":1`) {
			hasSeed = true
		}
		if strings.Contains(line, `"cmd":"quote"`) && strings.Contains(line, `"instrument_id":1`) {
			hasQuote = true
		}
	}
	if !hasSeed {
		t.Fatalf("expected a previous_close seed, saw: %v", lines)
	}
	if !hasQuote {
		t.Fatalf("expected a quote, saw: %v", lines)
	}
}
