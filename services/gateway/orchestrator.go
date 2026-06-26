package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/cors"
)

// MatchingEngineClient manages a TCP connection to the C++ matching engine.
type MatchingEngineClient struct {
	mu       sync.Mutex
	addr     string
	conn     net.Conn
	reader   *bufio.Reader
	enabled  bool
	lastErr  string
}

func NewMatchingEngineClient(host string, port int) *MatchingEngineClient {
	return &MatchingEngineClient{
		addr:    fmt.Sprintf("%s:%d", host, port),
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

func (c *MatchingEngineClient) IsEnabled() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.enabled }
func (c *MatchingEngineClient) LastError() string { c.mu.Lock(); defer c.mu.Unlock(); return c.lastErr }

// OrderResponse from the matching engine
type MatchingEngineResponse struct {
	OrderID      uint64 `json:"order_id"`
	InstrumentID uint32 `json:"instrument_id"`
	FillPrice    uint32 `json:"fill_price"`
	FillQty      uint32 `json:"fill_qty"`
	Status       string `json:"status"`
	Success      bool   `json:"success"`
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
	Side        string  `json:"side"`   // BUY or SELL
	Price       float64 `json:"price"`
	Qty         float64 `json:"qty"`
	OrderType   string  `json:"order_type"` // LIMIT or MARKET
	ClientOrdID string  `json:"cl_ord_id"`
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
			MaxDrawdownLimit: 0.10,
			MarketDataPort:   8080,
			OrderEntryPort:   9090,
			MaxOrderRate:     10000,
			MaxCancelRate:    5000,
			MaxPositionLimit: 100000,
		},
		shutdownCh:  make(chan struct{}),
		logger:      slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})),
		wsHub:       NewWebSocketHub(),
		matchClient: NewMatchingEngineClient("127.0.0.1", 9092), // Route to Risk Analytics instead of Execution Core
	}
	orch.loadConfig()
	return orch
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

func (o *Orchestrator) RecordOrder()          { o.orderCount.Add(1) }
func (o *Orchestrator) RecordReject()         { o.rejectCount.Add(1) }
func (o *Orchestrator) RecordTrade()          { o.tradeCount.Add(1) }
func (o *Orchestrator) RecordLatency(ns uint64) { o.latencySum.Add(ns) }

func (o *Orchestrator) Shutdown() {
	close(o.shutdownCh)
	o.wg.Wait()
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

	r.HandleFunc("/services", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(o.GetServices())
	}).Methods("GET")

	r.Handle("/config", jwtAuthMiddleware(rbacMiddleware("admin")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(o.GetConfig())
	})))).Methods("GET")

	// POST /config — hot-reload risk parameters (JWT Admin required)
	r.Handle("/config", jwtAuthMiddleware(rbacMiddleware("admin")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
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
	})))).Methods("POST")

	// POST /order — submit a new order (JWT Trader required)
	// Forwards to the matching engine TCP server, or falls back to simulated fill.
	r.Handle("/order", jwtAuthMiddleware(rbacMiddleware("trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
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
		case "BTC/USD": instID = 1
		case "ETH/USD": instID = 2
		case "AAPL":    instID = 3
		case "EUR/USD": instID = 4
		}

		side := "BID"
		if orderReq.Side == "SELL" {
			side = "ASK"
		}

		orderType := "LIMIT"
		if orderReq.OrderType == "MARKET" {
			orderType = "MARKET"
		}

		fillPrice := orderReq.Price
		var fillQty float64
		status := "FILLED"
		engineUsed := false

		// Try the matching engine
		if o.matchClient != nil && o.matchClient.IsEnabled() {
			matchJSON := fmt.Sprintf(
				`{"id":%d,"instrument_id":%d,"price":%.0f,"qty":%.0f,"side":"%s","type":"%s"}`,
				orderID, instID, orderReq.Price*10000, orderReq.Qty, side, orderType,
			)
			resp, err := o.matchClient.SendOrderJSON(matchJSON)
			if err == nil {
				var meResp MatchingEngineResponse
				if json.Unmarshal([]byte(resp), &meResp) == nil {
					engineUsed = true
					if meResp.Success {
						if meResp.FillPrice > 0 {
							fillPrice = float64(meResp.FillPrice) / 10000.0
							fillQty = float64(meResp.FillQty)
						}
						status = meResp.Status
					} else {
						status = "REJECTED"
						o.RecordReject()
					}
				}
			} else {
				o.logger.Warn("matching engine call failed, falling back to sim", "error", err)
			}
		}

		// Fallback: simulated fill
		if !engineUsed {
			if orderReq.OrderType == "MARKET" || fillPrice == 0 {
				slippage := orderReq.Price * 0.00005
				if orderReq.Side == "BUY" {
					fillPrice = orderReq.Price + slippage
				} else {
					fillPrice = orderReq.Price - slippage
				}
			}
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

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":      status,
			"exec_id":     execID,
			"cl_ord_id":   orderReq.ClientOrdID,
			"symbol":      orderReq.Symbol,
			"side":        orderReq.Side,
			"qty":         fillQty,
			"fill_price":  fillPrice,
			"latency_ns":  latencyNs,
			"engine":      engineUsed,
		})
	})))).Methods("POST")

	// WebSocket endpoint — real-time order book + trade notifications
	r.HandleFunc("/ws", o.wsHub.handleWebSocket)

	r.Handle("/metrics", promhttp.Handler())

	r.Handle("/stats", jwtAuthMiddleware(rbacMiddleware("admin")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		orders   := o.orderCount.Load()
		rejects  := o.rejectCount.Load()
		trades   := o.tradeCount.Load()
		latSum   := o.latencySum.Load()
		avgLat   := uint64(0)
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

// ============================================================================
// Main
// ============================================================================

// Main moved to main.go
