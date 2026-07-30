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
		books := make(map[string]*struct{
			Bids map[float64]float64
			Asks map[float64]float64
		})
		
		// Volume Analytics State
		type VolumeStats struct {
			TotalVolume   float64
			TotalValue    float64 // price * size
			CVD           float64
		}
		volStats := make(map[string]*VolumeStats)

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
				
				book := &struct{
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
				side, _ := msg["side"].(string) // maker side
				
				price, _ := strconv.ParseFloat(priceStr, 64)
				size, _ := strconv.ParseFloat(sizeStr, 64)
				
				vs, ok := volStats[normalizedID]
				if !ok {
					vs = &VolumeStats{}
					volStats[normalizedID] = vs
				}
				
				vs.TotalVolume += size
				vs.TotalValue += (price * size)
				
				// Maker side is sell -> Taker was BUY -> Positive CVD
				if side == "sell" {
					vs.CVD += size
				} else if side == "buy" {
					vs.CVD -= size
				}
				
				vwap := vs.TotalValue / vs.TotalVolume
				
				// Log trade to persistence layer
				if globalTickLogger != nil {
					tradeID, _ := msg["trade_id"].(float64)
					tradeIDStr := strconv.FormatFloat(tradeID, 'f', 0, 64)
					globalTickLogger.LogTrade(normalizedID, tradeIDStr, side, price, size, time.Now())
				}
				
				// Broadcast volume stats
				c.wsHub.BroadcastJSON(map[string]interface{}{
					"type": "volume_stats",
					"data": map[string]interface{}{
						"symbol": normalizedID,
						"volume": vs.TotalVolume,
						"vwap":   vwap,
						"cvd":    vs.CVD,
					},
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
				
				c.wsHub.BroadcastOrderBook(normalizedID, flatBids, flatAsks)
			}
		}
	}
}
