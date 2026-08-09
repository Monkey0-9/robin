'use client';
import React, { useState, useMemo } from 'react';
import { Layers, Activity, TrendingUp, TrendingDown, Target, Zap } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';

// ─── Black-Scholes (same as OptionsChain — self-contained) ─────────────────
function normalCDF(x: number): number {
  const p = 0.3275911;
  const a = [0.254829592, -0.284496736, 1.421413741, -1.453152027, 1.061405429];
  const sign = x < 0 ? -1 : 1;
  const t = 1 / (1 + p * Math.abs(x) / Math.SQRT2);
  let y = 1;
  for (let i = a.length - 1; i >= 0; i--) y = a[i] + t * y;
  y *= t * Math.exp(-(x * x) / 2);
  return 0.5 * (1 + sign * (1 - y));
}
function bsPrice(S: number, K: number, T: number, r: number, sigma: number, isCall: boolean): number {
  if (T <= 0) return Math.max(0, isCall ? S - K : K - S);
  const d1 = (Math.log(S / K) + (r + 0.5 * sigma * sigma) * T) / (sigma * Math.sqrt(T));
  const d2 = d1 - sigma * Math.sqrt(T);
  const disc = K * Math.exp(-r * T);
  return isCall
    ? S * normalCDF(d1) - disc * normalCDF(d2)
    : disc * normalCDF(-d2) - S * normalCDF(-d1);
}


const RFR = 0.0525;


function getBaseIV(symbol: string): number {
  const m: Record<string, number> = {
    'BTC/USD': 0.72, 'ETH/USD': 0.85, 'SOL/USD': 1.10,
    'AAPL': 0.28, 'TSLA': 0.65, 'MSFT': 0.25, 'NVDA': 0.55,
    'GOOGL': 0.30, 'AMZN': 0.35, 'SPY': 0.18, 'QQQ': 0.22,
    'EUR/USD': 0.08,
  };
  return m[symbol] || 0.35;
}

// ─── Strategy Definitions ───────────────────────────────────────────────────
type StrategyType =
  | 'covered_call' | 'protective_put' | 'bull_call_spread'
  | 'bear_put_spread' | 'long_straddle' | 'short_straddle'
  | 'iron_condor' | 'butterfly' | 'collar' | 'cash_secured_put';

interface StrategyDef {
  name: string;
  description: string;
  marketView: string;
  maxProfit: string;
  maxLoss: string;
  breakeven: string;
  icon: React.ReactNode;
  color: string;
  legs: string;
}

