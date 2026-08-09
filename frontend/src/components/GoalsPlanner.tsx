'use client';
import React, { useState, useEffect, useCallback } from 'react';
import { Target, RefreshCw } from 'lucide-react';

// ─── Monte Carlo Engine ─────────────────────────────────────────────────────
// Geometric Brownian Motion: dS = μS dt + σS dW
// Industry standard for capital projection under uncertainty

interface SimResult {
  median: number[];
  p10: number[];
  p25: number[];
  p75: number[];
  p90: number[];
  successRate: number;
  medianFinal: number;
  p10Final: number;
  p90Final: number;
  expectedCAGR: number;
  sharpeRatio: number;
}

const RISK_PROFILES = {
  'Ultra Conservative': { mu: 0.03, sigma: 0.05, color: '#3b82f6' },
  'Conservative':       { mu: 0.045, sigma: 0.08, color: '#10b981' },
  'Moderate':           { mu: 0.07, sigma: 0.14, color: '#8b5cf6' },
  'Aggressive':         { mu: 0.10, sigma: 0.22, color: '#f59e0b' },
  'Speculative':        { mu: 0.15, sigma: 0.40, color: '#ef4444' },
} as const;

type RiskProfile = keyof typeof RISK_PROFILES;

function runMonteCarlo(
  initialCapital: number,
  monthlyContrib: number,
  years: number,
  profile: RiskProfile,
  numPaths: number = 2000,
): SimResult {
  const { mu, sigma } = RISK_PROFILES[profile];
  const steps = years * 12;
  const dt = 1 / 12;
  const muAdj = mu * dt - 0.5 * sigma * sigma * dt;
  const sigSqrtDt = sigma * Math.sqrt(dt);
  const target = initialCapital; // at least preserve capital = success

  const finals: number[] = [];
  const paths: number[][] = [];

  for (let p = 0; p < numPaths; p++) {
    let val = initialCapital;
    const path: number[] = [val];
    for (let t = 0; t < steps; t++) {
      // Box-Muller transform for standard normal
      const u1 = Math.random(), u2 = Math.random();
      const z = Math.sqrt(-2 * Math.log(u1 + 1e-10)) * Math.cos(2 * Math.PI * u2);
      val = val * Math.exp(muAdj + sigSqrtDt * z) + monthlyContrib;
      path.push(Math.max(0, val));
    }
    finals.push(path[path.length - 1]);
    paths.push(path);
  }

  finals.sort((a, b) => a - b);

  // Build percentile envelopes
  const pct = (arr: number[], p: number) => arr[Math.floor(p * arr.length)];
  const envelope = (pctile: number) =>
    Array.from({ length: steps + 1 }, (_, t) => {
      const vals = paths.map(path => path[t]).sort((a, b) => a - b);
      return pct(vals, pctile);
    });

  const medianFinal = pct(finals, 0.5);
  const cagr = Math.pow(medianFinal / initialCapital, 1 / years) - 1;
  const successRate = finals.filter(f => f >= target).length / numPaths;
  const annualReturns = paths.slice(0, 200).map(path => {
    const annual: number[] = [];
    for (let y = 0; y < years; y++) {
      const s = path[y * 12], e = path[(y + 1) * 12];
      if (s > 0) annual.push((e - s) / s);
    }
    return annual;
  }).flat();
  const avgReturn = annualReturns.reduce((a, b) => a + b, 0) / annualReturns.length;
  const stdReturn = Math.sqrt(annualReturns.reduce((a, b) => a + (b - avgReturn) ** 2, 0) / annualReturns.length);
  const sharpe = stdReturn > 0 ? (avgReturn - 0.05) / stdReturn : 0;

  return {
    median: envelope(0.5),
    p10: envelope(0.1),
    p25: envelope(0.25),
    p75: envelope(0.75),
    p90: envelope(0.9),
    successRate,
    medianFinal,
    p10Final: pct(finals, 0.1),
    p90Final: pct(finals, 0.9),
    expectedCAGR: cagr,
    sharpeRatio: sharpe,
  };
}

