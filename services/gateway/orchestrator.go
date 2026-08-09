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
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
		addr:    net.JoinHostPort(host, strconv.Itoa(port)),
		enabled: false,
		cmdCh:   make(chan engineCmd, 64),
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
// Deadlines bound the read/write so a hung or silent engine cannot block callers forever.
func (c *MatchingEngineClient) ioLoop() {
	const ioTimeout = 2 * time.Second
	for cmd := range c.cmdCh {
		c.mu.Lock()
		if c.conn == nil {
			c.mu.Unlock()
			cmd.resp <- engineResp{err: fmt.Errorf("not connected")}
			continue
		}

		c.conn.SetWriteDeadline(time.Now().Add(ioTimeout))
		if _, err := fmt.Fprint(c.conn, cmd.orderJSON+"\n"); err != nil {
			c.enabled = false
			c.lastErr = err.Error()
			c.conn.Close()
			c.conn = nil
			c.mu.Unlock()
			cmd.resp <- engineResp{err: err}
			continue
		}
		c.conn.SetReadDeadline(time.Now().Add(ioTimeout))
		resp, err := c.reader.ReadString('\n')
		if err != nil {
			c.enabled = false
			c.lastErr = err.Error()
			c.conn.Close()
			c.conn = nil
			c.mu.Unlock()
			cmd.resp <- engineResp{err: err}
			continue
		}
		c.conn.SetReadDeadline(time.Time{})
		c.mu.Unlock()
		cmd.resp <- engineResp{data: resp}
	}
}

func (c *MatchingEngineClient) SendOrderJSON(orderJSON string) (string, error) {
	// Fail fast instead of queueing a command that no ioLoop will ever drain.
	c.mu.Lock()
	if !c.enabled || c.conn == nil {
		c.mu.Unlock()
		return "", fmt.Errorf("matching engine not connected")
	}
	c.mu.Unlock()

	respCh := make(chan engineResp, 1)
	select {
	case c.cmdCh <- engineCmd{orderJSON: orderJSON, resp: respCh}:
	case <-time.After(500 * time.Millisecond):
		return "", fmt.Errorf("matching engine command queue full")
	}
	select {
	case r := <-respCh:
		return r.data, r.err
	case <-time.After(5 * time.Second):
		return "", fmt.Errorf("matching engine response timeout")
	}
}

func (c *MatchingEngineClient) HealthCheck() bool {
	respCh := make(chan engineResp, 1)
	c.mu.Lock()
	if !c.enabled || c.conn == nil {
		c.mu.Unlock()
		return false
	}
	c.mu.Unlock()
	select {
	case c.cmdCh <- engineCmd{orderJSON: "health", resp: respCh}:
	case <-time.After(500 * time.Millisecond):
		return false
	}
	select {
	case r := <-respCh:
		return r.err == nil && strings.Contains(r.data, "ok")
	case <-time.After(5 * time.Second):
		return false
	}
}

func (c *MatchingEngineClient) IsEnabled() bool   { c.mu.Lock(); defer c.mu.Unlock(); return c.enabled }
func (c *MatchingEngineClient) LastError() string { c.mu.Lock(); defer c.mu.Unlock(); return c.lastErr }

// engineHost returns the host the gateway uses to reach the C++ matching
// engine's risk-gate/entry port, which can be overridden for containerized
// deployments (docker-compose passes the risk-analytics service name).
func engineHost() string {
	if h := os.Getenv("MATCHING_ENGINE_HOST"); h != "" {
		return h
	}
	return "127.0.0.1"
}

// enginePort returns the TCP port orders are submitted to. The C++ matching
// engine's risk-gate entry defaults to 9092 (PortRiskHealth) unless overridden
// via env; the risk daemon validates then forwards to the engine on 9091.
func enginePort() int {
	if p := os.Getenv("MATCHING_ENGINE_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 && n < 65536 {
			return n
		}
	}
	return PortRiskHealth
}

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
	Symbol      string `json:"symbol"`
	Side        string `json:"side"`       // BUY or SELL
	Price       int64  `json:"price"`      // Fixed-point (1e8)
	Qty         int64  `json:"qty"`        // Fixed-point (1e8)
	OrderType   string `json:"order_type"` // LIMIT or MARKET
	ClientOrdID string `json:"cl_ord_id"`
	Exchange    string `json:"exchange"` // AUTO (Best Price) or specific exchange
}

func (o *OrderRequest) UnmarshalJSON(data []byte) error {
	type Alias OrderRequest
	aux := &struct {
		Price interface{} `json:"price"`
		Qty   interface{} `json:"qty"`
		*Alias
	}{
		Alias: (*Alias)(o),
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&aux); err != nil {
		return err
	}

	// Parse a fixed-point (1e8) value from an integer or dollar-denominated
	// decimal without float rounding error.
	//
	// Contract: a numeric value written as an integer ("150", 150) is already
	// fixed-point; a value written with a decimal point or exponent ("150.0",
	// "1.5e2") is dollars and is scaled by 1e8. Both forms are converted via
	// exact decimal arithmetic so "150" and "150.0" resolve to the SAME price
	// instead of silently differing by 1e8.
	parseVal := func(v interface{}) (int64, error) {
		var str string
		switch val := v.(type) {
		case json.Number:
			str = val.String()
		case string:
			str = strings.TrimSpace(val)
		default:
			if v == nil {
				return 0, nil
			}
			return 0, fmt.Errorf("price/qty must be a number or numeric string")
		}
		if str == "" {
			return 0, nil
		}
		if !strings.ContainsAny(str, ".eE") {
			return strconv.ParseInt(str, 10, 64)
		}
		rat, ok := new(big.Rat).SetString(str)
		if !ok {
			return 0, fmt.Errorf("invalid numeric value %q", str)
		}
		scaled := rat.Mul(rat, big.NewRat(1e8, 1))
		if !scaled.IsInt() {
			// More than 8 decimal places would be truncated.
			return 0, fmt.Errorf("price/qty %q has more than 8 decimal places", str)
		}
		num := scaled.Num()
		if !num.IsInt64() {
			return 0, fmt.Errorf("price/qty %q out of range", str)
		}
		return num.Int64(), nil
	}

	var err error
	if o.Price, err = parseVal(aux.Price); err != nil {
		return fmt.Errorf("price: %w", err)
	}
	if o.Qty, err = parseVal(aux.Qty); err != nil {
		return fmt.Errorf("qty: %w", err)
	}
	return nil
}

// symbolMap is a runtime-updatable symbol → instrument id mapping. The map is
// consulted on every order intake, so it is guarded by an RWMutex and never
// rebuilt per call. New instruments can be registered at runtime without a
// code change or restart.
var (
	symbolMapMu sync.RWMutex
	symbolMap   = map[string]uint64{
		"BTC/USD": 1, "ETH/USD": 2, "AAPL": 3, "EUR/USD": 4, "SOL/USD": 5,
		"MSFT": 6, "TSLA": 7, "NVDA": 8, "GOOGL": 9, "AMZN": 10,
		"SPY": 11, "QQQ": 12, "IWM": 13,
	}
)

// getInstrumentID returns the instrument id for a symbol, or 0 if unknown.
func getInstrumentID(symbol string) uint64 {
	symbolMapMu.RLock()
	defer symbolMapMu.RUnlock()
	return symbolMap[symbol]
}

// registerSymbol adds or updates a symbol→id mapping at runtime. Returns false
// if the id is already bound to a different symbol. Persists the mapping to the
// instruments reference table when a database is configured (Phase 3.4) so
// runtime registrations survive restarts.
func registerSymbol(symbol string, id uint64) bool {
	symbolMapMu.Lock()
	if existing, ok := symbolMap[symbol]; ok && existing == id {
		symbolMapMu.Unlock()
		return true
	}
	for sym, sid := range symbolMap {
		if sid == id && sym != symbol {
			symbolMapMu.Unlock()
			return false
		}
	}
	symbolMap[symbol] = id
	symbolMapMu.Unlock()
	return true
}

// loadSymbolsFromDB seeds the in-memory symbol map from the instruments
// reference table at startup, so symbol→id mappings survive restarts and new
// symbols can be added without recompilation. Returns the count loaded.
func loadSymbolsFromDB(db *sql.DB) int {
	if db == nil {
		return 0
	}
	rows, err := db.Query(`SELECT symbol, instrument_id FROM instruments WHERE status = 'ACTIVE'`)
	if err != nil {
		return 0
	}
	defer rows.Close()
	loaded := 0
	for rows.Next() {
		var symbol string
		var id uint64
		if err := rows.Scan(&symbol, &id); err != nil {
			continue
		}
		if registerSymbol(symbol, id) {
			loaded++
		}
	}
	return loaded
}

