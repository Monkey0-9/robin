import React from 'react';
import { Briefcase } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';

export default function PositionsTable() {
  const positions = useTerminalStore(s => s.positions);

  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-8 border-b border-border bg-card px-3 flex items-center gap-2'>
        <Briefcase size={14} className='text-accent-purple' />
        <span className='text-xs font-bold text-white uppercase tracking-wider'>Open Positions</span>
      </div>
      <div className='flex-1 p-2 overflow-auto'>
        <table className='w-full text-left text-xs'>
          <thead className='text-[10px] text-text-dim uppercase border-b border-border'>
            <tr><th className='pb-2 pl-2'>Symbol</th><th className='pb-2'>Size</th><th className='pb-2'>Entry</th><th className='pb-2 text-right pr-2'>PnL</th></tr>
          </thead>
          <tbody className='font-mono'>
            {positions.length === 0 && (
              <tr><td colSpan={4} className='text-center py-4 text-text-dim italic'>No open positions</td></tr>
            )}
            {positions.map(p => (
              <tr key={p.id} className='hover:bg-hover group cursor-pointer border-b border-border/30'>
                <td className='py-2 pl-2'>
                  <span className='text-white font-bold'>{p.symbol}</span> 
                  <span className={`text-[9px] px-1 rounded ml-1 ${p.side === 'LONG' ? 'text-accent-green bg-accent-green-dim' : 'text-accent-red bg-accent-red-dim'}`}>{p.side}</span>
                </td>
                <td className='py-2 text-text-secondary'>{p.size}</td>
                <td className='py-2 text-text-secondary'>{p.entryPrice.toFixed(2)}</td>
                <td className={`py-2 text-right pr-2 ${p.unrealizedPnL >= 0 ? 'text-accent-green' : 'text-accent-red'}`}>
                  {p.unrealizedPnL >= 0 ? '+' : ''}${p.unrealizedPnL.toFixed(2)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}