import { create } from 'zustand';
import { useAuthStore } from './useAuthStore';

export interface Asset {
  symbol: string;
  name: string;
  currentPrice: number;
  dailyChangePct: number;
  type: 'crypto' | 'equity' | 'index' | 'fx';
}

export interface VolumeStats {
  symbol: string;
  volume: number;
  vwap: number;
  cvd: number;
}

export interface TechnicalIndicators {
  symbol: string;
  price: number;
  sma20: number;
  upperBand: number;
  lowerBand: number;
  macd: number;
  rsi: number;
  timestamp: number;
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

export interface WorkingOrder {
  id: string;
  symbol: string;
  side: 'BUY' | 'SELL';
  orderType: 'LIMIT' | 'MARKET' | 'STOP';
  qty: number;
  price: number;
  status: 'PENDING' | 'WORKING' | 'FILLED' | 'CANCELED';
  timestamp: Date;
  routedExchange: string;
}

interface TerminalState {
  assets: Asset[];
  selectedSymbol: string;
  notification: Notification | null;
  tradeHistory: Trade[];
  positions: Position[];
  workingOrders: WorkingOrder[];
  balance: number;
  equity: number;
  marginUtilization: number;
  routingMode: string;
  screenerAssets: any[];
  heatmapSectors: any[];
  sorQuotes: any[];
  portfolioWeights: { symbol: string; targetWeight: number }[];
  volumeStats: Record<string, VolumeStats>;
  indicators: Record<string, TechnicalIndicators>;
  
  orderBook: {
    bids: OrderBookLevel[];
    asks: OrderBookLevel[];
  };

  systemHealth: SystemHealth;

