// frontend/src/components/OptionsChain.tsx
import React, { useState } from 'react';

interface OptionContract {
  strike: number;
  callBid: number;
  callAsk: number;
  putBid: number;
  putAsk: number;
  volume: number;
  expiry: string;
}

export default function OptionsChain() {
  const [expiry, setExpiry] = useState('2026-09-18');
  
  const mockContracts: OptionContract[] = [
    { strike: 48000, callBid: 3200, callAsk: 3250, putBid: 120, putAsk: 130, volume: 450, expiry: '2026-09-18' },
    { strike: 49000, callBid: 2400, callAsk: 2450, putBid: 280, putAsk: 295, volume: 820, expiry: '2026-09-18' },
    { strike: 50000, callBid: 1720, callAsk: 1750, putBid: 560, putAsk: 580, volume: 1450, expiry: '2026-09-18' },
    { strike: 51000, callBid: 1150, callAsk: 1180, putBid: 980, putAsk: 1010, volume: 910, expiry: '2026-09-18' },
    { strike: 52000, callBid: 710, callAsk: 730, putBid: 1540, putAsk: 1570, volume: 620, expiry: '2026-09-18' },
  ];

  return (
    <div className="bg-panel border border-border rounded-lg flex flex-col h-full min-h-0 select-none">
      <div className="bg-card px-4 py-2 border-b border-border flex items-center justify-between">
        <span className="font-bold text-xs uppercase tracking-wider text-white">Options Chain</span>
        <select 
          value={expiry} 
          onChange={(e) => setExpiry(e.target.value)}
          className="bg-bg-base border border-border rounded text-[10px] px-2 py-0.5 text-text-secondary font-mono"
        >
          <option value="2026-09-18">18 SEP 2026 (Monthly)</option>
          <option value="2026-10-16">16 OCT 2026 (Monthly)</option>
        </select>
      </div>

      <div className="p-3 flex-1 overflow-auto scrollbar">
        <table className="w-full text-left text-[11px] border-collapse font-mono">
          <thead>
            <tr className="border-b border-border/60 text-text-dim text-[9px] uppercase">
              <th className="py-1">Call Bid/Ask</th>
              <th className="py-1 text-center">Strike</th>
              <th className="py-1 text-right">Put Bid/Ask</th>
              <th className="py-1 text-right">Vol</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border/20">
            {mockContracts.map((c, i) => (
              <tr key={i} className="hover:bg-accent-blue-dim/10">
                <td className="py-2 text-accent-green">
                  {c.callBid.toFixed(0)} / {c.callAsk.toFixed(0)}
                </td>
                <td className="py-2 text-center font-bold text-white bg-bg-base/40">
                  {c.strike.toLocaleString()}
                </td>
                <td className="py-2 text-right text-accent-red">
                  {c.putBid.toFixed(0)} / {c.putAsk.toFixed(0)}
                </td>
                <td className="py-2 text-right text-text-secondary">{c.volume}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
