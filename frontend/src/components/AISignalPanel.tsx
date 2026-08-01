'use client';

import React, { useState, useEffect, useCallback } from 'react';
import { BrainCircuit, RefreshCw } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';
import { useAuthStore } from '../store/useAuthStore';

const GATEWAY_URL = process.env.NEXT_PUBLIC_GATEWAY_URL || 'http://localhost:8080';

interface AISignal {
  symbol: string;
  action: 'BUY' | 'SELL' | 'HOLD';
  confidence: number;
  regime: string;
  sentiment: number;
  reason: string;
  price: number;
  entry_target: number;
  timestamp: number;
}

/**
 * AISignalPanel — real-time AI signal engine readout.
 *
 * Consumes GET /api/ai/signal (gateway -> Python AI-agent -> HMM regime +
 * FinBERT sentiment + LGBM signal). Degrades to a HOLD card if the Python
 * microservice is offline so the dashboard never bricks during a demo.
 */
export default function AISignalPanel() {
  const selectedSymbol = useTerminalStore(s => s.selectedSymbol);
  const [signal, setSignal] = useState<AISignal | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchSignal = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const symbol = selectedSymbol.replace('/', '-');
      const res = await fetch(`${GATEWAY_URL}/api/ai/signal?symbol=${encodeURIComponent(symbol)}`, {
        headers: { Authorization: `Bearer ${useAuthStore.getState().getToken() || ''}` },
        signal: AbortSignal.timeout(10_000),
      });
      if (!res.ok) throw new Error(`Gateway ${res.status}`);
      const data = await res.json();
      setSignal(data);
    } catch (err: any) {
      setError(err?.message || 'Signal unavailable');
      setSignal(null);
    } finally {
      setLoading(false);
    }
  }, [selectedSymbol]);

  useEffect(() => {
    fetchSignal();
    const id = setInterval(fetchSignal, 10_000);
    return () => clearInterval(id);
  }, [fetchSignal]);

  if (loading && !signal) {
    return (
      <div className="bg-panel border border-border rounded-lg p-4 h-full flex items-center justify-center">
        <RefreshCw size={16} className="animate-spin text-accent-purple" />
        <span className="ml-2 text-xs text-text-dim font-mono">Running AI pipeline…</span>
      </div>
    );
  }

  if (!signal) {
    return (
      <div className="bg-panel border border-border rounded-lg p-4 h-full flex flex-col items-center justify-center gap-2">
        <BrainCircuit size={18} className="text-text-dim" />
        <span className="text-xs text-text-dim font-mono">AI signal engine offline — {error || 'no data'}</span>
        <span className="text-[10px] text-text-dim/50">Start services/ai-agent (uvicorn) then retry.</span>
      </div>
    );
  }

  const isBuy = signal.action === 'BUY';
  const isSell = signal.action === 'SELL';
  const actionColor = isBuy ? 'text-accent-green' : isSell ? 'text-accent-red' : 'text-slate-400';
  const actionBg = isBuy ? 'bg-accent-green/15 border-accent-green/40' : isSell ? 'bg-accent-red/15 border-accent-red/40' : 'bg-slate-700/30 border-slate-600/40';
  const confPct = Math.round((signal.confidence || 0) * 100);

  return (
    <div className="bg-panel border border-border rounded-lg p-4 h-full flex flex-col overflow-hidden">
      <div className="flex items-center justify-between mb-3 flex-shrink-0">
        <div className="flex items-center gap-2">
          <BrainCircuit size={14} className="text-accent-purple" />
          <span className="text-xs font-bold text-white uppercase tracking-wider">AI Signal Engine</span>
        </div>
        <button
          onClick={fetchSignal}
          disabled={loading}
          className="text-text-dim hover:text-white transition-colors p-1"
          title="Refresh signal"
        >
          <RefreshCw size={12} className={loading ? 'animate-spin' : ''} />
        </button>
      </div>

      <div className="flex items-center justify-between mb-2 flex-shrink-0">
        <span className="text-xs text-text-secondary">{signal.symbol}</span>
        <span className={`text-sm font-bold px-2 py-0.5 rounded border ${actionColor} ${actionBg}`}>
          {signal.action}
        </span>
      </div>

      <div className="flex items-center justify-between mb-2 flex-shrink-0">
        <span className="text-[11px] text-text-dim">Confidence</span>
        <div className="flex items-center gap-2 flex-1 ml-3">
          <div className="w-full h-2 bg-slate-800 rounded-full overflow-hidden">
            <div
              className={`h-2 rounded-full transition-all duration-500 ${
                isBuy ? 'bg-accent-green' : isSell ? 'bg-accent-red' : 'bg-slate-500'
              }`}
              style={{ width: `${confPct}%` }}
            />
          </div>
          <span className="text-xs font-mono text-white w-10 text-right">{confPct}%</span>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-2 mt-1 flex-shrink-0">
        <div className="bg-slate-900 border border-border rounded p-2">
          <p className="text-[9px] text-text-dim uppercase tracking-wider">Regime</p>
          <p className="text-xs font-mono text-accent-purple font-bold">{signal.regime}</p>
        </div>
        <div className="bg-slate-900 border border-border rounded p-2">
          <p className="text-[9px] text-text-dim uppercase tracking-wider">Sentiment</p>
          <p className={`text-xs font-mono font-bold ${signal.sentiment >= 0 ? 'text-accent-green' : 'text-accent-red'}`}>
            {signal.sentiment >= 0 ? '+' : ''}{signal.sentiment.toFixed(2)}
          </p>
        </div>
        <div className="bg-slate-900 border border-border rounded p-2">
          <p className="text-[9px] text-text-dim uppercase tracking-wider">Live Price</p>
          <p className="text-xs font-mono text-white">
            ${signal.price > 0 ? signal.price.toLocaleString(undefined, { minimumFractionDigits: 2 }) : '—'}
          </p>
        </div>
        <div className="bg-slate-900 border border-border rounded p-2">
          <p className="text-[9px] text-text-dim uppercase tracking-wider">Entry Target</p>
          <p className="text-xs font-mono text-white">
            ${signal.entry_target > 0 ? signal.entry_target.toLocaleString(undefined, { minimumFractionDigits: 2 }) : '—'}
          </p>
        </div>
      </div>

      <p className="text-[10px] text-text-dim italic mt-2 leading-relaxed flex-1 overflow-auto">
        {signal.reason}
      </p>

      <div className="flex items-center justify-between mt-2 border-t border-border/50 pt-2 flex-shrink-0">
        <span className="text-[9px] text-text-dim font-mono">
          HMM + FinBERT + LGBM · live data
        </span>
        <span className="text-[9px] text-text-dim font-mono">
          {new Date(signal.timestamp).toLocaleTimeString()}
        </span>
      </div>
    </div>
  );
}