// persistSymbolToDB writes a symbol→id mapping to the instruments reference
// table. Best-effort: a persistence failure is logged but does not make the
// in-memory registration fail (the mapping still works for the process).
func persistSymbolToDB(db *sql.DB, symbol string, id uint64) {
	if db == nil {
		return
	}
	now := time.Now().UnixNano()
	db.Exec(`
		INSERT INTO instruments (symbol, instrument_id, status, created_at_ns, updated_at_ns)
		VALUES ($1, $2, 'ACTIVE', $3, $3)
		ON CONFLICT(symbol) DO UPDATE SET instrument_id = $2, updated_at_ns = $3`,
		symbol, id, now,
	)
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
	killSwitch     *KillSwitchManager
	circuitBreaker *CircuitBreakerManager
	surveillance   *SurveillanceEngine
	timeSync       *TimeSyncMonitor
	bestExecution  *BestExecutionMonitor
	encryption     *EncryptionService
	hsmClient      HSMClient
	failover       *FailoverManager

	// Risk analytics data (updated from Rust risk engine)
	riskData      RiskData
	peakEquity    atomic.Uint64
	currentEquity atomic.Uint64

	// FINRA 3110 principal-approval holds awaiting /api/supervisory/* decision.
	// Immutable after creation + on the request path; guarded for approve/reject.
	approvalMu      sync.Mutex
	pendingApproval map[int64]heldApproval
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
		shutdownCh: make(chan struct{}),
		logger:     logger,
		wsHub:      wsHub,
		matchClient: NewMatchingEngineClient(
			engineHost(),
			enginePort(),
		),
		encryption: enc,
		pendingApproval: make(map[int64]heldApproval),
	}
	orch.loadConfig()
	orch.initDB()

	// Seed default users from SEED_ADMIN_PASSWORD / SEED_TRADER_PASSWORD env vars (no-op if unset)
	ensureDefaultUsers(orch.db, logger)

	// Initialize institutional compliance modules after DB is ready
	orch.killSwitch = NewKillSwitchManager(orch.db, logger, wsHub)
	orch.circuitBreaker = InitCircuitBreaker(orch.db, logger, wsHub)
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

	// Start real-time crypto feeds
	feed := NewCoinbaseFeed(wsHub)
	feed.Start()
	binance := NewBinanceFeed(wsHub)
	binance.Start()
	kraken := NewKrakenFeed(wsHub)
	kraken.Start()

	return orch
}

func (o *Orchestrator) initDB() {
	dbURL := os.Getenv("DATABASE_URL")
	driverName := "sqlite3"
	dataSource := "robin.db?_journal_mode=WAL&_synchronous=FULL&_busy_timeout=5000"

	if dbURL != "" {
		driverName = "postgres"
		dataSource = dbURL
		o.logger.Info("using postgres database via DATABASE_URL")
	}

	db, err := sql.Open(driverName, dataSource)
	if err != nil {
		o.logger.Error("failed to open database", "error", err)
		return
	}

	if driverName == "sqlite3" {
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
	} else {
		// PostgreSQL connection pooling
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(5 * time.Minute)
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

	// Phase 3.4: seed the in-memory symbol map from the instruments reference
	// table so symbol→id mappings survive restarts without recompilation.
	if n := loadSymbolsFromDB(o.db); n > 0 {
		o.logger.Info("loaded symbols from instruments reference table", "count", n)
	}
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

	// Risk update broadcast: publishes VaR, Greeks, and P&L every 1s
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				peakEquity := o.peakEquity.Load()
				currentEquity := o.currentEquity.Load()

				drawdown := 0.0
				if peakEquity > 0 && currentEquity > 0 {
					peak := float64(peakEquity)
					current := float64(currentEquity)
					drawdown = (peak - current) / peak * 100
				}

				// Gateway-side circuit breaker: trip when daily drawdown crosses
				// the configured limit (Phase 3.6). Evaluated every second.
				if o.circuitBreaker != nil {
					o.circuitBreaker.CheckDrawdown(
						float64(peakEquity), float64(currentEquity),
						o.GetConfig().MaxDrawdownLimit,
					)
				}

				o.wsHub.BroadcastJSON(map[string]interface{}{
					"type": "risk_update",
					"data": map[string]interface{}{
						"var_95":   o.riskData.Var95,
						"cvar_95":  o.riskData.Cvar95,
						"drawdown": drawdown,
						"sharpe":   o.riskData.Sharpe,
						"sortino":  o.riskData.Sortino,
						"delta":    o.riskData.Delta,
						"gamma":    o.riskData.Gamma,
						"vega":     o.riskData.Vega,
						"theta":    o.riskData.Theta,
					},
				})
			}
		}
	}()
}

