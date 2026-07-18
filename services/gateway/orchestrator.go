package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/cors"
)

// MatchingEngineClient manages a TCP connection to the C++ matching engine.
type MatchingEngineClient struct {
	mu      sync.Mutex
	addr    string
	conn    net.Conn
	reader  *bufio.Reader
	enabled bool
	lastErr string
}

func NewMatchingEngineClient(host string, port int) *MatchingEngineClient {
	return &MatchingEngineClient{
		addr:    net.JoinHostPort(host, strconv.Itoa(port)),
		enabled: false,
	}
}

func (c *MatchingEngineClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	conn, err := net.DialTimeout("tcp", c.addr, 2*time.Second)
	if err != nil {
		c.enabled = false
		c.lastErr = err.Error()
		return err
	}
	c.conn = conn
	c.reader = bufio.NewReaderSize(conn, 4096)
	c.enabled = true
	c.lastErr = ""
	return nil
}

func (c *MatchingEngineClient) SendOrderJSON(orderJSON string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return "", fmt.Errorf("not connected")
	}
	if _, err := fmt.Fprint(c.conn, orderJSON); err != nil {
		c.enabled = false
		c.lastErr = err.Error()
		c.conn.Close()
		c.conn = nil
		return "", err
	}
	resp, err := c.reader.ReadString('\n')
	if err != nil {
		c.enabled = false
		c.lastErr = err.Error()
		c.conn.Close()
		c.conn = nil
		return "", err
	}
	return resp, nil
}

func (c *MatchingEngineClient) HealthCheck() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return false
	}
	if _, err := fmt.Fprint(c.conn, "health"); err != nil {
		return false
	}
	resp, err := c.reader.ReadString('\n')
	return err == nil && strings.Contains(resp, "ok")
}

func (c *MatchingEngineClient) IsEnabled() bool   { c.mu.Lock(); defer c.mu.Unlock(); return c.enabled }
func (c *MatchingEngineClient) LastError() string { c.mu.Lock(); defer c.mu.Unlock(); return c.lastErr }

// OrderResponse from the matching engine
type MatchingEngineResponse struct {
	OrderID      uint64 `json:"order_id"`
	InstrumentID uint32 `json:"instrument_id"`
	FillPrice    int64  `json:"fill_price"`
	FillQty      int64  `json:"fill_qty"`
	Status       string `json:"status"`
	Success      bool   `json:"success"`
	Error        string `json:"error"`
}

// ============================================================================
// Service Status
// ============================================================================

type ServiceStatus int32

const (
	StatusUnknown  ServiceStatus = 0
	StatusActive   ServiceStatus = 1
	StatusDegraded ServiceStatus = 2
	StatusFailed   ServiceStatus = 3
)

func (s ServiceStatus) String() string {
	switch s {
	case StatusActive:
		return "ACTIVE"
	case StatusDegraded:
		return "DEGRADED"
	case StatusFailed:
		return "FAILED"
	default:
		return "UNKNOWN"
	}
}

func (s ServiceStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// ============================================================================
// Data Structures
// ============================================================================

type ServiceHealth struct {
	Name      string        `json:"name"`
	Status    ServiceStatus `json:"status"`
	LatencyNs int64         `json:"latency_ns"`
	LastCheck time.Time     `json:"last_check"`
	Addr      string        `json:"addr"`
	CheckErr  string        `json:"last_error,omitempty"`
}

type TLSConfig struct {
	Enabled  bool   `json:"enabled"`
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
}

type HotReloadConfig struct {
	MaxDrawdownLimit float64   `json:"max_drawdown_limit"`
	MarketDataPort   int       `json:"market_data_port"`
	OrderEntryPort   int       `json:"order_entry_port"`
	MaxOrderRate     uint32    `json:"max_order_rate"`
	MaxCancelRate    uint32    `json:"max_cancel_rate"`
	MaxPositionLimit int64     `json:"max_position_limit"`
	TLS              TLSConfig `json:"tls"`
}

// ============================================================================
// Orchestrator
// ============================================================================

// OrderRequest is the JSON body for POST /order
type OrderRequest struct {
	Symbol      string  `json:"symbol"`
	Side        string  `json:"side"` // BUY or SELL
	Price       float64 `json:"price"`
	Qty         float64 `json:"qty"`
	OrderType   string  `json:"order_type"` // LIMIT or MARKET
	ClientOrdID string  `json:"cl_ord_id"`
	Exchange    string  `json:"exchange"` // AUTO (Best Price) or specific exchange
}

type Orchestrator struct {
	mu            sync.RWMutex
	services      map[string]*ServiceHealth
	config        HotReloadConfig
	configMutex   sync.RWMutex
	healthyCount  atomic.Int32
	degradedCount atomic.Int32
	failedCount   atomic.Int32
	totalChecks   atomic.Uint64
	shutdownCh    chan struct{}
	wg            sync.WaitGroup
	logger        *slog.Logger
	wsHub         *WebSocketHub
	db            *sql.DB

	orderCount  atomic.Uint64
	rejectCount atomic.Uint64
	tradeCount  atomic.Uint64
	latencySum  atomic.Uint64
	matchClient *MatchingEngineClient
}

func NewOrchestrator() *Orchestrator {
	orch := &Orchestrator{
		services: make(map[string]*ServiceHealth),
		config: HotReloadConfig{
			MaxDrawdownLimit: DrawdownLimit,
			MarketDataPort:   PortMarketData,
			OrderEntryPort:   PortOrchestrator,
			MaxOrderRate:     MaxOrdersPerSec,
			MaxCancelRate:    5000,
			MaxPositionLimit: PositionLimit,
		},
		shutdownCh:  make(chan struct{}),
		logger:      slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})),
		wsHub:       NewWebSocketHub(),
		matchClient: NewMatchingEngineClient("127.0.0.1", PortRiskHealth), // Route to Risk Analytics instead of Execution Core
	}
	orch.loadConfig()
	orch.initDB()
	return orch
}

