import React from 'react';
import { useTerminalStore } from '../store/useTerminalStore';
import { ShieldCheck, Zap } from 'lucide-react';

export default function BestPriceComparison() {
  const { selectedSymbol, sorQuotes, routingMode } = useTerminalStore();

  // Find best bid and best ask dynamically
  const bestBid = sorQuotes.length > 0 ? Math.max(...sorQuotes.map(q => q.bid)) : 0;
  const bestAsk = sorQuotes.length > 0 ? Math.min(...sorQuotes.map(q => q.ask)) : 0;

  // Format helper
  const formatPrice = (p: number) => {
    if (selectedSymbol === "EUR/USD") return p.toFixed(4);
    return p.toFixed(2);
  };

  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-8 border-b border-border bg-card px-3 flex items-center justify-between'>
        <div className='flex items-center gap-1.5'>
          <Zap size={13} className='text-accent-amber animate-pulse' />
          <span className='text-xs font-bold text-white uppercase tracking-wider'>Smart Order Router (SOR)</span>
        </div>
        <span className='text-[8px] font-mono font-bold text-accent-blue bg-accent-blue/15 px-1.5 py-0.5 rounded'>
          {routingMode === 'AUTO' ? 'AUTO-ROUTE ACTIVE' : `DIRECT ROUTING: ${routingMode}`}
        </span>
      </div>

      <div className='p-3 flex-1 flex flex-col justify-between overflow-auto gap-3'>
        <div className='space-y-1.5'>
          <div className='grid grid-cols-3 text-[9px] font-mono text-text-dim uppercase border-b border-border/30 pb-1 px-1'>
            <span>Exchange</span>
            <span className='text-right'>Bid (Sell)</span>
            <span className='text-right'>Ask (Buy)</span>
          </div>

          <div className='divide-y divide-border/20 font-mono text-xs'>
            {sorQuotes.length === 0 ? (
              <div className='text-center py-4 text-text-dim text-[10px]'>
                Connecting to SOR matrices...
              </div>
            ) : (
              sorQuotes.map((q) => {
                const isBestBid = q.bid === bestBid;
                const isBestAsk = q.ask === bestAsk;
                return (
                  <div key={q.exchange} className='grid grid-cols-3 py-1.5 px-1 hover:bg-hover rounded transition-colors items-center'>
                    <span className='font-bold text-white text-[10px]'>{q.exchange}</span>
                    <span className={`text-right text-[11px] font-semibold ${isBestBid ? 'text-accent-green bg-emerald-950/20 px-1.5 rounded' : 'text-text-secondary'}`}>
                      ${formatPrice(q.bid)}
                    </span>
                    <span className={`text-right text-[11px] font-semibold ${isBestAsk ? 'text-accent-red bg-rose-950/20 px-1.5 rounded' : 'text-text-secondary'}`}>
                      ${formatPrice(q.ask)}
                    </span>
                  </div>
                );
              })
            )}
          </div>
        </div>

        {/* Lower Banner explaining routing value */}
        <div className='bg-void/40 border border-border/40 rounded p-2 text-[10px] leading-relaxed text-text-secondary space-y-1.5 font-mono'>
          <div className='flex items-center gap-1.5 text-accent-blue font-bold text-[10px] uppercase'>
            <ShieldCheck size={13} />
            <span>Best Price algorithm active</span>
          </div>
          <p className='text-[9px]'>
            Scanning 30 liquidity venues. Dynamic best execution spreads are monitored in real-time. Average execution savings: <strong>+1.8 to +4.5 bps</strong>.
          </p>
        </div>
      </div>
    </div>
  );
}
