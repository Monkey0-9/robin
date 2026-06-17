// frontend/src/components/StrategyRoller.tsx
import React, { useState } from 'react';

export default function StrategyRoller() {
  const [spreadType, setSpreadType] = useState('Iron Condor');
  
  return (
    <div className="bg-panel border border-border rounded-lg flex flex-col h-full min-h-0 select-none">
      <div className="bg-card px-4 py-2 border-b border-border flex items-center justify-between">
        <span className="font-bold text-xs uppercase tracking-wider text-white">Options Strategy Roller</span>
        <select 
          value={spreadType} 
          onChange={(e) => setSpreadType(e.target.value)}
          className="bg-bg-base border border-border rounded text-[10px] px-2 py-0.5 text-text-secondary font-mono"
        >
          <option value="Iron Condor">Iron Condor</option>
          <option value="Bull Put Spread">Bull Put Spread</option>
          <option value="Bear Call Spread">Bear Call Spread</option>
          <option value="Straddle">Long Straddle</option>
        </select>
      </div>

      <div className="p-4 flex-1 flex flex-col justify-between font-mono text-xs space-y-3">
        <div className="space-y-2">
          <div className="flex justify-between border-b border-border/40 pb-1">
            <span className="text-text-secondary">Selected Strategy:</span>
            <span className="text-accent-blue font-bold">{spreadType}</span>
          </div>
          <div className="flex justify-between border-b border-border/40 pb-1">
            <span className="text-text-secondary">Leg 1 (Long Put):</span>
            <span className="text-white">Strike 48,000 | Premium $125</span>
          </div>
          <div className="flex justify-between border-b border-border/40 pb-1">
            <span className="text-text-secondary">Leg 2 (Short Put):</span>
            <span className="text-white">Strike 49,000 | Premium $285</span>
          </div>
          <div className="flex justify-between border-b border-border/40 pb-1">
            <span className="text-text-secondary">Leg 3 (Short Call):</span>
            <span className="text-white">Strike 51,000 | Premium $1,165</span>
          </div>
          <div className="flex justify-between border-b border-border/40 pb-1">
            <span className="text-text-secondary">Leg 4 (Long Call):</span>
            <span className="text-white">Strike 52,000 | Premium $720</span>
          </div>
        </div>

        <div className="bg-bg-base/60 p-2.5 rounded border border-border/60 space-y-1.5">
          <div className="flex justify-between text-[11px]">
            <span className="text-text-dim">Net Credit Received:</span>
            <span className="text-accent-green font-bold">$605.00</span>
          </div>
          <div className="flex justify-between text-[11px]">
            <span className="text-text-dim">Max Potential Loss:</span>
            <span className="text-accent-red font-bold">-$395.00</span>
          </div>
        </div>

        <button className="w-full bg-accent-blue hover:bg-blue-600 text-white font-bold text-xs py-1.5 rounded transition-all">
          Execute Spread Strategy
        </button>
      </div>
    </div>
  );
}