func (o *Orchestrator) initDB() {
	db, err := sql.Open("sqlite3", "robin.db")
	if err != nil {
		o.logger.Error("failed to open sqlite database", "error", err)
		return
	}

	// Optimize SQLite for concurrent access
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(1 * time.Hour)

	o.db = db

	schemaBytes, err := os.ReadFile("../../schema_sqlite.sql")
	if err != nil {
		schemaBytes, err = os.ReadFile("schema_sqlite.sql")
	}
	if err != nil {
		o.logger.Error("failed to read schema_sqlite.sql", "error", err)
		return
	}

	_, err = db.Exec(string(schemaBytes))
	if err != nil {
		o.logger.Error("failed to execute schema_sqlite.sql", "error", err)
		return
	}

	o.logger.Info("SQLite database initialized successfully")
}

func (o *Orchestrator) RegisterService(name string, addr string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.services[name] = &ServiceHealth{
		Name:   name,
		Status: StatusUnknown,
		Addr:   addr,
	}
	o.logger.Info("service registered", "name", name, "addr", addr)
}

func (o *Orchestrator) StartHealthProbes(ctx context.Context, interval time.Duration) {
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				o.runHealthChecks()
			}
		}
	}()
	o.logger.Info("health probes started", "interval", interval)

	// Try connecting to the matching engine
	go func() {
		for i := 0; i < 30; i++ {
			if err := o.matchClient.Connect(); err == nil {
				o.logger.Info("connected to matching engine", "addr", o.matchClient.addr)
				return
			}
			time.Sleep(1 * time.Second)
		}
		o.logger.Warn("could not connect to matching engine after 30s, using simulated fills", "addr", o.matchClient.addr)
	}()

	// Market-data broadcast goroutine: publishes synthetic order-book ticks every 500ms.
	// When a real market-data feed is connected, replace this with live data.
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		basePrice := map[string]float64{
			"BTC/USD": 64500.0,
			"ETH/USD": 3450.0,
			"AAPL":    185.30,
			"EUR/USD": 1.0850,
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for symbol, bp := range basePrice {
					// Random walk: ±0.025% per tick
					drift := bp * (1 + (rand.Float64()-0.5)*0.0005)
					basePrice[symbol] = drift
					bids, asks := buildOrderBookLevels(drift, 8)
					o.wsHub.BroadcastOrderBook(symbol, bids, asks)
				}
			}
		}
	}()
}

// buildOrderBookLevels generates synthetic bid/ask levels around midPrice.
func buildOrderBookLevels(midPrice float64, depth int) ([][2]float64, [][2]float64) {
	tick := math.Max(midPrice*0.0001, 0.01) // 1bps tick size, min $0.01
	bids := make([][2]float64, depth)
	asks := make([][2]float64, depth)
	for i := 0; i < depth; i++ {
		spread := tick * float64(i+1)
		size := math.Round((0.1+rand.Float64()*2)*100) / 100
		bids[i] = [2]float64{midPrice - spread, size}
		asks[i] = [2]float64{midPrice + spread, size}
	}
	return bids, asks
}

func (o *Orchestrator) runHealthChecks() {
	o.mu.RLock()
	names := make([]string, 0, len(o.services))
	for name := range o.services {
		names = append(names, name)
	}
	o.mu.RUnlock()

	var healthy, degraded, failed int32
	for _, name := range names {
		o.mu.RLock()
		svc := o.services[name]
		addr := svc.Addr
		o.mu.RUnlock()

		start := time.Now()
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		latency := time.Since(start)
		o.totalChecks.Add(1)

		o.mu.Lock()
		svc.LatencyNs = latency.Nanoseconds()
		svc.LastCheck = time.Now()
		if err != nil {
			svc.Status = StatusFailed
			svc.CheckErr = err.Error()
			failed++
			o.logger.Warn("service health check failed", "name", name, "addr", addr, "error", err)
		} else {
			conn.Close()
			svc.CheckErr = ""
			if latency > 10*time.Millisecond {
				svc.Status = StatusDegraded
				degraded++
				o.logger.Warn("service degraded", "name", name, "latency_ms", latency.Milliseconds())
			} else {
				svc.Status = StatusActive
				healthy++
			}
		}
		o.mu.Unlock()
	}

	o.healthyCount.Store(healthy)
	o.degradedCount.Store(degraded)
	o.failedCount.Store(failed)
}

