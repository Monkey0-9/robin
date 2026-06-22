import React from 'react';
import { LineChart, Maximize2, Settings, Download } from 'lucide-react';
export default function TradingViewChart() {
  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-10 border-b border-border bg-card px-3 flex items-center justify-between'>
        <div className='flex items-center gap-2'>
          <LineChart size={14} className='text-accent-blue' />
          <span className='text-xs font-bold text-white uppercase tracking-wider'>Advanced Chart</span>
          <div className='flex gap-1 ml-4'>
            {['1m', '5m', '15m', '1H', '4H', '1D'].map(t => (
              <button key={t} className='px-2 py-0.5 text-[10px] rounded hover:bg-hover text-text-secondary hover:text-white transition-colors'>{t}</button>
            ))}
          </div>
        </div>
        <div className='flex gap-2 text-text-dim'>
          <button className='hover:text-white transition-colors'><Settings size={14} /></button>
          <button className='hover:text-white transition-colors'><Download size={14} /></button>
          <button className='hover:text-white transition-colors'><Maximize2 size={14} /></button>
        </div>
      </div>
      <div className='flex-1 relative bg-[#0a0a0c] bg-opacity-50 overflow-hidden flex flex-col justify-center items-center'>
        <div className='absolute inset-0 opacity-10' style={{backgroundImage: 'linear-gradient(#26262c 1px, transparent 1px), linear-gradient(90deg, #26262c 1px, transparent 1px)', backgroundSize: '20px 20px'}}></div>
        <LineChart size={48} className='text-border-light mb-4' />
        <span className='text-text-dim text-sm font-mono z-10'>Interactive Chart Loading...</span>
      </div>
    </div>
  );
}