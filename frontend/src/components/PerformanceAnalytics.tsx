import React, { useState, useMemo } from 'react';
import { BarChart2, Activity, Award } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';

// ─── Statistical Engine ─────────────────────────────────────────────────────

function mean(arr: number[]): number {
  if (arr.length === 0) return 0;
  return arr.reduce((a, b) => a + b, 0) / arr.length;
}

function stdDev(arr: number[]): number {
  if (arr.length < 2) return 0;
  const m = mean(arr);
  return Math.sqrt(arr.reduce((a, b) => a + (b - m) ** 2, 0) / (arr.length - 1));
}

function sharpeRatio(returns: number[], riskFreeRate = 0.0525 / 252): number {
  if (returns.length < 2) return 0;
  const m = mean(returns);
  const s = stdDev(returns);
  return s > 0 ? ((m - riskFreeRate) / s) * Math.sqrt(252) : 0;
}

function sortinoRatio(returns: number[], riskFreeRate = 0.0525 / 252): number {
  if (returns.length < 2) return 0;
  const m = mean(returns);
  const downside = returns.filter(r => r < riskFreeRate);
  const ds = downside.length > 1 ? Math.sqrt(downside.reduce((a, b) => a + (b - riskFreeRate) ** 2, 0) / downside.length) : 0;
  return ds > 0 ? ((m - riskFreeRate) / ds) * Math.sqrt(252) : 0;
}

function maxDrawdown(equity: number[]): { mdd: number; peak: number; trough: number; drawdowns: number[] } {
  let mdd = 0;
  const drawdowns: number[] = [];
  let runPeak = equity[0] || 0;
  let peakVal = equity[0] || 0, troughVal = equity[0] || 0;
  equity.forEach(v => {
    if (v > runPeak) runPeak = v;
    const dd = runPeak > 0 ? (v - runPeak) / runPeak : 0;
    drawdowns.push(dd);
    if (dd < mdd) {
      mdd = dd; peakVal = runPeak; troughVal = v;
    }
  });
  return { mdd, peak: peakVal, trough: troughVal, drawdowns };
}

function calmarRatio(cagr: number, mdd: number): number {
  return Math.abs(mdd) > 0 ? cagr / Math.abs(mdd) : 0;
}

function winRate(trades: { realizedPnL: number }[]): number {
  const closed = trades.filter(t => t.realizedPnL !== 0);
  if (closed.length === 0) return 0;
  return closed.filter(t => t.realizedPnL > 0).length / closed.length;
}

function profitFactor(trades: { realizedPnL: number }[]): number {
  const wins = trades.filter(t => t.realizedPnL > 0).reduce((a, t) => a + t.realizedPnL, 0);
  const losses = trades.filter(t => t.realizedPnL < 0).reduce((a, t) => a + Math.abs(t.realizedPnL), 0);
  return losses > 0 ? wins / losses : wins > 0 ? Infinity : 0;
}

function var95(returns: number[]): number {
  if (returns.length < 5) return 0;
  const sorted = [...returns].sort((a, b) => a - b);
  return sorted[Math.floor(returns.length * 0.05)];
}

function cvar95(returns: number[]): number {
  if (returns.length < 5) return 0;
  const sorted = [...returns].sort((a, b) => a - b);
  const cutoff = Math.floor(returns.length * 0.05);
  const tail = sorted.slice(0, cutoff);
  return tail.length > 0 ? mean(tail) : 0;
}

// ─── Synthetic equity curve generator (for demo when no trades) ────────────
function generateDemoEquity(seed: number, n: number): number[] {
  let v = 100000, r = seed;
  const arr = [v];
  for (let i = 0; i < n; i++) {
    r = (r * 1664525 + 1013904223) & 0xffffffff;
    const rng = (r / 0xffffffff);
    const ret = 0.0004 + 0.015 * (rng * 2 - 1) + (rng > 0.95 ? -0.03 : 0);
    v *= (1 + ret);
    arr.push(Math.max(0, v));
  }
  return arr;
}

// ─── SVG Chart Components ───────────────────────────────────────────────────