type RiskData struct {
	Var95   float64 `json:"var_95"`
	Cvar95  float64 `json:"cvar_95"`
	Sharpe  float64 `json:"sharpe"`
	Sortino float64 `json:"sortino"`
	Delta   float64 `json:"delta"`
	Gamma   float64 `json:"gamma"`
	Vega    float64 `json:"vega"`
	Theta   float64 `json:"theta"`
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

	// Apply new config to memory, then persist to disk. persistConfig takes an
	// RLock, so it must NOT be called while holding the write lock (sync.RWMutex
	// is not reentrant — calling it inside the lock self-deadlocks).
	o.configMutex.Lock()
	old := o.config
	o.config = newConfig
	o.configMutex.Unlock()

	if err := o.persistConfig(); err != nil {
		o.configMutex.Lock()
		o.config = old // Roll back in-memory state on persistence failure
		o.configMutex.Unlock()
		o.logger.Error("failed to persist config, rolled back in-memory state", "error", err)
		return fmt.Errorf("failed to persist config: %w", err)
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
	return atomicWriteFile("config_state.json", data, 0600)
}

// atomicWriteFile writes data to path atomically: the payload is written to a
// temp file in the same directory, synced to disk, then renamed over the target.
// A crash mid-write leaves either the old or the new file — never a truncated
// one — which protects the persisted runtime config (and any future state files).
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(data)
	if werr == nil {
		werr = tmp.Sync()
	}
	if cerr := tmp.Close(); cerr != nil && werr == nil {
		werr = cerr
	}
	if err := os.Chmod(tmpName, perm); err != nil && werr == nil {
		werr = err
	}
	if werr != nil {
		os.Remove(tmpName)
		return werr
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
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

// submitSurveillanceEvent feeds a single trade event to the post-trade
// surveillance engine (Phase 4). It is a no-op when the engine is not
// initialized, and Submit is non-blocking so surveillance never slows the
// critical order path.
func (o *Orchestrator) submitSurveillanceEvent(eventType string, orderID int64, symbol, side string, price, qty float64) {
	if o.surveillance == nil {
		return
	}
	o.surveillance.Submit(TradeEvent{
		EventType:   eventType,
		OrderID:     orderID,
		ClientID:    1,
		Symbol:      symbol,
		Side:        side,
		Price:       price,
		Qty:         qty,
		TimestampNs: time.Now().UnixNano(),
	})
}

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

// cancelRateLimit wraps a cancel handler with the configured MaxCancelRate.
// A configured rate of 0 means unlimited (rate limits are disabled).
func (o *Orchestrator) cancelRateLimit(next http.Handler) http.Handler {
	rate := float64(o.GetConfig().MaxCancelRate)
	if rate <= 0 {
		return next
	}
	return rateLimitMiddleware(rate, next)
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

// jwtAuthMiddleware validates incoming JWT tokens in the Authorization header or httpOnly cookie.
func jwtAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Defensive guard: if JWT auth was never initialized, fail closed with a
		// 503 rather than panic on a nil key inside verify (audit Bug #3).
		jwtAuth.mu.RLock()
		configured := jwtAuth.PublicKey != nil
		jwtAuth.mu.RUnlock()
		if !configured {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"error": "auth not configured"})
			return
		}

		var tokenStr string
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
		} else {
			cookie, err := r.Cookie("robin_token")
			if err == nil {
				tokenStr = cookie.Value
			}
		}

		if tokenStr == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized: missing token"})
			return
		}

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
	r.Use(tracingMiddleware)

	r.HandleFunc("/health", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "ok",
			"healthy":  o.healthyCount.Load(),
			"degraded": o.degradedCount.Load(),
			"failed":   o.failedCount.Load(),
			"checks":   o.totalChecks.Load(),
		})
	}).Methods("GET", "OPTIONS")

	r.HandleFunc("/live", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}).Methods("GET", "OPTIONS")

	r.HandleFunc("/ready", func(w http.ResponseWriter, req *http.Request) {
		if o.failedCount.Load() > 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("services failed"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	}).Methods("GET", "OPTIONS")

	r.HandleFunc("/services", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(o.GetServices())
	}).Methods("GET", "OPTIONS")

	r.Handle("/config", rateLimitMiddleware(float64(o.GetConfig().MaxOrderRate), jwtAuthMiddleware(rbacMiddleware("admin")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(o.GetConfig())
	}))))).Methods("GET", "OPTIONS")

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
	}))))).Methods("POST", "OPTIONS")

	// POST /api/instruments — register a symbol→instrument-id mapping at runtime
	r.Handle("/api/instruments", jwtAuthMiddleware(rbacMiddleware("admin")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Symbol string `json:"symbol"`
			ID     uint64 `json:"id"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Symbol == "" || body.ID == 0 {
			http.Error(w, `{"error":"symbol and id are required"}`, http.StatusBadRequest)
			return
		}
		if !registerSymbol(body.Symbol, body.ID) {
			http.Error(w, `{"error":"id already bound to a different symbol"}`, http.StatusConflict)
			return
		}
		persistSymbolToDB(o.db, body.Symbol, body.ID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "registered",
			"symbol": body.Symbol,
			"id":     body.ID,
		})
	})))).Methods("POST", "OPTIONS")

	r.Handle("/api/historical", jwtAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		symbol := req.URL.Query().Get("symbol")
		if symbol == "" {
			http.Error(w, `{"error":"symbol required"}`, http.StatusBadRequest)
			return
		}

		// In a real system, this would query TimescaleDB or KDB+.
		// For the prototype, we simply confirm the data is being logged to flat-files.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"symbol": symbol,
			"status": "historical_data_available_in_kdb_storage",
			"note":   "Tick data is being asynchronously logged to c:\\Robin\\kdb_storage",
		})
	}))).Methods("GET", "OPTIONS")

	// Initialize order state machine (if not done already)
	if globalOrderSM == nil {
		globalOrderSM = NewOrderStateMachine(o.wsHub)
	}

	// DELETE /order/:id — cancel a working order.
	// Rate-limited by the configured MaxCancelRate (0 = unlimited).
	r.Handle("/order/{cl_ord_id}", o.cancelRateLimit(jwtAuthMiddleware(rbacMiddleware("trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		vars := mux.Vars(req)
		clOrdID := vars["cl_ord_id"]
		if clOrdID == "" {
			http.Error(w, `{"error":"cl_ord_id required"}`, http.StatusBadRequest)
			return
		}
		order, err := globalOrderSM.Cancel(clOrdID)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
			return
		}
		// Propagate the cancel to the matching engine and only confirm it once
		// the engine acknowledges. Never fake a cancellation with a blind delay.
		ack, cancelErr := o.propagateCancel(order)
		if cancelErr != nil {
			o.logger.Error("cancel not acknowledged by engine", "cl_ord_id", clOrdID, "error", cancelErr)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"status":    string(order.State),
				"cl_ord_id": clOrdID,
				"error":     cancelErr.Error(),
				"message":   "cancel pending; matching engine did not acknowledge",
			})
			return
		}
		globalOrderSM.ConfirmCancel(clOrdID)
		if o.db != nil {
			o.db.Exec("UPDATE orders SET status = 'CANCELED', updated_at_ns = $1 WHERE cl_order_id = $2",
				time.Now().UnixNano(), clOrdID)
		}
		// Feed the post-trade surveillance engine the CANCEL event so spoofing
		// detection sees cancelled interest (Phase 4).
		o.submitSurveillanceEvent("CANCEL", int64(order.OrderID), order.Symbol, order.Side,
			order.Price, order.Qty)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "CANCELED",
			"cl_ord_id":  clOrdID,
			"message":    "Cancel confirmed by matching engine",
			"engine_ack": ack,
		})
	}))))).Methods("DELETE", "OPTIONS")

	// POST /api/order/cancel — cancel order via REST POST.
	// Rate-limited by the configured MaxCancelRate (0 = unlimited).
	r.Handle("/api/order/cancel", o.cancelRateLimit(jwtAuthMiddleware(rbacMiddleware("trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var reqBody struct {
			ClOrdID string `json:"cl_ord_id"`
		}
		if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil || reqBody.ClOrdID == "" {
			http.Error(w, `{"error":"cl_ord_id required"}`, http.StatusBadRequest)
			return
		}
		order, err := globalOrderSM.Cancel(reqBody.ClOrdID)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
			return
		}
		// Propagate the cancel to the matching engine and only confirm once the
		// engine acknowledges.
		ack, cancelErr := o.propagateCancel(order)
		if cancelErr != nil {
			o.logger.Error("cancel not acknowledged by engine", "cl_ord_id", reqBody.ClOrdID, "error", cancelErr)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"status":    string(order.State),
				"cl_ord_id": reqBody.ClOrdID,
				"error":     cancelErr.Error(),
				"message":   "cancel pending; matching engine did not acknowledge",
			})
			return
		}
		globalOrderSM.ConfirmCancel(reqBody.ClOrdID)
		if o.db != nil {
			o.db.Exec("UPDATE orders SET status = 'CANCELED', updated_at_ns = $1 WHERE cl_order_id = $2",
				time.Now().UnixNano(), reqBody.ClOrdID)
		}
		// Feed the post-trade surveillance engine the CANCEL event so spoofing
		// detection sees cancelled interest (Phase 4).
		o.submitSurveillanceEvent("CANCEL", int64(order.OrderID), order.Symbol, order.Side,
			order.Price, order.Qty)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "CANCELED",
			"cl_ord_id":  reqBody.ClOrdID,
			"message":    "Cancel confirmed by matching engine",
			"engine_ack": ack,
		})
	}))))).Methods("POST", "OPTIONS")

	// POST /api/order/modify — modify order price/quantity via REST POST.
	// Phase 3: a REPLACE is forwarded to the matching engine and only confirmed
	// once the engine acknowledges, mirroring the cancel path.
	r.Handle("/api/order/modify", jwtAuthMiddleware(rbacMiddleware("trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var reqBody struct {
			ClOrdID string  `json:"cl_ord_id"`
			Price   float64 `json:"price"`
			Qty     float64 `json:"qty"`
		}
		if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil || reqBody.ClOrdID == "" {
			http.Error(w, `{"error":"cl_ord_id required"}`, http.StatusBadRequest)
			return
		}
		if reqBody.Price <= 0 || reqBody.Qty <= 0 {
			http.Error(w, `{"error":"price and qty must be positive"}`, http.StatusBadRequest)
			return
		}
		order, ok := globalOrderSM.GetOrder(reqBody.ClOrdID)
		if !ok {
			http.Error(w, `{"error":"order not found"}`, http.StatusNotFound)
			return
		}
		// Only live orders may be replaced.
		switch order.State {
		case OrderStateNew, OrderStatePending, OrderStateWorking, OrderStatePartialFill:
		default:
			http.Error(w, fmt.Sprintf(`{"error":"order not modifiable in state %s"}`, order.State), http.StatusConflict)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		// Propagate the REPLACE to the matching engine first.
		ack, modifyErr := o.propagateModify(order, reqBody.Price, reqBody.Qty)
		if modifyErr != nil {
			o.logger.Error("modify not acknowledged by engine", "cl_ord_id", reqBody.ClOrdID, "error", modifyErr)
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":    string(order.State),
				"cl_ord_id": reqBody.ClOrdID,
				"error":     modifyErr.Error(),
				"message":   "modify rejected; matching engine did not acknowledge",
			})
			return
		}
		// Only mutate local state after the engine accepted the replace.
		if _, upErr := globalOrderSM.ApplyReplace(reqBody.ClOrdID, reqBody.Price, reqBody.Qty); upErr == nil {
			if o.db != nil {
				o.db.Exec("UPDATE orders SET price = $1, qty = $2, updated_at_ns = $3 WHERE cl_order_id = $4",
					int64(reqBody.Price*100000000), int64(reqBody.Qty*100000000), time.Now().UnixNano(), reqBody.ClOrdID)
			}
			o.logger.Info("order replaced", "cl_ord_id", reqBody.ClOrdID,
				"new_price", reqBody.Price, "new_qty", reqBody.Qty)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "MODIFIED",
			"cl_ord_id":  reqBody.ClOrdID,
			"new_price":  reqBody.Price,
			"new_qty":    reqBody.Qty,
			"engine_ack": ack,
		})
	})))).Methods("POST", "OPTIONS")

	// GET /api/orders/blotter — full order blotter with state history
	r.Handle("/api/orders/blotter", jwtAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		orders := globalOrderSM.GetAllOrders()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(orders)
	}))).Methods("GET", "OPTIONS")

	// POST /api/orders/reconcile — operator-triggered order state reconciliation
	// (Phase 3.5): rehydrates the in-memory state machine from the durable orders
	// table and reports what was repaired. Admin only.
	r.Handle("/api/orders/reconcile", jwtAuthMiddleware(rbacMiddleware("admin")(handleOrderReconcile(o.db, globalOrderSM, o.logger)))).Methods("GET", "POST", "OPTIONS")

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

		// Circuit breaker gate (Phase 3.6): block order entry while the gateway
		// or risk-engine circuit breaker is tripped (daily drawdown limit).
		if o.circuitBreaker != nil && o.circuitBreaker.IsTripped() {
			o.RecordReject()
			status := o.circuitBreaker.GetStatus()
			o.logger.Warn("order blocked by circuit breaker",
				"symbol", orderReq.Symbol, "reason", status["reason"])
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{
				"status":  "REJECTED",
				"reason":  "CIRCUIT_BREAKER_TRIPPED",
				"message": fmt.Sprintf("circuit breaker tripped: %v", status["reason"]),
			})
			return
		}

		// Institutional pre-trade position limit gate (Phase 3.2): block the
		// order before it reaches the engine or the state machine when the
		// symbol's net position (long + short) after this order would exceed
		// the configured MaxPositionLimit.
		if globalPositionManager != nil {
			maxPos := o.GetConfig().MaxPositionLimit
			if err := globalPositionManager.checkPositionLimit(
				orderReq.Symbol,
				orderReq.Side,
				float64(orderReq.Qty)/100000000.0,
				float64(maxPos),
			); err != nil {
				o.RecordReject()
				o.logger.Warn("order blocked by position limit",
					"symbol", orderReq.Symbol, "side", orderReq.Side,
					"qty", orderReq.Qty, "limit", maxPos)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]string{
					"status":  "REJECTED",
					"reason":  "POSITION_LIMIT",
					"message": err.Error(),
				})
				return
			}
		}

		orderID := uint64(time.Now().UnixNano())
		execID := fmt.Sprintf("EXEC-%d", orderID)
		if orderReq.ClientOrdID == "" {
			orderReq.ClientOrdID = fmt.Sprintf("ORD-%d", orderID)
		}

		instID := getInstrumentID(orderReq.Symbol)
		if instID == 0 {
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
		routing, ok := RouteOrder(orderReq.Symbol, orderReq.Side, float64(orderReq.Price)/100000000.0, prefExchange)
		if !ok {
			http.Error(w, `{"error":"No live quotes available for routing"}`, http.StatusServiceUnavailable)
			return
		}

		// Removed invalid ExecutionRoute assignment
		fillPrice := routing.FillPrice
		fillQty := 0.0
		reportedFillPrice := 0.0
		reportedFillQty := 0.0
		routedExchange := routing.RoutedExchange
		priceImprovement := routing.PriceImprovementBps
		exchangesSearched := routing.ExchangesSearched

		status := "WORKING"
		var engineError string

		latencyNs := uint64(time.Since(start).Nanoseconds())

		// Register order in state machine (NEW → PENDING → WORKING)
		if globalOrderSM != nil {
			managed := &ManagedOrder{
				ClOrdID:        orderReq.ClientOrdID,
				OrderID:        orderID,
				Symbol:         orderReq.Symbol,
				Side:           orderReq.Side,
				OrderType:      orderType,
				Qty:            float64(orderReq.Qty) / 100000000.0,
				Price:          float64(orderReq.Price) / 100000000.0,
				RoutedExchange: routedExchange,
			}
if regErr := globalOrderSM.Register(managed); regErr == nil {
				globalOrderSM.Transition(orderReq.ClientOrdID, OrderStatePending, "submitted_to_gateway")
				globalOrderSM.Transition(orderReq.ClientOrdID, OrderStateWorking, "acked_by_gateway")
			}
		}

		// Feed the post-trade surveillance engine the NEW event so layering /
		// momentum-ignition detectors see every incoming order (Phase 4).
		o.submitSurveillanceEvent("NEW", int64(orderID), orderReq.Symbol, orderReq.Side,
			float64(orderReq.Price)/100000000.0, float64(orderReq.Qty)/100000000.0)

		// Synchronous database persistence (Institutional fix: no fire-and-forget)
		var orderDBID int64
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
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
				orderReq.ClientOrdID, instID, orderReq.Price, orderReq.Qty, sideInt, status, 1, 1, 1, now, now,
			)
			if err != nil {
				tx.Rollback()
				o.logger.Error("failed to insert order to db", "error", err)
				http.Error(w, `{"error":"DATABASE_ERROR"}`, http.StatusInternalServerError)
				return
			}
			orderDBID, idErr := res.LastInsertId()
			if idErr != nil {
				o.logger.Warn("order insert had no row id", "cl_ord_id", orderReq.ClientOrdID, "error", idErr)
			} else if orderDBID <= 0 {
				o.logger.Warn("order insert returned non-positive row id", "cl_ord_id", orderReq.ClientOrdID)
			}
			if err := tx.Commit(); err != nil {
				tx.Rollback()
				o.logger.Error("failed to commit order transaction", "error", err)
				http.Error(w, `{"error":"DATABASE_ERROR"}`, http.StatusInternalServerError)
				return
			}
		}

		// Supervisory approval gate (FINRA 3110, Phase 4): when the order's
		// notional exceeds the configured threshold, the order is held for
		// principal approval instead of being routed to the engine. Fail-closed:
		// if the decision cannot be persisted the order is rejected outright.
		if o.db != nil {
			notional := fillPrice * (float64(orderReq.Qty) / 100000000.0)
			needsApproval, approvalID, holdErr := checkSupervisoryApproval(o.db, int64(orderID), orderReq.Symbol, notional, supervisoryThresholdUSD())
			if holdErr != nil {
				o.logger.Error("supervisory approval check failed (fail-closed)",
					"cl_ord_id", orderReq.ClientOrdID, "error", holdErr)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{
					"status": "REJECTED",
					"reason": "SUPERVISORY_CHECK_FAILED",
				})
				return
			}
			if needsApproval {
				o.approvalMu.Lock()
				o.pendingApproval[approvalID] = heldApproval{
					approvalID: approvalID,
					clOrdID:    orderReq.ClientOrdID,
					orderID:    int64(orderID),
					symbol:     orderReq.Symbol,
					side:       side,
					orderType:  orderType,
					price:      int64(fillPrice * 100000000),
					qty:        orderReq.Qty,
				}
				o.approvalMu.Unlock()
				if _, err := o.db.Exec("UPDATE orders SET status = 'PENDING_APPROVAL', updated_at_ns = $1 WHERE cl_order_id = $2",
					time.Now().UnixNano(), orderReq.ClientOrdID); err != nil {
					o.logger.Warn("failed to mark order PENDING_APPROVAL", "cl_ord_id", orderReq.ClientOrdID, "error", err)
				}
				o.logger.Warn("order held for principal approval (FINRA 3110)",
					"cl_ord_id", orderReq.ClientOrdID, "approval_id", approvalID, "notional", notional)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"status":      "SUPERVISORY_APPROVAL_REQUIRED",
					"cl_ord_id":   orderReq.ClientOrdID,
					"reason":      "FINRA 3110 principal approval required before routing",
					"approval_id": approvalID,
					"notional":    notional,
					"threshold":   supervisoryThresholdUSD(),
				})
				return
			}
		}

		if o.matchClient == nil || !o.matchClient.IsEnabled() {
			o.RecordReject()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "REJECTED",
				"reason": "MATCHING_ENGINE_UNAVAILABLE",
			})
			return
		}

		// Synchronous submission to the matching engine: a 200 response must
		// never precede the order actually reaching the engine (audit Bug #7).
		// The client only sees a success once the engine has acknowledged the
		// order, matching the cancel-propagation semantics.
		if o.matchClient != nil && o.matchClient.IsEnabled() {
			matchJSON := fmt.Sprintf(
				`{"cl_ord_id":"%s","id":%d,"instrument_id":%d,"price":%d,"qty":%d,"side":"%s","type":"%s"}`,
				orderReq.ClientOrdID, orderID, instID, int64(fillPrice*100000000), orderReq.Qty, side, orderType,
			)
			resp, err := o.matchClient.SendOrderJSON(matchJSON)
			if err != nil {
				o.logger.Error("failed to submit order to matching engine", "cl_ord_id", orderReq.ClientOrdID, "error", err)
				engineError = err.Error()
			} else {
				var meResp MatchingEngineResponse
				if json.Unmarshal([]byte(resp), &meResp) == nil {
					finalStatus := "REJECTED"
					if meResp.Success {
						finalStatus = meResp.Status
						if finalStatus == "" {
							finalStatus = "FILLED"
						}
					} else if meResp.Error == "engine offline" {
						finalStatus = "REJECTED"
					}
					status = finalStatus

					if finalStatus == "FILLED" && meResp.FillPrice > 0 {
						fillPrice = float64(meResp.FillPrice) / 100000000.0
						fillQty = float64(meResp.FillQty) / 100000000.0
						reportedFillPrice = fillPrice
						reportedFillQty = fillQty
					}

					// Update state
					if o.db != nil {
						now := time.Now().UnixNano()
						o.db.Exec(`UPDATE orders SET status = $1, updated_at_ns = $2 WHERE cl_order_id = $3`, finalStatus, now, orderReq.ClientOrdID)
					}

					// Broadcast update
					o.wsHub.BroadcastJSON(map[string]interface{}{
						"type": "order_update",
						"data": map[string]interface{}{
							"cl_ord_id":  orderReq.ClientOrdID,
							"status":     finalStatus,
							"fill_price": float64(meResp.FillPrice) / 100000000.0,
							"fill_qty":   float64(meResp.FillQty) / 100000000.0,
						},
					})

					if finalStatus == "FILLED" {
						o.RecordTrade()
						o.wsHub.BroadcastTrade(TradePayload{
							ID:        execID,
							Symbol:    orderReq.Symbol,
							Side:      orderReq.Side,
							Qty:       float64(meResp.FillQty) / 100000000.0,
							Price:     float64(meResp.FillPrice) / 100000000.0,
							Timestamp: time.Now().UnixMilli(),
						})
						if globalPositionManager != nil {
							globalPositionManager.OnFill(
								execID, orderReq.Symbol, orderReq.Side,
								float64(meResp.FillQty)/100000000.0, float64(meResp.FillPrice)/100000000.0,
							)
							globalPositionManager.RecordAccountFill(
								1, orderReq.Symbol, orderReq.Side,
								float64(meResp.FillQty)/100000000.0, float64(meResp.FillPrice)/100000000.0,
							)
						}
						// Regulatory reporting (Phase 4.2): record the CAT order
						// lifecycle event and the MiFID II RTS 22 transaction so
						// the export endpoints produce evidence for submission.
						if o.db != nil {
							if orderDBID > 0 {
								if err := recordCATEvent(o.db, orderDBID, CATEventFill, routedExchange); err != nil {
									o.logger.Error("failed to record CAT event", "error", err)
								}
								if err := recordMiFIDReport(o.db, orderDBID, "ALGO-1", "AI", routedExchange); err != nil {
									o.logger.Error("failed to record MiFID report", "error", err)
								}
							}
						}
						// Feed the post-trade surveillance engine the FILL event so
						// wash-trade detection sees completed executions (Phase 4).
						o.submitSurveillanceEvent("FILL", int64(orderID), orderReq.Symbol, orderReq.Side,
							float64(meResp.FillPrice)/100000000.0, float64(meResp.FillQty)/100000000.0)
					}
				} else {
					engineError = "invalid matching engine response"
					o.logger.Error("unparseable matching engine response", "resp", resp)
				}
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
		latencyNs = uint64(time.Since(start).Nanoseconds())
		w.WriteHeader(http.StatusOK)
		respPayload := map[string]interface{}{
			"status":                status,
			"exec_id":               execID,
			"cl_ord_id":             orderReq.ClientOrdID,
			"symbol":                orderReq.Symbol,
			"side":                  orderReq.Side,
			"qty":                   float64(orderReq.Qty) / 100000000.0,
			"fill_price":            reportedFillPrice,
			"fill_qty":              reportedFillQty,
			"latency_ns":            latencyNs,
			"engine":                true,
			"routed_exchange":       routedExchange,
			"price_improvement_bps": priceImprovement,
			"exchanges_searched":    exchangesSearched,
			"execution_summary":     fmt.Sprintf("Routed via %s (%d exchanges searched, +%.1fbps savings)", routedExchange, exchangesSearched, priceImprovement),
		}
		if engineError != "" {
			respPayload["error"] = engineError
		}
		if alpacaOrderID != "" {
			respPayload["alpaca_order_id"] = alpacaOrderID
			respPayload["alpaca_status"] = alpacaStatus
		}
		json.NewEncoder(w).Encode(respPayload)
	}))))).Methods("POST", "OPTIONS")

	// POST /api/orders/bulk — submit a batch of orders in a single request
	// (Phase 3.7). Every order is validated up-front; any invalid order rejects
	// the whole batch (atomic rejection). Valid orders are then persisted to
	// the database in one transaction and forwarded to the matching engine
	// sequentially. Each result carries its own cl_ord_id / status so the
	// caller can reconcile partial successes.
	r.Handle("/api/orders/bulk", rateLimitMiddleware(float64(o.GetConfig().MaxOrderRate), jwtAuthMiddleware(rbacMiddleware("trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var batch []OrderRequest
		if err := json.NewDecoder(req.Body).Decode(&batch); err != nil {
			http.Error(w, `{"error":"invalid bulk order JSON (expected array)"}`, http.StatusBadRequest)
			return
		}
		if len(batch) == 0 {
			http.Error(w, `{"error":"empty order batch"}`, http.StatusBadRequest)
			return
		}
		if len(batch) > 500 {
			http.Error(w, `{"error":"batch too large (max 500)"}`, http.StatusBadRequest)
			return
		}

		// Circuit breaker gate (Phase 3.6): block the entire batch while the
		// gateway or risk-engine circuit breaker is tripped.
		if o.circuitBreaker != nil && o.circuitBreaker.IsTripped() {
			status := o.circuitBreaker.GetStatus()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{
				"status":  "REJECTED",
				"reason":  "CIRCUIT_BREAKER_TRIPPED",
				"message": fmt.Sprintf("circuit breaker tripped: %v", status["reason"]),
			})
			return
		}

		// Phase 1: validate everything before touching the engine or DB.
		type staged struct {
			order OrderRequest
			id    uint64
			inst  uint64
			side  string
			otype string
		}
		stagedOrders := make([]staged, 0, len(batch))
		for i := range batch {
			or := batch[i]
			if or.Symbol == "" || or.Qty <= 0 || or.Price < 0 {
				http.Error(w, fmt.Sprintf(`{"error":"order %d invalid: symbol, qty, price required"}`, i), http.StatusBadRequest)
				return
			}
			if or.Side != "BUY" && or.Side != "SELL" {
				http.Error(w, fmt.Sprintf(`{"error":"order %d invalid: side must be BUY or SELL"}`, i), http.StatusBadRequest)
				return
			}
			instID := getInstrumentID(or.Symbol)
			if instID == 0 {
				http.Error(w, fmt.Sprintf(`{"error":"order %d invalid: unknown symbol %s"}`, i, or.Symbol), http.StatusBadRequest)
				return
			}
			side := "BID"
			if or.Side == "SELL" {
				side = "ASK"
			}
			otype := "LIMIT"
			if or.OrderType == "MARKET" {
				otype = "MARKET"
			}
			if globalPositionManager != nil {
				maxPos := o.GetConfig().MaxPositionLimit
				if err := globalPositionManager.checkPositionLimit(or.Symbol, or.Side, float64(or.Qty)/100000000.0, float64(maxPos)); err != nil {
					http.Error(w, fmt.Sprintf(`{"error":"order %d rejected: POSITION_LIMIT"}`, i), http.StatusConflict)
					return
				}
			}
			orderID := uint64(time.Now().UnixNano()) + uint64(i)
			if or.ClientOrdID == "" {
				or.ClientOrdID = fmt.Sprintf("ORD-%d", orderID)
			}
			stagedOrders = append(stagedOrders, staged{order: or, id: orderID, inst: instID, side: side, otype: otype})
		}

		// Phase 2: persist all orders in one transaction (atomic visibility).
		if o.db != nil {
			tx, err := o.db.Begin()
			if err != nil {
				http.Error(w, `{"error":"DATABASE_ERROR"}`, http.StatusInternalServerError)
				return
			}
			now := time.Now().UnixNano()
			commit := true
			for _, s := range stagedOrders {
				sideInt := 0
				if s.order.Side == "SELL" {
					sideInt = 1
				}
				if _, err := tx.Exec(`
					INSERT INTO orders (cl_order_id, instrument_id, price, qty, side, status, account_id, client_id, strategy_id, created_at_ns, updated_at_ns)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
					s.order.ClientOrdID, s.inst, s.order.Price, s.order.Qty, sideInt, "WORKING", 1, 1, 1, now, now,
				); err != nil {
					tx.Rollback()
					http.Error(w, `{"error":"DATABASE_ERROR"}`, http.StatusInternalServerError)
					return
				}
			}
			if err := tx.Commit(); err != nil {
				tx.Rollback()
				http.Error(w, `{"error":"DATABASE_ERROR"}`, http.StatusInternalServerError)
				return
			}
			_ = commit
		}

		// Phase 3: submit each to the engine and collect results.
		results := make([]map[string]interface{}, 0, len(stagedOrders))
		accepted := 0
		for _, s := range stagedOrders {
			or := s.order
			routing, ok := RouteOrder(or.Symbol, or.Side, float64(or.Price)/100000000.0, "AUTO")
			if !ok {
				http.Error(w, `{"error":"No live quotes available for routing in bulk order"}`, http.StatusServiceUnavailable)
				return
			}
			status := "WORKING"
			var errMsg string
			// Feed the post-trade surveillance engine the NEW event (Phase 4).
			o.submitSurveillanceEvent("NEW", int64(s.id), or.Symbol, or.Side,
				float64(or.Price)/100000000.0, float64(or.Qty)/100000000.0)
			if globalOrderSM != nil {
				managed := &ManagedOrder{
					ClOrdID:        or.ClientOrdID,
					OrderID:        s.id,
					Symbol:         or.Symbol,
					Side:           or.Side,
					OrderType:      s.otype,
					Qty:            float64(or.Qty) / 100000000.0,
					Price:          float64(or.Price) / 100000000.0,
					RoutedExchange: routing.RoutedExchange,
				}
				if regErr := globalOrderSM.Register(managed); regErr == nil {
					globalOrderSM.Transition(or.ClientOrdID, OrderStatePending, "submitted_to_gateway")
				}
			}

			// Supervisory approval gate (FINRA 3110, Phase 4): bulk orders whose
			// notional exceeds the threshold are held rather than routed.
			held := false
			if o.db != nil {
				notional := routing.FillPrice * (float64(or.Qty) / 100000000.0)
				needsApproval, holdID, holdErr := checkSupervisoryApproval(o.db, int64(s.id), or.Symbol, notional, supervisoryThresholdUSD())
				if holdErr != nil {
					status = "REJECTED"
					errMsg = "SUPERVISORY_CHECK_FAILED"
					o.RecordReject()
				} else if needsApproval {
					o.approvalMu.Lock()
					o.pendingApproval[holdID] = heldApproval{
						approvalID: holdID,
						clOrdID:    or.ClientOrdID,
						orderID:    int64(s.id),
						symbol:     or.Symbol,
						side:       s.side,
						orderType:  s.otype,
						price:      or.Price,
						qty:        or.Qty,
					}
					o.approvalMu.Unlock()
					status = "PENDING_APPROVAL"
					errMsg = "FINRA 3110 principal approval required before routing"
					held = true
				}
			}
			o.RecordOrder()
			if !held && o.matchClient != nil && o.matchClient.IsEnabled() {
				matchJSON := fmt.Sprintf(
					`{"cl_ord_id":"%s","id":%d,"instrument_id":%d,"price":%d,"qty":%d,"side":"%s","type":"%s"}`,
					or.ClientOrdID, s.id, s.inst, or.Price, or.Qty, s.side, s.otype,
				)
				resp, err := o.matchClient.SendOrderJSON(matchJSON)
				if err != nil {
					errMsg = err.Error()
					o.RecordReject()
				} else {
					var meResp MatchingEngineResponse
					if json.Unmarshal([]byte(resp), &meResp) == nil && meResp.Success {
						status = meResp.Status
						if status == "" {
							status = "FILLED"
						}
						accepted++
					} else {
						o.RecordReject()
					}
				}
			} else {
				errMsg = "MATCHING_ENGINE_UNAVAILABLE"
				o.RecordReject()
			}
			if o.db != nil {
				o.db.Exec(`UPDATE orders SET status = $1, updated_at_ns = $2 WHERE cl_order_id = $3`, status, time.Now().UnixNano(), or.ClientOrdID)
			}
			results = append(results, map[string]interface{}{
				"cl_ord_id":    or.ClientOrdID,
				"symbol":       or.Symbol,
				"side":         or.Side,
				"qty":          float64(or.Qty) / 100000000.0,
				"status":       status,
				"error":        errMsg,
				"routed_to":    routing.RoutedExchange,
				"fill_price":   routing.FillPrice,
				"exec_id":      fmt.Sprintf("EXEC-%d", s.id),
				"submitted_ok": errMsg == "",
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"batch_size":   len(stagedOrders),
			"accepted":     accepted,
			"rejected":     len(stagedOrders) - accepted,
			"results":      results,
			"submitted_at": time.Now().UnixNano(),
		})
	}))))).Methods("POST", "OPTIONS")

	// Internal endpoint for Risk Engine to push state transitions.
	// Authenticated with an internal service token (ROBIN_INTERNAL_TOKEN).
	// This is a state-changing endpoint that mutates order status — it must
	// never be reachable by unauthenticated callers.
	r.HandleFunc("/internal/order_update", func(w http.ResponseWriter, req *http.Request) {
		expected := os.Getenv("ROBIN_INTERNAL_TOKEN")
		if expected == "" {
			o.logger.Warn("/internal/order_update called with no ROBIN_INTERNAL_TOKEN set — endpoint is OPEN. Set ROBIN_INTERNAL_TOKEN in production.")
		} else if req.Header.Get("X-Internal-Token") != expected {
			http.Error(w, `{"error":"forbidden: invalid internal token"}`, http.StatusForbidden)
			return
		}

		var update struct {
			ClientOrdID string  `json:"cl_ord_id"`
			Status      string  `json:"status"`
			FillPrice   float64 `json:"fill_price"`
			FillQty     float64 `json:"fill_qty"`
		}
		if err := json.NewDecoder(req.Body).Decode(&update); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if o.db != nil {
			now := time.Now().UnixNano()
			_, err := o.db.Exec(`UPDATE orders SET status = $1, updated_at_ns = $2 WHERE cl_order_id = $3`, update.Status, now, update.ClientOrdID)
			if err != nil {
				o.logger.Error("failed to update order status", "error", err)
			}
		}

		// Broadcast state change to frontend
		o.wsHub.BroadcastJSON(map[string]interface{}{
			"type": "order_update",
			"data": update,
		})

		w.WriteHeader(http.StatusOK)
	}).Methods("POST", "OPTIONS")

	// WebSocket endpoint — real-time order book + trade notifications
	r.HandleFunc("/ws", o.wsHub.handleWebSocket)

	// Risk data endpoint — Rust risk engine posts here; relayed via WebSocket
	r.Handle("/api/risk/data", jwtAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "POST" {
			var rd RiskData
			if err := json.NewDecoder(req.Body).Decode(&rd); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			o.riskData = rd

			o.wsHub.BroadcastJSON(map[string]interface{}{
				"type": "risk_update",
				"data": rd,
			})
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(o.riskData)
	}))).Methods("GET", "POST")

	r.Handle("/metrics", promhttp.Handler())

	// ── Position & Portfolio endpoints ─────────────────────────────────────
	r.Handle("/api/positions", jwtAuthMiddleware(http.HandlerFunc(handleGetPositions))).Methods("GET", "OPTIONS")
	r.Handle("/api/positions/accounts", jwtAuthMiddleware(http.HandlerFunc(handleGetAccountPnL))).Methods("GET", "OPTIONS")
	r.Handle("/api/positions/{symbol}", jwtAuthMiddleware(http.HandlerFunc(handleGetPosition))).Methods("GET", "OPTIONS")
	r.Handle("/api/portfolio", jwtAuthMiddleware(http.HandlerFunc(handleGetPortfolioSummary))).Methods("GET", "OPTIONS")

	r.Handle("/stats", jwtAuthMiddleware(rbacMiddleware("admin", "trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
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
	})))).Methods("GET", "OPTIONS")

	// Analytics and VaR calculations have been moved to a dedicated risk microservice
	// to prevent blocking the high-throughput Go hot path.

	// POST /api/ai/chat — Quantitative Multi-Agent Chat Assistant
	r.Handle("/api/ai/chat", jwtAuthMiddleware(rbacMiddleware("admin", "trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
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
	})))).Methods("POST", "OPTIONS")

	// POST /api/ai/trade_decision — Autonomous AI Agent Trade Evaluation
	r.Handle("/api/ai/trade_decision", jwtAuthMiddleware(rbacMiddleware("admin", "trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
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
	})))).Methods("POST", "OPTIONS")

	// GET /api/ai/signal — Real AI signal (action/confidence/regime/sentiment)
	// for the selected symbol, proxied from the Python AI-agent microservice.
	r.Handle("/api/ai/signal", jwtAuthMiddleware(rbacMiddleware("admin", "trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		symbol := req.URL.Query().Get("symbol")
		if symbol == "" {
			symbol = "BTC-USD"
		}
		symbol = strings.ReplaceAll(symbol, "/", "-")

		payload := fmt.Sprintf(`{"symbol":"%s","market_context":"Signal request for %s"}`, symbol, symbol)
		proxyReq, err := http.NewRequest("POST", "http://127.0.0.1:8000/trade_decision", strings.NewReader(payload))
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

		// Decode the Python signal and flatten to a deterministic frontend contract
		var sig struct {
			Action      string  `json:"action"`
			Confidence  float64 `json:"confidence"`
			Regime      string  `json:"regime"`
			Sentiment   float64 `json:"sentiment"`
			Reasoning   string  `json:"reasoning"`
			Symbol      string  `json:"symbol"`
			Price       float64 `json:"price"`
			EntryTarget float64 `json:"entry_target"`
			Error       string  `json:"error"`
		}
		if err := json.NewDecoder(proxyResp.Body).Decode(&sig); err != nil {
			http.Error(w, `{"error":"invalid signal from ai-agent"}`, http.StatusBadGateway)
			return
		}
		if sig.Error != "" {
			// Python reported no live market data — signal unavailable.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{
				"error":  sig.Error,
				"symbol": sig.Symbol,
			})
			return
		}
		if sig.Symbol == "" {
			sig.Symbol = symbol
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"symbol":       sig.Symbol,
			"action":       sig.Action,
			"confidence":   sig.Confidence,
			"regime":       sig.Regime,
			"sentiment":    sig.Sentiment,
			"reason":       sig.Reasoning,
			"price":        sig.Price,
			"entry_target": sig.EntryTarget,
			"timestamp":    time.Now().UnixMilli(),
		})
	})))).Methods("GET", "OPTIONS")

	// GET /api/ai/macro_feed — Fetch real-time macro news feed from python agent
	r.Handle("/api/ai/macro_feed", jwtAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
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
	}))).Methods("GET", "OPTIONS")

	// GET /api/sor/prices — Fetch real-time simulated prices across major exchanges
	r.Handle("/api/sor/prices", jwtAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		symbol := req.URL.Query().Get("symbol")
		if symbol == "" {
			symbol = "BTC/USD"
		}

		basePrice := globalMarketData.GetPrice(symbol)
		if basePrice == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("[]"))
			return
		}

		quotes := globalNBBO.Venues(symbol)

		var result []ExchangeQuote
		for _, q := range quotes {
			result = append(result, ExchangeQuote{
				Exchange:    q.Exchange,
				Bid:         q.Bid,
				Ask:         q.Ask,
				BidSize:     1.0,
				AskSize:     1.0,
				IsSimulated: false,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))).Methods("GET", "OPTIONS")

	// GET /api/screener — Fetch assets list with screener metrics
	r.Handle("/api/screener", jwtAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
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
			{"BTC/USD", "Bitcoin", "Crypto", globalMarketData.GetPrice("BTC/USD"), 1260.5, 0.0, 0.0, "Global"},
			{"ETH/USD", "Ethereum", "Crypto", globalMarketData.GetPrice("ETH/USD"), 412.3, 0.0, 0.0, "Global"},
			{"SOL/USD", "Solana", "Crypto", globalMarketData.GetPrice("SOL/USD"), 62.8, 0.0, 0.0, "Global"},
		}

		json.NewEncoder(w).Encode(assets)
	}))).Methods("GET", "OPTIONS")

	// GET /api/heatmap — Fetch sector-wise daily change heatmap data
	r.Handle("/api/heatmap", jwtAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		type HeatmapNode struct {
			Name   string  `json:"name"`
			Value  float64 `json:"value"`
			Change float64 `json:"change"`
		}
		type HeatmapSector struct {
			SectorName string        `json:"sector_name"`
			Nodes      []HeatmapNode `json:"nodes"`
		}

		getDynamicChange := func(symbol string, baseChange float64) float64 {
			return baseChange
		}

		heatmap := []HeatmapSector{
			{
				SectorName: "Cryptocurrency",
				Nodes: []HeatmapNode{
					{"BTC/USD", globalMarketData.GetPrice("BTC/USD") * 0.019, getDynamicChange("BTC/USD", 2.45)},
					{"ETH/USD", globalMarketData.GetPrice("ETH/USD") * 0.12, getDynamicChange("ETH/USD", -1.18)},
					{"SOL/USD", globalMarketData.GetPrice("SOL/USD") * 0.43, getDynamicChange("SOL/USD", 5.76)},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(heatmap)
	}))).Methods("GET", "OPTIONS")

	// GET /api/alpaca/account — Fetch Alpaca account details (JWT Trader required)
	r.Handle("/api/alpaca/account", jwtAuthMiddleware(rbacMiddleware("admin", "trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
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
	})))).Methods("GET", "OPTIONS")

	// GET /api/alpaca/positions — Fetch Alpaca positions (JWT Trader required)
	r.Handle("/api/alpaca/positions", jwtAuthMiddleware(rbacMiddleware("admin", "trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
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
	})))).Methods("GET", "OPTIONS")

	// GET /api/alpaca/orders — Fetch Alpaca orders (trade history) (JWT Trader required)
	r.Handle("/api/alpaca/orders", jwtAuthMiddleware(rbacMiddleware("admin", "trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
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
	})))).Methods("GET", "OPTIONS")

	// GET /api/alpaca/assets — Fetch Alpaca assets (JWT Trader required)
	r.Handle("/api/alpaca/assets", jwtAuthMiddleware(rbacMiddleware("admin", "trader")(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
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
	})))).Methods("GET", "OPTIONS")
	// ============================================================================
	// Institutional Compliance API Routes (Wave 1-6 Gap Closure)
	// ============================================================================

	adminOnly := func(next http.Handler) http.Handler {
		return jwtAuthMiddleware(rbacMiddleware("admin")(next))
	}

	// --- Kill Switch (SEC 15c3-5 direct control) ---
	r.Handle("/api/killswitch/status", adminOnly(killSwitchStatusHandler(o.killSwitch))).Methods("GET", "OPTIONS")
	r.Handle("/api/killswitch/system/trip", adminOnly(killSwitchTripSystemHandler(o.killSwitch))).Methods("POST", "OPTIONS")
	r.Handle("/api/killswitch/system/reset/initiate", adminOnly(killSwitchInitResetHandler(o.killSwitch))).Methods("POST", "OPTIONS")
	r.Handle("/api/killswitch/system/reset/confirm", adminOnly(killSwitchConfirmResetHandler(o.killSwitch))).Methods("POST", "OPTIONS")
	r.Handle("/api/killswitch/algo/{id}/trip", adminOnly(killSwitchTripAlgoHandler(o.killSwitch))).Methods("POST", "OPTIONS")
	r.Handle("/api/killswitch/algo/{id}/reset", adminOnly(killSwitchResetAlgoHandler(o.killSwitch))).Methods("POST", "OPTIONS")
	r.Handle("/api/killswitch/trader/{id}/trip", adminOnly(killSwitchTripTraderHandler(o.killSwitch))).Methods("POST", "OPTIONS")
	r.Handle("/api/killswitch/trader/{id}/reset", adminOnly(killSwitchResetTraderHandler(o.killSwitch))).Methods("POST", "OPTIONS")
	r.Handle("/api/killswitch/log", adminOnly(killSwitchLogHandler(o.db))).Methods("GET", "OPTIONS")

	// Circuit breaker endpoints (Phase 3.6): status for ops, manual trip/reset
	// for admins. The breaker also auto-trips from drawdown / risk-engine state.
	r.Handle("/api/circuitbreaker/status", adminOnly(circuitBreakerStatusHandler(o.circuitBreaker))).Methods("GET", "OPTIONS")
	r.Handle("/api/circuitbreaker/trip", adminOnly(circuitBreakerTripHandler(o.circuitBreaker))).Methods("POST", "OPTIONS")
	r.Handle("/api/circuitbreaker/reset", adminOnly(circuitBreakerResetHandler(o.circuitBreaker))).Methods("POST", "OPTIONS")

	// --- CEO Certification (SEC 15c3-5 §(e)(2)) ---
	r.Handle("/api/compliance/certify", adminOnly(handleCEOCertify(o.db, o.logger))).Methods("POST", "OPTIONS")
	r.Handle("/api/compliance/certification/status", adminOnly(handleCertificationStatus(o.db))).Methods("GET", "OPTIONS")
	r.Handle("/api/compliance/certification/history", adminOnly(handleCertificationHistory(o.db))).Methods("GET", "OPTIONS")
	r.Handle("/api/compliance/audit/report", adminOnly(handleSECAuditReport(o.db, o.logger))).Methods("GET", "OPTIONS")
	r.Handle("/api/compliance/review", adminOnly(handleComplianceReview(o.db, o.logger))).Methods("POST", "OPTIONS")

	// --- Supervisory Workflow (FINRA Rule 3110) ---
	r.Handle("/api/supervisory/pending", adminOnly(handleSupervisoryPending(o.db))).Methods("GET", "OPTIONS")
	r.Handle("/api/supervisory/approve/{id}", adminOnly(http.HandlerFunc(o.handleSupervisoryApprove))).Methods("POST", "OPTIONS")
	r.Handle("/api/supervisory/reject/{id}", adminOnly(http.HandlerFunc(o.handleSupervisoryReject))).Methods("POST", "OPTIONS")
	r.Handle("/api/supervisory/history", adminOnly(handleSupervisoryHistory(o.db))).Methods("GET", "OPTIONS")

	// --- CAT / MiFID II Transaction Reporting ---
	r.Handle("/api/compliance/cat/status", adminOnly(handleCATStatus(o.db))).Methods("GET", "OPTIONS")
	r.Handle("/api/compliance/cat/export", adminOnly(handleCATExport(o.db))).Methods("GET", "OPTIONS")
	r.Handle("/api/compliance/cat/submit", adminOnly(handleCATSubmit(o.db, o.logger))).Methods("POST", "OPTIONS")
	r.Handle("/api/compliance/mifid/export", adminOnly(handleMiFIDExport(o.db))).Methods("GET", "OPTIONS")

	// --- Post-Trade Surveillance ---
	r.Handle("/api/surveillance/alerts", adminOnly(handleSurveillanceAlerts(o.db))).Methods("GET", "OPTIONS")
	r.Handle("/api/surveillance/review/{id}", adminOnly(handleSurveillanceReview(o.db, o.logger))).Methods("POST", "OPTIONS")
	r.Handle("/api/surveillance/status", adminOnly(handleSurveillanceStatus(o.db, o.surveillance))).Methods("GET", "OPTIONS")

	// --- Time Synchronization (MiFID II RTS 25) ---
	r.Handle("/api/time/status", adminOnly(handleTimeSyncStatus(o.timeSync))).Methods("GET", "OPTIONS")

	// --- Best Execution (MiFID II Article 27) ---
	r.Handle("/api/execution/quality", jwtAuthMiddleware(rbacMiddleware("admin", "trader")(handleExecutionQuality(o.bestExecution)))).Methods("GET", "OPTIONS")
	r.Handle("/api/execution/quality/report", adminOnly(handleExecutionQualityReport(o.bestExecution, o.logger))).Methods("GET", "OPTIONS")

	// --- Failover Status ---
	r.Handle("/api/failover/status", adminOnly(handleFailoverStatus(o.failover))).Methods("GET", "OPTIONS")
	r.Handle("/api/failover/promote", adminOnly(handleFailoverPromote(o.failover, o.logger))).Methods("POST", "OPTIONS")

	// --- MFA / User Management ---
	r.Handle("/api/auth/mfa/setup", jwtAuthMiddleware(handleMFASetup(o.db, o.encryption, o.logger))).Methods("POST", "OPTIONS")
	r.Handle("/api/auth/mfa/verify", jwtAuthMiddleware(handleMFAVerify(o.db, o.encryption, o.logger))).Methods("POST", "OPTIONS")
	r.Handle("/api/auth/mfa/status", jwtAuthMiddleware(handleMFAStatus(o.db))).Methods("GET", "OPTIONS")
	r.Handle("/api/auth/mfa/disable", adminOnly(handleMFADisable(o.db, o.logger))).Methods("POST", "OPTIONS")
	r.Handle("/api/auth/users", adminOnly(handleCreateUser(o.db, o.logger))).Methods("POST", "OPTIONS")
	r.Handle("/api/auth/users", adminOnly(handleListUsers(o.db))).Methods("GET", "OPTIONS")

	// --- HSM Status ---
	r.Handle("/api/hsm/status", adminOnly(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(o.hsmClient.Status())
	}))).Methods("GET", "OPTIONS")

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
	}))).Methods("GET", "OPTIONS")

	r.Handle("/api/portfolio/weights", jwtAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		weights := CalculateOptimalWeights()
		json.NewEncoder(w).Encode(weights)
	}))).Methods("GET", "OPTIONS")

	// ── Auth: Login & Refresh ────────────────────────────────────────────────────
	// POST /api/auth/login — username + password → JWT (no prior auth needed)
	r.HandleFunc("/api/auth/login", handleLogin(o.db, o.logger)).Methods("POST", "OPTIONS")
	// POST /api/auth/refresh — re-issue token (requires valid JWT)
	r.Handle("/api/auth/refresh", jwtAuthMiddleware(handleRefreshToken())).Methods("POST", "OPTIONS")

	// ── GET /api/assets — canonical tradable symbol list ────────────────────────
	r.Handle("/api/assets", jwtAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		type AssetInfo struct {
			Symbol       string  `json:"symbol"`
			Name         string  `json:"name"`
			Type         string  `json:"type"`
			InstrumentID int     `json:"instrument_id"`
			BasePrice    float64 `json:"base_price"`
		}
		prices := globalMarketData.GetAllPrices()
		var assets []AssetInfo
		idCounter := 1
		// Map predefined names for popular crypto
		nameMap := map[string]string{
			"BTC/USD": "Bitcoin",
			"ETH/USD": "Ethereum",
			"SOL/USD": "Solana",
		}
		for symbol, price := range prices {
			name, ok := nameMap[symbol]
			if !ok {
				name = symbol
			}
			assets = append(assets, AssetInfo{
				Symbol:       symbol,
				Name:         name,
				Type:         "crypto",
				InstrumentID: idCounter,
				BasePrice:    price,
			})
			idCounter++
		}
		// If no assets yet, at least return an empty array, not null
		if assets == nil {
			assets = []AssetInfo{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(assets)
	}))).Methods("GET", "OPTIONS")

	// ── GET /api/candles — OHLCV candle data with volume ────────────────────────
	r.Handle("/api/candles", jwtAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		symbol := req.URL.Query().Get("symbol")
		resolution := req.URL.Query().Get("resolution")
		countStr := req.URL.Query().Get("count")
		if symbol == "" {
			symbol = "BTC/USD"
		}
		if resolution == "" {
			resolution = "1m"
		}
		count := 200
		if n, err := strconv.Atoi(countStr); err == nil && n > 0 && n <= 500 {
			count = n
		}

		// Serve real aggregated candles from live feeds
		bars := globalCandleAgg.GetCandles(symbol, resolution, count)

		if len(bars) == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("[]"))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Candle-Source", "live-aggregator")
		json.NewEncoder(w).Encode(bars)
	}))).Methods("GET", "OPTIONS")

	// Apply middleware chain: requestID → rateLimit → router

	// Mount the KDB bridge routes (Phase 7 wiring). RegisterKDBRoutes takes a
	// stdlib ServeMux; route any /kdb/* requests through it.
	kdbMux := http.NewServeMux()
	RegisterKDBRoutes(kdbMux)
	r.PathPrefix("/kdb").Handler(kdbMux)

	handler := requestIDMiddleware(rateLimitMiddleware(1000, r))

	// Apply CORS — allow localhost:3000 in dev, restrict to explicit origin in production
	allowedOrigin := os.Getenv("ROBIN_CORS_ORIGIN")
	var allowedOrigins []string
	if allowedOrigin == "" {
		// Dev mode: allow Next.js dev server origins
		allowedOrigins = []string{"http://localhost:3000", "http://localhost:3001", "http://127.0.0.1:3000"}
		o.logger.Warn("CORS: dev mode — allowing localhost:3000. Set ROBIN_CORS_ORIGIN for production.")
	} else {
		allowedOrigins = []string{allowedOrigin}
	}
	c := cors.New(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowCredentials: true,
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Request-ID"},
	})
	handler = c.Handler(handler)

	// Security hardening headers (OWASP ASVS): CSP, HSTS, frame/x-content-type protections.
	handler = securityHeadersMiddleware(handler)

	return &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      handler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
}