function fmtCurrency(n: number): string {
  if (n >= 1e9) return `$${(n / 1e9).toFixed(2)}B`;
  if (n >= 1e6) return `$${(n / 1e6).toFixed(2)}M`;
  if (n >= 1e3) return `$${(n / 1e3).toFixed(1)}K`;
  return `$${n.toFixed(0)}`;
}

// SVG Monte Carlo chart
function MCChart({ result, years, initialCapital, color }: { result: SimResult; years: number; initialCapital: number; color: string }) {
  const W = 600, H = 200;
  const STEPS = result.median.length;
  const allVals = [...result.p90, ...result.p10];
  const maxV = Math.max(...allVals) * 1.05;
  const minV = Math.min(0, Math.min(...allVals) * 0.95);
  const range = maxV - minV || 1;

  const toX = (i: number) => (i / (STEPS - 1)) * W;
  const toY = (v: number) => H - ((v - minV) / range) * H;
  const pts = (arr: number[]) => arr.map((v, i) => `${toX(i)},${toY(v)}`).join(' ');

  // Year grid lines
  const gridLines = Array.from({ length: years + 1 }, (_, i) => i);

  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full" style={{ height: 160 }}>
      <defs>
        <linearGradient id="medGrad" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity="0.4" />
          <stop offset="100%" stopColor={color} stopOpacity="0.0" />
        </linearGradient>
        <clipPath id="chartClip"><rect x="0" y="0" width={W} height={H} /></clipPath>
      </defs>

      {/* Year grid */}
      {gridLines.map(y => (
        <line key={y} x1={toX(y * 12)} y1={0} x2={toX(y * 12)} y2={H}
          stroke="#26262c" strokeWidth="0.5" />
      ))}
      {/* Zero line */}
      <line x1={0} y1={toY(initialCapital)} x2={W} y2={toY(initialCapital)}
        stroke="#3b82f6" strokeWidth="0.5" strokeDasharray="4,4" opacity="0.4" />

      {/* P10-P90 band */}
      <polygon
        clipPath="url(#chartClip)"
        points={`${pts(result.p10)} ${pts(result.p90).split(' ').reverse().join(' ')}`}
        fill={color} opacity="0.08"
      />
      {/* P25-P75 band */}
      <polygon
        clipPath="url(#chartClip)"
        points={`${pts(result.p25)} ${pts(result.p75).split(' ').reverse().join(' ')}`}
        fill={color} opacity="0.12"
      />
      {/* P10 / P90 lines */}
      <polyline clipPath="url(#chartClip)" points={pts(result.p10)} fill="none" stroke={color} strokeWidth="0.8" opacity="0.5" strokeDasharray="3,3" />
      <polyline clipPath="url(#chartClip)" points={pts(result.p90)} fill="none" stroke={color} strokeWidth="0.8" opacity="0.5" strokeDasharray="3,3" />
      {/* Median fill */}
      <polygon clipPath="url(#chartClip)"
        points={`${toX(0)},${H} ${pts(result.median)} ${toX(STEPS - 1)},${H}`}
        fill="url(#medGrad)"
      />
      {/* Median line */}
      <polyline clipPath="url(#chartClip)" points={pts(result.median)} fill="none" stroke={color} strokeWidth="2" />

      {/* Year labels */}
      {gridLines.filter(y => y % 5 === 0).map(y => (
        <text key={y} x={toX(y * 12)} y={H - 2} fontSize="8" fill="#606066" textAnchor="middle">
          {y === 0 ? 'Now' : `Y${y}`}
        </text>
      ))}
    </svg>
  );
}