const STRATEGIES: Record<StrategyType, StrategyDef> = {
  covered_call: {
    name: 'Covered Call',
    description: 'Own 100 shares + sell 1 OTM call. Generates income, caps upside.',
    marketView: 'Neutral to Slightly Bullish',
    maxProfit: 'Strike − Cost Basis + Premium',
    maxLoss: 'Stock price → $0 minus premium received',
    breakeven: 'Stock Cost Basis − Call Premium',
    icon: <TrendingUp size={14} />, color: 'accent-green',
    legs: 'Long 100 shares + Short 1 Call',
  },
  protective_put: {
    name: 'Protective Put',
    description: 'Own 100 shares + buy 1 ATM put. Insurance against downside.',
    marketView: 'Bullish with Downside Protection',
    maxProfit: 'Unlimited (capped by put cost)',
    maxLoss: 'Stock Price − Strike + Put Premium',
    breakeven: 'Stock Cost Basis + Put Premium',
    icon: <Layers size={14} />, color: 'accent-blue',
    legs: 'Long 100 shares + Long 1 Put',
  },
  bull_call_spread: {
    name: 'Bull Call Spread',
    description: 'Buy lower call, sell higher call. Defined risk bullish play.',
    marketView: 'Moderately Bullish',
    maxProfit: 'Spread Width − Net Debit',
    maxLoss: 'Net Debit Paid',
    breakeven: 'Lower Strike + Net Debit',
    icon: <TrendingUp size={14} />, color: 'accent-green',
    legs: 'Long 1 Call (lower K) + Short 1 Call (higher K)',
  },
  bear_put_spread: {
    name: 'Bear Put Spread',
    description: 'Buy higher put, sell lower put. Defined risk bearish play.',
    marketView: 'Moderately Bearish',
    maxProfit: 'Spread Width − Net Debit',
    maxLoss: 'Net Debit Paid',
    breakeven: 'Higher Strike − Net Debit',
    icon: <TrendingDown size={14} />, color: 'accent-red',
    legs: 'Long 1 Put (higher K) + Short 1 Put (lower K)',
  },
  long_straddle: {
    name: 'Long Straddle',
    description: 'Buy ATM call + put. Profits from large moves in either direction.',
    marketView: 'High Volatility Expected',
    maxProfit: 'Unlimited (both directions)',
    maxLoss: 'Total Premium Paid',
    breakeven: 'Strike ± Total Premium',
    icon: <Activity size={14} />, color: 'accent-purple',
    legs: 'Long 1 ATM Call + Long 1 ATM Put',
  },
  short_straddle: {
    name: 'Short Straddle',
    description: 'Sell ATM call + put. Profits from low volatility / time decay.',
    marketView: 'Low Volatility Expected',
    maxProfit: 'Total Premium Received',
    maxLoss: 'Theoretically Unlimited',
    breakeven: 'Strike ± Total Premium',
    icon: <Target size={14} />, color: 'accent-amber',
    legs: 'Short 1 ATM Call + Short 1 ATM Put',
  },
  iron_condor: {
    name: 'Iron Condor',
    description: 'Sell OTM strangle + buy further OTM wings. Range-bound income.',
    marketView: 'Low Volatility / Sideways',
    maxProfit: 'Net Premium Received',
    maxLoss: 'Wing Width − Net Premium',
    breakeven: 'Inner Strikes ± Net Credit',
    icon: <Target size={14} />, color: 'accent-amber',
    legs: 'Short Call Spread + Short Put Spread',
  },
  butterfly: {
    name: 'Long Butterfly',
    description: 'Buy 2 wings, sell 2 ATM calls. Profits from low volatility at strike.',
    marketView: 'Stock Pinned at ATM Strike',
    maxProfit: 'Wing Width − Net Debit',
    maxLoss: 'Net Debit Paid',
    breakeven: 'Lower Wing + Debit, Upper Wing − Debit',
    icon: <Activity size={14} />, color: 'accent-purple',
    legs: 'Long 1 Low Call + Short 2 ATM Calls + Long 1 High Call',
  },
  collar: {
    name: 'Collar',
    description: 'Own stock + sell call + buy put. Fully hedged long equity position.',
    marketView: 'Neutral — Capital Protection',
    maxProfit: 'Call Strike − Stock Price + Net Credit',
    maxLoss: 'Stock Price − Put Strike + Net Debit',
    breakeven: 'Stock Price + Net Premium Paid',
    icon: <Layers size={14} />, color: 'accent-blue',
    legs: 'Long 100 shares + Short OTM Call + Long OTM Put',
  },
  cash_secured_put: {
    name: 'Cash-Secured Put',
    description: 'Sell ATM/OTM put backed by cash. Earn income or acquire stock cheaper.',
    marketView: 'Neutral to Bullish',
    maxProfit: 'Premium Received',
    maxLoss: 'Strike − Premium (stock → $0)',
    breakeven: 'Strike − Premium',
    icon: <TrendingUp size={14} />, color: 'accent-green',
    legs: 'Short 1 Put + Cash Reserve = Strike × 100',
  },
};

