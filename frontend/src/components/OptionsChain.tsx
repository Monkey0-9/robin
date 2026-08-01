import React from 'react';
import { Layers } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';

export default function OptionsChain() {
  const { selectedSymbol } = useTerminalStore();

  return (
    <div className="bg-panel border border-border rounded-lg flex flex-col h-full min-h-0 select-none shadow-lg">
      <div className="bg-card px-4 py-2 border-b border-border flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Layers size={14} className="text-accent-purple" />
          <span className="font-bold text-xs uppercase tracking-wider text-white">Options Chain ({selectedSymbol})</span>
        </div>
      </div>

      <div className="p-4 flex-1 flex flex-col items-center justify-center gap-2">
        <span className="text-text-dim text-xs">Options chain requires a live market-data feed</span>
        <span className="text-[10px] text-text-dim/60 font-mono">No synthetic strikes are displayed</span>
      </div>
    </div>
  );
}