function EquityCurveChart({ data, color = '#3b82f6' }: { data: number[]; color?: string }) {
  const W = 600, H = 120;
  if (data.length < 2) return null;
  const min = Math.min(...data), max = Math.max(...data), range = max - min || 1;
  const toX = (i: number) => (i / (data.length - 1)) * W;
  const toY = (v: number) => H - ((v - min) / range) * H;
  const points = data.map((v, i) => `${toX(i)},${toY(v)}`).join(' ');
  const zeroY = toY(data[0]);

  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full" style={{ height: 120 }}>
      <defs>
        <linearGradient id="eqGrad" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity="0.3" />
          <stop offset="100%" stopColor={color} stopOpacity="0" />
        </linearGradient>
        <clipPath id="eqClip"><rect x="0" y="0" width={W} height={H} /></clipPath>
      </defs>
      <line x1={0} y1={zeroY} x2={W} y2={zeroY} stroke="#26262c" strokeWidth="1" strokeDasharray="4,4" />
      <polygon clipPath="url(#eqClip)"
        points={`${toX(0)},${H} ${points} ${toX(data.length - 1)},${H}`}
        fill="url(#eqGrad)" />
      <polyline clipPath="url(#eqClip)" points={points} fill="none" stroke={color} strokeWidth="1.5" />
    </svg>
  );
}

function DrawdownChart({ drawdowns }: { drawdowns: number[] }) {
  const W = 600, H = 80;
  if (drawdowns.length < 2) return null;
  const min = Math.min(...drawdowns, -0.001), max = 0, range = max - min || 1;
  const toX = (i: number) => (i / (drawdowns.length - 1)) * W;
  const toY = (v: number) => H - ((v - min) / range) * H;
  const points = drawdowns.map((v, i) => `${toX(i)},${toY(v)}`).join(' ');

  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full" style={{ height: 80 }}>
      <defs>
        <clipPath id="ddClip"><rect x="0" y="0" width={W} height={H} /></clipPath>
      </defs>
      <line x1={0} y1={toY(0)} x2={W} y2={toY(0)} stroke="#26262c" strokeWidth="0.5" />
      <polygon clipPath="url(#ddClip)"
        points={`${toX(0)},${toY(0)} ${points} ${toX(drawdowns.length - 1)},${toY(0)}`}
        fill="rgba(239,68,68,0.15)" />
      <polyline clipPath="url(#ddClip)" points={points} fill="none" stroke="#ef4444" strokeWidth="1" />
    </svg>
  );
}

function ReturnDistChart({ returns }: { returns: number[] }) {
  const W = 300, H = 80, BINS = 20;
  if (returns.length < 5) return null;
  const min = Math.min(...returns), max = Math.max(...returns);
  const step = (max - min) / BINS || 0.001;
  const bins = Array.from({ length: BINS }, (_, i) => {
    const lo = min + i * step, hi = lo + step;
    return { lo, hi, count: returns.filter(r => r >= lo && r < hi).length };
  });
  const maxCount = Math.max(...bins.map(b => b.count));
  const toX = (i: number) => (i / BINS) * W;
  const barW = W / BINS - 1;
  const toH = (count: number) => (count / (maxCount || 1)) * H;

  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full" style={{ height: 80 }}>
      {bins.map((bin, i) => {
        const h = toH(bin.count);
        const isPositive = (bin.lo + bin.hi) / 2 >= 0;
        return (
          <rect key={i} x={toX(i)} y={H - h} width={barW} height={h}
            fill={isPositive ? 'rgba(16,185,129,0.6)' : 'rgba(239,68,68,0.6)'}
          />
        );
      })}
      <line x1={W / 2} y1={0} x2={W / 2} y2={H} stroke="#f59e0b" strokeWidth="1" strokeDasharray="2,2" />
    </svg>
  );
}

// ─── Main Component ─────────────────────────────────────────────────────────

