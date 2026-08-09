// ============================================================================
// Robin Trading Platform — Order Management System (Go)
// ============================================================================
// State machine for order lifecycle tracking.
// Reads signals from C++ live_feed via stdin pipe, routes to exchanges,
// tracks order state, and updates audit log.
//
// Order state machine:
//   NEW → PENDING_NEW → LIVE → PARTIALLY_FILLED → FILLED
//                           ↘ CANCELLED
//                           ↘ REJECTED
//
// Exchange connectors (paper trading):
//   - Binance Testnet (crypto)
//   - Alpaca Paper (equities)
// ============================================================================

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

const numOrderWorkers = 10

// ─── Order state machine ───────────────────────────────────────────────────────

type OrderState int32

const (
	StateNew OrderState = iota
	StatePendingNew
	StateLive
	StatePartiallyFilled
	StateFilled
	StateCancelled
	StateRejected
)

func (s OrderState) String() string {
	switch s {
	case StateNew:
		return "NEW"
	case StatePendingNew:
		return "PENDING_NEW"
	case StateLive:
		return "LIVE"
	case StatePartiallyFilled:
		return "PARTIALLY_FILLED"
	case StateFilled:
		return "FILLED"
	case StateCancelled:
		return "CANCELLED"
	case StateRejected:
		return "REJECTED"
	default:
		return "UNKNOWN"
	}
}

// ─── Order ────────────────────────────────────────────────────────────────────

type Order struct {
	ClientOrderID string     `json:"cl_ord_id"`
	Symbol        string     `json:"symbol"`
	Side          string     `json:"side"` // "BUY" | "SELL"
	Qty           float64    `json:"qty"`
	Price         float64    `json:"price"`
	KellyFraction float64    `json:"kelly_fraction"`
	Confidence    float64    `json:"confidence"`
	Exchange      string     `json:"exchange"` // "binance" | "alpaca"
	State         OrderState `json:"state"`
	FilledQty     float64    `json:"filled_qty"`
	FilledAvgPx   float64    `json:"filled_avg_px"`
	Reason        string     `json:"reason"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	ExchangeID    string     `json:"exchange_id,omitempty"`
	mu            sync.RWMutex
}

func (o *Order) Transition(next OrderState, note string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Terminal state check
	if o.State == StateFilled || o.State == StateCancelled || o.State == StateRejected {
		return fmt.Errorf("invalid transition: order %s is in terminal state %s", o.ClientOrderID, o.State)
	}

	valid := false
	switch o.State {
	case StateNew:
		valid = (next == StatePendingNew || next == StateRejected || next == StateLive)
	case StatePendingNew:
		valid = (next == StateLive || next == StateRejected || next == StateCancelled)
	case StateLive:
		valid = (next == StatePartiallyFilled || next == StateFilled || next == StateCancelled || next == StateRejected)
	case StatePartiallyFilled:
		valid = (next == StateFilled || next == StateCancelled)
	}

	if !valid {
		return fmt.Errorf("invalid transition: %s → %s for order %s", o.State, next, o.ClientOrderID)
	}

	log.Printf("[OMS] %s: %s → %s (%s)", o.ClientOrderID, o.State, next, note)
	o.State = next
	o.UpdatedAt = time.Now().UTC()
	return nil
}

func (o *Order) ToJSON() []byte {
	o.mu.RLock()
	defer o.mu.RUnlock()
	b, _ := json.Marshal(o)
	return b
}

// ─── C++ Signal (JSON published by live_feed.exe) ─────────────────────────────

type CppSignal struct {
	Type       string  `json:"type"` // "SIGNAL"
	Symbol     string  `json:"symbol"`
	Side       string  `json:"side"` // "BUY" | "SELL"
	Price      float64 `json:"price"`
	Confidence float64 `json:"confidence"`
	Kelly      float64 `json:"kelly"`
	Reason     string  `json:"reason"`
	Strategy   int     `json:"strategy"`
	Ts         uint64  `json:"ts"`
}

// ─── Audit log (immutable append) ─────────────────────────────────────────────

type AuditLogger struct {
	db *sql.DB
}

func NewAuditLogger(path string) (*AuditLogger, error) {
	if err := os.MkdirAll("logs", 0755); err != nil {
		return nil, err
	}
	dbPath := "logs/audit.db"
	dbURL := os.Getenv("DATABASE_URL")
	driverName := "sqlite3"
	dataSource := dbPath

	if dbURL != "" {
		driverName = "postgres"
		dataSource = dbURL
	}

	db, err := sql.Open(driverName, dataSource)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts DATETIME DEFAULT CURRENT_TIMESTAMP,
			event TEXT NOT NULL,
			data TEXT NOT NULL
		);
	`)
	if err != nil {
		return nil, err
	}

	return &AuditLogger{db: db}, nil
}

