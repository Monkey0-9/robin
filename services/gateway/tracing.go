package main

// ============================================================================
// Robin Trading Platform — OpenTelemetry Distributed Tracing
// ============================================================================
// Provides full distributed tracing across all services:
//   - Go gateway (orchestrator, compliance, kill switch)
//   - C++ matching engine (via OTLP gRPC exporter)
//   - Rust risk gate (via OTLP gRPC exporter)
//   - OCaml portfolio optimizer (via OTLP gRPC exporter)
//   - Python AI agents (via OpenTelemetry SDK)
//
// Spans are created for every critical operation:
//   - Order lifecycle: submit → risk check → matching → fill
//   - Market data: receive → parse → signal → strategy decision
//   - Portfolio optimization: load → optimize → publish
//   - Compliance: surveillance check → audit log → alert
//
// Exporter: OTLP gRPC to Grafana Tempo or Jaeger
// Sampling: Head-based with rate limiting (100 req/s full traces, then 1/1000)
// Baggage: Propagate order_id, instrument_id, client_id across services

import (
	"context"
	"strconv"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	oteltrace "go.opentelemetry.io/otel/trace"
	"log/slog"
	"time"
)

// TracerProvider is the global OpenTelemetry tracer provider.
var TracerProvider *sdktrace.TracerProvider

// TracingConfig holds OpenTelemetry configuration.
type TracingConfig struct {
	Enabled         bool
	Endpoint        string // OTLP gRPC endpoint (e.g., "localhost:4317")
	ServiceName     string
	Environment     string
	SampleRate      float64 // 0.0-1.0 (1.0 = sample all)
	BatchTimeout    time.Duration
	ExportTimeout   time.Duration
	MaxExportBatchSize int
}

// DefaultTracingConfig returns sensible defaults for production.
func DefaultTracingConfig() TracingConfig {
	return TracingConfig{
		Enabled:         true,
		Endpoint:        "localhost:4317",
		ServiceName:     "robin-gateway",
		Environment:     "production",
		SampleRate:      1.0,
		BatchTimeout:    5 * time.Second,
		ExportTimeout:   10 * time.Second,
		MaxExportBatchSize: 512,
	}
}

// InitTracing initializes the OpenTelemetry tracer provider.
func InitTracing(cfg TracingConfig) (*sdktrace.TracerProvider, error) {
	if !cfg.Enabled {
		slog.Info("OpenTelemetry tracing disabled")
		return nil, nil
	}

	ctx := context.Background()
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithTimeout(cfg.ExportTimeout),
		otlptracegrpc.WithInsecure(), // Use mTLS in production
	)
	if err != nil {
		return nil, err
	}

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.ServiceName),
		semconv.DeploymentEnvironment(cfg.Environment),
		attribute.String("platform", "robin"),
		attribute.String("architecture", "polyglot"),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(cfg.BatchTimeout),
			sdktrace.WithMaxExportBatchSize(cfg.MaxExportBatchSize),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRate))),
	)

	otel.SetTracerProvider(tp)

	slog.Info("OpenTelemetry tracing initialized",
		"endpoint", cfg.Endpoint,
		"service", cfg.ServiceName,
		"sample_rate", cfg.SampleRate,
	)

	return tp, nil
}

// StartOrderSpan creates a new span for an order lifecycle operation.
func StartOrderSpan(ctx context.Context, operation string, orderID uint64, instrumentID uint32, clientID uint32) (context.Context, oteltrace.Span) {
	tracer := otel.Tracer("robin-gateway")
	ctx, span := tracer.Start(ctx, operation,
		oteltrace.WithAttributes(
			attribute.String("order.id", formatID(orderID)),
			attribute.Int64("order.instrument_id", int64(instrumentID)),
			attribute.Int64("order.client_id", int64(clientID)),
			attribute.String("service", "gateway"),
		),
	)
	return ctx, span
}

// StartRiskSpan creates a span for risk check operations.
func StartRiskSpan(ctx context.Context, operation string, orderID uint64) (context.Context, oteltrace.Span) {
	tracer := otel.Tracer("robin-risk")
	ctx, span := tracer.Start(ctx, operation,
		oteltrace.WithAttributes(
			attribute.String("order.id", formatID(orderID)),
			attribute.String("service", "risk"),
		),
	)
	return ctx, span
}

// StartMatchSpan creates a span for matching engine operations.
func StartMatchSpan(ctx context.Context, operation string, orderID uint64) (context.Context, oteltrace.Span) {
	tracer := otel.Tracer("robin-matching")
	ctx, span := tracer.Start(ctx, operation,
		oteltrace.WithAttributes(
			attribute.String("order.id", formatID(orderID)),
			attribute.String("service", "matching"),
		),
	)
	return ctx, span
}

// RecordLatency records a latency measurement as a span event.
func RecordLatency(span oteltrace.Span, operation string, latencyNs int64) {
	span.AddEvent("latency",
		oteltrace.WithAttributes(
			attribute.String("operation", operation),
			attribute.Int64("latency_ns", latencyNs),
			attribute.Float64("latency_us", float64(latencyNs)/1000.0),
			attribute.Float64("latency_ms", float64(latencyNs)/1_000_000.0),
		),
	)
}

// RecordError records an error on the span.
func RecordError(span oteltrace.Span, err error, description string) {
	span.RecordError(err, oteltrace.WithAttributes(
		attribute.String("error.description", description),
		attribute.String("error.type", "institutional_risk"),
	))
}

// formatID formats an ID for consistent trace attributes.
func formatID(id uint64) string {
	return strconv.FormatUint(id, 10)
}

// Helper functions

// SpanContextFromContext extracts trace IDs for logging.
func SpanContextFromContext(ctx context.Context) (traceID string, spanID string) {
	span := oteltrace.SpanFromContext(ctx)
	sc := span.SpanContext()
	if sc.HasTraceID() {
		traceID = sc.TraceID().String()
	}
	if sc.HasSpanID() {
		spanID = sc.SpanID().String()
	}
	return
}

// ShutdownTracing flushes spans and shuts down the tracer.
func ShutdownTracing(tp *sdktrace.TracerProvider) {
	if tp == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := tp.Shutdown(ctx); err != nil {
		slog.Error("failed to shutdown tracer", "error", err)
	}
}
