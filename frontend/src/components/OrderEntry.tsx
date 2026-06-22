import React, { useState } from 'react';
import { Send } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';

export default function OrderEntry() {
  const { selectedSymbol, assets, submitOrder, balance } = useTerminalStore();
  const currentPrice = assets.find(a => a.symbol === selectedSymbol)?.currentPrice || 0;
  
  const [side, setSide] = useState<'BUY' | 'SELL'>('BUY');
  const [sizeStr, setSizeStr] = useState('1.0');
  const size = parseFloat(sizeStr) || 0;
  const marginReq = currentPrice * size * 0.05;

  const handleSubmit = () => {
    submitOrder(selectedSymbol, side, currentPrice, size, true);
  };

  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-8 border-b border-border bg-card px-3 flex items-center'>
        <span className='text-xs font-bold text-white uppercase tracking-wider'>Order Ticket</span>
      </div>
      <div className='p-3 flex flex-col gap-3 flex-1 overflow-auto'>
        <div className='flex rounded bg-hover p-1'>
          <button 
            onClick={() => setSide('BUY')}
            className={`flex-1 py-1 text-xs font-bold rounded shadow-md transition-colors ${side === 'BUY' ? 'bg-accent-green text-white' : 'text-text-dim hover:text-white'}`}>BUY</button>
          <button 
            onClick={() => setSide('SELL')}
            className={`flex-1 py-1 text-xs font-bold rounded shadow-md transition-colors ${side === 'SELL' ? 'bg-accent-red text-white' : 'text-text-dim hover:text-white'}`}>SELL</button>
        </div>
        <div className='flex gap-2 text-[10px] font-bold uppercase'>
          <button className='flex-1 py-1 border border-border text-text-dim rounded hover:border-text-secondary'>Limit</button>
          <button className='flex-1 py-1 border border-accent-blue text-accent-blue rounded'>Market</button>
          <button className='flex-1 py-1 border border-border text-text-dim rounded hover:border-text-secondary'>Stop</button>
        </div>
        <div className='space-y-2'>
          <div className='flex flex-col gap-1'>
            <label className='text-[10px] text-text-dim uppercase'>Price (USD)</label>
            <input type='text' disabled className='bg-card border border-border rounded px-2 py-1.5 text-sm font-mono text-white opacity-50' value={currentPrice.toFixed(2)} />
          </div>
          <div className='flex flex-col gap-1'>
            <label className='text-[10px] text-text-dim uppercase'>Size</label>
            <input type='number' step='0.1' className='bg-card border border-border rounded px-2 py-1.5 text-sm font-mono text-white focus:outline-none focus:border-accent-blue' value={sizeStr} onChange={e => setSizeStr(e.target.value)} />
          </div>
        </div>
        <div className='mt-auto space-y-2'>
          <div className='flex justify-between text-[10px] text-text-secondary'>
            <span>Avail. Balance:</span>
            <span className='font-mono'>${balance.toFixed(2)}</span>
          </div>
          <div className='flex justify-between text-[10px] text-text-secondary'>
            <span>Margin Req:</span>
            <span className='font-mono text-white'>${marginReq.toFixed(2)}</span>
          </div>
          <button 
            onClick={handleSubmit}
            className={`w-full py-2 text-white font-bold text-xs rounded shadow-lg transition-all flex items-center justify-center gap-2 ${side === 'BUY' ? 'bg-accent-green hover:bg-emerald-500 shadow-emerald-500/20' : 'bg-accent-red hover:bg-rose-500 shadow-rose-500/20'}`}>
            <Send size={14} /> SUBMIT {side} ORDER
          </button>
        </div>
      </div>
    </div>
  );
}