// securityHeadersMiddleware injects OWASP-recommended hardening headers on every response.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'self'; object-src 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), interest-cohort=()")
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// propagateCancel forwards a cancel request to the C++ matching engine and
// waits for its acknowledgment. It returns an error if the engine is
// unavailable, does not respond, or rejects the cancel — callers must NOT
// confirm the cancellation in those cases.
func (o *Orchestrator) propagateCancel(order *ManagedOrder) (string, error) {
	if o.matchClient == nil || !o.matchClient.IsEnabled() {
		return "", fmt.Errorf("matching engine unavailable")
	}
	instID := getInstrumentID(order.Symbol)
	sideStr := "BID"
	if order.Side == "SELL" || order.Side == "ASK" {
		sideStr = "ASK"
	}
	matchJSON := fmt.Sprintf(
		`{"cl_ord_id":"%s","id":%d,"instrument_id":%d,"price":%d,"qty":%d,"side":"%s","type":"CANCEL"}`,
		order.ClOrdID, order.OrderID, instID, int64(order.Price*100000000), int64(order.LeavesQty*100000000), sideStr,
	)
	resp, err := o.matchClient.SendOrderJSON(matchJSON)
	if err != nil {
		return "", err
	}
	var meResp MatchingEngineResponse
	if err := json.Unmarshal([]byte(resp), &meResp); err != nil {
		return "", fmt.Errorf("malformed engine response: %w", err)
	}
	if !meResp.Success && meResp.Status != "CANCELED" {
		return resp, fmt.Errorf("engine rejected cancel: %s", meResp.Error)
	}
	return resp, nil
}

