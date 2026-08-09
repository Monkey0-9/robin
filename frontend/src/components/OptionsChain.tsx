'use client';
import React, { useState, useMemo, useCallback } from 'react';
import { Layers } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';

// ─── Black-Scholes Engine ──────────────────────────────────────────────────
// Institutional standard: exact closed-form solution for European options
// Reference: Black, F. & Scholes, M. (1973). The Journal of Political Economy.

function normalCDF(x: number): number {
  const a1 = 0.254829592, a2 = -0.284496736, a3 = 1.421413741;
  const a4 = -1.453152027, a5 = 1.061405429, p = 0.3275911;
  const sign = x < 0 ? -1 : 1;
  const t = 1 / (1 + p * Math.abs(x) / Math.SQRT2);
  const y = 1 - (((((a5 * t + a4) * t + a3) * t + a2) * t + a1) * t * Math.exp(-(x * x) / 2));
  return 0.5 * (1 + sign * y);
}

function normalPDF(x: number): number {
  return Math.exp(-0.5 * x * x) / Math.sqrt(2 * Math.PI);
}

interface BSResult {
  call: number; put: number;
  callDelta: number; putDelta: number;
  gamma: number; vega: number;
  callTheta: number; putTheta: number;
  callRho: number; putRho: number;
  iv: number;
  d1: number; d2: number;
  intrinsicCall: number; intrinsicPut: number;
  timeValue: number;
  probability_itm: number;
}

function blackScholes(S: number, K: number, T: number, r: number, sigma: number): BSResult {
  if (T <= 0 || S <= 0 || K <= 0 || sigma <= 0) {
    return {
      call: Math.max(0, S - K), put: Math.max(0, K - S),
      callDelta: S > K ? 1 : 0, putDelta: S > K ? 0 : -1,
      gamma: 0, vega: 0, callTheta: 0, putTheta: 0,
      callRho: 0, putRho: 0, iv: sigma,
      d1: 0, d2: 0,
      intrinsicCall: Math.max(0, S - K), intrinsicPut: Math.max(0, K - S),
      timeValue: 0, probability_itm: S > K ? 1 : 0,
    };
  }
  const sqrtT = Math.sqrt(T);
  const d1 = (Math.log(S / K) + (r + 0.5 * sigma * sigma) * T) / (sigma * sqrtT);
  const d2 = d1 - sigma * sqrtT;
  const Nd1 = normalCDF(d1), Nd2 = normalCDF(d2);
  const Nnd1 = normalCDF(-d1), Nnd2 = normalCDF(-d2);
  const discountedK = K * Math.exp(-r * T);

  const call = S * Nd1 - discountedK * Nd2;
  const put = discountedK * Nnd2 - S * Nnd1;
  const gamma = normalPDF(d1) / (S * sigma * sqrtT);
  const vega = (S * normalPDF(d1) * sqrtT) / 100;
  const callTheta = (-(S * normalPDF(d1) * sigma) / (2 * sqrtT) - r * discountedK * Nd2) / 365;
  const putTheta = (-(S * normalPDF(d1) * sigma) / (2 * sqrtT) + r * discountedK * Nnd2) / 365;
  const callRho = (K * T * Math.exp(-r * T) * Nd2) / 100;
  const putRho = -(K * T * Math.exp(-r * T) * Nnd2) / 100;
  const intrinsicCall = Math.max(0, S - K);
  const intrinsicPut = Math.max(0, K - S);

  return {
    call, put, callDelta: Nd1, putDelta: Nd1 - 1,
    gamma, vega, callTheta, putTheta, callRho, putRho,
    iv: sigma, d1, d2,
    intrinsicCall, intrinsicPut,
    timeValue: call - intrinsicCall,
    probability_itm: Nd2,
  };
}

interface OptionRow {
  strike: number;
  expiry: string;
  daysToExpiry: number;
  call: BSResult;
  put: BSResult;
  openInterestCall: number;
  openInterestPut: number;
  volumeCall: number;
  volumePut: number;
}

const RISK_FREE_RATE = 0.0525;

const EXPIRY_LABELS = [
  { label: '1W', days: 7 },
  { label: '2W', days: 14 },
  { label: '1M', days: 30 },
  { label: '3M', days: 90 },
  { label: '6M', days: 180 },
  { label: '1Y', days: 365 },
];

