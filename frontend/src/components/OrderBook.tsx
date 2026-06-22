import React from 'react';
import { List } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';

export default function OrderBook() {
  const { orderBook, assets, selectedSymbol } = useTerminalStore();
  const currentPrice = assets.find(a => a.symbol === selectedSymbol)?.currentPrice || 0;

  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-8 border-b border-border bg-card px-3 flex items-center gap-2'>
        <List size={14} className='text-accent-amber' />
        <span className='text-xs font-bold text-white uppercase tracking-wider'>L2 Order Book</span>
      </div>
      <div className='flex-1 flex flex-col p-2 text-xs font-mono overflow-hidden'>
        <div className='grid grid-cols-3 text-text-dim px-2 mb-1 uppercase text-[9px] font-bold'>
          <span>Price</span><span className='text-right'>Size</span><span className='text-right'>Total</span>
        </div>
        <div className='flex-1 flex flex-col justify-end gap-0.5 mb-1'>
          {[...orderBook.asks].reverse().map((ask, i) => (
            <div key={i} className='grid grid-cols-3 px-2 py-0.5 hover:bg-hover rounded relative overflow-hidden group cursor-pointer'>
              <div className='absolute right-0 top-0 bottom-0 bg-accent-red-dim opacity-20' style={{width: Math.min((ask.total / 10) * 100, 100) + '%'}}></div>
              <span className='text-accent-red z-10'>{ask.price.toFixed(2)}</span>
              <span className='text-right text-text-secondary z-10'>{ask.size.toFixed(2)}</span>
              <span className='text-right text-white z-10'>{ask.total.toFixed(2)}</span>
            </div>
          ))}
        </div>
        <div className='py-2 text-center border-y border-border my-1 flex justify-between items-center px-4 bg-hover/50 rounded'>
          <span className='text-accent-green font-bold text-sm'>{currentPrice.toFixed(2)}</span>
          <span className='text-text-dim'>Spread: $1.00</span>
        </div>
        <div className='flex-1 flex flex-col gap-0.5 mt-1'>
          {orderBook.bids.map((bid, i) => (
            <div key={i} className='grid grid-cols-3 px-2 py-0.5 hover:bg-hover rounded relative overflow-hidden group cursor-pointer'>
              <div className='absolute right-0 top-0 bottom-0 bg-accent-green-dim opacity-20' style={{width: Math.min((bid.total / 10) * 100, 100) + '%'}}></div>
              <span className='text-accent-green z-10'>{bid.price.toFixed(2)}</span>
              <span className='text-right text-text-secondary z-10'>{bid.size.toFixed(2)}</span>
              <span className='text-right text-white z-10'>{bid.total.toFixed(2)}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}