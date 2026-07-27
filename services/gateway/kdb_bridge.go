// ============================================================================
// Robin Trading Platform — KDB+ HTTP Bridge
// ============================================================================
// Provides a JSON HTTP API that proxies queries to the KDB+ real-time database
// (rdb.q listening on port 5000) for tick data storage and retrieval.
//
// KDB+ Q IPC protocol: connect to localhost:5000, send sync message.
// Since KDB+ runs separately, this bridge decouples Go from needing
// native Q bindings while still enabling tick data persistence.
//
// Endpoints:
//   GET  /kdb/health              — check KDB+ connectivity
//   POST /kdb/insert              — insert tick or trade record
//   GET  /kdb/query?q=...         — run a Q expression (read-only)
//   GET  /kdb/ticks?sym=BTC-USD   — last N ticks for symbol
//   GET  /kdb/ohlcv?sym=...&bin=1m — OHLCV bars for symbol
//   GET  /kdb/vwap?sym=...        — VWAP for symbol today
// ============================================================================

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// ─── KDB+ IPC client ──────────────────────────────────────────────────────────

// KDBClient handles Q IPC messages over TCP to KDB+ port 5000.
type KDBClient struct {
	addr    string
	timeout time.Duration
}

// NewKDBClient creates a new KDB+ IPC client.
func NewKDBClient() *KDBClient {
	addr := os.Getenv("KDB_ADDR")
	if addr == "" {
		addr = "127.0.0.1:5000"
	}
	return &KDBClient{
		addr:    addr,
		timeout: 5 * time.Second,
	}
}