export default function PerformanceAnalytics() {
  const { tradeHistory, equity, riskData } = useTerminalStore();
  const [activeView, setActiveView] = useState<'curve' | 'drawdown' | 'distribution'>('curve');
  const [benchmark, setBenchmark] = useState<'SPY' | 'BTC' | 'NONE'>('SPY');

  // Build equity curve from trade history
  const equityCurve = useMemo(() => {
    if (tradeHistory.length < 3) {
      return generateDemoEquity(42, 252);
    }
    let v = 100000;
    const curve = [v];
    [...tradeHistory].reverse().forEach(t => {
      v += t.realizedPnL;
      curve.push(Math.max(0, v));
    });
    curve.push(equity > 0 ? equity : v);
    return curve;
  }, [tradeHistory, equity]);

  const dailyReturns = useMemo(() => {
    const curve = equityCurve;
    return curve.slice(1).map((v, i) => curve[i] > 0 ? (v - curve[i]) / curve[i] : 0);
  }, [equityCurve]);

  const metrics = useMemo(() => {
    const curve = equityCurve;
    const returns = dailyReturns;
    const initial = curve[0], final = curve[curve.length - 1];
    const years = Math.max(curve.length / 252, 1 / 252);
    const cagr = Math.pow(final / (initial || 1), 1 / years) - 1;
    const { mdd, peak, trough, drawdowns } = maxDrawdown(curve);
    const trades = tradeHistory;
    const wr = winRate(trades);
    const pf = profitFactor(trades);
    const avgWin = trades.filter(t => t.realizedPnL > 0).length > 0
      ? mean(trades.filter(t => t.realizedPnL > 0).map(t => t.realizedPnL)) : 0;
    const avgLoss = trades.filter(t => t.realizedPnL < 0).length > 0
      ? mean(trades.filter(t => t.realizedPnL < 0).map(t => Math.abs(t.realizedPnL))) : 0;

    return {
      totalReturn: (final - initial) / (initial || 1),
      cagr,
      sharpe: sharpeRatio(returns),
      sortino: sortinoRatio(returns),
      calmar: calmarRatio(cagr, mdd),
      mdd,
      mddPeak: peak,
      mddTrough: trough,
      var95: var95(returns),
      cvar95: cvar95(returns),
      winRate: wr,
      profitFactor: pf,
      avgWin,
      avgLoss,
      rrRatio: avgLoss > 0 ? avgWin / avgLoss : 0,
      totalTrades: trades.length,
      winningTrades: trades.filter(t => t.realizedPnL > 0).length,
      losingTrades: trades.filter(t => t.realizedPnL < 0).length,
      grossPnL: trades.reduce((a, t) => a + t.realizedPnL, 0),
      drawdowns,
      volatility: stdDev(returns) * Math.sqrt(252),
    };
  }, [equityCurve, dailyReturns, tradeHistory]);

  const fmtPct = (n: number) => `${(n * 100).toFixed(2)}%`;
  const fmtNum = (n: number, d = 2) => n.toFixed(d);

  const ratingColor = (v: number, good: number, great: number) =>
    v >= great ? 'text-accent-green' : v >= good ? 'text-accent-amber' : 'text-accent-red';

  return (
    <div className="h-full flex flex-col overflow-hidden bg-bg-base">
      {/* Sub-header */}
      <div className="bg-panel border-b border-border px-4 py-2 flex items-center justify-between flex-shrink-0">
        <div className="flex items-center gap-2">
          <BarChart2 size={14} className="text-accent-blue" />
          <span className="text-sm font-bold text-white">Performance Analytics</span>
        </div>
        <div className="flex items-center gap-2 text-[10px]">
          <span className="text-text-dim">Benchmark:</span>
          {(['SPY', 'BTC', 'NONE'] as const).map(b => (
            <button key={b} onClick={() => setBenchmark(b)}
              className={`px-2 py-0.5 rounded font-bold border transition-all ${benchmark === b ? 'border-accent-blue text-accent-blue bg-accent-blue/10' : 'border-border text-text-dim hover:text-white'}`}>
              {b}
            </button>
          ))}
          <div className="h-4 w-px bg-border mx-1" />
          {(['curve', 'drawdown', 'distribution'] as const).map(v => (
            <button key={v} onClick={() => setActiveView(v)}
              className={`px-2 py-0.5 rounded font-bold border transition-all capitalize ${activeView === v ? 'border-accent-purple text-accent-purple bg-accent-purple/10' : 'border-border text-text-dim hover:text-white'}`}>
              {v === 'curve' ? 'Equity' : v === 'drawdown' ? 'Drawdown' : 'Returns Dist.'}
            </button>
          ))}
        </div>
      </div>

      <div className="flex-1 overflow-auto scrollbar p-4 space-y-4">
        {/* Chart Panel */}
        <div className="bg-panel border border-border rounded-lg p-4 space-y-2">
          <div className="flex items-center justify-between text-[10px] font-mono">
            <span className="font-bold text-white uppercase tracking-wider">
              {activeView === 'curve' ? 'Equity Curve' : activeView === 'drawdown' ? 'Drawdown Profile' : 'Return Distribution'}
            </span>
            <div className="flex items-center gap-3 text-text-secondary">
              <span>Total Return: <span className={`font-bold ${metrics.totalReturn >= 0 ? 'text-accent-green' : 'text-accent-red'}`}>{fmtPct(metrics.totalReturn)}</span></span>
              <span>CAGR: <span className={`font-bold ${metrics.cagr >= 0 ? 'text-accent-green' : 'text-accent-red'}`}>{fmtPct(metrics.cagr)}</span></span>
              <span>Samples: <span className="text-white">{equityCurve.length}</span></span>
            </div>
          </div>
          {activeView === 'curve' && <EquityCurveChart data={equityCurve} />}
          {activeView === 'drawdown' && <DrawdownChart drawdowns={metrics.drawdowns} />}
          {activeView === 'distribution' && <ReturnDistChart returns={dailyReturns} />}
        </div>

        {/* Core Metrics Grid */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          {[
            { label: 'Sharpe Ratio', value: fmtNum(metrics.sharpe), color: ratingColor(metrics.sharpe, 1, 2), note: '>2 = Excellent' },
            { label: 'Sortino Ratio', value: fmtNum(metrics.sortino), color: ratingColor(metrics.sortino, 1.5, 3), note: 'Downside-adjusted' },
            { label: 'Calmar Ratio', value: fmtNum(metrics.calmar), color: ratingColor(metrics.calmar, 1, 2), note: 'CAGR / Max Drawdown' },
            { label: 'Max Drawdown', value: fmtPct(metrics.mdd), color: metrics.mdd > -0.20 ? 'text-accent-green' : metrics.mdd > -0.40 ? 'text-accent-amber' : 'text-accent-red', note: `Peak: $${(metrics.mddPeak / 1000).toFixed(0)}K` },
          ].map(m => (
            <div key={m.label} className="bg-panel border border-border rounded-lg p-3 space-y-1">
              <div className="text-[9px] uppercase text-text-dim font-bold">{m.label}</div>
              <div className={`text-xl font-black font-mono ${m.color}`}>{m.value}</div>
              <div className="text-[9px] text-text-dim">{m.note}</div>
            </div>
          ))}
        </div>

        {/* Risk Metrics + Trade Stats */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {/* Risk Metrics */}
          <div className="bg-panel border border-border rounded-lg p-4 space-y-3">
            <div className="text-[10px] font-bold uppercase tracking-wider text-white flex items-center gap-2">
              <Activity size={12} className="text-accent-red" /> Risk Metrics
            </div>
            <div className="space-y-2 text-[11px] font-mono">
              {[
                { label: 'VaR (95%, 1-Day)', value: fmtPct(metrics.var95), color: 'text-accent-red' },
                { label: 'CVaR / Expected Shortfall (95%)', value: fmtPct(metrics.cvar95), color: 'text-accent-red' },
                { label: 'Annualized Volatility', value: fmtPct(metrics.volatility), color: 'text-accent-amber' },
                { label: 'Live VaR (95%)', value: riskData ? fmtPct(-riskData.var_95) : 'N/A', color: 'text-accent-purple' },
                { label: 'Live Sharpe (Gateway)', value: riskData ? fmtNum(riskData.sharpe) : 'N/A', color: 'text-accent-green' },
                { label: 'Live Delta', value: riskData ? fmtNum(riskData.delta, 4) : 'N/A', color: 'text-text-secondary' },
                { label: 'Live Theta', value: riskData ? fmtNum(riskData.theta, 4) : 'N/A', color: 'text-text-secondary' },
                { label: 'Live Vega', value: riskData ? fmtNum(riskData.vega, 4) : 'N/A', color: 'text-text-secondary' },
              ].map(r => (
                <div key={r.label} className="flex justify-between items-center border-b border-border/20 pb-1.5">
                  <span className="text-text-secondary">{r.label}</span>
                  <span className={`font-bold ${r.color}`}>{r.value}</span>
                </div>
              ))}
            </div>
          </div>

          {/* Trade Statistics */}
          <div className="bg-panel border border-border rounded-lg p-4 space-y-3">
            <div className="text-[10px] font-bold uppercase tracking-wider text-white flex items-center gap-2">
              <Award size={12} className="text-accent-green" /> Trade Statistics
            </div>
            <div className="space-y-2 text-[11px] font-mono">
              {[
                { label: 'Total Trades', value: metrics.totalTrades.toString(), color: 'text-white' },
                { label: 'Winning Trades', value: `${metrics.winningTrades} (${fmtPct(metrics.winRate)})`, color: 'text-accent-green' },
                { label: 'Losing Trades', value: metrics.losingTrades.toString(), color: 'text-accent-red' },
                { label: 'Profit Factor', value: isFinite(metrics.profitFactor) ? fmtNum(metrics.profitFactor) : '∞', color: ratingColor(metrics.profitFactor, 1.5, 2.5) },
                { label: 'Average Win', value: `$${metrics.avgWin.toFixed(2)}`, color: 'text-accent-green' },
                { label: 'Average Loss', value: `$${metrics.avgLoss.toFixed(2)}`, color: 'text-accent-red' },
                { label: 'Reward/Risk Ratio', value: fmtNum(metrics.rrRatio), color: ratingColor(metrics.rrRatio, 1.5, 2) },
                { label: 'Gross P&L', value: `$${metrics.grossPnL.toFixed(2)}`, color: metrics.grossPnL >= 0 ? 'text-accent-green' : 'text-accent-red' },
              ].map(r => (
                <div key={r.label} className="flex justify-between items-center border-b border-border/20 pb-1.5">
                  <span className="text-text-secondary">{r.label}</span>
                  <span className={`font-bold ${r.color}`}>{r.value}</span>
                </div>
              ))}
            </div>
          </div>
        </div>

        {/* Win Rate Gauge */}
        <div className="bg-panel border border-border rounded-lg p-4">
          <div className="text-[10px] font-bold uppercase tracking-wider text-white mb-3">Performance Scorecard</div>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3 text-[10px] font-mono">
            {[
              {
                label: 'Win Rate', value: fmtPct(metrics.winRate), width: metrics.winRate * 100,
                good: 50, great: 60,
                color: metrics.winRate >= 0.6 ? 'bg-accent-green' : metrics.winRate >= 0.5 ? 'bg-accent-amber' : 'bg-accent-red'
              },
              {
                label: 'Sharpe Score', value: fmtNum(metrics.sharpe), width: Math.min(100, metrics.sharpe * 33),
                good: 33, great: 66,
                color: metrics.sharpe >= 2 ? 'bg-accent-green' : metrics.sharpe >= 1 ? 'bg-accent-amber' : 'bg-accent-red'
              },
              {
                label: 'Drawdown Control', value: fmtPct(metrics.mdd), width: Math.max(0, 100 + metrics.mdd * 100 * 5),
                good: 60, great: 80,
                color: metrics.mdd > -0.1 ? 'bg-accent-green' : metrics.mdd > -0.2 ? 'bg-accent-amber' : 'bg-accent-red'
              },
              {
                label: 'Profit Factor', value: isFinite(metrics.profitFactor) ? fmtNum(metrics.profitFactor) : '∞',
                width: Math.min(100, (metrics.profitFactor / 3) * 100),
                good: 33, great: 66,
                color: metrics.profitFactor >= 2 ? 'bg-accent-green' : metrics.profitFactor >= 1.5 ? 'bg-accent-amber' : 'bg-accent-red'
              },
            ].map(m => (
              <div key={m.label} className="space-y-1.5">
                <div className="flex justify-between">
                  <span className="text-text-dim">{m.label}</span>
                  <span className="text-white font-bold">{m.value}</span>
                </div>
                <div className="w-full bg-card h-2 rounded-full overflow-hidden">
                  <div className={`h-full rounded-full transition-all duration-700 ${m.color}`}
                    style={{ width: `${Math.max(2, Math.min(100, m.width))}%` }} />
                </div>
              </div>
            ))}
          </div>
        </div>

        {/* Footer disclaimer */}
        <div className="text-[9px] text-text-dim font-mono text-center pb-2">
          Performance metrics are simulated from trade log history · Past performance does not guarantee future results · All figures for educational purposes only
        </div>
      </div>
    </div>
  );
}