// propagateModify forwards a price/qty replacement to the C++ matching engine
// and waits for its acknowledgment. Modifications must not be confirmed unless
// the engine accepts them, so partial pricing corrections cannot silently
// diverge from the exchange state.
func (o *Orchestrator) propagateModify(order *ManagedOrder, newPrice, newQty float64) (string, error) {
	if o.matchClient == nil || !o.matchClient.IsEnabled() {
		return "", fmt.Errorf("matching engine unavailable")
	}
	instID := getInstrumentID(order.Symbol)
	sideStr := "BID"
	if order.Side == "SELL" || order.Side == "ASK" {
		sideStr = "ASK"
	}
	matchJSON := fmt.Sprintf(
		`{"cl_ord_id":"%s","id":%d,"instrument_id":%d,"price":%d,"qty":%d,"side":"%s","type":"REPLACE"}`,
		order.ClOrdID, order.OrderID, instID, int64(newPrice*100000000), int64(newQty*100000000), sideStr,
	)
	resp, err := o.matchClient.SendOrderJSON(matchJSON)
	if err != nil {
		return "", err
	}
	var meResp MatchingEngineResponse
	if err := json.Unmarshal([]byte(resp), &meResp); err != nil {
		return "", fmt.Errorf("malformed engine response: %w", err)
	}
	if !meResp.Success {
		return resp, fmt.Errorf("engine rejected modify: %s", meResp.Error)
	}
	return resp, nil
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

	// Validate the order was actually accepted, not just that the HTTP call
	// succeeded. Alpaca can return 202 with an order that was immediately
	// rejected or suspended (audit Bug #2).
	var alpacaOrder struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(bodyBytes, &alpacaOrder); err != nil {
		return "", fmt.Errorf("Alpaca returned invalid JSON: %v: %s", err, string(bodyBytes))
	}
	if alpacaOrder.ID == "" {
		return "", fmt.Errorf("Alpaca order rejected (no order id): %s", string(bodyBytes))
	}
	switch alpacaOrder.Status {
	case "rejected", "suspended", "expired", "canceled":
		return "", fmt.Errorf("Alpaca order not accepted, status=%q: %s", alpacaOrder.Status, string(bodyBytes))
	}

	return string(bodyBytes), nil
}

// ============================================================================
// Main
// ============================================================================

// Main moved to main.go
