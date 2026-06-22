import { create } from 'zustand';

interface Asset {
  symbol: string;
  name: string;
  currentPrice: number;
  dailyChangePct: number;
  type: 'crypto' | 'equity' | 'index' | 'fx';
}

interface Position {
  id: string;
  symbol: string;
  side: 'LONG' | 'SHORT';
  size: number;
  entryPrice: number;
  marginRequired: number;
  unrealizedPnL: number;
}

interface Trade {
  id: string;
  symbol: string;
  side: 'BUY' | 'SELL';
  qty: number;
  price: number;
  realizedPnL: number;
  timestamp: Date;
}

interface OrderBookLevel {
  price: number;
  size: number;
  total: number;
}

interface Notification {
  message: string;
  type: 'success' | 'error' | 'info';
}

interface SystemHealth {
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
}

export const useTerminalStore = create<TerminalState>((set, get) => ({
  assets: [
    { symbol: 'BTC/USD', name: 'Bitcoin / US Dollar', currentPrice: 64500.50, dailyChangePct: 2.4, type: 'crypto' },
    { symbol: 'ETH/USD', name: 'Ethereum / US Dollar', currentPrice: 3450.20, dailyChangePct: -1.2, type: 'crypto' },
    { symbol: 'AAPL', name: 'Apple Inc.', currentPrice: 185.30, dailyChangePct: 0.5, type: 'equity' },
    { symbol: 'EUR/USD', name: 'Euro / US Dollar', currentPrice: 1.0850, dailyChangePct: 0.1, type: 'fx' },
  ],
  selectedSymbol: 'BTC/USD',
  notification: null,
  tradeHistory: [],
  positions: [],
  workingOrders: [],
  balance: 100000,
  equity: 100000,
  marginUtilization: 0,

  orderBook: { bids: [], asks: [] },
  systemHealth: { healthy: 0, degraded: 0, failed: 0, latencyNs: 65000 },

  init: () => {
    console.log("Terminal store initialized");
    get().showNotification("Connected to Gateway", "success");
    
    // Simulate live ticking data for selected asset
    setInterval(() => {
      set((state) => {
        const newAssets = state.assets.map(a => {
          const move = a.currentPrice * (1 + (Math.random() - 0.5) * 0.0005);
          return { ...a, currentPrice: move };
        });

        // Update positions PnL
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

        // Simulate order book
        const currentBtcPrice = newAssets.find(a => a.symbol === state.selectedSymbol)?.currentPrice || 64500;
        const bids: OrderBookLevel[] = [];
        const asks: OrderBookLevel[] = [];
        let totalBid = 0;
        let totalAsk = 0;
        for(let i=0; i<8; i++) {
          const bidSize = Number((Math.random() * 2).toFixed(2));
          totalBid += bidSize;
          bids.push({ price: currentBtcPrice - (i+1)*0.5, size: bidSize, total: totalBid });
          
          const askSize = Number((Math.random() * 2).toFixed(2));
          totalAsk += askSize;
          asks.push({ price: currentBtcPrice + (i+1)*0.5, size: askSize, total: totalAsk });
        }

        return { 
          assets: newAssets,
          positions: newPositions,
          equity: newEquity,
          marginUtilization: marginUtil,
          orderBook: { bids, asks }
        };
      });
    }, 500);

    // Poll the Go Gateway
    setInterval(async () => {
      try {
        const statsRes = await fetch('http://localhost:8080/stats');
        const stats = await statsRes.json();
        
        const healthRes = await fetch('http://localhost:8080/health');
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
  },

  submitOrder: (symbol, side, price, size, isMarket) => {
    set((state) => {
      const marginRequired = price * size * 0.05; // 5% margin
      if (marginRequired > state.balance) {
        state.showNotification("Insufficient margin", "error");
        return state;
      }

      // Automatically fill the order and turn into position
      const newPosition: Position = {
        id: Math.random().toString(36).substring(7),
        symbol,
        side: side === 'BUY' ? 'LONG' : 'SHORT',
        size,
        entryPrice: price,
        marginRequired,
        unrealizedPnL: 0
      };

      const newTrade: Trade = {
        id: Math.random().toString(36).substring(7),
        symbol,
        side,
        qty: size,
        price,
        realizedPnL: 0,
        timestamp: new Date()
      };

      state.showNotification(`Order FILLED: ${side} ${size} ${symbol} @ ${price}`, "success");

      return {
        balance: state.balance - marginRequired, // deduct margin
        positions: [...state.positions, newPosition],
        tradeHistory: [newTrade, ...state.tradeHistory]
      };
    });
  },

  dismissNotification: () => set({ notification: null }),
  exportToCSV: () => {
    console.log("Exporting trades to CSV...");
    get().showNotification("Exporting...", "info");
  },
  showNotification: (message, type) => set({ notification: { message, type } }),
  setSelectedSymbol: (symbol) => set({ selectedSymbol: symbol })
}));
