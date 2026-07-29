import React, { useState } from 'react';
import { Send } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';
import SkeletonLoader from './SkeletonLoader';

export default function OrderEntry() {
  const { selectedSymbol, assets, submitOrder, balance, routingMode, setRoutingMode } = useTerminalStore();
  const currentPrice = assets.find(a => a.symbol === selectedSymbol)?.currentPrice || 0;
  
  const [side, setSide] = useState<'BUY' | 'SELL'>('BUY');
  const [orderType, setOrderType] = useState<'MARKET' | 'LIMIT' | 'STOP'>('MARKET');
  const [sizeStr, setSizeStr] = useState('1.0');
  const [customPriceStr, setCustomPriceStr] = useState('');

  const size = parseFloat(sizeStr) || 0;
  const effectivePrice = orderType === 'MARKET' ? currentPrice : (parseFloat(customPriceStr) || currentPrice);
  const marginReq = effectivePrice * size * 0.05;

  const handleSubmit = () => {
    submitOrder(selectedSymbol, side, effectivePrice, size, orderType === 'MARKET', orderType);
  };

  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-8 border-b border-border bg-card px-3 flex items-center justify-between'>
        <span className='text-xs font-bold text-white uppercase tracking-wider'>Order Ticket</span>
        <span className='text-[9px] font-mono text-accent-blue font-bold px-1.5 py-0.5 rounded bg-accent-blue/10'>
          {orderType} ORDER
        </span>
      </div>
      {(!currentPrice || currentPrice === 0) ? (
        <div className="flex-1 p-3">
          <SkeletonLoader lines={5} height="h-full" />
        </div>
      ) : (
      <div className='p-3 flex flex-col gap-3 flex-1 overflow-auto'>
        <div className='flex rounded bg-hover p-1'>
          <button 
            onClick={() => setSide('BUY')}
            className={`flex-1 py-1 text-xs font-bold rounded shadow-md transition-colors ${side === 'BUY' ? 'bg-accent-green text-white' : 'text-text-dim hover:text-white'}`}>BUY</button>
          <button 
            onClick={() => setSide('SELL')}
            className={`flex-1 py-1 text-xs font-bold rounded shadow-md transition-colors ${side === 'SELL' ? 'bg-accent-red text-white' : 'text-text-dim hover:text-white'}`}>SELL</button>
        </div>
        <div className='flex gap-1 text-[10px] font-bold uppercase'>
          <button 
            onClick={() => {
              setOrderType('LIMIT');
              if (!customPriceStr) setCustomPriceStr(currentPrice > 0 ? currentPrice.toFixed(2) : '100.00');
            }}
            className={`flex-1 py-1 border rounded transition-colors ${orderType === 'LIMIT' ? 'border-accent-blue text-accent-blue bg-accent-blue/10' : 'border-border text-text-dim hover:border-text-secondary'}`}>
            Limit
          </button>
          <button 
            onClick={() => setOrderType('MARKET')}
            className={`flex-1 py-1 border rounded transition-colors ${orderType === 'MARKET' ? 'border-accent-blue text-accent-blue bg-accent-blue/10' : 'border-border text-text-dim hover:border-text-secondary'}`}>
            Market
          </button>
          <button 
            onClick={() => {
              setOrderType('STOP');
              if (!customPriceStr) setCustomPriceStr(currentPrice > 0 ? currentPrice.toFixed(2) : '100.00');
            }}
            className={`flex-1 py-1 border rounded transition-colors ${orderType === 'STOP' ? 'border-accent-blue text-accent-blue bg-accent-blue/10' : 'border-border text-text-dim hover:border-text-secondary'}`}>
            Stop
          </button>
        </div>
        <div className='space-y-2'>
          <div className='flex flex-col gap-1'>
            <div className='flex justify-between text-[10px] uppercase text-text-dim'>
              <span>Price (USD)</span>
              <span>{orderType === 'MARKET' ? 'Market Price' : 'Target Price'}</span>
            </div>
            {orderType === 'MARKET' ? (
              <input type='text' disabled className='bg-card border border-border rounded px-2 py-1.5 text-sm font-mono text-white opacity-60 cursor-not-allowed' value={currentPrice > 0 ? currentPrice.toFixed(2) : '0.00'} />
            ) : (
              <input 
                type='number' 
                step='0.01' 
                className='bg-card border border-accent-blue/60 rounded px-2 py-1.5 text-sm font-mono text-white focus:outline-none focus:border-accent-blue' 
                value={customPriceStr || (currentPrice > 0 ? currentPrice.toFixed(2) : '')} 
                onChange={e => setCustomPriceStr(e.target.value)} 
              />
            )}
          </div>
          <div className='flex flex-col gap-1'>
            <label className='text-[10px] text-text-dim uppercase'>Size</label>
            <input type='number' step='0.1' className='bg-card border border-border rounded px-2 py-1.5 text-sm font-mono text-white focus:outline-none focus:border-accent-blue' value={sizeStr} onChange={e => setSizeStr(e.target.value)} />
          </div>
          <div className='flex flex-col gap-1'>
            <label className='text-[10px] text-text-dim uppercase'>Exchange Routing</label>
            <select
              value={routingMode}
              onChange={e => setRoutingMode(e.target.value)}
              className='bg-card border border-border rounded px-2 py-1.5 text-xs font-mono text-white focus:outline-none focus:border-accent-blue cursor-pointer'
            >
              <option value="AUTO">Best Price (Auto-Route)</option>
              <option value="NYSE">NYSE</option>
              <option value="NASDAQ">NASDAQ</option>
              <option value="Xetra">Xetra</option>
              <option value="Tradegate">Tradegate</option>
              <option value="LSE">LSE</option>
              <option value="Robin Pools">Robin Pools (Dark Pool)</option>
            </select>
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
            <Send size={14} /> SUBMIT {side} {orderType} ORDER
          </button>
        </div>
      </div>
      )}
    </div>
  );
}