  init: () => void;
  dismissNotification: () => void;
  exportToCSV: () => void;
  showNotification: (msg: string, type: 'success' | 'error' | 'info') => void;
  submitOrder: (symbol: string, side: 'BUY' | 'SELL', price: number, size: number, isMarket?: boolean, orderType?: 'MARKET' | 'LIMIT' | 'STOP') => Promise<void>;
  cancelOrder: (id: string) => Promise<void>;
  setSelectedSymbol: (symbol: string) => void;
  setRoutingMode: (mode: string) => void;
  fetchScreenerData: () => Promise<void>;
  fetchHeatmapData: () => Promise<void>;
  fetchSorPrices: (symbol: string) => Promise<void>;
  fetchAlpacaState: () => Promise<void>;
  fetchPortfolioWeights: () => Promise<void>;
}

const GATEWAY_URL = process.env.NEXT_PUBLIC_GATEWAY_URL || 'http://localhost:8080';
const WS_URL = GATEWAY_URL.replace(/^http/, 'ws') + '/ws';

/** Helper: get the current in-memory JWT from the auth store. */
const getToken = () => useAuthStore.getState().getToken() || '';


function createWebSocket(
  onMessage: (data: any) => void,
  onDisconnect: () => void,
): WebSocket {
  const url = `${WS_URL}`;
  const token = getToken();
  const ws = new WebSocket(url, token ? ['token', token] : []);
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

  portfolioWeights: [],
  volumeStats: {},
  indicators: {},

  orderBook: { bids: [], asks: [] },
  systemHealth: { healthy: 4, degraded: 0, failed: 0, latencyNs: 65000 },

  init: () => {
    console.log('Terminal store initialized');
    get().showNotification('Connected to Gateway', 'success');

    // Seed assets immediately from /api/assets (no auth required)
    fetch(`${GATEWAY_URL}/api/assets`)
      .then(r => r.ok ? r.json() : [])
      .then((data: any[]) => {
        if (Array.isArray(data) && data.length > 0) {
          const assets = data.map((a: any) => ({
            symbol: a.symbol,
            name: a.name,
            currentPrice: a.base_price || 0,
            dailyChangePct: 0,
            type: (a.type === 'crypto' ? 'crypto' : a.type === 'fx' ? 'fx' : 'equity') as 'crypto' | 'equity' | 'index' | 'fx',
          }));
          set({ assets, selectedSymbol: assets[0].symbol });
        }
      })
      .catch(e => console.warn('Failed to seed assets from /api/assets:', e));

    let ws: WebSocket | null = null;
    let wsConnected = false;
    let reconnectAttempts = 0;
    const maxReconnectDelay = 30000;

    function connect() {
      if (ws) {
        ws.onclose = null;
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
            const orderBookBids: OrderBookLevel[] = bids.slice(0, 20).map(([price, size]: number[], i: number) => ({
              price,
              size,
              total: bids.slice(0, i + 1).reduce((s: number, [_, sz]: number[]) => s + sz, 0),
            }));
            const orderBookAsks: OrderBookLevel[] = asks.slice(0, 20).map(([price, size]: number[], i: number) => ({
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
          } else if (data.type === 'order_update') {
            const update = data.data;
            set((state) => {
              const currentOrders = [...state.workingOrders];
              const idx = currentOrders.findIndex(o => o.id === update.cl_ord_id);
              if (idx !== -1) {
                if (update.status === 'FILLED' || update.status === 'CANCELED' || update.status === 'REJECTED') {
                  // Remove from working orders
                  currentOrders.splice(idx, 1);
                } else {
                  // Update status
                  currentOrders[idx] = { ...currentOrders[idx], status: update.status };
                }
              }
              return { workingOrders: currentOrders };
            });
          } else if (data.type === 'volume_stats') {
            const stats = data.data;
            set((state) => ({
              volumeStats: {
                ...state.volumeStats,
                [stats.symbol]: {
                  symbol: stats.symbol,
                  volume: stats.volume,
                  vwap: stats.vwap,
                  cvd: stats.cvd,
                }
              }
            }));
          } else if (data.type === 'indicators') {
            const inds = data.data;
            set((state) => ({
              indicators: {
                ...state.indicators,
                [inds.symbol]: inds
              }
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

    // Poll the Go Gateway
    setInterval(async () => {
      try {
        const statsRes = await fetch(`${GATEWAY_URL}/stats`, {
          headers: { Authorization: `Bearer ${getToken()}` }
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
      } catch {
        set({ systemHealth: { healthy: 0, degraded: 0, failed: 5, latencyNs: 0 } });
      }
    }, 2000);

    // Initial triggers for screener, heatmap, quotes, alpaca, portfolio
    get().fetchScreenerData();
    get().fetchHeatmapData();
    get().fetchSorPrices(get().selectedSymbol);
    get().fetchAlpacaState();
    get().fetchPortfolioWeights();

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

  submitOrder: async (symbol, side, price, size, isMarket = true, orderType = 'MARKET') => {
    const state = get();
    const clOrdId = `ORD-${Date.now()}-${Math.random().toString(36).substring(2, 6).toUpperCase()}`;

    // Record in workingOrders state for all orders initially
    const workingOrd: WorkingOrder = {
      id: clOrdId,
      symbol,
      side,
      orderType,
      qty: size,
      price,
      status: 'WORKING',
      timestamp: new Date(),
      routedExchange: state.routingMode === 'AUTO' ? 'Robin Pools' : state.routingMode,
    };
    
    set((s) => ({
      workingOrders: [workingOrd, ...s.workingOrders],
    }));
    
    state.showNotification(`Order submitted: ${side} ${size} ${symbol} @ $${price.toFixed(2)}`, 'info');

    // Attempt to submit via gateway
    try {
      const res = await fetch(`${GATEWAY_URL}/order`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${getToken()}`,
        },
        body: JSON.stringify({
          symbol,
          side,
          price,
          qty: size,
          order_type: orderType,
          cl_ord_id: clOrdId,
          exchange: state.routingMode,
        }),
        signal: AbortSignal.timeout(3000),
      });

      if (!res.ok) {
        throw new Error("Order rejected by gateway");
      }
    } catch (e) {
      set((s) => ({
        workingOrders: s.workingOrders.filter(w => w.id !== clOrdId),
      }));
      state.showNotification('Order Execution Failed: Gateway Offline / Server Unreachable', 'error');
    }
  },

  cancelOrder: async (id: string) => {
    set((s) => ({
      workingOrders: s.workingOrders.filter(w => w.id !== id),
    }));
    get().showNotification(`Working order ${id} canceled`, 'info');
  },

  fetchPortfolioWeights: async () => {
    try {
      const res = await fetch(`${GATEWAY_URL}/api/portfolio/weights`, {
        headers: { Authorization: `Bearer ${getToken()}` }
      });
      if (res.ok) {
        const weights = await res.json();
        set({ portfolioWeights: weights });
      }
    } catch (e) {
      console.error('Failed to fetch portfolio weights', e);
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
      const headers = { Authorization: `Bearer ${getToken()}` };

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

