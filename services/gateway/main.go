package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	logger.Info("Robin Gateway Orchestrator starting", "version", "1.1.0")

	if err := InitJWTAuth(); err != nil {
		logger.Error("JWT init failed", "error", err)
		os.Exit(1)
	}

	tp, err := InitTracer(context.Background(), "gateway-orchestrator")
	if err != nil {
		logger.Error("failed to init tracer", "error", err)
	} else {
		defer func() {
			if err := tp.Shutdown(context.Background()); err != nil {
				logger.Error("failed to shutdown tracer", "error", err)
			}
		}()
	}

	// Enforce JWT key check for production runtime
	if jwtAuth.PublicKey == nil {
		logger.Error("no JWT key configured (set ROBIN_JWT_PUBKEY_FILE or ROBIN_GATEWAY_API_TOKEN), refusing to start insecurely")
		os.Exit(1)
	}

	orch := NewOrchestrator()
	orch.RegisterService("ExecutionCore", "127.0.0.1:9091")
	orch.RegisterService("RiskAnalytics", "127.0.0.1:9092")
	orch.RegisterService("MarketData", "127.0.0.1:9093")
	orch.RegisterService("PortfolioEngine", "127.0.0.1:9094")
	orch.RegisterService("Compliance", "127.0.0.1:9095")

	// NOTE: MarketData (9093) and PortfolioEngine (9094) are optional external services.
	// Their health status will show as FAILED until they connect — this is intentional
	// to prevent false-positive health reporting.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize position manager (fetches live prices from AI agent)
	initPositionManager()

	// Initialize tick logger for flat-file persistence
	kdbDir := os.Getenv("ROBIN_DATA_DIR")
	if kdbDir == "" {
		kdbDir = filepath.Join(".", "kdb_storage")
	} else {
		kdbDir = filepath.Join(kdbDir, "kdb_storage")
	}
	if err := InitTickLogger(kdbDir); err != nil {
		logger.Error("Failed to initialize tick logger", "error", err, "path", kdbDir)
	}

	orch.StartHealthProbes(ctx, 5*time.Second)

	// Circuit breaker integration (Phase 3.6): poll the risk engine's metrics
	// endpoint so a risk-side drawdown trip halts gateway order entry.
	if orch.circuitBreaker != nil {
		orch.circuitBreaker.StartRiskPolling(ctx)
	}

	// Phase 2 wiring: stream live NBBO/best prices + Reg SHO previous closes
	// into the risk daemon so price-collar, correlation, and the short-sale
	// circuit breaker act on real market data. Best-effort and non-fatal.
	go StartRiskMarketFeed(ctx)

	// Phase 4 wiring: start the post-trade surveillance and time-sync engines.
	// They are constructed in NewOrchestrator but only begin consuming events
	// once Start(ctx) is invoked; without this they were silent dead code.
	if orch.surveillance != nil {
		orch.surveillance.Start(ctx)
	}
	if orch.timeSync != nil {
		orch.timeSync.Start(ctx)
	}

	httpPort := 8080
	if p := os.Getenv("ORCH_PORT"); p != "" {
		if val, err := strconv.Atoi(p); err == nil {
			httpPort = val
		}
	}

	httpServer := orch.setupHTTPServer(httpPort)

	tlsCfg := orch.GetConfig().TLS
	if tlsCfg.Enabled || os.Getenv("ORCH_MTLS_ENABLED") == "1" {
		certFile := envOrDefault("ORCH_TLS_CERT", tlsCfg.CertFile)
		keyFile := envOrDefault("ORCH_TLS_KEY", tlsCfg.KeyFile)
		caFile := envOrDefault("ORCH_CA_CERT", "")
		if certFile != "" && keyFile != "" && caFile != "" {
			caCert, err := os.ReadFile(caFile)
			if err != nil {
				logger.Error("failed to read CA cert for mTLS", "error", err)
				os.Exit(1)
			}
			caPool := x509.NewCertPool()
			if !caPool.AppendCertsFromPEM(caCert) {
				logger.Error("failed to parse CA cert PEM for mTLS", "file", caFile)
				os.Exit(1)
			}
			httpServer.TLSConfig = &tls.Config{
				MinVersion: tls.VersionTLS12,
				ClientCAs:  caPool,
				ClientAuth: tls.RequireAndVerifyClientCert,
			}
			go func() {
				logger.Info("mTLS server listening", "port", httpPort)
				if err := httpServer.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
					logger.Error("TLS server error", "error", err)
					os.Exit(1)
				}
			}()
		} else {
			logger.Error("mTLS enabled but cert/key/ca missing, refusing to start insecurely")
			os.Exit(1)
		}
	} else {
		go startHTTP(httpServer, logger)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	logger.Info("shutdown signal received", "signal", sig.String())

	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown error", "error", err)
	}
	orch.Shutdown()
}

func startHTTP(srv *http.Server, logger *slog.Logger) {
	logger.Info("HTTP server listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("HTTP server error", "error", err)
		os.Exit(1)
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