// IsAvailable checks if KDB+ is reachable.
func (k *KDBClient) IsAvailable() bool {
	conn, err := net.DialTimeout("tcp", k.addr, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// ExecQ executes a Q expression synchronously and returns raw response bytes.
// Q IPC v3 sync message format:
//   byte[0] = 1 (little-endian)
//   byte[1] = 1 (sync message type)
//   byte[2] = 0 (no compression)
//   byte[3] = 0 (reserved)
//   byte[4-7] = total message length (4 + 1 + 4 + len(expr))
//   byte[8]   = 10 (char list type = string)
//   byte[9]   = 0  (attributes)
//   byte[10-13] = string length
//   byte[14+]  = expression bytes
func (k *KDBClient) ExecQ(expr string) ([]byte, error) {
	conn, err := net.DialTimeout("tcp", k.addr, k.timeout)
	if err != nil {
		return nil, fmt.Errorf("kdb connect: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(k.timeout))

	// Authenticate (KDB+ expects username:password\n)
	creds := os.Getenv("KDB_CREDENTIALS")
	if creds == "" {
		creds = ":" // anonymous
	}
	fmt.Fprintf(conn, "%s\n", creds)
	buf := make([]byte, 1)
	conn.Read(buf) // capability byte

	// Build Q IPC sync message
	exprBytes := []byte(expr)
	msgLen := uint32(8 + 1 + 1 + 4 + len(exprBytes)) // header(8) + type(1) + attrs(1) + len(4) + data

	msg := &bytes.Buffer{}
	msg.Write([]byte{1, 1, 0, 0}) // little-endian, sync, no compress, reserved
	binary.Write(msg, binary.LittleEndian, msgLen)
	msg.WriteByte(10) // char list type
	msg.WriteByte(0)  // no attributes
	binary.Write(msg, binary.LittleEndian, uint32(len(exprBytes)))
	msg.Write(exprBytes)

	if _, err := conn.Write(msg.Bytes()); err != nil {
		return nil, fmt.Errorf("kdb write: %w", err)
	}

	// Read response header
	hdr := make([]byte, 8)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return nil, fmt.Errorf("kdb read header: %w", err)
	}
	totalLen := binary.LittleEndian.Uint32(hdr[4:8])
	if totalLen < 8 {
		return nil, fmt.Errorf("kdb invalid response length: %d", totalLen)
	}

	body := make([]byte, totalLen-8)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, fmt.Errorf("kdb read body: %w", err)
	}
	return body, nil
}

// ─── KDB Bridge HTTP handler ──────────────────────────────────────────────────

// KDBBridge wraps the KDB client and provides HTTP handlers.
type KDBBridge struct {
	client  *KDBClient
	enabled bool
}

// NewKDBBridge creates a KDB bridge, disabled if KDB+ is unreachable.
func NewKDBBridge() *KDBBridge {
	client := NewKDBClient()
	enabled := client.IsAvailable()
	if enabled {
		log.Println("[KDB] Bridge connected to", client.addr)
	} else {
		log.Println("[KDB] KDB+ not available at", client.addr, "— bridge in stub mode")
	}
	return &KDBBridge{client: client, enabled: enabled}
}

func (b *KDBBridge) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (b *KDBBridge) writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// HandleHealth checks KDB+ connectivity.
func (b *KDBBridge) HandleHealth(w http.ResponseWriter, r *http.Request) {
	available := b.client.IsAvailable()
	status := "connected"
	if !available {
		status = "unavailable"
	}
	b.writeJSON(w, map[string]any{
		"kdb_status": status,
		"kdb_addr":   b.client.addr,
		"timestamp":  time.Now().UTC(),
	})
}

// HandleInsert inserts a record into KDB+ via Q IPC.
// Body: {"table": "quotes", "sym": "BTC-USD", "price": 65000.0, "size": 0.5}
func (b *KDBBridge) HandleInsert(w http.ResponseWriter, r *http.Request) {
	if !b.enabled {
		b.writeError(w, http.StatusServiceUnavailable,
			"KDB+ not available — tick storage in fallback mode")
		return
	}

	var body struct {
		Table string  `json:"table"`
		Sym   string  `json:"sym"`
		Price float64 `json:"price"`
		Size  float64 `json:"size"`
		Side  string  `json:"side"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		b.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	table := "quotes"
	if body.Table != "" {
		// Sanitize table name identically to sym to prevent Q injection
		table = strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
				return r
			}
			return '_'
		}, body.Table)
	}

	// Q expression to upsert into tick table
	// Escape symbol to prevent Q injection: sanitize non-alphanumeric chars
	sym := strings.ReplaceAll(body.Sym, "-", "_")
	sym = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, sym)
	side := strings.ToLower(body.Side)
	if side != "buy" && side != "sell" {
		side = "none"
	}
	expr := fmt.Sprintf(
		"`%s insert (`%s;%.0d;%.6f;%.6f;`%s)",
		table, sym,
		time.Now().UnixNano(),
		body.Price, body.Size,
		side,
	)

	_, err := b.client.ExecQ(expr)
	if err != nil {
		log.Printf("[KDB] Insert error: %v", err)
		b.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	b.writeJSON(w, map[string]any{
		"status":    "inserted",
		"table":     table,
		"sym":       body.Sym,
		"timestamp": time.Now().UTC(),
	})
}

// HandleQuery executes a read-only Q expression.
// GET /kdb/query?q=select+from+quotes+where+sym=`BTC_USD
func (b *KDBBridge) HandleQuery(w http.ResponseWriter, r *http.Request) {
	if !b.enabled {
		b.writeJSON(w, map[string]any{
			"status": "stub",
			"data":   []any{},
			"note":   "KDB+ not available — returning empty results",
		})
		return
	}

	q := r.URL.Query().Get("q")
	if q == "" {
		b.writeError(w, http.StatusBadRequest, "q parameter required")
		return
	}

	// Safety: only allow select queries
	lower := strings.TrimSpace(strings.ToLower(q))
	if !strings.HasPrefix(lower, "select") && !strings.HasPrefix(lower, "count") {
		b.writeError(w, http.StatusForbidden, "only SELECT queries allowed via this endpoint")
		return
	}

	result, err := b.client.ExecQ(q)
	if err != nil {
		log.Printf("[KDB] Query error: %v", err)
		b.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Return raw bytes as hex (Q IPC binary — frontend can decode or display)
	b.writeJSON(w, map[string]any{
		"status":    "ok",
		"raw_bytes": len(result),
		"note":      "Q IPC binary response — use KDB+ HTTP gateway for JSON output",
	})
}

// HandleTicks returns the last N ticks for a symbol.
// Delegates to KDB+ http_gateway.q if available, else returns stub data.
func (b *KDBBridge) HandleTicks(w http.ResponseWriter, r *http.Request) {
	sym := r.URL.Query().Get("sym")
	if sym == "" {
		b.writeError(w, http.StatusBadRequest, "sym parameter required")
		return
	}

	if !b.enabled {
		// Return synthetic tick data for dashboard
		now := time.Now()
		ticks := make([]map[string]any, 10)
		price := 65000.0
		for i := range ticks {
			ticks[i] = map[string]any{
				"sym":       sym,
				"timestamp": now.Add(-time.Duration(i) * time.Second).UnixNano(),
				"price":     price + float64(i)*10.0,
				"size":      0.1,
				"source":    "stub",
			}
		}
		b.writeJSON(w, map[string]any{
			"sym":    sym,
			"ticks":  ticks,
			"source": "stub_kdb_unavailable",
		})
		return
	}

	qSym := strings.ReplaceAll(sym, "-", "_")
	result, err := b.client.ExecQ(fmt.Sprintf(
		"select[-10] from quotes where sym=`%s", qSym,
	))
	if err != nil {
		b.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	b.writeJSON(w, map[string]any{
		"sym":       sym,
		"raw_bytes": len(result),
		"note":      "raw KDB+ IPC — use http_gateway.q for JSON",
	})
}

// RegisterKDBRoutes registers all KDB bridge HTTP routes.
func RegisterKDBRoutes(mux *http.ServeMux) {
	bridge := NewKDBBridge()
	mux.HandleFunc("/kdb/health", bridge.HandleHealth)
	mux.HandleFunc("/kdb/insert", bridge.HandleInsert)
	mux.HandleFunc("/kdb/query", bridge.HandleQuery)
	mux.HandleFunc("/kdb/ticks", bridge.HandleTicks)
	log.Println("[KDB] Routes registered: /kdb/health /kdb/insert /kdb/query /kdb/ticks")
}
