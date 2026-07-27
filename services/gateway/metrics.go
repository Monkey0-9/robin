package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// OrderLatency tracks end-to-end order processing latency
	OrderLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "robin_order_latency_ns",
		Help:    "Order processing latency in nanoseconds",
		Buckets: prometheus.ExponentialBuckets(1000, 2, 20), // 1us to ~524ms
	}, []string{"symbol", "side", "status"})

	// TradeLatency tracks trade execution latency from order submission to fill
	TradeLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "robin_trade_latency_ns",
		Help:    "Trade execution latency in nanoseconds",
		Buckets: prometheus.ExponentialBuckets(1000, 2, 20),
	}, []string{"symbol", "side"})

	// RiskCheckLatency tracks risk gate latency
	RiskCheckLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "robin_risk_check_latency_ns",
		Help:    "Risk check latency in nanoseconds",
		Buckets: prometheus.ExponentialBuckets(100, 2, 15), // 100ns to ~1.6ms
	}, []string{"check_type"})

	// ServiceHealthLatency tracks health check probes to downstream services
	ServiceHealthLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "robin_health_probe_latency_ns",
		Help:    "Health probe latency in nanoseconds",
		Buckets: prometheus.ExponentialBuckets(1000, 2, 15),
	}, []string{"service_name", "status"})

	// MatchingEngineLatency tracks matching engine response time
	MatchingEngineLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "robin_matching_engine_latency_ns",
		Help:    "Matching engine response latency in nanoseconds",
		Buckets: prometheus.ExponentialBuckets(100, 2, 20),
	}, []string{"instrument_id"})

	// OrderCount is a counter for total orders processed
	OrderCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "robin_orders_total",
		Help: "Total number of orders processed",
	}, []string{"symbol", "side", "status"})

	// TradeCount is a counter for total trades executed
	TradeCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "robin_trades_total",
		Help: "Total number of trades executed",
	}, []string{"symbol", "side"})

	// RejectCount is a counter for rejected orders
	RejectCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "robin_rejects_total",
		Help: "Total number of rejected orders",
	}, []string{"symbol", "reason"})

	// PositionGauge tracks current position size per symbol
	PositionGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "robin_position_qty",
		Help: "Current position quantity per symbol",
	}, []string{"symbol", "account_id"})

	// UnrealizedPnLGauge tracks current unrealized P&L per symbol
	UnrealizedPnLGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "robin_unrealized_pnl",
		Help: "Current unrealized profit/loss",
	}, []string{"symbol"})

	// ConnectionStatus tracks downstream service connection state
	ConnectionStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "robin_connection_status",
		Help: "Connection status to downstream services (1=connected, 0=disconnected)",
	}, []string{"service_name"})

	// StrategySignalGauge tracks the latest signal per strategy
	StrategySignalGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "robin_strategy_signal",
		Help: "Latest strategy signal strength",
	}, []string{"strategy_id", "symbol"})

	// KillSwitchStatus tracks the current kill switch state
	KillSwitchStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "robin_kill_switch_active",
		Help: "Kill switch active state (1=tripped, 0=normal)",
	})
)
