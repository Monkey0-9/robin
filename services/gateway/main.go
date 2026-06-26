package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	logger.Info("Robin Gateway Orchestrator starting", "version", "1.1.0")

	// Enforce JWT key check for production runtime
	if jwtAuth.publicKey == nil && jwtAuth.hmacKey == nil {
		logger.Error("no JWT key configured (set ROBIN_JWT_PUBKEY or ROBIN_GATEWAY_API_TOKEN), refusing to start insecurely")
		os.Exit(1)
	}

	orch := NewOrchestrator()
	orch.RegisterService("ExecutionCore", "127.0.0.1:9091")
	orch.RegisterService("RiskAnalytics", "127.0.0.1:9092")
	orch.RegisterService("MarketData", "127.0.0.1:9093")
	orch.RegisterService("PortfolioEngine", "127.0.0.1:9094")
	orch.RegisterService("Compliance", "127.0.0.1:9095")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orch.StartHealthProbes(ctx, 100*time.Millisecond)

	httpPort := 8080
	if p := os.Getenv("ORCH_PORT"); p != "" {
		if val, err := strconv.Atoi(p); err == nil {
			httpPort = val
		}
	}

	httpServer := orch.setupHTTPServer(httpPort)

	tlsCfg := orch.GetConfig().TLS
	if tlsCfg.Enabled {
		tlsCfg.CertFile = envOrDefault("ORCH_TLS_CERT", tlsCfg.CertFile)
		tlsCfg.KeyFile = envOrDefault("ORCH_TLS_KEY", tlsCfg.KeyFile)
		if tlsCfg.CertFile != "" && tlsCfg.KeyFile != "" {
			caCert, err := os.ReadFile(tlsCfg.CertFile)
			if err == nil {
				caPool := x509.NewCertPool()
				if caPool.AppendCertsFromPEM(caCert) {
					httpServer.TLSConfig = &tls.Config{
						MinVersion: tls.VersionTLS12,
						ClientCAs:  caPool,
					}
				}
			}
			go func() {
				logger.Info("TLS server listening", "port", httpPort)
				if err := httpServer.ListenAndServeTLS(tlsCfg.CertFile, tlsCfg.KeyFile); err != nil && err != http.ErrServerClosed {
					logger.Error("TLS server error", "error", err)
					os.Exit(1)
				}
			}()
		} else {
			logger.Warn("TLS enabled but cert/key missing, falling back to plain HTTP", "port", httpPort)
			go startHTTP(httpServer, logger)
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
