import React from 'react';
import { useTerminalStore } from '../store/useTerminalStore';
import { LayoutGrid } from 'lucide-react';

export default function Heatmap() {
  const { heatmapSectors, setSelectedSymbol } = useTerminalStore();

  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-10 border-b border-border bg-card px-3 flex items-center justify-between'>
        <div className='flex items-center gap-2'>
          <LayoutGrid size={14} className='text-accent-blue' />
          <span className='text-xs font-bold text-white uppercase tracking-wider'>Market Heatmap (Sector Exposure)</span>
        </div>
      </div>

      <div className='flex-1 overflow-auto p-4 flex flex-col gap-4 bg-[#07070a]'>
        {heatmapSectors.length === 0 ? (
          <div className='text-center py-8 text-text-dim text-xs font-mono'>
            Loading sector metrics...
          </div>
        ) : (
          heatmapSectors.map((sector) => (
            <div key={sector.sector_name} className='space-y-1.5'>
              <div className='text-[10px] uppercase text-text-dim font-bold tracking-wider'>
                {sector.sector_name}
              </div>
              <div className='grid grid-cols-2 md:grid-cols-4 gap-2'>
                {sector.nodes.map((node: any) => {
                  const isPositive = node.change >= 0;
                  // Color strength based on change
                  let bgClass = 'bg-neutral-800';
                  let borderClass = 'border-neutral-700';
                  let textClass = 'text-white';
                  
                  if (node.change > 2.0) {
                    bgClass = 'bg-emerald-950/80 hover:bg-emerald-900';
                    borderClass = 'border-emerald-800/80';
                    textClass = 'text-emerald-400';
                  } else if (node.change > 0.0) {
                    bgClass = 'bg-emerald-950/40 hover:bg-emerald-900/60';
                    borderClass = 'border-emerald-900/60';
                    textClass = 'text-emerald-500';
                  } else if (node.change < -2.0) {
                    bgClass = 'bg-red-950/80 hover:bg-red-900';
                    borderClass = 'border-red-800/80';
                    textClass = 'text-red-400';
                  } else if (node.change < 0.0) {
                    bgClass = 'bg-red-950/40 hover:bg-red-900/60';
                    borderClass = 'border-red-900/60';
                    textClass = 'text-red-500';
                  }

                  return (
                    <div
                      key={node.name}
                      onClick={() => {
                        const sym = node.name;
                        setSelectedSymbol(sym);
                      }}
                      className={`border rounded p-3 flex flex-col justify-between h-20 transition-all cursor-pointer select-none ${bgClass} ${borderClass}`}
                    >
                      <div className='flex justify-between items-start'>
                        <span className='font-bold text-xs text-white'>{node.name}</span>
                        <span className={`text-[9px] font-mono font-bold ${textClass}`}>
                          {isPositive ? '+' : ''}{node.change.toFixed(2)}%
                        </span>
                      </div>
                      <div className='flex justify-between items-end text-[9px] font-mono text-text-dim'>
                        <span>MCap</span>
                        <span className='text-white font-semibold'>${node.value.toFixed(0)}B</span>
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          ))
        )}
      </div>
    </div>
  );
}
