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

// engineCmd is sent to the matching engine's dedicated I/O goroutine.
type engineCmd struct {
	orderJSON string
	resp      chan engineResp
}

type engineResp struct {
	data string
	err  error
}

// MatchingEngineClient manages a TCP connection to the C++ matching engine.
type MatchingEngineClient struct {
	mu      sync.Mutex
	addr    string
	conn    net.Conn
	reader  *bufio.Reader
	enabled bool
	lastErr string

	cmdCh chan engineCmd
}

func NewMatchingEngineClient(host string, port int) *MatchingEngineClient {
	return &MatchingEngineClient{
		addr:   net.JoinHostPort(host, strconv.Itoa(port)),
		enabled: false,
		cmdCh:  make(chan engineCmd, 64),
	}
}

func (c *MatchingEngineClient) Connect() error {
	c.mu.Lock()
	conn, err := net.DialTimeout("tcp", c.addr, 2*time.Second)
	if err != nil {
		c.enabled = false
		c.lastErr = err.Error()
		c.mu.Unlock()
		return err
	}
	c.conn = conn
	c.reader = bufio.NewReaderSize(conn, 4096)
	c.enabled = true
	c.lastErr = ""
	c.mu.Unlock()

	go c.ioLoop()
	return nil
}

// ioLoop is a dedicated goroutine that handles all I/O on the connection.
// This prevents head-of-line blocking by keeping send/receive off the caller's goroutine.
func (c *MatchingEngineClient) ioLoop() {
	for cmd := range c.cmdCh {
		c.mu.Lock()
		if c.conn == nil {
			c.mu.Unlock()
			cmd.resp <- engineResp{err: fmt.Errorf("not connected")}
			continue
		}
		conn := c.conn
		reader := c.reader
		c.mu.Unlock()

		if _, err := fmt.Fprint(conn, cmd.orderJSON+"\n"); err != nil {
			c.mu.Lock()
			c.enabled = false
			c.lastErr = err.Error()
			conn.Close()
			c.conn = nil
			c.mu.Unlock()
			cmd.resp <- engineResp{err: err}
			continue
		}
		resp, err := reader.ReadString('\n')
		if err != nil {
			c.mu.Lock()
			c.enabled = false
			c.lastErr = err.Error()
			conn.Close()
			c.conn = nil
			c.mu.Unlock()
			cmd.resp <- engineResp{err: err}
			continue
		}
		cmd.resp <- engineResp{data: resp}
	}
}

func (c *MatchingEngineClient) SendOrderJSON(orderJSON string) (string, error) {
	respCh := make(chan engineResp, 1)
	c.cmdCh <- engineCmd{orderJSON: orderJSON, resp: respCh}
	r := <-respCh
	return r.data, r.err
}

func (c *MatchingEngineClient) HealthCheck() bool {
	respCh := make(chan engineResp, 1)
	c.cmdCh <- engineCmd{orderJSON: "health", resp: respCh}
	r := <-respCh
	return r.err == nil && strings.Contains(r.data, "ok")
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
	Price       int64  `json:"price"` // Fixed-point (1e8)
	Qty         int64  `json:"qty"`   // Fixed-point (1e8)
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

	// Institutional compliance modules (Gap closure Wave 1-6)
	killSwitch      *KillSwitchManager
	surveillance    *SurveillanceEngine
	timeSync        *TimeSyncMonitor
	bestExecution   *BestExecutionMonitor
	encryption      *EncryptionService
	hsmClient       HSMClient
	failover        *FailoverManager
	// aiRateLimit tracks AI signal rate for feedback-loop prevention
	aiOrderCount    atomic.Uint64
	aiLastResetNs   atomic.Int64
}

func NewOrchestrator() *Orchestrator {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	wsHub := NewWebSocketHub()

	// Encryption service (must be first — used by MFA and HSM)
	enc, encErr := NewEncryptionService()
	if encErr != nil {
		logger.Error("failed to initialize encryption service", "error", encErr)
		os.Exit(1)
	}

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
		logger:      logger,
		wsHub:       wsHub,
		matchClient: NewMatchingEngineClient("127.0.0.1", PortRiskHealth),
		encryption:  enc,
	}
	orch.loadConfig()
	orch.initDB()

	// Initialize institutional compliance modules after DB is ready
	orch.killSwitch = NewKillSwitchManager(orch.db, logger, wsHub)
	orch.surveillance = NewSurveillanceEngine(orch.db, logger)
	orch.bestExecution = NewBestExecutionMonitor(orch.db, logger)
	orch.hsmClient = NewCloudHSMClient(enc)
	orch.failover = NewFailoverManager(
		os.Getenv("ROBIN_PRIMARY_ADDR"),
		os.Getenv("ROBIN_STANDBY_ADDR"),
		logger,
	)

	// Time sync monitor (NTP/PTP)
	ntpServer := os.Getenv("ROBIN_NTP_SERVER")
	ptpGM := os.Getenv("ROBIN_PTP_GRANDMASTER")
	orch.timeSync = NewTimeSyncMonitor(ntpServer, ptpGM, logger)

	return orch
}