func (a *AuditLogger) Log(event string, data any) {
	b, _ := json.Marshal(data)
	_, err := a.db.Exec("INSERT INTO audit_log (event, data) VALUES ($1, $2)", event, string(b))
	if err != nil {
		log.Printf("[AUDIT] SQLite write error: %v", err)
	}
}

func (a *AuditLogger) Close() { a.db.Close() }

// ─── OMS ──────────────────────────────────────────────────────────────────────

type OMS struct {
	orders     map[string]*Order
	mu         sync.RWMutex
	audit      *AuditLogger
	binance    *BinanceConnector
	alpaca     *AlpacaConnector
	seqNum     atomic.Uint64
	killSwitch atomic.Bool // Emergency stop

	orderCh chan *Order
	wg      sync.WaitGroup
}

func NewOMS(audit *AuditLogger) *OMS {
	oms := &OMS{
		orders:  make(map[string]*Order),
		audit:   audit,
		binance: NewBinanceConnector(),
		alpaca:  NewAlpacaConnector(),
		orderCh: make(chan *Order, 100),
	}
	for i := 0; i < numOrderWorkers; i++ {
		oms.wg.Add(1)
		go oms.orderWorker()
	}
	return oms
}

func (o *OMS) orderWorker() {
	defer o.wg.Done()
	ctx := context.Background()
	for order := range o.orderCh {
		o.routeOrder(ctx, order)
	}
}

func (o *OMS) Kill() {
	o.killSwitch.Store(true)
	log.Println("[OMS] KILL SWITCH ENGAGED — no new orders will be sent.")
}

func (o *OMS) IsKilled() bool { return o.killSwitch.Load() }

// GenerateClientOrderID generates a unique, human-readable order ID
func (o *OMS) GenerateClientOrderID(symbol, side string) string {
	seq := o.seqNum.Add(1)
	ts := time.Now().UTC().UnixNano() / 1e6 // ms
	return fmt.Sprintf("ROBIN-%s-%s-%d-%d", symbol, side, ts, seq)
}

// ─── Pre-trade Compliance (SEC Rule 15c3-5) ─────────────────────────
func checkRule15c35(symbol string, side string, qty, price, notional, portfolioValue float64) error {
	// 1. Fat Finger (Erroneous Order) Limits
	if notional > 5000000.0 {
		return fmt.Errorf("Rule 15c3-5(c)(1)(ii): Notional value $%.2f exceeds fat-finger hard limit of $5,000,000", notional)
	}
	if qty > 1000000.0 {
		return fmt.Errorf("Rule 15c3-5(c)(1)(ii): Quantity %.2f exceeds maximum share limit", qty)
	}

	// 2. Margin & Capital Limits
	if notional > portfolioValue*1.5 { // Max 1.5x leverage
		return fmt.Errorf("Rule 15c3-5(c)(1)(i): Order notional $%.2f exceeds available margin threshold", notional)
	}

	// 3. Naked Short Sale Prevention
	if side == "SELL" {
		// In a real system, query the Position Manager to verify locate or long inventory.
		// For our prototype, block massive shorting without locate.
		if notional > portfolioValue*0.5 {
			return fmt.Errorf("Rule 15c3-5(c)(1)(ii): Short sale volume exceeds overnight naked short threshold without explicit locate")
		}
	}
	return nil
}

