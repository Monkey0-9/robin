package main

import (
	"encoding/json"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// MarketDataCache holds the latest L1/L2 data for symbol tracking
type MarketDataCache struct {
	mu     sync.RWMutex
	prices map[string]float64
}

var globalMarketData = &MarketDataCache{
	prices: make(map[string]float64),
}

func (m *MarketDataCache) UpdatePrice(symbol string, price float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prices[symbol] = price
}

func (m *MarketDataCache) GetPrice(symbol string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.prices[symbol]
}

func (m *MarketDataCache) GetAllPrices() map[string]float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	prices := make(map[string]float64)
	for k, v := range m.prices {
		prices[k] = v
	}
	return prices
}

type CoinbaseFeed struct {
	wsHub *WebSocketHub
}

func NewCoinbaseFeed(hub *WebSocketHub) *CoinbaseFeed {
	return &CoinbaseFeed{wsHub: hub}
}

func (c *CoinbaseFeed) Start() {
	go c.connectAndListen()
}

func (c *CoinbaseFeed) connectAndListen() {
	url := "wss://ws-feed.exchange.coinbase.com"

	for {
		slog.Info("Connecting to Coinbase WebSocket", "url", url)
		conn, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			slog.Error("Coinbase WebSocket connection failed, retrying...", "error", err)
			time.Sleep(5 * time.Second)
			continue
		}

		subscribeMsg := map[string]interface{}{
			"type": "subscribe",
			"product_ids": []string{
				"BTC-USD",
				"ETH-USD",
				"SOL-USD",
			},
			"channels": []string{
				"ticker",
				"level2",
				"matches",
			},
		}

		if err := conn.WriteJSON(subscribeMsg); err != nil {
			slog.Error("Failed to subscribe to Coinbase", "error", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		// L2 Book State
		books := make(map[string]*struct {
			Bids map[float64]float64
			Asks map[float64]float64
		})

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				slog.Error("Coinbase read error, reconnecting", "error", err)
				conn.Close()
				break
			}

			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}

			msgType, _ := msg["type"].(string)
			productID, _ := msg["product_id"].(string)
			normalizedID := strings.Replace(productID, "-", "/", 1)

			if msgType == "ticker" {
				priceStr, _ := msg["price"].(string)
				price, err := strconv.ParseFloat(priceStr, 64)
				if err == nil {
					globalMarketData.UpdatePrice(normalizedID, price)

					// Calculate Indicators
					inds := globalIndicators.AddPrice(normalizedID, price)
					if inds != nil {
						c.wsHub.BroadcastJSON(map[string]interface{}{
							"type": "indicators",
							"data": map[string]interface{}{
								"symbol":    normalizedID,
								"price":     price,
								"sma20":     inds["sma20"],
								"upperBand": inds["upperBand"],
								"lowerBand": inds["lowerBand"],
								"macd":      inds["macd"],
								"rsi":       inds["rsi"],
								"timestamp": time.Now().UnixMilli(),
							},
						})
					}
				}
			} else if msgType == "snapshot" {
				bidsList, _ := msg["bids"].([]interface{})
				asksList, _ := msg["asks"].([]interface{})

				book := &struct {
					Bids map[float64]float64
					Asks map[float64]float64
				}{
					Bids: make(map[float64]float64),
					Asks: make(map[float64]float64),
				}

				for _, b := range bidsList {
					level := b.([]interface{})
					price, _ := strconv.ParseFloat(level[0].(string), 64)
					size, _ := strconv.ParseFloat(level[1].(string), 64)
					book.Bids[price] = size
				}
				for _, a := range asksList {
					level := a.([]interface{})
					price, _ := strconv.ParseFloat(level[0].(string), 64)
					size, _ := strconv.ParseFloat(level[1].(string), 64)
					book.Asks[price] = size
				}
				books[normalizedID] = book
			} else if msgType == "match" {
				priceStr, _ := msg["price"].(string)
				sizeStr, _ := msg["size"].(string)
				makerSide, _ := msg["side"].(string)

				price, _ := strconv.ParseFloat(priceStr, 64)
				size, _ := strconv.ParseFloat(sizeStr, 64)

				// Maker side is the resting side; the taker (aggressor) is the opposite.
				takerSide := "sell"
				if makerSide == "sell" {
					takerSide = "buy"
				}

				tradeID, _ := msg["trade_id"].(float64)
				tradeIDStr := strconv.FormatFloat(tradeID, 'f', 0, 64)

				ingestTrade(c.wsHub, NormalizedTick{
					Symbol:    normalizedID,
					Price:     price,
					Size:      size,
					Side:      takerSide,
					TradeID:   tradeIDStr,
					Venue:     "coinbase",
					Timestamp: time.Now(),
				})
			} else if msgType == "l2update" {
				changes, _ := msg["changes"].([]interface{})
				book, ok := books[normalizedID]
				if !ok {
					continue
				}

				for _, c := range changes {
					change := c.([]interface{})
					side := change[0].(string)
					price, _ := strconv.ParseFloat(change[1].(string), 64)
					size, _ := strconv.ParseFloat(change[2].(string), 64)

					if side == "buy" {
						if size == 0 {
							delete(book.Bids, price)
						} else {
							book.Bids[price] = size
						}
					} else {
						if size == 0 {
							delete(book.Asks, price)
						} else {
							book.Asks[price] = size
						}
					}

					// Log L2 Update to persistence layer
					if globalTickLogger != nil {
						globalTickLogger.LogL2Update(normalizedID, side, price, size, time.Now())
					}
				}

				// Reconstruct top 20 levels for broadcast
				var flatBids [][2]float64
				var flatAsks [][2]float64
				for p, s := range book.Bids {
					flatBids = append(flatBids, [2]float64{p, s})
				}
				for p, s := range book.Asks {
					flatAsks = append(flatAsks, [2]float64{p, s})
				}

				// Sort Bids descending
				sort.Slice(flatBids, func(i, j int) bool {
					return flatBids[i][0] > flatBids[j][0]
				})

				// Sort Asks ascending
				sort.Slice(flatAsks, func(i, j int) bool {
					return flatAsks[i][0] < flatAsks[j][0]
				})

				// Truncate to top 20 levels
				if len(flatBids) > 20 {
					flatBids = flatBids[:20]
				}
				if len(flatAsks) > 20 {
					flatAsks = flatAsks[:20]
				}

				// Publish real best bid/ask into the SOR NBBO cache so routing
				// decisions use live market data (Phase 3.1), not synthetic quotes
				// alone.
				if len(flatBids) > 0 && len(flatAsks) > 0 {
					globalNBBO.Publish(normalizedID, "Coinbase",
						flatBids[0][0], flatAsks[0][0],
						flatBids[0][1], flatAsks[0][1])
				}
				globalMarketData.UpdatePrice(normalizedID, (flatBids[0][0]+flatAsks[0][0])/2)

				c.wsHub.BroadcastOrderBook(normalizedID, flatBids, flatAsks)
			}
		}
	}
}