func (o *Orchestrator) initDB() {
	db, err := sql.Open("sqlite3", "robin.db?_journal_mode=WAL&_synchronous=FULL&_busy_timeout=5000")
	if err != nil {
		o.logger.Error("failed to open sqlite database", "error", err)
		return
	}

	// SQLite WAL mode for improved write durability (RTO/RPO gap closure)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(1 * time.Hour)

	// Enable WAL mode explicitly
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=FULL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA temp_store=MEMORY;",
		"PRAGMA cache_size=-64000;", // 64MB page cache
	} {
		if _, err := db.Exec(pragma); err != nil {
			o.logger.Warn("SQLite PRAGMA failed", "pragma", pragma, "error", err)
		}
	}

	o.db = db

	// Migration check for missing columns in pre-existing tables
	migrations := []struct{ table, colName, colType string }{
		// orders
		{"orders", "algo_id", "TEXT NOT NULL DEFAULT ''"},
		{"orders", "decision_maker", "TEXT NOT NULL DEFAULT ''"},
		{"orders", "liquidity_provision", "INTEGER NOT NULL DEFAULT 0"},
		{"orders", "fdid", "TEXT NOT NULL DEFAULT ''"},
		{"orders", "rfid", "TEXT NOT NULL DEFAULT ''"},
		{"orders", "manta", "TEXT NOT NULL DEFAULT ''"},
		{"orders", "exchange", "TEXT NOT NULL DEFAULT ''"},
		{"orders", "entry_time_ns", "INTEGER NOT NULL DEFAULT 0"},
		{"orders", "first_route_ns", "INTEGER NOT NULL DEFAULT 0"},
		// trades
		{"trades", "fee", "INTEGER NOT NULL DEFAULT 0"},
		{"trades", "slippage_bps", "INTEGER NOT NULL DEFAULT 0"},
		// audit_log
		{"audit_log", "sequence_monotonic", "INTEGER NOT NULL DEFAULT 0"},
		{"audit_log", "gps_time_ns", "INTEGER NOT NULL DEFAULT 0"},
		{"audit_log", "user_id", "INTEGER NOT NULL DEFAULT 0"},
		{"audit_log", "ip_address", "TEXT NOT NULL DEFAULT ''"},
		{"audit_log", "retention_expires_at_ns", "INTEGER NOT NULL DEFAULT 0"},
	}
	for _, m := range migrations {
		db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;", m.table, m.colName, m.colType))
	}

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

	o.logger.Info("SQLite database initialized successfully (WAL mode, FULL sync)")
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
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Institutional Setup:
				// Do NOT generate synthetic prices. Wait for real market data updates 
				// from the execution core via UDP multicast or shared memory ring buffers.
				// (See: INST-004 Market Data Integration).
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
		statusLabel := "active"
		if err != nil {
			svc.Status = StatusFailed
			svc.CheckErr = err.Error()
			failed++
			statusLabel = "failed"
			o.logger.Warn("service health check failed", "name", name, "addr", addr, "error", err)
		} else {
			conn.Close()
			svc.CheckErr = ""
			if latency > 10*time.Millisecond {
				svc.Status = StatusDegraded
				degraded++
				statusLabel = "degraded"
				o.logger.Warn("service degraded", "name", name, "latency_ms", latency.Milliseconds())
			} else {
				svc.Status = StatusActive
				healthy++
			}
		}
		ServiceHealthLatency.WithLabelValues(name, statusLabel).Observe(float64(latency.Nanoseconds()))
		connectionVal := 0.0
		if err == nil {
			connectionVal = 1.0
		}
		ConnectionStatus.WithLabelValues(name).Set(connectionVal)
		o.mu.Unlock()
	}

	o.healthyCount.Store(healthy)
	o.degradedCount.Store(degraded)
	o.failedCount.Store(failed)
}