// ─── P&L Payoff Calculator ──────────────────────────────────────────────────
function computePayoff(strategy: StrategyType, S: number, spotRange: number[], iv: number, dte: number): number[] {
  const T = dte / 365;
  const atm = S;
  const otm1 = S * 1.05;
  const otm2 = S * 1.10;
  const itm1 = S * 0.95;
  const itm2 = S * 0.90;

  return spotRange.map(spot => {
    switch (strategy) {
      case 'covered_call': {
        const callPrem = bsPrice(atm, otm1, T, RFR, iv, true);
        return (spot - atm) + callPrem - Math.max(0, spot - otm1);
      }
      case 'protective_put': {
        const putPrem = bsPrice(atm, itm1, T, RFR, iv, false);
        return (spot - atm) - putPrem + Math.max(0, itm1 - spot);
      }
      case 'bull_call_spread': {
        const longCall = bsPrice(atm, atm, T, RFR, iv, true);
        const shortCall = bsPrice(atm, otm1, T, RFR, iv, true);
        const debit = longCall - shortCall;
        return Math.max(0, spot - atm) - Math.max(0, spot - otm1) - debit;
      }
      case 'bear_put_spread': {
        const longPut = bsPrice(atm, atm, T, RFR, iv, false);
        const shortPut = bsPrice(atm, itm1, T, RFR, iv, false);
        const debit = longPut - shortPut;
        return Math.max(0, atm - spot) - Math.max(0, itm1 - spot) - debit;
      }
      case 'long_straddle': {
        const c = bsPrice(atm, atm, T, RFR, iv, true);
        const p = bsPrice(atm, atm, T, RFR, iv, false);
        return Math.max(0, spot - atm) + Math.max(0, atm - spot) - c - p;
      }
      case 'short_straddle': {
        const c = bsPrice(atm, atm, T, RFR, iv, true);
        const p = bsPrice(atm, atm, T, RFR, iv, false);
        return c + p - Math.max(0, spot - atm) - Math.max(0, atm - spot);
      }
      case 'iron_condor': {
        const shortCall = bsPrice(atm, otm1, T, RFR, iv, true);
        const longCall = bsPrice(atm, otm2, T, RFR, iv, true);
        const shortPut = bsPrice(atm, itm1, T, RFR, iv, false);
        const longPut = bsPrice(atm, itm2, T, RFR, iv, false);
        const callPnl = (shortCall - longCall) - (Math.max(0, spot - otm1) - Math.max(0, spot - otm2));
        const putPnl = (shortPut - longPut) - (Math.max(0, itm1 - spot) - Math.max(0, itm2 - spot));
        return callPnl + putPnl;
      }
      case 'butterfly': {
        const lc1 = bsPrice(atm, itm1, T, RFR, iv, true);
        const sc = bsPrice(atm, atm, T, RFR, iv, true);
        const lc2 = bsPrice(atm, otm1, T, RFR, iv, true);
        const cost = lc1 + lc2 - 2 * sc;
        return Math.max(0, spot - itm1) - 2 * Math.max(0, spot - atm) + Math.max(0, spot - otm1) - cost;
      }
      case 'collar': {
        const callPrem = bsPrice(atm, otm1, T, RFR, iv, true);
        const putPrem = bsPrice(atm, itm1, T, RFR, iv, false);
        const net = callPrem - putPrem;
        return (spot - atm) + net - Math.max(0, spot - otm1) + Math.max(0, itm1 - spot);
      }
      case 'cash_secured_put': {
        const p = bsPrice(atm, atm, T, RFR, iv, false);
        return p - Math.max(0, atm - spot);
      }
      default:
        return 0;
    }
  });
}

const STRATEGY_LIST = Object.keys(STRATEGIES) as StrategyType[];

// Mini SVG payoff chart
function PayoffChart({ payoffs, spotRange, spot }: { payoffs: number[]; spotRange: number[]; spot: number }) {
  const min = Math.min(...payoffs);
  const max = Math.max(...payoffs);
  const range = max - min || 1;
  const w = 180, h = 60;

  const toX = (i: number) => (i / (payoffs.length - 1)) * w;
  const toY = (v: number) => h - ((v - min) / range) * h;

  const points = payoffs.map((v, i) => `${toX(i)},${toY(v)}`).join(' ');
  const zeroY = toY(0);
  const spotX = toX(spotRange.findIndex(s => s >= spot));

  return (
    <svg width={w} height={h} className="w-full" viewBox={`0 0 ${w} ${h}`}>
      <defs>
        <linearGradient id="pnlGrad" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="#10b981" stopOpacity="0.3" />
          <stop offset="100%" stopColor="#ef4444" stopOpacity="0.2" />
        </linearGradient>
      </defs>
      <line x1={0} y1={zeroY} x2={w} y2={zeroY} stroke="#26262c" strokeWidth="1" strokeDasharray="3,3" />
      <polygon
        points={`${toX(0)},${h} ${points} ${toX(payoffs.length - 1)},${h}`}
        fill="url(#pnlGrad)"
      />
      <polyline points={points} fill="none" stroke="#3b82f6" strokeWidth="1.5" />
      <line x1={spotX} y1={0} x2={spotX} y2={h} stroke="#f59e0b" strokeWidth="1" strokeDasharray="2,2" />
    </svg>
  );
}

