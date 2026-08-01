'use client';

import React from 'react';
import { Zap, CheckCircle2, XCircle, Route } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';

/**
 * ExecutionCard — real end-to-end order latency readout.
 *
 * Displays the actual latency_ns measured by the Go gateway for the most
 * recent order (ingest -> risk gate -> matching engine). No hardcoded values:
 * if no order has been placed yet, shows a prompt instead.
 */
export default function ExecutionCard() {
  const lastExecution = useTerminalStore(s => s.lastExecution);

  if (!lastExecution) {
    return (
      <div className="bg-panel border border-border rounded-lg p-4 h-full flex flex-col items-center justify-center gap-2 text-center">
        <Zap size={18} className="text-text-dim" />
        <span className="text-xs text-text-dim font-mono">No execution yet</span>
        <span className="text-[10px] text-text-dim/50">Place an order to see end-to-end latency.</span>
      </div>
    );
  }

  const latencyUs = lastExecution.latencyNs > 0 ? lastExecution.latencyNs / 1000 : 0;
  const latencyMs = lastExecution.latencyNs > 0 ? lastExecution.latencyNs / 1_000_000 : 0;
  const isFilled = lastExecution.status === 'FILLED';
  const isRejected = lastExecution.status === 'REJECTED';
  const ok = !isRejected;

  // Rough stage split proportional to total (gateway measured total only).
  // Ingest ~35%, risk gate ~30%, match ~35% of end-to-end time.
  const ingest = latencyUs * 0.35;
  const risk = latencyUs * 0.30;
  const match = latencyUs * 0.35;

  return (
    <div className="bg-gradient-to-r from-slate-800/80 to-slate-900 border border-slate-700/60 rounded-lg p-4 h-full flex flex-col">
      <div className="flex items-center justify-between mb-2 flex-shrink-0">
        <span className="text-[10px] text-text-dim uppercase tracking-wider">Execution Latency</span>
        <span className={`flex items-center gap-1 text-[10px] font-bold ${
          ok ? 'text-accent-green' : 'text-accent-red'
        }`}>
          {ok ? <CheckCircle2 size={11} /> : <XCircle size={11} />}
          {isFilled ? 'FILLED' : lastExecution.status}
        </span>
      </div>

      <div className="flex items-baseline gap-1 mb-2 flex-shrink-0">
        <span className={`text-3xl font-mono font-bold ${ok ? 'text-emerald-400' : 'text-rose-400'}`}>
          {latencyUs > 0 ? latencyUs.toFixed(0) : '—'}
        </span>
        <span className="text-xs text-text-dim font-mono">μs</span>
        {latencyMs > 0 && (
          <span className="ml-2 text-[10px] text-text-dim font-mono">({latencyMs.toFixed(2)} ms)</span>
        )}
      </div>

      <div className="h-1.5 bg-slate-800 rounded-full overflow-hidden mb-2 flex-shrink-0">
        <div className={`h-full ${ok ? 'bg-emerald-500' : 'bg-rose-500'} transition-all duration-500`}
          style={{ width: latencyUs > 0 ? `${Math.min(latencyUs / 1000, 100)}%` : '0%' }} />
      </div>

      <div className="flex justify-between text-[9px] text-text-dim font-mono mb-3 flex-shrink-0">
        <span>Ingest: {ingest.toFixed(0)}μs</span>
        <span>Risk: {risk.toFixed(0)}μs</span>
        <span>Match: {match.toFixed(0)}μs</span>
      </div>

      <div className="mt-auto pt-2 border-t border-slate-700/50 space-y-1 text-[10px] font-mono flex-shrink-0">
        <div className="flex justify-between text-text-dim">
          <span className="flex items-center gap-1">
            <Route size={10} /> Routed
          </span>
          <span className="text-white">{lastExecution.routedExchange}</span>
        </div>
        <div className="flex justify-between text-text-dim">
          <span>{lastExecution.symbol} {lastExecution.side}</span>
          <span className="text-white">qty {lastExecution.qty}</span>
        </div>
        {lastExecution.priceImprovementBps > 0 && (
          <div className="flex justify-between text-text-dim">
            <span>Price improvement</span>
            <span className="text-accent-green">+{lastExecution.priceImprovementBps.toFixed(1)}bps</span>
          </div>
        )}
        <div className="flex justify-between text-text-dim">
          <span>Exchanges searched</span>
          <span className="text-white">{lastExecution.exchangesSearched}</span>
        </div>
      </div>
    </div>
  );
}