// ─── Binance WebSocket Feed ──────────────────────────────────────────────────

type BinanceFeed struct {
	wsHub *WebSocketHub
}

func NewBinanceFeed(hub *WebSocketHub) *BinanceFeed {
	return &BinanceFeed{wsHub: hub}
}

func (b *BinanceFeed) Start() {
	go b.connectAndListen()
}

func (b *BinanceFeed) connectAndListen() {
	for {
		conn, _, err := websocket.DefaultDialer.Dial(
			"wss://stream.binance.com:9443/ws/btcusdt@depth20@100ms/ethusdt@depth20@100ms/btcusdt@trade/ethusdt@trade",
			nil,
		)
		if err != nil {
			slog.Error("Binance WebSocket connection failed, retrying...", "error", err)
			time.Sleep(5 * time.Second)
			continue
		}

		slog.Info("Connected to Binance WebSocket")
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				slog.Error("Binance read error, reconnecting", "error", err)
				conn.Close()
				time.Sleep(5 * time.Second)
				break
			}

			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}

			data, ok := msg["data"].(map[string]interface{})
			if !ok {
				continue
			}

			// Map Binance symbol to internal format
			rawSymbol, _ := data["s"].(string)
			var normalizedID string
			switch rawSymbol {
			case "BTCUSDT":
				normalizedID = "BTC/USD"
			case "ETHUSDT":
				normalizedID = "ETH/USD"
			default:
				continue
			}

			// Update price from trade events
			if p, ok := data["p"].(string); ok {
				if price, err := strconv.ParseFloat(p, 64); err == nil {
					globalMarketData.UpdatePrice(normalizedID, price)
				}
			}

			// Ingest trade events (taker side derived from isBuyerMaker)
			if data["e"] == "trade" {
				price, _ := strconv.ParseFloat(data["p"].(string), 64)
				size, _ := strconv.ParseFloat(data["q"].(string), 64)
				takerSide := "buy"
				if isBuyerMaker, _ := data["m"].(bool); isBuyerMaker {
					// Buyer is maker -> taker (aggressor) is the seller
					takerSide = "sell"
				}
				tradeID := ""
				if t, ok := data["t"].(float64); ok {
					tradeID = strconv.FormatFloat(t, 'f', 0, 64)
				}
				tsMillis, _ := data["T"].(float64)
				ingestTrade(b.wsHub, NormalizedTick{
					Symbol:    normalizedID,
					Price:     price,
					Size:      size,
					Side:      takerSide,
					TradeID:   tradeID,
					Venue:     "binance",
					Timestamp: time.UnixMilli(int64(tsMillis)),
				})
			}

			// Broadcast depth snapshot
			if bidsRaw, ok := data["bids"].([]interface{}); ok {
				var flatBids [][2]float64
				for _, b := range bidsRaw {
					level := b.([]interface{})
					price, _ := strconv.ParseFloat(level[0].(string), 64)
					size, _ := strconv.ParseFloat(level[1].(string), 64)
					flatBids = append(flatBids, [2]float64{price, size})
				}
				asksRaw, _ := data["asks"].([]interface{})
				var flatAsks [][2]float64
				for _, a := range asksRaw {
					level := a.([]interface{})
					price, _ := strconv.ParseFloat(level[0].(string), 64)
					size, _ := strconv.ParseFloat(level[1].(string), 64)
					flatAsks = append(flatAsks, [2]float64{price, size})
				}
				if len(flatBids) > 0 && len(flatAsks) > 0 {
					// Publish real best bid/ask into the SOR NBBO cache so routing
					// decisions use live market data (Phase 3.1), not synthetic quotes.
					globalNBBO.Publish(normalizedID, "Binance",
						flatBids[0][0], flatAsks[0][0],
						flatBids[0][1], flatAsks[0][1])
					globalMarketData.UpdatePrice(normalizedID, (flatBids[0][0]+flatAsks[0][0])/2)
					b.wsHub.BroadcastOrderBook(normalizedID, flatBids, flatAsks)
				}
			}
		}
	}
}

