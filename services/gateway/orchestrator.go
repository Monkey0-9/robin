package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

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

type ServiceHealth struct {
	Name      string        `json:"name"`
	Status    ServiceStatus `json:"status"`
	Latency   time.Duration `json:"latency_ns"`
	LastCheck time.Time     `json:"last_check"`
	Addr      string        `json:"addr"`
}

type HotReloadConfig struct {
	MaxDrawdownLimit float64 `json:"max_drawdown_limit"`
	MarketDataPort   int     `json:"market_data_port"`
	OrderEntryPort   int     `json:"order_entry_port"`
	MaxOrderRate     uint32  `json:"max_order_rate"`
	MaxCancelRate    uint32  `json:"max_cancel_rate"`
	MaxPositionLimit int64   `json:"max_position_limit"`
}

type Orchestrator struct {
	mu              sync.RWMutex
	services        map[string]*ServiceHealth
	config          HotReloadConfig
	configMutex     sync.RWMutex
	healthyCount    atomic.Int32
	degradedCount   atomic.Int32
	failedCount     atomic.Int32
	totalChecks     atomic.Uint64
	shutdownCh      chan struct{}
	wg              sync.WaitGroup

	orderCount      atomic.Uint64
	rejectCount     atomic.Uint64
	tradeCount      atomic.Uint64
	latencySum      atomic.Uint64
}

func NewOrchestrator() *Orchestrator {
	return &Orchestrator{
		services: make(map[string]*ServiceHealth),
		config: HotReloadConfig{
			MaxDrawdownLimit: 0.10,
			MarketDataPort:   8080,
			OrderEntryPort:   9090,
			MaxOrderRate:     10000,
			MaxCancelRate:    5000,
			MaxPositionLimit: 100000,
		},
		shutdownCh: make(chan struct{}),
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
	log.Printf("[ORCHESTRATOR] Registered service: %s @ %s", name, addr)
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
	log.Printf("[ORCHESTRATOR] Health probes started (interval=%v)", interval)
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
		svc.Latency = latency
		svc.LastCheck = time.Now()

		if err != nil {
			svc.Status = StatusFailed
			failed++
			log.Printf("[ORCHESTRATOR] %s FAILED (%v)", name, err)
		} else {
			conn.Close()
			if latency > 10*time.Millisecond {
				svc.Status = StatusDegraded
				degraded++
				log.Printf("[ORCHESTRATOR] %s DEGRADED (latency=%v)", name, latency)
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
	defer o.configMutex.Unlock()

	var newConfig HotReloadConfig
	if err := json.Unmarshal(jsonConfig, &newConfig); err != nil {
		return fmt.Errorf("config parse error: %w", err)
	}

	o.config = newConfig
	log.Printf("[ORCHESTRATOR] Config hot-reloaded: %+v", o.config)
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

func (o *Orchestrator) RecordOrder() {
	o.orderCount.Add(1)
}

func (o *Orchestrator) RecordReject() {
	o.rejectCount.Add(1)
}

func (o *Orchestrator) RecordTrade() {
	o.tradeCount.Add(1)
}

func (o *Orchestrator) RecordLatency(ns uint64) {
	o.latencySum.Add(ns)
}

func (o *Orchestrator) Shutdown() {
	close(o.shutdownCh)
	o.wg.Wait()
	log.Println("[ORCHESTRATOR] Shutdown complete")
}

func (o *Orchestrator) setupHTTPServer(port int) *http.Server {
	r := mux.NewRouter()

	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "ok",
			"healthy":  o.healthyCount.Load(),
			"degraded": o.degradedCount.Load(),
			"failed":   o.failedCount.Load(),
			"checks":   o.totalChecks.Load(),
		})
	}).Methods("GET")

	r.HandleFunc("/services", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(o.GetServices())
	}).Methods("GET")

	r.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(o.GetConfig())
	}).Methods("GET")

	r.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		raw, _ := json.Marshal(body)
		if err := o.HotReloadConfig(raw); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "reloaded"})
	}).Methods("POST")

	r.HandleFunc("/metrics", promhttp.Handler().ServeHTTP)

	r.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		orders := o.orderCount.Load()
		rejects := o.rejectCount.Load()
		trades := o.tradeCount.Load()
		latSum := o.latencySum.Load()
		avgLat := uint64(0)
		if trades > 0 {
			avgLat = latSum / trades
		}
		json.NewEncoder(w).Encode(map[string]uint64{
			"orders":     orders,
			"rejects":    rejects,
			"trades":     trades,
			"avg_lat_ns": avgLat,
		})
	}).Methods("GET")

	return &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: r,
	}
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)
	log.Println("[ORCHESTRATOR] Quantum Trading Gateway Orchestrator starting...")

	orch := NewOrchestrator()

	orch.RegisterService("ExecutionCore", "127.0.0.1:9091")
	orch.RegisterService("RiskAnalytics", "127.0.0.1:9092")
	orch.RegisterService("MarketData", "127.0.0.1:9093")
	orch.RegisterService("PortfolioEngine", "127.0.0.1:9094")
	orch.RegisterService("Compliance", "127.0.0.1:9095")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orch.StartHealthProbes(ctx, 100*time.Millisecond)

	httpServer := orch.setupHTTPServer(8080)
	go func() {
		log.Printf("[ORCHESTRATOR] HTTP server listening on :8080")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[ORCHESTRATOR] HTTP server error: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	log.Println("[ORCHESTRATOR] Shutting down...")
	cancel()
	httpServer.Shutdown(context.Background())
	orch.Shutdown()
}
