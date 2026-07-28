import { create } from 'zustand';

export interface Asset {
  symbol: string;
  name: string;
  currentPrice: number;
  dailyChangePct: number;
  type: 'crypto' | 'equity' | 'index' | 'fx';
}

export interface Position {
  id: string;
  symbol: string;
  side: 'LONG' | 'SHORT';
  size: number;
  entryPrice: number;
  marginRequired: number;
  unrealizedPnL: number;
}

export interface Trade {
  id: string;
  symbol: string;
  side: 'BUY' | 'SELL';
  qty: number;
  price: number;
  realizedPnL: number;
  timestamp: Date;
}

export interface OrderBookLevel {
  price: number;
  size: number;
  total: number;
}

export interface Notification {
  message: string;
  type: 'success' | 'error' | 'info';
}

export interface SystemHealth {
  healthy: number;
  degraded: number;
  failed: number;
  latencyNs: number;
}

interface TerminalState {
  assets: Asset[];
  selectedSymbol: string;
  notification: Notification | null;
  tradeHistory: Trade[];
  positions: Position[];
  workingOrders: any[];
  balance: number;
  equity: number;
  marginUtilization: number;
  routingMode: string;
  screenerAssets: any[];
  heatmapSectors: any[];
  sorQuotes: any[];
  
  orderBook: {
    bids: OrderBookLevel[];
    asks: OrderBookLevel[];
  };

  systemHealth: SystemHealth;

  init: () => void;
  dismissNotification: () => void;
  exportToCSV: () => void;
  showNotification: (msg: string, type: 'success' | 'error' | 'info') => void;
  submitOrder: (symbol: string, side: 'BUY' | 'SELL', price: number, size: number, isMarket: boolean) => void;
  setSelectedSymbol: (symbol: string) => void;
  setRoutingMode: (mode: string) => void;
  fetchScreenerData: () => Promise<void>;
  fetchHeatmapData: () => Promise<void>;
  fetchSorPrices: (symbol: string) => Promise<void>;
  fetchAlpacaState: () => Promise<void>;
}

const GATEWAY_URL = process.env.NEXT_PUBLIC_GATEWAY_URL || 'http://localhost:8080';
const WS_URL = GATEWAY_URL.replace(/^http/, 'ws') + '/ws';
const JWT_TOKEN = process.env.NEXT_PUBLIC_GATEWAY_API_TOKEN || '';



function createWebSocket(
  onMessage: (data: any) => void,
  onDisconnect: () => void,
): WebSocket {
  const url = `${WS_URL}`;
  const ws = new WebSocket(url, ['token', JWT_TOKEN]);
  ws.onopen = () => console.log('WebSocket connected');
  ws.onmessage = (event) => {
    try {
      const parsed = JSON.parse(event.data);
      onMessage(parsed);
    } catch { /* ignore malformed */ }
  };
  ws.onclose = () => {
    console.log('WebSocket disconnected');
    onDisconnect();
  };
  ws.onerror = () => ws.close();
  return ws;
}