func (o *Orchestrator) HotReloadConfig(jsonConfig []byte) error {
	o.configMutex.Lock()

	var newConfig HotReloadConfig
	if err := json.Unmarshal(jsonConfig, &newConfig); err != nil {
		o.configMutex.Unlock()
		return fmt.Errorf("config parse error: %w", err)
	}
	old := o.config
	o.config = newConfig
	o.configMutex.Unlock()

	if err := o.persistConfig(); err != nil {
		o.logger.Error("failed to persist config", "error", err)
	}

	o.logger.Info("config hot-reloaded",
		"old_max_drawdown", old.MaxDrawdownLimit,
		"new_max_drawdown", newConfig.MaxDrawdownLimit,
		"old_max_order_rate", old.MaxOrderRate,
		"new_max_order_rate", newConfig.MaxOrderRate,
	)
	return nil
}

func (o *Orchestrator) loadConfig() {
	o.configMutex.Lock()
	defer o.configMutex.Unlock()
	data, err := os.ReadFile("config_state.json")
	if err == nil {
		var newConfig HotReloadConfig
		if err := json.Unmarshal(data, &newConfig); err == nil {
			o.config = newConfig
			o.logger.Info("loaded config from disk", "file", "config_state.json")
			return
		}
	}
	o.logger.Info("using default config")
}

func (o *Orchestrator) persistConfig() error {
	o.configMutex.RLock()
	data, err := json.MarshalIndent(o.config, "", "  ")
	o.configMutex.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile("config_state.json", data, 0644)
}

func (o *Orchestrator) GetConfig() HotReloadConfig {
	o.configMutex.RLock()
	defer o.configMutex.RUnlock()
	return o.config
}

func (o *Orchestrator) GetServices() []*ServiceHealth {
	o.mu.RLock()
	defer o.mu.RUnlock()
	result := make([]*ServiceHealth, 0, len(o.services))
	for _, svc := range o.services {
		result = append(result, svc)
	}
	return result
}

func (o *Orchestrator) RecordOrder()            { o.orderCount.Add(1) }
func (o *Orchestrator) RecordReject()           { o.rejectCount.Add(1) }
func (o *Orchestrator) RecordTrade()            { o.tradeCount.Add(1) }
func (o *Orchestrator) RecordLatency(ns uint64) { o.latencySum.Add(ns) }

func (o *Orchestrator) Shutdown() {
	close(o.shutdownCh)
	o.wg.Wait()
	if o.db != nil {
		o.db.Close()
	}
	o.logger.Info("orchestrator shutdown complete")
}

// ============================================================================
// Rate Limiter (token bucket, in-process)
// ============================================================================

type tokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per nanosecond
	lastRefill time.Time
}

func newTokenBucket(ratePerSec float64) *tokenBucket {
	return &tokenBucket{
		tokens:     ratePerSec,
		maxTokens:  ratePerSec,
		refillRate: ratePerSec / 1e9,
		lastRefill: time.Now(),
	}
}

func (tb *tokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	now := time.Now()
	elapsed := float64(now.Sub(tb.lastRefill).Nanoseconds())
	tb.tokens = min64(tb.maxTokens, tb.tokens+elapsed*tb.refillRate)
	tb.lastRefill = now
	if tb.tokens >= 1.0 {
		tb.tokens--
		return true
	}
	return false
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// rateLimitMiddleware limits HTTP requests per second (default 1000/s)
func rateLimitMiddleware(ratePerSec float64, next http.Handler) http.Handler {
	bucket := newTokenBucket(ratePerSec)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !bucket.Allow() {
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requestIDMiddleware adds a unique request ID to each request for tracing
func requestIDMiddleware(next http.Handler) http.Handler {
	var counter atomic.Uint64
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := counter.Add(1)
		w.Header().Set("X-Request-ID", fmt.Sprintf("robin-%d-%d", time.Now().UnixNano(), id))
		next.ServeHTTP(w, r)
	})
}

// Removed gatewayAPIToken plain-text fallback mechanism.

// jwtAuthMiddleware enforces a Bearer token for sensitive endpoints.
// Uses jwtAuth.verify for signature verification and crypto/subtle.ConstantTimeCompare for static tokens.
func jwtAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			w.Header().Set("WWW-Authenticate", `Bearer realm="robin-gateway"`)
			http.Error(w, `{"error":"unauthorized: missing bearer token"}`, http.StatusUnauthorized)
			return
		}
		provided := strings.TrimPrefix(authHeader, "Bearer ")

		// Enforce strict JWT signature verification
		claims, err := jwtAuth.verify(provided)
		if err == nil {
			ctx := context.WithValue(r.Context(), "jwt_claims", claims)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		http.Error(w, `{"error":"unauthorized: invalid token"}`, http.StatusUnauthorized)
	})
}

