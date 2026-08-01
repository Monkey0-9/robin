package main

import (
	"encoding/csv"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// TickLogger handles asynchronous appending of tick data to flat files.
type TickLogger struct {
	mu       sync.Mutex
	dir      string
	writers  map[string]*csv.Writer
	files    map[string]*os.File
	flushCh  chan struct{}
}

var globalTickLogger *TickLogger

func InitTickLogger(storageDir string) error {
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return err
	}
	
	globalTickLogger = &TickLogger{
		dir:     storageDir,
		writers: make(map[string]*csv.Writer),
		files:   make(map[string]*os.File),
		flushCh: make(chan struct{}),
	}
	
	go globalTickLogger.flushLoop()
	return nil
}

func (t *TickLogger) getWriter(symbol, tickType string) (*csv.Writer, error) {
	// Date-based filename, e.g., BTC-USD_trades_2026-07-29.csv
	safeSymbol := strings.ReplaceAll(symbol, "/", "-")
	dateStr := time.Now().UTC().Format("2006-01-02")
	fileName := fmt.Sprintf("%s_%s_%s.csv", safeSymbol, tickType, dateStr)
	
	if w, exists := t.writers[fileName]; exists {
		return w, nil
	}
	
	filePath := filepath.Join(t.dir, fileName)
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return nil, err
	}

	fileExists := false
	if _, err := os.Stat(filePath); err == nil {
		fileExists = true
	}
	
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	
	w := csv.NewWriter(f)
	
	if !fileExists {
		// Write headers
		if tickType == "trades" {
			w.Write([]string{"timestamp", "trade_id", "side", "price", "size"})
		} else if tickType == "l2" {
			w.Write([]string{"timestamp", "side", "price", "size"})
		}
		w.Flush()
	}
	
	t.files[fileName] = f
	t.writers[fileName] = w
	return w, nil
}

func (t *TickLogger) LogTrade(symbol, tradeID, side string, price, size float64, ts time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	w, err := t.getWriter(symbol, "trades")
	if err != nil {
		slog.Error("Failed to get tick writer", "error", err)
		return
	}
	
	w.Write([]string{
		fmt.Sprintf("%d", ts.UnixNano()),
		tradeID,
		side,
		fmt.Sprintf("%f", price),
		fmt.Sprintf("%f", size),
	})
}

func (t *TickLogger) LogL2Update(symbol, side string, price, size float64, ts time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	w, err := t.getWriter(symbol, "l2")
	if err != nil {
		slog.Error("Failed to get tick writer", "error", err)
		return
	}
	
	w.Write([]string{
		fmt.Sprintf("%d", ts.UnixNano()),
		side,
		fmt.Sprintf("%f", price),
		fmt.Sprintf("%f", size),
	})
}

func (t *TickLogger) flushLoop() {
	ticker := time.NewTicker(1 * time.Second)
	for {
		select {
		case <-ticker.C:
			t.mu.Lock()
			for _, w := range t.writers {
				w.Flush()
			}
			t.mu.Unlock()
		}
	}
}