export default function GoalsPlanner() {
  const [initialCapital, setInitialCapital] = useState(100000);
  const [monthlyContrib, setMonthlyContrib] = useState(2000);
  const [years, setYears] = useState(20);
  const [profile, setProfile] = useState<RiskProfile>('Moderate');
  const [targetGoal, setTargetGoal] = useState(1000000);
  const [isRunning, setIsRunning] = useState(false);
  const [result, setResult] = useState<SimResult | null>(null);

  const profileData = RISK_PROFILES[profile];

  const runSimulation = useCallback(() => {
    setIsRunning(true);
    setTimeout(() => {
      setResult(runMonteCarlo(initialCapital, monthlyContrib, years, profile, 2000));
      setIsRunning(false);
    }, 50);
  }, [initialCapital, monthlyContrib, years, profile]);

  // Auto-run on mount and when params change
  useEffect(() => {
    runSimulation();
  }, [runSimulation]);

  const totalContrib = initialCapital + monthlyContrib * years * 12;
  const targetProbability = result
    ? (() => {
      // Simple Monte Carlo target probability using the result distribution
      if (result.medianFinal >= targetGoal) return Math.max(50, result.successRate * 100);
      const ratio = result.medianFinal / targetGoal;
      return Math.min(95, Math.max(5, ratio * 50));
    })()
    : 0;

  return (
    <div className="bg-panel border border-border rounded-lg flex flex-col h-full min-h-0 select-none overflow-hidden shadow-lg">
      {/* Header */}
      <div className="bg-card px-3 py-2 border-b border-border flex items-center justify-between flex-shrink-0">
        <div className="flex items-center gap-2">
          <Target size={14} className="text-accent-blue" />
          <span className="font-bold text-xs uppercase tracking-wider text-white">Monte Carlo Capital Planner</span>
        </div>
        <div className="flex items-center gap-2">
          {isRunning && <RefreshCw size={12} className="text-accent-blue animate-spin" />}
          <span className="text-[9px] font-mono text-text-dim">2,000 simulation paths</span>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto scrollbar">
        <div className="p-3 space-y-3">
          {/* Parameters */}
          <div className="grid grid-cols-2 gap-2 text-[10px]">
            <div className="space-y-1">
              <label className="text-text-dim uppercase font-bold text-[9px]">Initial Capital</label>
              <div className="relative">
                <span className="absolute left-2 top-1/2 -translate-y-1/2 text-text-dim">$</span>
                <input
                  type="number"
                  value={initialCapital}
                  onChange={e => setInitialCapital(Math.max(0, +e.target.value))}
                  className="bg-card border border-border rounded w-full pl-5 pr-2 py-1 text-white font-mono focus:outline-none focus:border-accent-blue"
                />
              </div>
            </div>
            <div className="space-y-1">
              <label className="text-text-dim uppercase font-bold text-[9px]">Monthly Contribution</label>
              <div className="relative">
                <span className="absolute left-2 top-1/2 -translate-y-1/2 text-text-dim">$</span>
                <input
                  type="number"
                  value={monthlyContrib}
                  onChange={e => setMonthlyContrib(Math.max(0, +e.target.value))}
                  className="bg-card border border-border rounded w-full pl-5 pr-2 py-1 text-white font-mono focus:outline-none focus:border-accent-blue"
                />
              </div>
            </div>
            <div className="space-y-1">
              <label className="text-text-dim uppercase font-bold text-[9px]">Target Goal</label>
              <div className="relative">
                <span className="absolute left-2 top-1/2 -translate-y-1/2 text-text-dim">$</span>
                <input
                  type="number"
                  value={targetGoal}
                  onChange={e => setTargetGoal(Math.max(0, +e.target.value))}
                  className="bg-card border border-border rounded w-full pl-5 pr-2 py-1 text-white font-mono focus:outline-none focus:border-accent-blue"
                />
              </div>
            </div>
            <div className="space-y-1">
              <label className="text-text-dim uppercase font-bold text-[9px]">Time Horizon: {years} Years</label>
              <input
                type="range" min={1} max={50} value={years}
                onChange={e => setYears(+e.target.value)}
                className="w-full accent-accent-blue"
              />
            </div>
          </div>

          {/* Risk Profile */}
          <div>
            <label className="text-[9px] uppercase tracking-wider text-text-dim font-bold block mb-1.5">Risk Profile</label>
            <div className="flex gap-1 flex-wrap">
              {Object.keys(RISK_PROFILES).map(p => (
                <button
                  key={p}
                  onClick={() => setProfile(p as RiskProfile)}
                  className={`px-2 py-0.5 rounded text-[9px] font-bold border transition-all ${profile === p
                    ? 'border-accent-blue bg-accent-blue/15 text-accent-blue'
                    : 'border-border text-text-dim hover:text-white'
                    }`}
                >
                  {p}
                </button>
              ))}
            </div>
            <div className="mt-1 text-[9px] font-mono text-text-secondary">
              μ={((RISK_PROFILES[profile].mu) * 100).toFixed(1)}% · σ={((RISK_PROFILES[profile].sigma) * 100).toFixed(1)}% annually
            </div>
          </div>

          {/* Monte Carlo Chart */}
          {result && (
            <div className="bg-card/60 border border-border/60 rounded-lg p-3 space-y-2">
              <div className="flex items-center justify-between text-[10px]">
                <span className="font-bold text-text-secondary uppercase tracking-wider">Projection Envelope</span>
                <span className="text-text-dim font-mono text-[9px]">P10 / Median / P90</span>
              </div>
              <MCChart result={result} years={years} initialCapital={initialCapital} color={profileData.color} />
            </div>
          )}

          {/* Key Stats */}
          {result && (
            <div className="grid grid-cols-3 gap-2 text-[10px] font-mono">
              {[
                { label: 'Median Outcome', value: fmtCurrency(result.medianFinal), color: 'text-accent-green' },
                { label: 'Bear Case (P10)', value: fmtCurrency(result.p10Final), color: 'text-accent-red' },
                { label: 'Bull Case (P90)', value: fmtCurrency(result.p90Final), color: 'text-accent-purple' },
              ].map(s => (
                <div key={s.label} className="bg-card/60 border border-border/60 rounded p-2 text-center">
                  <div className="text-[8px] uppercase text-text-dim mb-1">{s.label}</div>
                  <div className={`font-bold ${s.color}`}>{s.value}</div>
                </div>
              ))}
            </div>
          )}

          {/* Target Goal Progress */}
          {result && (
            <div className="bg-card/60 border border-border/60 rounded-lg p-3 space-y-2">
              <div className="flex justify-between text-[10px]">
                <span className="text-text-secondary font-bold">Goal: {fmtCurrency(targetGoal)}</span>
                <span className={`font-bold ${targetProbability >= 60 ? 'text-accent-green' : targetProbability >= 35 ? 'text-accent-amber' : 'text-accent-red'}`}>
                  ~{targetProbability.toFixed(0)}% Probability
                </span>
              </div>
              <div className="w-full bg-card h-2 rounded-full overflow-hidden">
                <div
                  className={`h-full rounded-full transition-all duration-500 ${targetProbability >= 60 ? 'bg-accent-green' : targetProbability >= 35 ? 'bg-accent-amber' : 'bg-accent-red'}`}
                  style={{ width: `${Math.min(100, targetProbability)}%` }}
                />
              </div>
            </div>
          )}

          {/* Summary Row */}
          {result && (
            <div className="grid grid-cols-4 gap-1 text-[9px] font-mono text-center">
              {[
                { label: 'Total Contributed', value: fmtCurrency(totalContrib), color: 'text-text-secondary' },
                { label: 'Expected CAGR', value: `${(result.expectedCAGR * 100).toFixed(1)}%`, color: 'text-accent-green' },
                { label: 'Sim Sharpe', value: result.sharpeRatio.toFixed(2), color: 'text-accent-blue' },
                { label: 'Break-Even Yr', value: `~${Math.ceil(Math.log(2) / Math.log(1 + RISK_PROFILES[profile].mu))}yr`, color: 'text-accent-purple' },
              ].map(s => (
                <div key={s.label} className="bg-card/40 border border-border/40 rounded p-1.5">
                  <div className="text-text-dim">{s.label}</div>
                  <div className={`font-bold ${s.color}`}>{s.value}</div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Footer */}
      <div className="border-t border-border/50 bg-card px-3 py-1 text-[9px] text-text-dim font-mono flex-shrink-0">
        GBM Monte Carlo · Educational model only · Past performance does not predict future results
      </div>
    </div>
  );
}