// rbacMiddleware enforces role-based access control based on JWT claims.
func rbacMiddleware(allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value("jwt_claims").(jwt.MapClaims)
			if !ok {
				http.Error(w, `{"error":"unauthorized: missing claims"}`, http.StatusUnauthorized)
				return
			}
			role, _ := claims["role"].(string)
			for _, allowed := range allowedRoles {
				if role == allowed {
					next.ServeHTTP(w, r)
					return
				}
			}
			http.Error(w, `{"error":"forbidden: insufficient permissions"}`, http.StatusForbidden)
		})
	}
}

// ============================================================================
// HTTP Server Setup
// ============================================================================

func (o *Orchestrator) setupHTTPServer(port int) *http.Server {
	r := mux.NewRouter()

	r.HandleFunc("/health", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "ok",
			"healthy":  o.healthyCount.Load(),
			"degraded": o.degradedCount.Load(),
			"failed":   o.failedCount.Load(),
			"checks":   o.totalChecks.Load(),
		})
	}).Methods("GET")

	r.HandleFunc("/live", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}).Methods("GET")

	r.HandleFunc("/ready", func(w http.ResponseWriter, req *http.Request) {
		if o.failedCount.Load() > 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("services failed"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	}).Methods("GET")

	r.HandleFunc("/services", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(o.GetServices())
	}).Methods("GET")

	r.Handle("/config", rateLimitMiddleware(float64(o.GetConfig().MaxOrderRate), jwtAuthMiddleware(rbacMiddleware("admin")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(o.GetConfig())
	}))))).Methods("GET")

	// POST /config — hot-reload risk parameters (JWT Admin required)
	r.Handle("/config", rateLimitMiddleware(float64(o.GetConfig().MaxOrderRate), jwtAuthMiddleware(rbacMiddleware("admin")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		raw, _ := json.Marshal(body)
		if err := o.HotReloadConfig(raw); err != nil {
			o.logger.Error("config reload failed", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "reloaded"})
	}))))).Methods("POST")

	// POST /order — submit a new order (JWT Trader required)
	// Forwards to the matching engine TCP server, or falls back to simulated fill.
	r.Handle("/order", rateLimitMiddleware(float64(o.GetConfig().MaxOrderRate), jwtAuthMiddleware(rbacMiddleware("trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var orderReq OrderRequest
		if err := json.NewDecoder(req.Body).Decode(&orderReq); err != nil {
			http.Error(w, `{"error":"invalid order JSON"}`, http.StatusBadRequest)
			return
		}

		if orderReq.Symbol == "" || orderReq.Qty <= 0 || orderReq.Price < 0 {
			http.Error(w, `{"error":"symbol, qty, and price are required"}`, http.StatusBadRequest)
			return
		}
		if orderReq.Side != "BUY" && orderReq.Side != "SELL" {
			http.Error(w, `{"error":"side must be BUY or SELL"}`, http.StatusBadRequest)
			return
		}

		start := time.Now()
		o.RecordOrder()

		orderID := uint64(time.Now().UnixNano())
		execID := fmt.Sprintf("EXEC-%d", orderID)
		if orderReq.ClientOrdID == "" {
			orderReq.ClientOrdID = fmt.Sprintf("ORD-%d", orderID)
		}

		// Map symbol to instrument_id
		instID := uint64(1)
		switch orderReq.Symbol {
		case "BTC/USD":
			instID = 1
		case "ETH/USD":
			instID = 2
		case "AAPL":
			instID = 3
		case "EUR/USD":
			instID = 4
		}

		side := "BID"
		if orderReq.Side == "SELL" {
			side = "ASK"
		}

		orderType := "LIMIT"
		if orderReq.OrderType == "MARKET" {
			orderType = "MARKET"
		}

		// Run SOR selection
		prefExchange := orderReq.Exchange
		if prefExchange == "" {
			prefExchange = "AUTO"
		}
		routing := RouteOrder(orderReq.Symbol, orderReq.Side, orderReq.Price, prefExchange)

		fillPrice := routing.FillPrice
		routedExchange := routing.RoutedExchange
		priceImprovement := routing.PriceImprovementBps
		exchangesSearched := routing.ExchangesSearched

		var fillQty float64
		status := "FILLED"
		engineUsed := false
		var engineError string

		// Try the matching engine
		if o.matchClient != nil && o.matchClient.IsEnabled() {
			matchJSON := fmt.Sprintf(
				`{"id":%d,"instrument_id":%d,"price":%.0f,"qty":%.0f,"side":"%s","type":"%s"}`,
				orderID, instID, fillPrice*100000000, orderReq.Qty*100000000, side, orderType,
			)
			resp, err := o.matchClient.SendOrderJSON(matchJSON)
			if err == nil {
				var meResp MatchingEngineResponse
				if json.Unmarshal([]byte(resp), &meResp) == nil {
					engineUsed = true
					if meResp.Success {
						if meResp.FillPrice > 0 {
							fillPrice = float64(meResp.FillPrice) / 100000000.0
							fillQty = float64(meResp.FillQty) / 100000000.0
						}
						status = meResp.Status
					} else {
						status = "REJECTED"
						engineError = meResp.Error
						o.RecordReject()
					}
				}
			} else {
				o.logger.Warn("matching engine call failed, falling back to sim", "error", err)
			}
		}

		// Fallback: simulated fill
		if !engineUsed {
			fillQty = orderReq.Qty
		}

		latencyNs := uint64(time.Since(start).Nanoseconds())
		if status == "FILLED" || !engineUsed {
			o.RecordTrade()
		}
		o.RecordLatency(latencyNs)

		// Broadcast trade via WebSocket
		if status == "FILLED" {
			o.wsHub.BroadcastTrade(TradePayload{
				ID:        execID,
				Symbol:    orderReq.Symbol,
				Side:      orderReq.Side,
				Qty:       fillQty,
				Price:     fillPrice,
				Timestamp: time.Now().UnixMilli(),
			})
		}

		o.logger.Info("order processed",
			"cl_ord_id", orderReq.ClientOrdID,
			"symbol", orderReq.Symbol,
			"side", orderReq.Side,
			"qty", orderReq.Qty,
			"fill_price", fillPrice,
			"status", status,
			"engine", engineUsed,
			"latency_ns", latencyNs,
		)

		// Async database persistence
		go func(clOrdID string, instID uint64, price float64, qty float64, side string, status string, fPx float64, fQty float64) {
			if o.db == nil {
				return
			}
			sideInt := 0
			if side == "SELL" {
				sideInt = 1
			}
			now := time.Now().UnixNano()

			tx, err := o.db.Begin()
			if err != nil {
				o.logger.Error("failed to begin db transaction", "error", err)
				return
			}
			defer tx.Rollback()

			res, err := tx.Exec(`
				INSERT INTO orders (cl_order_id, instrument_id, price, qty, side, status, account_id, client_id, strategy_id, created_at_ns, updated_at_ns)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				clOrdID, instID, int64(price*100000000), int64(qty*100000000), sideInt, status, 1, 1, 1, now, now,
			)
			if err != nil {
				o.logger.Error("failed to insert order to db", "error", err)
				return
			}

			if status == "FILLED" {
				orderDBID, err := res.LastInsertId()
				if err != nil {
					o.logger.Error("failed to get last insert id for order", "error", err)
					return
				}

				_, err = tx.Exec(`
					INSERT INTO trades (order_id, instrument_id, execution_price, execution_qty, side, maker_taker, executed_at_ns)
					VALUES (?, ?, ?, ?, ?, ?, ?)`,
					orderDBID, instID, int64(fPx*100000000), int64(fQty*100000000), sideInt, "TAKER", now,
				)
				if err != nil {
					o.logger.Error("failed to insert trade to db", "error", err)
					return
				}
			}

			if err := tx.Commit(); err != nil {
				o.logger.Error("failed to commit db transaction", "error", err)
			}
		}(orderReq.ClientOrdID, instID, orderReq.Price, orderReq.Qty, orderReq.Side, status, fillPrice, fillQty)

		var alpacaOrderID string
		var alpacaStatus string

		// Also forward to Alpaca Paper API if configured
		alpacaResp, alpacaErr := o.SendOrderToAlpaca(orderReq.Symbol, orderReq.Qty, orderReq.Side, orderReq.OrderType, orderReq.Price)
		if alpacaErr == nil {
			o.logger.Info("Successfully forwarded order to Alpaca Paper API", "response", alpacaResp)
			var alpacaMap map[string]interface{}
			if json.Unmarshal([]byte(alpacaResp), &alpacaMap) == nil {
				if idVal, ok := alpacaMap["id"].(string); ok {
					alpacaOrderID = idVal
				}
				if statusVal, ok := alpacaMap["status"].(string); ok {
					alpacaStatus = statusVal
				}
			}
		} else {
			o.logger.Warn("Failed to forward order to Alpaca Paper API", "error", alpacaErr)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		respPayload := map[string]interface{}{
			"status":                 status,
			"exec_id":                execID,
			"cl_ord_id":              orderReq.ClientOrdID,
			"symbol":                 orderReq.Symbol,
			"side":                   orderReq.Side,
			"qty":                    fillQty,
			"fill_price":             fillPrice,
			"latency_ns":             latencyNs,
			"engine":                 engineUsed,
			"routed_exchange":        routedExchange,
			"price_improvement_bps":  priceImprovement,
			"exchanges_searched":     exchangesSearched,
			"execution_summary":      fmt.Sprintf("Routed via %s (%d exchanges searched, +%.1fbps savings)", routedExchange, exchangesSearched, priceImprovement),
		}
		if engineError != "" {
			respPayload["error"] = engineError
		}
		if alpacaOrderID != "" {
			respPayload["alpaca_order_id"] = alpacaOrderID
			respPayload["alpaca_status"] = alpacaStatus
		}
		json.NewEncoder(w).Encode(respPayload)
	}))))).Methods("POST")

	// WebSocket endpoint — real-time order book + trade notifications
	r.HandleFunc("/ws", o.wsHub.handleWebSocket)

	r.Handle("/metrics", promhttp.Handler())

	r.Handle("/stats", jwtAuthMiddleware(rbacMiddleware("admin")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		orders := o.orderCount.Load()
		rejects := o.rejectCount.Load()
		trades := o.tradeCount.Load()
		latSum := o.latencySum.Load()
		avgLat := uint64(0)
		if trades > 0 {
			avgLat = latSum / trades
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]uint64{
			"orders":     orders,
			"rejects":    rejects,
			"trades":     trades,
			"avg_lat_ns": avgLat,
		})
	})))).Methods("GET")

	// POST /api/analytics/pricing — Black-Scholes options pricing calculator
	r.HandleFunc("/api/analytics/pricing", func(w http.ResponseWriter, req *http.Request) {
		var reqBody struct {
			Spot   float64 `json:"spot"`
			Strike float64 `json:"strike"`
			Vol    float64 `json:"vol"`
			Rate   float64 `json:"rate"`
			Time   float64 `json:"time"`
		}
		if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
			http.Error(w, `{"error":"invalid request JSON"}`, http.StatusBadRequest)
			return
		}
		if reqBody.Spot <= 0 || reqBody.Strike <= 0 || reqBody.Vol <= 0 || reqBody.Time <= 0 {
			http.Error(w, `{"error":"spot, strike, vol, and time must be positive numbers"}`, http.StatusBadRequest)
			return
		}

		// Black-Scholes Formula
		d1 := (math.Log(reqBody.Spot/reqBody.Strike) + (reqBody.Rate+reqBody.Vol*reqBody.Vol/2.0)*reqBody.Time) / (reqBody.Vol * math.Sqrt(reqBody.Time))
		d2 := d1 - reqBody.Vol*math.Sqrt(reqBody.Time)

		// Cumulative Standard Normal Distribution Approximation (Abramowitz & Stegun)
		var cnd func(float64) float64
		cnd = func(x float64) float64 {
			if x < 0 {
				return 1.0 - cnd(-x)
			}
			k := 1.0 / (1.0 + 0.2316419*x)
			a1, a2, a3, a4, a5 := 0.319381530, -0.356563782, 1.781477937, -1.821255978, 1.330274429
			n := 1.0 - (1.0/math.Sqrt(2.0*math.Pi))*math.Exp(-0.5*x*x)*(a1*k+a2*k*k+a3*math.Pow(k, 3)+a4*math.Pow(k, 4)+a5*math.Pow(k, 5))
			return n
		}

		callPrice := reqBody.Spot*cnd(d1) - reqBody.Strike*math.Exp(-reqBody.Rate*reqBody.Time)*cnd(d2)
		putPrice := reqBody.Strike*math.Exp(-reqBody.Rate*reqBody.Time)*cnd(-d2) - reqBody.Spot*cnd(-d1)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"call_price": callPrice,
			"put_price":  putPrice,
			"d1":         d1,
			"d2":         d2,
		})
	}).Methods("POST")

	// POST /api/analytics/var — Value-at-Risk & Conditional Value-at-Risk
	r.HandleFunc("/api/analytics/var", func(w http.ResponseWriter, req *http.Request) {
		var reqBody struct {
			Weights    map[string]float64 `json:"weights"`
			Confidence float64            `json:"confidence"`
		}
		if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
			http.Error(w, `{"error":"invalid request JSON"}`, http.StatusBadRequest)
			return
		}
		if reqBody.Confidence <= 0 || reqBody.Confidence >= 1 {
			reqBody.Confidence = 0.95
		}

		// Parametric VaR mock based on simulated covariance
		volMap := map[string]float64{"BTC": 0.65, "ETH": 0.70, "AAPL": 0.25}
		var portVol float64
		for sym, w := range reqBody.Weights {
			vol := volMap[sym]
			if vol == 0 {
				vol = 0.30
			}
			portVol += w * vol
		}

		// Z-score approximation
		zScore := 1.645 // Default 95%
		if reqBody.Confidence > 0.98 {
			zScore = 2.33 // 99%
		}

		varValue := 10000000.0 // Simulated portfolio of $10M
		varCalc := varValue * portVol * zScore * math.Sqrt(1.0/252.0)
		cvarCalc := varCalc * (math.Exp(-zScore*zScore/2.0) / (math.Sqrt(2.0*math.Pi) * (1.0 - reqBody.Confidence)))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"portfolio_value": varValue,
			"var_1d":          varCalc,
			"cvar_1d":         cvarCalc,
			"volatility_ann":  portVol,
			"confidence":      reqBody.Confidence,
		})
	}).Methods("POST")

	// POST /api/ai/chat — Quantitative Multi-Agent Chat Assistant
	r.Handle("/api/ai/chat", jwtAuthMiddleware(rbacMiddleware("trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		proxyReq, err := http.NewRequest("POST", "http://127.0.0.1:8000/chat", req.Body)
		if err != nil {
			http.Error(w, `{"error":"failed to create proxy request"}`, http.StatusInternalServerError)
			return
		}
		proxyReq.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 15 * time.Second}
		proxyResp, err := client.Do(proxyReq)
		if err != nil {
			http.Error(w, `{"error":"failed to reach python ai-agent"}`, http.StatusBadGateway)
			return
		}
		defer proxyResp.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(proxyResp.StatusCode)
		io.Copy(w, proxyResp.Body)
	})))).Methods("POST")

	// POST /api/ai/trade_decision — Autonomous AI Agent Trade Evaluation
	r.Handle("/api/ai/trade_decision", jwtAuthMiddleware(rbacMiddleware("trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		proxyReq, err := http.NewRequest("POST", "http://127.0.0.1:8000/trade_decision", req.Body)
		if err != nil {
			http.Error(w, `{"error":"failed to create proxy request"}`, http.StatusInternalServerError)
			return
		}
		proxyReq.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 15 * time.Second}
		proxyResp, err := client.Do(proxyReq)
		if err != nil {
			http.Error(w, `{"error":"failed to reach python ai-agent"}`, http.StatusBadGateway)
			return
		}
		defer proxyResp.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(proxyResp.StatusCode)
		io.Copy(w, proxyResp.Body)
	})))).Methods("POST")

	// GET /api/ai/macro_feed — Fetch real-time macro news feed from python agent
	r.HandleFunc("/api/ai/macro_feed", func(w http.ResponseWriter, req *http.Request) {
		proxyReq, err := http.NewRequest("GET", "http://127.0.0.1:8000/macro_news", nil)
		if err != nil {
			http.Error(w, `{"error":"failed to create proxy request"}`, http.StatusInternalServerError)
			return
		}
		client := &http.Client{Timeout: 5 * time.Second}
		proxyResp, err := client.Do(proxyReq)
		if err != nil {
			http.Error(w, `{"error":"failed to reach python ai-agent"}`, http.StatusBadGateway)
			return
		}
		defer proxyResp.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(proxyResp.StatusCode)
		io.Copy(w, proxyResp.Body)
	}).Methods("GET")

	// GET /api/sor/prices — Fetch real-time simulated prices across major exchanges
	r.HandleFunc("/api/sor/prices", func(w http.ResponseWriter, req *http.Request) {
		symbol := req.URL.Query().Get("symbol")
		if symbol == "" {
			symbol = "BTC/USD"
		}

		basePrice := 64500.0
		switch symbol {
		case "BTC/USD":
			basePrice = 64500.0
		case "ETH/USD":
			basePrice = 3450.0
		case "SOL/USD":
			basePrice = 145.0
		case "AAPL":
			basePrice = 185.30
		case "MSFT":
			basePrice = 420.0
		case "TSLA":
			basePrice = 175.0
		case "NVDA":
			basePrice = 120.0
		case "EUR/USD":
			basePrice = 1.0850
		}

		quotes := GenerateQuotes(symbol, basePrice)
		displayExchanges := []string{"NYSE", "NASDAQ", "LSE", "Xetra", "Tradegate"}
		var result []ExchangeQuote
		for _, name := range displayExchanges {
			for _, q := range quotes {
				if q.Exchange == name {
					result = append(result, q)
					break
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}).Methods("GET")

	// GET /api/screener — Fetch assets list with screener metrics
	r.HandleFunc("/api/screener", func(w http.ResponseWriter, req *http.Request) {
		type ScreenerAsset struct {
			Symbol        string  `json:"symbol"`
			Name          string  `json:"name"`
			AssetClass    string  `json:"asset_class"`
			Price         float64 `json:"price"`
			MarketCapBill float64 `json:"market_cap_bill"`
			PeRatio       float64 `json:"pe_ratio"`
			DivYield      float64 `json:"div_yield"`
			Country       string  `json:"country"`
		}
		assets := []ScreenerAsset{
			{"BTC/USD", "Bitcoin", "Crypto", 64500.5, 1260.5, 0.0, 0.0, "Global"},
			{"ETH/USD", "Ethereum", "Crypto", 3450.2, 412.3, 0.0, 0.0, "Global"},
			{"SOL/USD", "Solana", "Crypto", 145.0, 62.8, 0.0, 0.0, "Global"},
			{"AAPL", "Apple Inc.", "Equities", 185.30, 2890.0, 28.5, 0.52, "US"},
			{"MSFT", "Microsoft Corp.", "Equities", 420.0, 3120.0, 35.2, 0.71, "US"},
			{"TSLA", "Tesla Inc.", "Equities", 175.0, 560.0, 60.1, 0.0, "US"},
			{"NVDA", "NVIDIA Corp.", "Equities", 120.0, 2980.0, 72.4, 0.03, "US"},
			{"EUR/USD", "Euro / US Dollar", "FX", 1.0850, 0.0, 0.0, 0.0, "EU"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(assets)
	}).Methods("GET")

	// GET /api/heatmap — Fetch sector-wise daily change heatmap data
	r.HandleFunc("/api/heatmap", func(w http.ResponseWriter, req *http.Request) {
		type HeatmapNode struct {
			Name   string  `json:"name"`
			Value  float64 `json:"value"`
			Change float64 `json:"change"`
		}
		type HeatmapSector struct {
			SectorName string        `json:"sector_name"`
			Nodes      []HeatmapNode `json:"nodes"`
		}
		heatmap := []HeatmapSector{
			{
				SectorName: "Technology",
				Nodes: []HeatmapNode{
					{"AAPL", 2890.0, 0.52},
					{"MSFT", 3120.0, -0.34},
					{"NVDA", 2980.0, 4.12},
				},
			},
			{
				SectorName: "Automotive",
				Nodes: []HeatmapNode{
					{"TSLA", 560.0, -1.85},
				},
			},
			{
				SectorName: "Cryptocurrency",
				Nodes: []HeatmapNode{
					{"BTC/USD", 1260.5, 2.45},
					{"ETH/USD", 412.3, -1.18},
					{"SOL/USD", 62.8, 5.76},
				},
			},
			{
				SectorName: "Foreign Exchange",
				Nodes: []HeatmapNode{
					{"EUR/USD", 150.0, 0.08},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(heatmap)
	}).Methods("GET")

	// GET /api/alpaca/account — Fetch Alpaca account details (JWT Trader required)
	r.Handle("/api/alpaca/account", jwtAuthMiddleware(rbacMiddleware("trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		alpacaEndpoint := os.Getenv("ALPACA_API_ENDPOINT")
		if alpacaEndpoint == "" {
			alpacaEndpoint = "https://paper-api.alpaca.markets/v2"
		}
		keyID := os.Getenv("ALPACA_API_KEY_ID")
		secretKey := os.Getenv("ALPACA_API_SECRET_KEY")

		if keyID == "" || secretKey == "" {
			http.Error(w, `{"error":"Alpaca credentials not configured"}`, http.StatusBadRequest)
			return
		}

		u := fmt.Sprintf("%s/account", alpacaEndpoint)
		rReq, err := http.NewRequest("GET", u, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rReq.Header.Set("APCA-API-KEY-ID", keyID)
		rReq.Header.Set("APCA-API-SECRET-KEY", secretKey)

		client := &http.Client{}
		resp, err := client.Do(rReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	})))).Methods("GET")

	// GET /api/alpaca/positions — Fetch Alpaca positions (JWT Trader required)
	r.Handle("/api/alpaca/positions", jwtAuthMiddleware(rbacMiddleware("trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		alpacaEndpoint := os.Getenv("ALPACA_API_ENDPOINT")
		if alpacaEndpoint == "" {
			alpacaEndpoint = "https://paper-api.alpaca.markets/v2"
		}
		keyID := os.Getenv("ALPACA_API_KEY_ID")
		secretKey := os.Getenv("ALPACA_API_SECRET_KEY")

		if keyID == "" || secretKey == "" {
			http.Error(w, `{"error":"Alpaca credentials not configured"}`, http.StatusBadRequest)
			return
		}

		u := fmt.Sprintf("%s/positions", alpacaEndpoint)
		rReq, err := http.NewRequest("GET", u, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rReq.Header.Set("APCA-API-KEY-ID", keyID)
		rReq.Header.Set("APCA-API-SECRET-KEY", secretKey)

		client := &http.Client{}
		resp, err := client.Do(rReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	})))).Methods("GET")

	// Apply middleware chain: requestID → rateLimit → router
	handler := requestIDMiddleware(rateLimitMiddleware(1000, r))

	// Apply CORS — allow localhost:3000 (Next.js dev) and all origins for WebSocket upgrade
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000", "http://localhost:3001"},
		AllowCredentials: true,
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Request-ID"},
	})
	handler = c.Handler(handler)

	return &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      handler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
}

// SendOrderToAlpaca posts a new order to the Alpaca Paper Trading API
func (o *Orchestrator) SendOrderToAlpaca(symbol string, qty float64, side string, orderType string, price float64) (string, error) {
	alpacaEndpoint := os.Getenv("ALPACA_API_ENDPOINT")
	if alpacaEndpoint == "" {
		alpacaEndpoint = "https://paper-api.alpaca.markets/v2"
	}
	keyID := os.Getenv("ALPACA_API_KEY_ID")
	secretKey := os.Getenv("ALPACA_API_SECRET_KEY")

	if keyID == "" || secretKey == "" {
		return "", fmt.Errorf("Alpaca credentials not configured")
	}

	url := fmt.Sprintf("%s/orders", alpacaEndpoint)

	alpacaSide := "buy"
	if strings.ToUpper(side) == "SELL" {
		alpacaSide = "sell"
	}

	alpacaType := "limit"
	if strings.ToUpper(orderType) == "MARKET" {
		alpacaType = "market"
	}

	reqBody := map[string]interface{}{
		"symbol":        symbol,
		"qty":           fmt.Sprintf("%.4f", qty),
		"side":          alpacaSide,
		"type":          alpacaType,
		"time_in_force": "gtc",
	}

	if alpacaType == "limit" {
		reqBody["limit_price"] = fmt.Sprintf("%.2f", price)
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("APCA-API-KEY-ID", keyID)
	req.Header.Set("APCA-API-SECRET-KEY", secretKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("Alpaca API returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return string(bodyBytes), nil
}

// ============================================================================
// Main
// ============================================================================

// Main moved to main.go
