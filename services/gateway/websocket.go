package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		// In production, configure explicit allowed origins
		return origin == "http://localhost:3000" || origin == "http://localhost:3001"
	},
}

type wsClient struct {
	conn   *websocket.Conn
	send   chan []byte
	userID string
}

func (c *wsClient) writePump() {
	defer func() {
		c.conn.Close()
	}()
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			slog.Warn("WebSocket write error", "user", c.userID, "error", err)
			return
		}
	}
}

// WebSocketHub manages a set of connected WebSocket clients and supports
// broadcasting order-book updates, trade notifications, and other real-time events.
type WebSocketHub struct {
	clients sync.Map // conn -> *wsClient
}

func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{}
}

// handleWebSocket upgrades an HTTP connection to WebSocket after JWT validation.
// The token is extracted from the Authorization header, Sec-WebSocket-Protocol, or cookie.
func (hub *WebSocketHub) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	tokenStr := ""
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		tokenStr = strings.TrimPrefix(auth, "Bearer ")
	} else if protocols := r.Header.Get("Sec-WebSocket-Protocol"); protocols != "" {
		// Example: Sec-WebSocket-Protocol: token, <actual-jwt>
		parts := strings.Split(protocols, ",")
		for i, p := range parts {
			if strings.TrimSpace(p) == "token" && i+1 < len(parts) {
				tokenStr = strings.TrimSpace(parts[i+1])
				break
			}
		}
	} else if cookie, err := r.Cookie("jwt_token"); err == nil {
		tokenStr = cookie.Value
	}

	if tokenStr == "" {
		http.Error(w, `{"error":"missing token"}`, http.StatusUnauthorized)
		return
	}

	claims, err := jwtAuth.verify(tokenStr)
	if err != nil {
		slog.Warn("WebSocket JWT verification failed", "error", err)
		http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
		return
	}

	role, _ := claims["role"].(string)
	if role != "trader" {
		http.Error(w, `{"error":"forbidden: insufficient permissions"}`, http.StatusForbidden)
		return
	}

	userID := "unknown"
	if sub, ok := claims["sub"].(string); ok {
		userID = sub
	}

	header := make(http.Header)
	if r.Header.Get("Sec-WebSocket-Protocol") != "" {
		header.Set("Sec-WebSocket-Protocol", "token")
	}

	conn, err := upgrader.Upgrade(w, r, header)
	if err != nil {
		slog.Error("WebSocket upgrade failed", "error", err)
		return
	}

	client := &wsClient{
		conn:   conn,
		send:   make(chan []byte, 256),
		userID: userID,
	}

	hub.clients.Store(conn, client)
	go client.writePump()

	count := 0
	hub.clients.Range(func(_, _ interface{}) bool {
		count++
		return true
	})

	slog.Info("WebSocket client connected", "user", userID, "total_clients", count)

	go hub.readPump(conn, userID)
}

// readPump reads messages from the WebSocket connection until it is closed.
func (hub *WebSocketHub) readPump(conn *websocket.Conn, userID string) {
	defer func() {
		if val, ok := hub.clients.LoadAndDelete(conn); ok {
			client := val.(*wsClient)
			close(client.send)
		}
		count := 0
		hub.clients.Range(func(_, _ interface{}) bool {
			count++
			return true
		})
		slog.Info("WebSocket client disconnected", "user", userID, "total_clients", count)
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Warn("WebSocket read error", "user", userID, "error", err)
			}
			break
		}
		slog.Debug("WebSocket message received", "user", userID, "message", string(message))
		// Future: route subscription requests, pings, etc.
	}
}

// Types for broadcast payloads

type OrderBookUpdate struct {
	Type string           `json:"type"`
	Data OrderBookPayload `json:"data"`
}

type OrderBookPayload struct {
	Symbol string       `json:"symbol"`
	Bids   [][2]float64 `json:"bids"` // [price, size]
	Asks   [][2]float64 `json:"asks"` // [price, size]
}

type TradeNotification struct {
	Type string       `json:"type"`
	Data TradePayload `json:"data"`
}

type TradePayload struct {
	ID        string  `json:"id"`
	Symbol    string  `json:"symbol"`
	Side      string  `json:"side"`
	Qty       float64 `json:"qty"`
	Price     float64 `json:"price"`
	Timestamp int64   `json:"timestamp"`
}

// BroadcastOrderBook sends an order-book snapshot to all connected clients.
func (hub *WebSocketHub) BroadcastOrderBook(symbol string, bids, asks [][2]float64) {
	payload := OrderBookUpdate{
		Type: "orderbook",
		Data: OrderBookPayload{
			Symbol: symbol,
			Bids:   bids,
			Asks:   asks,
		},
	}
	hub.broadcast(payload)
}

// BroadcastTrade sends a trade notification to all connected clients.
func (hub *WebSocketHub) BroadcastTrade(trade TradePayload) {
	payload := TradeNotification{
		Type: "trade",
		Data: trade,
	}
	hub.broadcast(payload)
}

func (hub *WebSocketHub) broadcast(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("WebSocket broadcast marshal error", "error", err)
		return
	}

	hub.clients.Range(func(key, value interface{}) bool {
		client := value.(*wsClient)
		select {
		case client.send <- data:
		default:
			slog.Warn("WebSocket client buffer full, dropping connection", "user", client.userID)
			if val, ok := hub.clients.LoadAndDelete(key); ok {
				c := val.(*wsClient)
				close(c.send)
			}
		}
		return true
	})
}
