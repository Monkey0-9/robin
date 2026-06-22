import React from 'react';
import { BrainCircuit } from 'lucide-react';
export default function AIPanel() {
  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-8 border-b border-border bg-card px-3 flex items-center gap-2'>
        <BrainCircuit size={14} className='text-accent-purple' />
        <span className='text-xs font-bold text-white uppercase tracking-wider'>AI Signal Inference</span>
      </div>
      <div className='flex-1 p-4 flex flex-col gap-4'>
        <div className='bg-hover border border-border rounded p-3'>
          <div className='text-xs font-bold text-white mb-2 flex items-center gap-2'><span className='w-2 h-2 rounded-full bg-accent-purple animate-pulse'></span> BTC Volatility Breakout</div>
          <p className='text-xs text-text-secondary leading-relaxed'>Deep learning model identifies a 78% probability of upward volatility expansion in the next 4 hours based on order book imbalance and options flow.</p>
          <div className='mt-3 flex justify-between items-center'>
            <span className='text-[10px] px-2 py-1 bg-accent-purple/20 text-accent-purple rounded font-mono font-bold'>CONFIDENCE: HIGH</span>
            <button className='text-xs text-accent-blue hover:underline'>View Setup</button>
          </div>
        </div>
      </div>
    </div>
  );
}