// OnSignal processes a trading signal from the C++ engine
func (o *OMS) OnSignal(ctx context.Context, sig CppSignal, portfolioValue float64) error {
	if o.IsKilled() {
		return fmt.Errorf("kill switch active — ignoring signal")
	}

	// Determine exchange by symbol suffix
	exchange := "binance"
	if len(sig.Symbol) <= 5 && sig.Symbol != "BTC" {
		// Short ticker = equity
		exchange = "alpaca"
	}
	if sig.Symbol == "EURUSD=X" || sig.Symbol == "GBPUSD=X" {
		exchange = "oanda"
	}

	// Kelly position sizing: notional = portfolio × kelly × confidence
	notional := portfolioValue * sig.Kelly * sig.Confidence
	const maxNotional = 5000.0 // Hard cap $5K per order in paper mode
	if notional > maxNotional {
		notional = maxNotional
	}
	if notional < 1.0 {
		log.Printf("[OMS] Notional $%.2f too small, skipping signal %s", notional, sig.Symbol)
		return nil
	}
	qty := notional / sig.Price

	// SEC Rule 15c3-5 Pre-Trade Compliance Check
	if err := checkRule15c35(sig.Symbol, sig.Side, qty, sig.Price, notional, portfolioValue); err != nil {
		log.Printf("[OMS] PRE-TRADE REJECT (15c3-5): %v", err)
		o.audit.Log("PRE_TRADE_REJECT", map[string]any{
			"symbol": sig.Symbol, "side": sig.Side, "error": err.Error(),
		})
		return err
	}

	clID := o.GenerateClientOrderID(sig.Symbol, sig.Side)
	order := &Order{
		ClientOrderID: clID,
		Symbol:        sig.Symbol,
		Side:          sig.Side,
		Qty:           qty,
		Price:         sig.Price,
		KellyFraction: sig.Kelly,
		Confidence:    sig.Confidence,
		Exchange:      exchange,
		State:         StateNew,
		Reason:        sig.Reason,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}

	o.mu.Lock()
	o.orders[clID] = order
	o.mu.Unlock()

	o.audit.Log("ORDER_NEW", map[string]any{
		"cl_ord_id": clID,
		"symbol":    sig.Symbol,
		"side":      sig.Side,
		"qty":       qty,
		"price":     sig.Price,
		"notional":  notional,
		"exchange":  exchange,
	})

	log.Printf("[OMS] Routing %s %s %.6f %s @ %.4f (notional=$%.2f) → %s",
		clID, sig.Side, qty, sig.Symbol, sig.Price, notional, exchange)

	// Route to exchange via bounded worker pool
	select {
	case o.orderCh <- order:
	default:
		log.Printf("[OMS] Order channel full, dropping order %s", clID)
	}
	return nil
}

func (o *OMS) routeOrder(ctx context.Context, order *Order) {
	order.Transition(StatePendingNew, "routing to exchange")

	var (
		exchOrderID string
		err         error
	)

	switch order.Exchange {
	case "binance":
		exchOrderID, err = o.binance.SubmitOrder(ctx, order)
	case "alpaca":
		exchOrderID, err = o.alpaca.SubmitOrder(ctx, order)
	default:
		err = fmt.Errorf("unsupported exchange: %s", order.Exchange)
	}

	if err != nil {
		order.Transition(StateRejected, err.Error())
		o.audit.Log("ORDER_REJECTED", map[string]any{
			"cl_ord_id": order.ClientOrderID, "error": err.Error(),
		})
		return
	}

	order.mu.Lock()
	order.ExchangeID = exchOrderID
	order.mu.Unlock()

	order.Transition(StateLive, "exchange ack: "+exchOrderID)
	o.audit.Log("ORDER_LIVE", map[string]any{
		"cl_ord_id":   order.ClientOrderID,
		"exchange_id": exchOrderID,
	})

	// Poll for fill (paper trading fills near-instantly)
	o.pollForFill(ctx, order)
}

func (o *OMS) pollForFill(ctx context.Context, order *Order) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(30 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			order.Transition(StateCancelled, "fill timeout")
			o.audit.Log("ORDER_TIMEOUT", map[string]any{"cl_ord_id": order.ClientOrderID})
			return
		case <-ticker.C:
			// In paper mode, assume immediate fill
			order.mu.Lock()
			if order.State == StateLive || order.State == StatePartiallyFilled {
				order.FilledQty = order.Qty
				order.FilledAvgPx = order.Price
				order.mu.Unlock()
				order.Transition(StateFilled, "paper fill")
				o.audit.Log("ORDER_FILLED", map[string]any{
					"cl_ord_id":     order.ClientOrderID,
					"filled_qty":    order.Qty,
					"filled_avg_px": order.Price,
				})
			} else {
				order.mu.Unlock()
			}
			return
		}
	}
}

// GetOpenOrders returns all orders not in terminal state
func (o *OMS) GetOpenOrders() []*Order {
	o.mu.RLock()
	defer o.mu.RUnlock()
	var open []*Order
	for _, ord := range o.orders {
		ord.mu.RLock()
		s := ord.State
		ord.mu.RUnlock()
		if s != StateFilled && s != StateCancelled && s != StateRejected {
			open = append(open, ord)
		}
	}
	return open
}

// ─── Binance Testnet Connector ─────────────────────────────────────────────────

type BinanceConnector struct {
	apiKey    string
	apiSecret string
	baseURL   string
	client    *http.Client
}