export const useTerminalStore = create<TerminalState>((set, get) => ({
  assets: [],
  selectedSymbol: 'BTC/USD',
  notification: null,
  tradeHistory: [],
  positions: [],
  workingOrders: [],
  balance: 0,
  equity: 0,
  marginUtilization: 0,
  routingMode: 'AUTO',
  screenerAssets: [],
  heatmapSectors: [],
  sorQuotes: [],

  orderBook: { bids: [], asks: [] },
  systemHealth: { healthy: 0, degraded: 0, failed: 0, latencyNs: 65000 },

  init: () => {
    console.log("Terminal store initialized");
    get().showNotification("Connected to Gateway", "success");

    let ws: WebSocket | null = null;
    let wsConnected = false;
    let reconnectAttempts = 0;
    const maxReconnectDelay = 30000;

    function connect() {
      // Always terminate the previous socket before creating a new one.
      // This prevents accumulating stale WebSocket objects in memory on reconnects.
      if (ws) {
        ws.onclose = null; // Prevent recursive reconnect trigger
        ws.onerror = null;
        if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
          ws.close();
        }
        ws = null;
      }

      ws = createWebSocket(
        (data) => {
          wsConnected = true;
          reconnectAttempts = 0;

          if (data.type === 'orderbook') {
            const { symbol, bids, asks } = data.data;
            const orderBookBids: OrderBookLevel[] = bids.slice(0, 8).map(([price, size]: number[], i: number) => ({
              price,
              size,
              total: bids.slice(0, i + 1).reduce((s: number, [_, sz]: number[]) => s + sz, 0),
            }));
            const orderBookAsks: OrderBookLevel[] = asks.slice(0, 8).map(([price, size]: number[], i: number) => ({
              price,
              size,
              total: asks.slice(0, i + 1).reduce((s: number, [_, sz]: number[]) => s + sz, 0),
            }));

            set((state) => {
              const newAssets = state.assets.map(a => {
                if (a.symbol === symbol) {
                  const mid = (bids[0]?.[0] + asks[0]?.[0]) / 2;
                  return { ...a, currentPrice: mid || a.currentPrice };
                }
                return a;
              });

              let totalUnrealized = 0;
              let totalMargin = 0;
              const newPositions = state.positions.map(p => {
                const currentAsset = newAssets.find(a => a.symbol === p.symbol);
                const currentPrice = currentAsset ? currentAsset.currentPrice : p.entryPrice;
                const pnl = p.side === 'LONG'
                  ? (currentPrice - p.entryPrice) * p.size
                  : (p.entryPrice - currentPrice) * p.size;
                totalUnrealized += pnl;
                totalMargin += p.marginRequired;
                return { ...p, unrealizedPnL: pnl };
              });

              const newEquity = state.balance + totalUnrealized;
              const marginUtil = newEquity > 0 ? (totalMargin / newEquity) * 100 : 0;

              return {
                assets: newAssets,
                positions: newPositions,
                equity: newEquity,
                marginUtilization: marginUtil,
                orderBook: { bids: orderBookBids, asks: orderBookAsks },
              };
            });
          } else if (data.type === 'trade') {
            const trade = data.data;
            set((state) => ({
              tradeHistory: [{
                id: trade.id,
                symbol: trade.symbol,
                side: trade.side,
                qty: trade.qty,
                price: trade.price,
                realizedPnL: 0,
                timestamp: new Date(trade.timestamp),
              }, ...state.tradeHistory],
            }));
          }
        },
        () => {
          wsConnected = false;
          reconnectAttempts++;
          const delay = Math.min(1000 * Math.pow(2, reconnectAttempts), maxReconnectDelay);
          console.log(`WebSocket reconnecting in ${delay}ms (attempt ${reconnectAttempts})`);
          setTimeout(connect, delay);
        },
      );
    }

    connect();

    // Price ticking fallback removed to preserve real-time data integrity.

    // Poll the Go Gateway
    setInterval(async () => {
      try {
        const statsRes = await fetch(`${GATEWAY_URL}/stats`, {
          headers: { Authorization: `Bearer ${JWT_TOKEN}` }
        });
        const stats = await statsRes.json();
        
        const healthRes = await fetch(`${GATEWAY_URL}/health`);
        const health = await healthRes.json();

        set({
          systemHealth: {
            healthy: health.healthy,
            degraded: health.degraded,
            failed: health.failed,
            latencyNs: stats.avg_lat_ns || 65000
          }
        });
      } catch (e) {
        set({ systemHealth: { healthy: 0, degraded: 0, failed: 5, latencyNs: 0 } });
      }
    }, 2000);

    // Initial triggers for screener, heatmap, and quotes
    get().fetchScreenerData();
    get().fetchHeatmapData();
    get().fetchSorPrices(get().selectedSymbol);
    get().fetchAlpacaState();

    // Fetch screener data every 5 seconds
    setInterval(() => {
      get().fetchScreenerData();
    }, 5000);

    // Fetch heatmap data every 10 seconds
    setInterval(() => {
      get().fetchHeatmapData();
    }, 10000);

    // Fetch SOR prices every 2 seconds
    setInterval(() => {
      get().fetchSorPrices(get().selectedSymbol);
    }, 2000);
  },

  submitOrder: async (symbol, side, price, size, isMarket) => {
    const state = get();
    // NOTE: Margin validation is intentionally delegated to the backend.
    // The server returns 4xx/5xx with an error message if the order is rejected.
    // Never validate risk constraints in the browser — they are trivially bypassable.
    const clOrdId = `ORD-${Date.now()}-${Math.random().toString(36).substring(2, 6).toUpperCase()}`;

    // Attempt to submit via gateway
    try {
      const res = await fetch(`${GATEWAY_URL}/order`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${JWT_TOKEN}`,
        },
        body: JSON.stringify({
          symbol,
          side,
          price,
          qty: size,
          order_type: isMarket ? 'MARKET' : 'LIMIT',
          cl_ord_id: clOrdId,
          exchange: state.routingMode,
        }),
        signal: AbortSignal.timeout(3000),
      });

      if (res.ok) {
        const fill = await res.json();
        const fillPrice = fill.fill_price ?? price;
        const routedExchange = fill.routed_exchange || 'Robin Pools';
        const priceImprovement = fill.price_improvement_bps || 0.0;

        set((s) => {
          const newPosition = {
            id: fill.exec_id ?? clOrdId,
            symbol,
            side: (side === 'BUY' ? 'LONG' : 'SHORT') as 'LONG' | 'SHORT',
            size,
            entryPrice: fillPrice,
            marginRequired: 0, // Populated from backend response on next Alpaca state refresh
            unrealizedPnL: 0,
            routedExchange,
          };
          const newTrade = {
            id: fill.exec_id ?? clOrdId,
            symbol,
            side: side as 'BUY' | 'SELL',
            qty: size,
            price: fillPrice,
            realizedPnL: 0,
            timestamp: new Date(),
            routedExchange,
            priceImprovement,
          };
          s.showNotification(
            `Order FILLED via ${routedExchange}: ${side} ${size} ${symbol} @ $${fillPrice.toFixed(2)} (+${priceImprovement.toFixed(1)}bps saved)`,
            'success'
          );
          return {
            // Balance and positions are refreshed from Alpaca on next poll cycle.
            // Optimistically append this fill to trade history for immediate UX feedback.
            positions: [...s.positions, newPosition],
            tradeHistory: [newTrade, ...s.tradeHistory],
          };
        });
        return;
      }
    } catch (e) {
      state.showNotification('Order Execution Failed: Gateway Offline / Server Unreachable', 'error');
    }
  },

  dismissNotification: () => set({ notification: null }),

  exportToCSV: () => {
    const { tradeHistory } = get();
    if (tradeHistory.length === 0) {
      get().showNotification('No trades to export', 'info');
      return;
    }
    const header = 'ID,Symbol,Side,Qty,Price,RealizedPnL,Timestamp,RoutedExchange,PriceImprovementBps\n';
    const rows = tradeHistory
      .map((t: any) =>
        [t.id, t.symbol, t.side, t.qty, t.price, t.realizedPnL, t.timestamp.toISOString(), t.routedExchange || '', t.priceImprovement || 0].join(',')
      )
      .join('\n');
    const blob = new Blob([header + rows], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.setAttribute('href', url);
    link.setAttribute('download', `robin_trades_${new Date().toISOString().slice(0, 10)}.csv`);
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
    get().showNotification(`Exported ${tradeHistory.length} trades to CSV`, 'success');
  },

  showNotification: (message, type) => set({ notification: { message, type } }),
  setSelectedSymbol: (symbol) => {
    set({ selectedSymbol: symbol });
    get().fetchSorPrices(symbol);
  },

  setRoutingMode: (mode) => set({ routingMode: mode }),

  fetchScreenerData: async () => {
    try {
      const res = await fetch(`${GATEWAY_URL}/api/screener`);
      if (res.ok) {
        const data = await res.json();
        set({ screenerAssets: data });
      }
    } catch (e) {
      console.error("Failed to fetch screener data", e);
    }
  },

  fetchHeatmapData: async () => {
    try {
      const res = await fetch(`${GATEWAY_URL}/api/heatmap`);
      if (res.ok) {
        const data = await res.json();
        set({ heatmapSectors: data });
      }
    } catch (e) {
      console.error("Failed to fetch heatmap data", e);
    }
  },

  fetchSorPrices: async (symbol) => {
    try {
      const res = await fetch(`${GATEWAY_URL}/api/sor/prices?symbol=${encodeURIComponent(symbol)}`);
      if (res.ok) {
        const data = await res.json();
        set({ sorQuotes: data });
      }
    } catch (e) {
      console.error("Failed to fetch SOR prices", e);
    }
  },

  fetchAlpacaState: async () => {
    try {
      const headers = { Authorization: `Bearer ${JWT_TOKEN}` };

      // 1. Fetch Account (balance, equity)
      const accRes = await fetch(`${GATEWAY_URL}/api/alpaca/account`, { headers });
      if (accRes.ok) {
        const acc = await accRes.json();
        set({
          balance: parseFloat(acc.cash || "0"),
          equity: parseFloat(acc.portfolio_value || "0"),
          marginUtilization: parseFloat(acc.initial_margin || "0") / (parseFloat(acc.portfolio_value || "1")) * 100
        });
      }

      // 2. Fetch Positions
      const posRes = await fetch(`${GATEWAY_URL}/api/alpaca/positions`, { headers });
      if (posRes.ok) {
        const posData = await posRes.json();
        const positions: Position[] = posData.map((p: any) => ({
          id: p.asset_id,
          symbol: p.symbol,
          side: p.side === 'long' ? 'LONG' : 'SHORT',
          size: Math.abs(parseFloat(p.qty)),
          entryPrice: parseFloat(p.avg_entry_price),
          marginRequired: 0,
          unrealizedPnL: parseFloat(p.unrealized_pl),
          routedExchange: p.exchange
        }));
        set({ positions });
      }

      // 3. Fetch Orders / Trade History
      const ordRes = await fetch(`${GATEWAY_URL}/api/alpaca/orders`, { headers });
      if (ordRes.ok) {
        const ordData = await ordRes.json();
        const trades = ordData.filter((o: any) => o.status === 'filled').map((o: any) => ({
          id: o.id,
          symbol: o.symbol,
          side: o.side.toUpperCase(),
          qty: parseFloat(o.filled_qty),
          price: parseFloat(o.filled_avg_price),
          realizedPnL: 0,
          timestamp: new Date(o.filled_at || o.updated_at),
          routedExchange: o.exchange
        }));
        set({ tradeHistory: trades });
      }

      // 4. Fetch Assets
      const assetsRes = await fetch(`${GATEWAY_URL}/api/alpaca/assets`, { headers });
      if (assetsRes.ok) {
        const assetsData = await assetsRes.json();
        const activeAssets = assetsData.filter((a: any) => a.tradable && a.fractionable).slice(0, 100);
        const assets: Asset[] = activeAssets.map((a: any) => ({
          symbol: a.symbol,
          name: a.name,
          currentPrice: 0,
          dailyChangePct: 0,
          type: a.class === 'crypto' ? 'crypto' : 'equity'
        }));
        if (assets.length > 0) {
          set({ assets });
          // Optionally set first asset as selected if none is selected
          const currentSymbol = get().selectedSymbol;
          if (!currentSymbol || currentSymbol === 'BTC/USD') {
             set({ selectedSymbol: assets[0].symbol });
          }
        }
      }

    } catch (e) {
      console.error("Failed to fetch Alpaca state", e);
    }
  },
}));

