import React from 'react';
import { Zap } from 'lucide-react';

export default function StrategyRoller() {
  return (
    <div className="bg-panel border border-border rounded-lg flex flex-col h-full min-h-0 select-none shadow-lg">
      <div className="bg-card px-4 py-2 border-b border-border flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Zap size={14} className="text-accent-amber" />
          <span className="font-bold text-xs uppercase tracking-wider text-white">Options Strategy Roller</span>
        </div>
      </div>

      <div className="p-4 flex-1 flex flex-col items-center justify-center gap-2">
        <span className="text-text-dim text-xs">Options strategies require a live options feed</span>
        <span className="text-[10px] text-text-dim/60 font-mono">No synthetic premiums are displayed or executed</span>
      </div>
    </div>
  );
}