// ─── Kraken WebSocket Feed ────────────────────────────────────────────────────

type KrakenFeed struct {
	wsHub *WebSocketHub
}

func NewKrakenFeed(hub *WebSocketHub) *KrakenFeed {
	return &KrakenFeed{wsHub: hub}
}

func (k *KrakenFeed) Start() {
	go k.connectAndListen()
}

func (k *KrakenFeed) connectAndListen() {
	for {
		conn, _, err := websocket.DefaultDialer.Dial("wss://ws.kraken.com", nil)
		if err != nil {
			slog.Error("Kraken WebSocket connection failed, retrying...", "error", err)
			time.Sleep(5 * time.Second)
			continue
		}

		sub := map[string]interface{}{
			"event": "subscribe",
			"pair":  []string{"XBT/USD", "ETH/USD"},
			"subscription": map[string]interface{}{
				"name":  "book",
				"depth": 10,
			},
		}
		if err := conn.WriteJSON(sub); err != nil {
			conn.Close()
			continue
		}

		tradeSub := map[string]interface{}{
			"event": "subscribe",
			"pair":  []string{"XBT/USD", "ETH/USD"},
			"subscription": map[string]interface{}{
				"name": "trade",
			},
		}
		if err := conn.WriteJSON(tradeSub); err != nil {
			conn.Close()
			continue
		}

		slog.Info("Connected to Kraken WebSocket")
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				slog.Error("Kraken read error, reconnecting", "error", err)
				conn.Close()
				time.Sleep(5 * time.Second)
				break
			}

			var msg json.RawMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}

			var arr []interface{}
			if err := json.Unmarshal(msg, &arr); err != nil || len(arr) < 4 {
				continue
			}

			channelName, _ := arr[2].(string)
			if channelName != "book-10" && channelName != "trade" {
				continue
			}

			pair, _ := arr[3].(string)
			var normalizedID string
			switch pair {
			case "XBT/USD":
				normalizedID = "BTC/USD"
			case "ETH/USD":
				normalizedID = "ETH/USD"
			default:
				continue
			}

			data, _ := arr[1].(map[string]interface{})

			// Kraken trade channel: arr[1] is a list of [price, volume, time, side, orderType, misc]
			if channelName == "trade" {
				trades, _ := arr[1].([]interface{})
				for _, t := range trades {
					if trade, ok := t.([]interface{}); ok && len(trade) >= 4 {
						price, _ := strconv.ParseFloat(trade[0].(string), 64)
						size, _ := strconv.ParseFloat(trade[1].(string), 64)
						tsNanos, _ := strconv.ParseFloat(trade[2].(string), 64)
						side, _ := trade[3].(string) // maker side in Kraken

						// Maker side is the resting side; the taker (aggressor) is the opposite.
						takerSide := "sell"
						if side == "sell" {
							takerSide = "buy"
						}

						ingestTrade(k.wsHub, NormalizedTick{
							Symbol:    normalizedID,
							Price:     price,
							Size:      size,
							Side:      takerSide,
							Venue:     "kraken",
							Timestamp: time.UnixMilli(int64(tsNanos * 1000.0)),
						})
					}
				}
				continue
			}

			if data == nil {
				continue
			}

			var flatBids [][2]float64
			var flatAsks [][2]float64

			if bs, ok := data["bs"].([]interface{}); ok {
				for _, b := range bs {
					if level, ok := b.([]interface{}); ok && len(level) >= 2 {
						price, _ := strconv.ParseFloat(level[0].(string), 64)
						size, _ := strconv.ParseFloat(level[1].(string), 64)
						flatBids = append(flatBids, [2]float64{price, size})
					}
				}
			}
			if as, ok := data["as"].([]interface{}); ok {
				for _, a := range as {
					if level, ok := a.([]interface{}); ok && len(level) >= 2 {
						price, _ := strconv.ParseFloat(level[0].(string), 64)
						size, _ := strconv.ParseFloat(level[1].(string), 64)
						flatAsks = append(flatAsks, [2]float64{price, size})
					}
				}
			}

			if len(flatBids) > 0 || len(flatAsks) > 0 {
				k.wsHub.BroadcastOrderBook(normalizedID, flatBids, flatAsks)
				// Update best bid/ask as mid price
				if len(flatBids) > 0 && len(flatAsks) > 0 {
					// Publish real best bid/ask into the SOR NBBO cache so routing
					// decisions use live market data (Phase 3.1), not synthetic quotes.
					globalNBBO.Publish(normalizedID, "Kraken",
						flatBids[0][0], flatAsks[0][0],
						flatBids[0][1], flatAsks[0][1])
					mid := (flatBids[0][0] + flatAsks[0][0]) / 2
					globalMarketData.UpdatePrice(normalizedID, mid)
				}
			}
		}
	}
}