func NewBinanceConnector() *BinanceConnector {
	baseURL := os.Getenv("BINANCE_TESTNET_URL")
	if baseURL == "" {
		baseURL = "https://testnet.binance.vision"
	}
	return &BinanceConnector{
		apiKey:    os.Getenv("BINANCE_API_KEY"),
		apiSecret: os.Getenv("BINANCE_API_SECRET"),
		baseURL:   baseURL,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (b *BinanceConnector) SubmitOrder(ctx context.Context, o *Order) (string, error) {
	if b.apiKey == "" {
		// Sandbox mode: simulate order ID
		return fmt.Sprintf("BIN-PAPER-%d", time.Now().UnixNano()), nil
	}

	// Normalise symbol: "BTC-USD" → "BTCUSDT"
	sym := o.Symbol
	switch sym {
	case "BTC-USD":
		sym = "BTCUSDT"
	case "ETH-USD":
		sym = "ETHUSDT"
	}

	side := o.Side
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	params := url.Values{}
	params.Set("symbol", sym)
	params.Set("side", side)
	params.Set("type", "MARKET")
	params.Set("quantity", strconv.FormatFloat(o.Qty, 'f', 6, 64))
	params.Set("timestamp", ts)
	params.Set("recvWindow", "5000")

	// HMAC-SHA256 signature
	mac := hmac.New(sha256.New, []byte(b.apiSecret))
	mac.Write([]byte(params.Encode()))
	params.Set("signature", hex.EncodeToString(mac.Sum(nil)))

	req, err := http.NewRequestWithContext(ctx, "POST",
		b.baseURL+"/api/v3/order?"+params.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-MBX-APIKEY", b.apiKey)

	resp, err := b.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("binance error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		OrderID int64 `json:"orderId"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	return strconv.FormatInt(result.OrderID, 10), nil
}

// ─── Alpaca Paper Connector ────────────────────────────────────────────────────

type AlpacaConnector struct {
	apiKey    string
	apiSecret string
	baseURL   string
	client    *http.Client
}

func NewAlpacaConnector() *AlpacaConnector {
	baseURL := os.Getenv("ALPACA_BASE_URL")
	if baseURL == "" {
		baseURL = "https://paper-api.alpaca.markets"
	}
	return &AlpacaConnector{
		apiKey:    os.Getenv("ALPACA_API_KEY"),
		apiSecret: os.Getenv("ALPACA_SECRET_KEY"),
		baseURL:   baseURL,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (a *AlpacaConnector) SubmitOrder(ctx context.Context, o *Order) (string, error) {
	if a.apiKey == "" {
		return fmt.Sprintf("ALP-PAPER-%d", time.Now().UnixNano()), nil
	}

	// Alpaca requires lowercase side ("buy"/"sell"); Order.Side is "BUY"/"SELL".
	alpacaSide := "buy"
	if strings.EqualFold(o.Side, "SELL") || strings.EqualFold(o.Side, "ASK") {
		alpacaSide = "sell"
	}

	body := map[string]any{
		"symbol":        o.Symbol,
		"qty":           fmt.Sprintf("%.4f", o.Qty),
		"side":          alpacaSide,
		"type":          "market",
		"time_in_force": "day",
	}
	b, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST",
		a.baseURL+"/v2/orders", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("APCA-API-KEY-ID", a.apiKey)
	req.Header.Set("APCA-API-SECRET-KEY", a.apiSecret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return "", fmt.Errorf("alpaca error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	return result.ID, nil
}

// ─── Signal reader (reads JSON lines from stdin or pipe) ──────────────────────

func runSignalReader(ctx context.Context, oms *OMS, portfolioValue float64) {
	scanner := bufio.NewScanner(os.Stdin)
	log.Println("[OMS] Waiting for signals on stdin (pipe from live_feed.exe) ...")

	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		line := scanner.Text()
		if len(line) == 0 {
			continue
		}
		var sig CppSignal
		if err := json.Unmarshal([]byte(line), &sig); err != nil {
			log.Printf("[OMS] Parse error: %v | raw: %s", err, line)
			continue
		}
		if sig.Type != "SIGNAL" {
			continue
		}
		log.Printf("[OMS] Signal received: %s %s @ %.4f (conf=%.2f, kelly=%.4f)",
			sig.Side, sig.Symbol, sig.Price, sig.Confidence, sig.Kelly)

		if err := oms.OnSignal(ctx, sig, portfolioValue); err != nil {
			log.Printf("[OMS] OnSignal error: %v", err)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[OMS] Scanner error: %v", err)
	}
}

// ─── Main ─────────────────────────────────────────────────────────────────────

// RunOMS starts the OMS signal reader loop.
// Call from main.go when ROBIN_MODE=paper or live.
func RunOMS(portfolioValue float64) {
	audit, err := NewAuditLogger("logs/oms_audit.jsonl")
	if err != nil {
		log.Fatalf("AuditLogger init failed: %v", err)
	}
	defer audit.Close()

	mode := os.Getenv("ROBIN_MODE")
	if mode == "" {
		mode = "paper"
	}
	log.Printf("[OMS] Starting — mode=%s portfolio=$%.2f", mode, portfolioValue)

	oms := NewOMS(audit)

	if os.Getenv("ROBIN_KILL") == "1" {
		oms.Kill()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runSignalReader(ctx, oms, portfolioValue)
	log.Println("[OMS] Shutdown complete.")
}