function getSkewedIV(strike: number, spot: number, baseIV: number): number {
  const moneyness = Math.log(strike / spot);
  const skewSlope = -0.15;
  const kurtosis = 0.08;
  return baseIV * Math.exp(skewSlope * moneyness + kurtosis * moneyness * moneyness);
}

function getBaseIV(symbol: string): number {
  const ivMap: Record<string, number> = {
    'BTC/USD': 0.72, 'ETH/USD': 0.85, 'SOL/USD': 1.10,
    'AAPL': 0.28, 'TSLA': 0.65, 'MSFT': 0.25, 'NVDA': 0.55,
    'GOOGL': 0.30, 'AMZN': 0.35, 'SPY': 0.18, 'QQQ': 0.22,
    'EUR/USD': 0.08, 'IWM': 0.24,
  };
  return ivMap[symbol] || 0.35;
}

export default function OptionsChain() {
  const { selectedSymbol, assets } = useTerminalStore();
  const asset = assets.find(a => a.symbol === selectedSymbol);
  const spotPrice = asset?.currentPrice || 0;

  const [selectedExpiry, setSelectedExpiry] = useState(EXPIRY_LABELS[2]);
  const [showGreeks, setShowGreeks] = useState(false);
  const [selectedCall, setSelectedCall] = useState<OptionRow | null>(null);
  const [selectedPut, setSelectedPut] = useState<OptionRow | null>(null);

  const baseIV = getBaseIV(selectedSymbol);

  const strikes = useMemo(() => {
    if (spotPrice <= 0) return [];
    const isCrypto = ['BTC/USD', 'ETH/USD', 'SOL/USD'].includes(selectedSymbol);
    const step = isCrypto ? 0.025 : 0.05;
    const numStrikes = 12;
    const result: number[] = [];
    for (let i = -numStrikes; i <= numStrikes; i++) {
      const raw = spotPrice * Math.exp(i * step);
      const magnitude = Math.pow(10, Math.floor(Math.log10(raw)) - 1);
      result.push(Math.round(raw / magnitude) * magnitude);
    }
    return [...new Set(result)].sort((a, b) => a - b);
  }, [spotPrice, selectedSymbol]);

  const pseudoRandom = (seed: number) => {
    const x = Math.sin(seed) * 10000;
    return x - Math.floor(x);
  };

  const optionChain: OptionRow[] = useMemo(() => {
    if (spotPrice <= 0 || strikes.length === 0) return [];
    const T = selectedExpiry.days / 365;
    return strikes.map(strike => {
      const iv = getSkewedIV(strike, spotPrice, baseIV);
      const bs = blackScholes(spotPrice, strike, T, RISK_FREE_RATE, iv);
      const dist = Math.exp(-0.5 * Math.pow((strike - spotPrice) / (spotPrice * 0.05), 2));
      const baseOI = Math.floor(5000 + 45000 * dist);
      const baseVol = Math.floor(500 + 8000 * dist);
      return {
        strike, expiry: selectedExpiry.label, daysToExpiry: selectedExpiry.days,
        call: bs, put: bs,
        openInterestCall: Math.floor(baseOI * (0.7 + pseudoRandom(strike * 1.1) * 0.6)),
        openInterestPut: Math.floor(baseOI * (0.8 + pseudoRandom(strike * 1.2) * 0.4)),
        volumeCall: Math.floor(baseVol * (0.5 + pseudoRandom(strike * 1.3) * 1.0)),
        volumePut: Math.floor(baseVol * (0.6 + pseudoRandom(strike * 1.4) * 0.9)),
      };
    });
  }, [strikes, spotPrice, selectedExpiry, baseIV]);

  const atmIdx = useMemo(() => {
    let best = 0;
    let bestDist = Infinity;
    optionChain.forEach((row, i) => {
      const d = Math.abs(row.strike - spotPrice);
      if (d < bestDist) { bestDist = d; best = i; }
    });
    return best;
  }, [optionChain, spotPrice]);

  const fmt = useCallback((n: number, dec = 2) => n.toFixed(dec), []);
  const fmtPrice = useCallback((n: number) => {
    if (n < 0.01) return n.toFixed(4);
    if (n < 1) return n.toFixed(3);
    return n.toFixed(2);
  }, []);

  const totalCallOI = optionChain.reduce((s, r) => s + r.openInterestCall, 0);
  const totalPutOI = optionChain.reduce((s, r) => s + r.openInterestPut, 0);
  const pcRatio = totalCallOI > 0 ? (totalPutOI / totalCallOI) : 0;
  const maxPain = optionChain.reduce((best, row) => {
    const pain = row.openInterestCall * Math.max(0, spotPrice - row.strike) +
      row.openInterestPut * Math.max(0, row.strike - spotPrice);
    return pain > best.pain ? { strike: row.strike, pain } : best;
  }, { strike: 0, pain: -Infinity }).strike;

  if (spotPrice <= 0) {
    return (
      <div className="bg-panel border border-border rounded-lg flex flex-col h-full min-h-0 select-none shadow-lg">
        <div className="bg-card px-4 py-2 border-b border-border flex items-center gap-2">
          <Layers size={14} className="text-accent-purple" />
          <span className="font-bold text-xs uppercase tracking-wider text-white">Options Chain ({selectedSymbol})</span>
        </div>
        <div className="flex-1 flex items-center justify-center text-text-dim text-xs font-mono">
          Waiting for live price feed...
        </div>
      </div>
    );
  }

  return (
    <div className="bg-panel border border-border rounded-lg flex flex-col h-full min-h-0 select-none shadow-lg overflow-hidden">
      <div className="bg-card px-3 py-2 border-b border-border flex items-center justify-between flex-shrink-0">
        <div className="flex items-center gap-2">
          <Layers size={14} className="text-accent-purple" />
          <span className="font-bold text-xs uppercase tracking-wider text-white">
            Options Chain — {selectedSymbol}
          </span>
          <span className="text-[10px] font-mono text-text-dim">
            Spot: <span className="text-white font-bold">${spotPrice.toLocaleString(undefined, { minimumFractionDigits: 2 })}</span>
          </span>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setShowGreeks(g => !g)}
            className={`text-[9px] px-2 py-0.5 rounded border font-bold uppercase tracking-wider transition-colors ${showGreeks ? 'border-accent-purple text-accent-purple bg-accent-purple/10' : 'border-border text-text-dim hover:text-white'}`}
          >
            {showGreeks ? 'Hide' : 'Show'} Greeks
          </button>
        </div>
      </div>

      <div className="bg-card/50 px-3 py-1.5 border-b border-border/50 flex items-center gap-4 text-[10px] font-mono flex-shrink-0">
        {EXPIRY_LABELS.map(exp => (
          <button
            key={exp.label}
            onClick={() => setSelectedExpiry(exp)}
            className={`px-2 py-0.5 rounded transition-all font-bold ${selectedExpiry.label === exp.label
              ? 'bg-accent-purple text-white'
              : 'text-text-dim hover:text-white'}`}
          >
            {exp.label}
          </button>
        ))}
        <div className="ml-auto flex items-center gap-3 text-text-secondary">
          <span>P/C Ratio: <span className={pcRatio > 1 ? 'text-accent-red font-bold' : 'text-accent-green font-bold'}>{pcRatio.toFixed(2)}</span></span>
          <span>Max Pain: <span className="text-accent-amber font-bold">${maxPain.toLocaleString()}</span></span>
          <span>IV: <span className="text-accent-purple font-bold">{(baseIV * 100).toFixed(0)}%</span></span>
        </div>
      </div>

      <div className="grid text-[9px] font-bold uppercase tracking-wider text-text-dim border-b border-border bg-card/30 flex-shrink-0 px-2 py-1"
        style={{ gridTemplateColumns: showGreeks ? '1fr 0.7fr 0.6fr 0.5fr 0.5fr 0.7fr 0.8fr 0.6fr 0.5fr 0.5fr 0.7fr 1fr' : '1fr 0.7fr 0.6fr 0.7fr 1fr 0.7fr 0.6fr 1fr' }}>
        {showGreeks ? (
          <>
            <span className="text-accent-green text-right pr-2">CALL Bid</span>
            <span className="text-accent-green text-right">Δ</span>
            <span className="text-accent-green text-right">θ</span>
            <span className="text-accent-green text-right">Γ</span>
            <span className="text-accent-green text-right">OI</span>
            <span className="text-accent-green text-right">Vol</span>
            <span className="text-center text-white">STRIKE</span>
            <span className="text-accent-red">Vol</span>
            <span className="text-accent-red">OI</span>
            <span className="text-accent-red">Γ</span>
            <span className="text-accent-red">Δ</span>
            <span className="text-accent-red">PUT Bid</span>
          </>
        ) : (
          <>
            <span className="text-accent-green text-right pr-2">CALL</span>
            <span className="text-accent-green text-right">Δ</span>
            <span className="text-accent-green text-right">IV</span>
            <span className="text-accent-green text-right">OI</span>
            <span className="text-center text-white">STRIKE</span>
            <span className="text-accent-red">OI</span>
            <span className="text-accent-red">IV</span>
            <span className="text-accent-red">PUT</span>
          </>
        )}
      </div>

      <div className="flex-1 overflow-y-auto scrollbar text-[10px] font-mono">
        {optionChain.map((row, idx) => {
          const isATM = idx === atmIdx;
          const isITMCall = row.strike < spotPrice;
          const isITMPut = row.strike > spotPrice;
          const isSelectedCall = selectedCall?.strike === row.strike;
          const isSelectedPut = selectedPut?.strike === row.strike;

          return (
            <div
              key={row.strike}
              className={`grid px-2 py-[3px] border-b border-border/20 transition-colors ${isATM ? 'bg-accent-blue/8 border-l-2 border-l-accent-blue' : ''}`}
              style={{ gridTemplateColumns: showGreeks ? '1fr 0.7fr 0.6fr 0.5fr 0.5fr 0.7fr 0.8fr 0.6fr 0.5fr 0.5fr 0.7fr 1fr' : '1fr 0.7fr 0.6fr 0.7fr 1fr 0.7fr 0.6fr 1fr' }}
            >
              {showGreeks ? (
                <>
                  <button
                    onClick={() => setSelectedCall(isSelectedCall ? null : row)}
                    className={`text-right pr-2 font-bold transition-colors ${isITMCall ? 'text-accent-green bg-accent-green/5' : 'text-text-secondary'} ${isSelectedCall ? 'text-accent-green underline' : ''} hover:text-accent-green`}
                  >
                    {fmtPrice(row.call.call)}
                  </button>
                  <span className="text-right text-accent-green/80">{fmt(row.call.callDelta, 3)}</span>
                  <span className="text-right text-accent-amber/80">{fmt(row.call.callTheta, 4)}</span>
                  <span className="text-right text-text-secondary">{fmt(row.call.gamma, 5)}</span>
                  <span className="text-right text-text-secondary">{(row.openInterestCall / 1000).toFixed(1)}K</span>
                  <span className="text-right text-text-dim">{(row.volumeCall / 1000).toFixed(1)}K</span>
                  <span className={`text-center font-bold text-[11px] ${isATM ? 'text-accent-blue' : 'text-white'}`}>
                    {row.strike.toLocaleString()}
                    {isATM && <span className="ml-1 text-[8px] text-accent-blue">ATM</span>}
                  </span>
                  <span className="text-text-dim">{(row.volumePut / 1000).toFixed(1)}K</span>
                  <span className="text-text-secondary">{(row.openInterestPut / 1000).toFixed(1)}K</span>
                  <span className="text-text-secondary">{fmt(row.put.gamma, 5)}</span>
                  <span className="text-accent-red/80">{fmt(row.put.putDelta, 3)}</span>
                  <button
                    onClick={() => setSelectedPut(isSelectedPut ? null : row)}
                    className={`font-bold transition-colors ${isITMPut ? 'text-accent-red bg-accent-red/5' : 'text-text-secondary'} ${isSelectedPut ? 'text-accent-red underline' : ''} hover:text-accent-red`}
                  >
                    {fmtPrice(row.put.put)}
                  </button>
                </>
              ) : (
                <>
                  <button
                    onClick={() => setSelectedCall(isSelectedCall ? null : row)}
                    className={`text-right pr-2 font-bold transition-colors ${isITMCall ? 'text-accent-green' : 'text-text-secondary'} ${isSelectedCall ? 'underline' : ''} hover:text-accent-green`}
                  >
                    {fmtPrice(row.call.call)}
                  </button>
                  <span className={`text-right ${row.call.callDelta > 0.5 ? 'text-accent-green' : 'text-text-secondary'}`}>
                    {fmt(row.call.callDelta, 2)}
                  </span>
                  <span className="text-right text-accent-purple/80">{(row.call.iv * 100).toFixed(0)}%</span>
                  <span className="text-right text-text-dim">{(row.openInterestCall / 1000).toFixed(1)}K</span>
                  <span className={`text-center font-bold ${isATM ? 'text-accent-blue' : 'text-white'}`}>
                    {row.strike.toLocaleString()}
                    {isATM && <span className="ml-1 text-[8px] text-accent-blue">ATM</span>}
                  </span>
                  <span className="text-text-dim">{(row.openInterestPut / 1000).toFixed(1)}K</span>
                  <span className="text-accent-purple/80">{(row.put.iv * 100).toFixed(0)}%</span>
                  <button
                    onClick={() => setSelectedPut(isSelectedPut ? null : row)}
                    className={`font-bold transition-colors ${isITMPut ? 'text-accent-red' : 'text-text-secondary'} ${isSelectedPut ? 'underline' : ''} hover:text-accent-red`}
                  >
                    {fmtPrice(row.put.put)}
                  </button>
                </>
              )}
            </div>
          );
        })}
      </div>

      {/* Selected Option Details */}
      {(selectedCall || selectedPut) && (
        <div className="border-t border-border bg-card/60 px-3 py-2 flex-shrink-0 grid grid-cols-2 gap-2 text-[10px] font-mono">
          {selectedCall && (
            <div className="space-y-1">
              <div className="text-accent-green font-bold text-[11px]">
                CALL K={selectedCall.strike.toLocaleString()} / {selectedExpiry.label}
              </div>
              <div className="grid grid-cols-3 gap-x-3 gap-y-0.5 text-text-secondary">
                <span>Premium: <span className="text-white">${fmtPrice(selectedCall.call.call)}</span></span>
                <span>Delta: <span className="text-accent-green">{fmt(selectedCall.call.callDelta, 3)}</span></span>
                <span>Gamma: <span className="text-white">{fmt(selectedCall.call.gamma, 5)}</span></span>
                <span>Theta: <span className="text-accent-red">{fmt(selectedCall.call.callTheta, 4)}</span>/day</span>
                <span>Vega: <span className="text-accent-purple">{fmt(selectedCall.call.vega, 4)}</span></span>
                <span>IV: <span className="text-accent-amber">{(selectedCall.call.iv * 100).toFixed(1)}%</span></span>
                <span>Intrinsic: <span className="text-white">${fmtPrice(selectedCall.call.intrinsicCall)}</span></span>
                <span>Time Val: <span className="text-white">${fmtPrice(selectedCall.call.timeValue)}</span></span>
                <span>P(ITM): <span className="text-accent-green">{(selectedCall.call.probability_itm * 100).toFixed(1)}%</span></span>
              </div>
            </div>
          )}
          {selectedPut && (
            <div className="space-y-1">
              <div className="text-accent-red font-bold text-[11px]">
                PUT K={selectedPut.strike.toLocaleString()} / {selectedExpiry.label}
              </div>
              <div className="grid grid-cols-3 gap-x-3 gap-y-0.5 text-text-secondary">
                <span>Premium: <span className="text-white">${fmtPrice(selectedPut.put.put)}</span></span>
                <span>Delta: <span className="text-accent-red">{fmt(selectedPut.put.putDelta, 3)}</span></span>
                <span>Gamma: <span className="text-white">{fmt(selectedPut.put.gamma, 5)}</span></span>
                <span>Theta: <span className="text-accent-red">{fmt(selectedPut.put.putTheta, 4)}</span>/day</span>
                <span>Vega: <span className="text-accent-purple">{fmt(selectedPut.put.vega, 4)}</span></span>
                <span>IV: <span className="text-accent-amber">{(selectedPut.put.iv * 100).toFixed(1)}%</span></span>
                <span>Intrinsic: <span className="text-white">${fmtPrice(selectedPut.put.intrinsicPut)}</span></span>
                <span>Time Val: <span className="text-white">${fmtPrice(selectedPut.put.timeValue)}</span></span>
                <span>P(ITM): <span className="text-accent-red">{((1 - selectedPut.put.probability_itm) * 100).toFixed(1)}%</span></span>
              </div>
            </div>
          )}
        </div>
      )}

      {/* Footer */}
      <div className="border-t border-border/50 bg-card px-3 py-1 flex items-center justify-between text-[9px] text-text-dim font-mono flex-shrink-0">
        <span>Black-Scholes (1973) · RFR: {(RISK_FREE_RATE * 100).toFixed(2)}% · Volatility Smile Applied</span>
        <span>Click any premium to select</span>
      </div>
    </div>
  );
}