export default function StrategyRoller() {
  const { selectedSymbol, assets } = useTerminalStore();
  const asset = assets.find(a => a.symbol === selectedSymbol);
  const spot = asset?.currentPrice || 0;

  const [selectedStrategy, setSelectedStrategy] = useState<StrategyType>('covered_call');
  const [dte, setDte] = useState(30);
  const [contracts, setContracts] = useState(1);

  const iv = getBaseIV(selectedSymbol);
  const def = STRATEGIES[selectedStrategy];

  const spotRange = useMemo(() => {
    if (spot <= 0) return [];
    return Array.from({ length: 51 }, (_, i) => spot * (0.7 + i * 0.012));
  }, [spot]);

  const payoffs = useMemo(() => {
    if (spotRange.length === 0) return [];
    return computePayoff(selectedStrategy, spot, spotRange, iv, dte).map(v => v * contracts * 100);
  }, [selectedStrategy, spot, spotRange, iv, dte, contracts]);

  const maxProfit = payoffs.length ? Math.max(...payoffs) : 0;
  const maxLoss = payoffs.length ? Math.min(...payoffs) : 0;
  const currentPnl = payoffs.length ? payoffs[Math.floor(payoffs.length / 2)] : 0;

  return (
    <div className="bg-panel border border-border rounded-lg flex flex-col h-full min-h-0 select-none shadow-lg overflow-hidden">
      {/* Header */}
      <div className="bg-card px-3 py-2 border-b border-border flex items-center gap-2 flex-shrink-0">
        <Zap size={14} className="text-accent-amber" />
        <span className="font-bold text-xs uppercase tracking-wider text-white">Options Strategy Roller</span>
        <span className="ml-auto text-[10px] font-mono text-text-dim">{selectedSymbol} · Spot: ${spot.toLocaleString(undefined, { minimumFractionDigits: 2 })}</span>
      </div>

      <div className="flex-1 overflow-y-auto scrollbar p-3 space-y-3">
        {/* Strategy Grid Selector */}
        <div>
          <div className="text-[9px] uppercase tracking-wider text-text-dim font-bold mb-1.5">Select Strategy</div>
          <div className="grid grid-cols-2 gap-1">
            {STRATEGY_LIST.map(s => (
              <button
                key={s}
                onClick={() => setSelectedStrategy(s)}
                className={`px-2 py-1.5 rounded text-[10px] font-bold text-left flex items-center gap-1.5 transition-all border ${selectedStrategy === s
                  ? `border-${STRATEGIES[s].color} bg-${STRATEGIES[s].color}/10 text-${STRATEGIES[s].color}`
                  : 'border-border text-text-dim hover:border-text-secondary hover:text-white'
                  }`}
              >
                <span className={selectedStrategy === s ? `text-${STRATEGIES[s].color}` : 'text-text-dim'}>
                  {STRATEGIES[s].icon}
                </span>
                {STRATEGIES[s].name}
              </button>
            ))}
          </div>
        </div>

        {/* Strategy Info */}
        <div className="bg-card/60 border border-border/60 rounded-lg p-3 space-y-2">
          <div className="flex items-center justify-between">
            <span className="text-xs font-bold text-white">{def.name}</span>
            <span className="text-[9px] text-text-dim bg-hover px-2 py-0.5 rounded">{def.marketView}</span>
          </div>
          <p className="text-[10px] text-text-secondary leading-relaxed">{def.description}</p>
          <div className="text-[9px] font-mono text-accent-blue/80 border border-accent-blue/20 rounded px-2 py-1">
            Legs: {def.legs}
          </div>
        </div>

        {/* Parameters */}
        <div className="grid grid-cols-2 gap-2">
          <div className="space-y-1">
            <label className="text-[9px] uppercase tracking-wider text-text-dim font-bold">DTE: {dte} days</label>
            <input type="range" min={7} max={365} value={dte} onChange={e => setDte(+e.target.value)}
              className="w-full h-1.5 rounded-lg appearance-none cursor-pointer accent-accent-blue" />
          </div>
          <div className="space-y-1">
            <label className="text-[9px] uppercase tracking-wider text-text-dim font-bold">Contracts: {contracts}</label>
            <input type="range" min={1} max={50} value={contracts} onChange={e => setContracts(+e.target.value)}
              className="w-full h-1.5 rounded-lg appearance-none cursor-pointer accent-accent-purple" />
          </div>
        </div>

        {/* P&L Chart */}
        {payoffs.length > 0 && (
          <div className="bg-card/60 border border-border/60 rounded-lg p-3 space-y-2">
            <div className="flex items-center justify-between text-[10px]">
              <span className="font-bold text-text-secondary uppercase tracking-wider">Expiration P&L</span>
              <span className="text-text-dim font-mono">At-spot: <span className={currentPnl >= 0 ? 'text-accent-green font-bold' : 'text-accent-red font-bold'}>${currentPnl.toFixed(0)}</span></span>
            </div>
            <PayoffChart payoffs={payoffs} spotRange={spotRange} spot={spot} />
            <div className="grid grid-cols-3 gap-2 text-[10px] font-mono">
              <div className="text-center">
                <div className="text-text-dim text-[9px]">MAX PROFIT</div>
                <div className="text-accent-green font-bold">{maxProfit > 1e6 ? '∞' : `$${maxProfit.toFixed(0)}`}</div>
              </div>
              <div className="text-center border-x border-border/40">
                <div className="text-text-dim text-[9px]">B/E RANGE</div>
                <div className="text-accent-amber font-bold text-[9px]">{def.breakeven}</div>
              </div>
              <div className="text-center">
                <div className="text-text-dim text-[9px]">MAX LOSS</div>
                <div className="text-accent-red font-bold">{maxLoss < -1e6 ? '-∞' : `$${Math.abs(maxLoss).toFixed(0)}`}</div>
              </div>
            </div>
          </div>
        )}

        {/* Risk Metrics */}
        <div className="bg-card/60 border border-border/60 rounded-lg p-3 space-y-1.5 text-[10px] font-mono">
          <div className="text-[9px] uppercase tracking-wider text-text-dim font-bold mb-2">Risk/Reward Summary</div>
          {[
            { label: 'Max Profit', value: def.maxProfit, color: 'text-accent-green' },
            { label: 'Max Loss', value: def.maxLoss, color: 'text-accent-red' },
            { label: 'Breakeven', value: def.breakeven, color: 'text-accent-amber' },
            { label: 'Implied Vol', value: `${(iv * 100).toFixed(0)}% annualized`, color: 'text-accent-purple' },
            { label: 'Time Decay', value: `θ active — ${dte}d to expiry`, color: 'text-text-secondary' },
          ].map(r => (
            <div key={r.label} className="flex justify-between items-start gap-2 border-b border-border/20 pb-1">
              <span className="text-text-dim">{r.label}</span>
              <span className={`${r.color} text-right text-[9px] max-w-[55%]`}>{r.value}</span>
            </div>
          ))}
        </div>
      </div>

      {/* Footer */}
      <div className="border-t border-border/50 bg-card px-3 py-1 text-[9px] text-text-dim font-mono flex-shrink-0">
        Educational model only · Not financial advice · Prices derived from Black-Scholes
      </div>
    </div>
  );
}