func (o *Orchestrator) HotReloadConfig(jsonConfig []byte) error {
	var newConfig HotReloadConfig
	if err := json.Unmarshal(jsonConfig, &newConfig); err != nil {
		return fmt.Errorf("config parse error: %w", err)
	}

	// Validate config values BEFORE applying
	if newConfig.MaxDrawdownLimit <= 0 || newConfig.MaxDrawdownLimit > 1.0 {
		return fmt.Errorf("invalid max_drawdown_limit: %f (must be 0 < limit <= 1.0)", newConfig.MaxDrawdownLimit)
	}
	if newConfig.MaxOrderRate == 0 || newConfig.MaxOrderRate > 100000 {
		return fmt.Errorf("invalid max_order_rate: %d (must be 1-100000)", newConfig.MaxOrderRate)
	}
	if newConfig.MaxCancelRate > 100000 {
		return fmt.Errorf("invalid max_cancel_rate: %d (must be 0-100000)", newConfig.MaxCancelRate)
	}
	if newConfig.MaxPositionLimit < 0 {
		return fmt.Errorf("invalid max_position_limit: %d (must be >= 0)", newConfig.MaxPositionLimit)
	}

	o.configMutex.Lock()
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
	return os.WriteFile("config_state.json", data, 0600)
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

func (o *Orchestrator) RecordOrderHistogram(symbol, side, status string, latencyNs float64) {
	OrderLatency.WithLabelValues(symbol, side, status).Observe(latencyNs)
	OrderCount.WithLabelValues(symbol, side, status).Inc()
}

func (o *Orchestrator) RecordTradeHistogram(symbol, side string, latencyNs float64) {
	TradeLatency.WithLabelValues(symbol, side).Observe(latencyNs)
	TradeCount.WithLabelValues(symbol, side).Inc()
}

func (o *Orchestrator) RecordRejectHistogram(symbol, reason string) {
	RejectCount.WithLabelValues(symbol, reason).Inc()
}

func (o *Orchestrator) RecordRiskCheckHistogram(checkType string, latencyNs float64) {
	RiskCheckLatency.WithLabelValues(checkType).Observe(latencyNs)
}

func (o *Orchestrator) RecordMatchingEngineHistogram(instrumentID string, latencyNs float64) {
	MatchingEngineLatency.WithLabelValues(instrumentID).Observe(latencyNs)
}

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

// jwtAuthMiddleware validates incoming JWT tokens in the Authorization header.
func jwtAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized: missing token"})
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := jwtAuth.verify(tokenStr)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("unauthorized: %v", err)})
			return
		}

		ctx := context.WithValue(r.Context(), contextKeyJWTClaims, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// rbacMiddleware checks if the authenticated user has one of the allowed roles.
func rbacMiddleware(allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(contextKeyJWTClaims).(jwt.MapClaims)
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized: claims missing"})
				return
			}

			role, _ := claims["role"].(string)
			allowed := false
			for _, r := range allowedRoles {
				if r == role {
					allowed = true
					break
				}
			}

			if !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{"error": "forbidden: insufficient permissions"})
				return
			}

			next.ServeHTTP(w, r)
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

		// Dynamic Symbol Mapping
		// In production, this map should be loaded from a configuration or database at startup
		// to allow adding new symbols without recompiling the orchestrator.
		symbolMap := map[string]uint64{
			"BTC/USD": 1,
			"ETH/USD": 2,
			"AAPL":    3,
			"EUR/USD": 4,
		}
		
		instID, ok := symbolMap[orderReq.Symbol]
		if !ok {
			http.Error(w, `{"error":"unknown symbol"}`, http.StatusBadRequest)
			return
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
		routing := RouteOrder(orderReq.Symbol, orderReq.Side, float64(orderReq.Price)/100000000.0, prefExchange)

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
				`{"id":%d,"instrument_id":%d,"price":%d,"qty":%d,"side":"%s","type":"%s"}`,
				orderID, instID, int64(fillPrice*100000000), orderReq.Qty, side, orderType,
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

		// Explicit validation: Do not silently fallback to simulated fills on the hot path
		if !engineUsed {
			http.Error(w, `{"error":"MATCHING_ENGINE_UNAVAILABLE"}`, http.StatusServiceUnavailable)
			return
		}

		latencyNs := uint64(time.Since(start).Nanoseconds())
		if status == "FILLED" || !engineUsed {
			o.RecordTrade()
			o.RecordTradeHistogram(orderReq.Symbol, orderReq.Side, float64(latencyNs))
		}
		o.RecordLatency(latencyNs)
		o.RecordOrderHistogram(orderReq.Symbol, orderReq.Side, status, float64(latencyNs))
		if engineError != "" {
			o.RecordRejectHistogram(orderReq.Symbol, engineError)
		}
		if o.matchClient != nil && o.matchClient.IsEnabled() {
			o.RecordMatchingEngineHistogram(fmt.Sprintf("%d", instID), float64(latencyNs))
		}

		// Broadcast trade via WebSocket and update position manager
		if status == "FILLED" {
			o.wsHub.BroadcastTrade(TradePayload{
				ID:        execID,
				Symbol:    orderReq.Symbol,
				Side:      orderReq.Side,
				Qty:       fillQty,
				Price:     fillPrice,
				Timestamp: time.Now().UnixMilli(),
			})
			// Update real-time position tracker
			if globalPositionManager != nil {
				globalPositionManager.OnFill(
					execID, orderReq.Symbol, orderReq.Side,
					fillQty, fillPrice,
				)
			}
		}

		o.logger.Info("order processed",
			"cl_ord_id", orderReq.ClientOrdID,
			"symbol", orderReq.Symbol,
			"side", orderReq.Side,
			"qty", float64(orderReq.Qty)/100000000.0,
			"fill_price", fillPrice,
			"status", status,
			"engine", engineUsed,
			"latency_ns", latencyNs,
		)

		// Synchronous database persistence (Institutional fix: no fire-and-forget)
		if o.db != nil {
			sideInt := 0
			if orderReq.Side == "SELL" {
				sideInt = 1
			}
			now := time.Now().UnixNano()
			
			tx, err := o.db.Begin()
			if err != nil {
				o.logger.Error("failed to begin db transaction", "error", err)
				http.Error(w, `{"error":"DATABASE_ERROR"}`, http.StatusInternalServerError)
				return
			}
			
			res, err := tx.Exec(`
				INSERT INTO orders (cl_order_id, instrument_id, price, qty, side, status, account_id, client_id, strategy_id, created_at_ns, updated_at_ns)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				orderReq.ClientOrdID, instID, orderReq.Price, orderReq.Qty, sideInt, status, 1, 1, 1, now, now,
			)
			if err != nil {
				tx.Rollback()
				o.logger.Error("failed to insert order to db", "error", err)
				http.Error(w, `{"error":"DATABASE_ERROR"}`, http.StatusInternalServerError)
				return
			}
			
			if status == "FILLED" {
				orderDBID, err := res.LastInsertId()
				if err == nil {
					tx.Exec(`
						INSERT INTO trades (order_id, instrument_id, execution_price, execution_qty, side, maker_taker, executed_at_ns)
						VALUES (?, ?, ?, ?, ?, ?, ?)`,
						orderDBID, instID, int64(fillPrice*100000000), int64(fillQty*100000000), sideInt, "TAKER", now,
					)
				}
			}
			
			if err := tx.Commit(); err != nil {
				o.logger.Error("failed to commit db transaction", "error", err)
				http.Error(w, `{"error":"DATABASE_ERROR"}`, http.StatusInternalServerError)
				return
			}
		}

		var alpacaOrderID string
		var alpacaStatus string

		// Also forward to Alpaca Paper API if configured
		alpacaResp, alpacaErr := o.SendOrderToAlpaca(orderReq.Symbol, float64(orderReq.Qty)/100000000.0, orderReq.Side, orderReq.OrderType, float64(orderReq.Price)/100000000.0)
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

	// ── Position & Portfolio endpoints ─────────────────────────────────────
	r.HandleFunc("/api/positions", handleGetPositions).Methods("GET")
	r.HandleFunc("/api/positions/{symbol}", handleGetPosition).Methods("GET")
	r.HandleFunc("/api/portfolio", handleGetPortfolioSummary).Methods("GET")

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

	// Analytics and VaR calculations have been moved to a dedicated risk microservice
	// to prevent blocking the high-throughput Go hot path.

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
		
		vaultClient := NewVaultClient()
		keyID, err1 := vaultClient.GetSecret("secret/data/alpaca", "API_KEY_ID")
		secretKey, err2 := vaultClient.GetSecret("secret/data/alpaca", "API_SECRET_KEY")

		if err1 != nil || err2 != nil || keyID == "" || secretKey == "" {
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
		vaultClient := NewVaultClient()
		keyID, err1 := vaultClient.GetSecret("secret/data/alpaca", "API_KEY_ID")
		secretKey, err2 := vaultClient.GetSecret("secret/data/alpaca", "API_SECRET_KEY")

		if err1 != nil || err2 != nil || keyID == "" || secretKey == "" {
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

	// GET /api/alpaca/orders — Fetch Alpaca orders (trade history) (JWT Trader required)
	r.Handle("/api/alpaca/orders", jwtAuthMiddleware(rbacMiddleware("trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		alpacaEndpoint := os.Getenv("ALPACA_API_ENDPOINT")
		if alpacaEndpoint == "" {
			alpacaEndpoint = "https://paper-api.alpaca.markets/v2"
		}
		vaultClient := NewVaultClient()
		keyID, err1 := vaultClient.GetSecret("secret/data/alpaca", "API_KEY_ID")
		secretKey, err2 := vaultClient.GetSecret("secret/data/alpaca", "API_SECRET_KEY")

		if err1 != nil || err2 != nil || keyID == "" || secretKey == "" {
			http.Error(w, `{"error":"Alpaca credentials not configured"}`, http.StatusBadRequest)
			return
		}

		u := fmt.Sprintf("%s/orders?status=all", alpacaEndpoint)
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

	// GET /api/alpaca/assets — Fetch Alpaca assets (JWT Trader required)
	r.Handle("/api/alpaca/assets", jwtAuthMiddleware(rbacMiddleware("trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		alpacaEndpoint := os.Getenv("ALPACA_API_ENDPOINT")
		if alpacaEndpoint == "" {
			alpacaEndpoint = "https://paper-api.alpaca.markets/v2"
		}
		vaultClient := NewVaultClient()
		keyID, err1 := vaultClient.GetSecret("secret/data/alpaca", "API_KEY_ID")
		secretKey, err2 := vaultClient.GetSecret("secret/data/alpaca", "API_SECRET_KEY")

		if err1 != nil || err2 != nil || keyID == "" || secretKey == "" {
			http.Error(w, `{"error":"Alpaca credentials not configured"}`, http.StatusBadRequest)
			return
		}

		u := fmt.Sprintf("%s/assets?status=active", alpacaEndpoint)
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
	// ============================================================================
	// Institutional Compliance API Routes (Wave 1-6 Gap Closure)
	// ============================================================================

	adminOnly := func(next http.Handler) http.Handler {
		return jwtAuthMiddleware(rbacMiddleware("admin")(next))
	}

	// --- Kill Switch (SEC 15c3-5 direct control) ---
	r.Handle("/api/killswitch/status", adminOnly(killSwitchStatusHandler(o.killSwitch))).Methods("GET")
	r.Handle("/api/killswitch/system/trip", adminOnly(killSwitchTripSystemHandler(o.killSwitch))).Methods("POST")
	r.Handle("/api/killswitch/system/reset/initiate", adminOnly(killSwitchInitResetHandler(o.killSwitch))).Methods("POST")
	r.Handle("/api/killswitch/system/reset/confirm", adminOnly(killSwitchConfirmResetHandler(o.killSwitch))).Methods("POST")
	r.Handle("/api/killswitch/algo/{id}/trip", adminOnly(killSwitchTripAlgoHandler(o.killSwitch))).Methods("POST")
	r.Handle("/api/killswitch/algo/{id}/reset", adminOnly(killSwitchResetAlgoHandler(o.killSwitch))).Methods("POST")
	r.Handle("/api/killswitch/trader/{id}/trip", adminOnly(killSwitchTripTraderHandler(o.killSwitch))).Methods("POST")
	r.Handle("/api/killswitch/trader/{id}/reset", adminOnly(killSwitchResetTraderHandler(o.killSwitch))).Methods("POST")
	r.Handle("/api/killswitch/log", adminOnly(killSwitchLogHandler(o.db))).Methods("GET")

	// --- CEO Certification (SEC 15c3-5 §(e)(2)) ---
	r.Handle("/api/compliance/certify", adminOnly(handleCEOCertify(o.db, o.logger))).Methods("POST")
	r.Handle("/api/compliance/certification/status", adminOnly(handleCertificationStatus(o.db))).Methods("GET")
	r.Handle("/api/compliance/certification/history", adminOnly(handleCertificationHistory(o.db))).Methods("GET")
	r.Handle("/api/compliance/review", adminOnly(handleComplianceReview(o.db, o.logger))).Methods("POST")

	// --- Supervisory Workflow (FINRA Rule 3110) ---
	r.Handle("/api/supervisory/pending", adminOnly(handleSupervisoryPending(o.db))).Methods("GET")
	r.Handle("/api/supervisory/approve/{id}", adminOnly(handleSupervisoryApprove(o.db, o.logger))).Methods("POST")
	r.Handle("/api/supervisory/reject/{id}", adminOnly(handleSupervisoryReject(o.db, o.logger))).Methods("POST")
	r.Handle("/api/supervisory/history", adminOnly(handleSupervisoryHistory(o.db))).Methods("GET")

	// --- CAT / MiFID II Transaction Reporting ---
	r.Handle("/api/compliance/cat/status", adminOnly(handleCATStatus(o.db))).Methods("GET")
	r.Handle("/api/compliance/cat/export", adminOnly(handleCATExport(o.db))).Methods("GET")
	r.Handle("/api/compliance/cat/submit", adminOnly(handleCATSubmit(o.db, o.logger))).Methods("POST")
	r.Handle("/api/compliance/mifid/export", adminOnly(handleMiFIDExport(o.db))).Methods("GET")

	// --- Post-Trade Surveillance ---
	r.Handle("/api/surveillance/alerts", adminOnly(handleSurveillanceAlerts(o.db))).Methods("GET")
	r.Handle("/api/surveillance/review/{id}", adminOnly(handleSurveillanceReview(o.db, o.logger))).Methods("POST")
	r.Handle("/api/surveillance/status", adminOnly(handleSurveillanceStatus(o.db, o.surveillance))).Methods("GET")

	// --- Time Synchronization (MiFID II RTS 25) ---
	r.Handle("/api/time/status", adminOnly(handleTimeSyncStatus(o.timeSync))).Methods("GET")

	// --- Best Execution (MiFID II Article 27) ---
	r.Handle("/api/execution/quality", jwtAuthMiddleware(rbacMiddleware("admin", "trader")(handleExecutionQuality(o.bestExecution)))).Methods("GET")
	r.Handle("/api/execution/quality/report", adminOnly(handleExecutionQualityReport(o.bestExecution, o.logger))).Methods("GET")

	// --- Failover Status ---
	r.Handle("/api/failover/status", adminOnly(handleFailoverStatus(o.failover))).Methods("GET")
	r.Handle("/api/failover/promote", adminOnly(handleFailoverPromote(o.failover, o.logger))).Methods("POST")

	// --- MFA / User Management ---
	r.Handle("/api/auth/mfa/setup", jwtAuthMiddleware(handleMFASetup(o.db, o.encryption, o.logger))).Methods("POST")
	r.Handle("/api/auth/mfa/verify", jwtAuthMiddleware(handleMFAVerify(o.db, o.encryption, o.logger))).Methods("POST")
	r.Handle("/api/auth/mfa/status", jwtAuthMiddleware(handleMFAStatus(o.db))).Methods("GET")
	r.Handle("/api/auth/mfa/disable", adminOnly(handleMFADisable(o.db, o.logger))).Methods("POST")
	r.Handle("/api/auth/users", adminOnly(handleCreateUser(o.db, o.logger))).Methods("POST")
	r.Handle("/api/auth/users", adminOnly(handleListUsers(o.db))).Methods("GET")

	// --- HSM Status ---
	r.Handle("/api/hsm/status", adminOnly(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(o.hsmClient.Status())
	}))).Methods("GET")

	// --- Portfolio Optimizer ---
	r.Handle("/api/portfolio/status", jwtAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 2 * time.Second}
		stats, err := QueryPortfolioOptimizer(client)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(stats)
	}))).Methods("GET")

	r.Handle("/api/portfolio/weights", jwtAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		weights := CalculateOptimalWeights()
		json.NewEncoder(w).Encode(weights)
	}))).Methods("GET")

	// Apply middleware chain: requestID → rateLimit → router
	handler := requestIDMiddleware(rateLimitMiddleware(1000, r))


	// Apply CORS — restrict to explicit origin in production
	allowedOrigin := os.Getenv("ROBIN_CORS_ORIGIN")
	if allowedOrigin == "" {
		allowedOrigin = "https://trade.robin.local" // Strict default
	}
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{allowedOrigin},
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
	vaultClient := NewVaultClient()
	keyID, err1 := vaultClient.GetSecret("secret/data/alpaca", "API_KEY_ID")
	secretKey, err2 := vaultClient.GetSecret("secret/data/alpaca", "API_SECRET_KEY")

	if err1 != nil || err2 != nil || keyID == "" || secretKey == "" {
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

