import React, { useState } from 'react';
import { Clock, History, X } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';

export default function OrdersTable() {
  const { workingOrders, tradeHistory, cancelOrder } = useTerminalStore();
  const [tab, setTab] = useState<'working' | 'history'>('working');

  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-8 border-b border-border bg-card px-3 flex items-center justify-between'>
        <div className='flex items-center gap-2'>
          {tab === 'working' ? (
            <Clock size={14} className='text-accent-blue' />
          ) : (
            <History size={14} className='text-accent-purple' />
          )}
          <span className='text-xs font-bold text-white uppercase tracking-wider'>
            {tab === 'working' ? 'Working Orders' : 'Trade History'}
          </span>
        </div>
        <div className='flex rounded bg-hover p-0.5 text-[9px] font-bold'>
          <button
            onClick={() => setTab('working')}
            className={`px-2 py-0.5 rounded transition-all ${tab === 'working' ? 'bg-accent-blue text-white' : 'text-text-dim hover:text-white'}`}
          >
            Working ({workingOrders.length})
          </button>
          <button
            onClick={() => setTab('history')}
            className={`px-2 py-0.5 rounded transition-all ${tab === 'history' ? 'bg-accent-blue text-white' : 'text-text-dim hover:text-white'}`}
          >
            History ({tradeHistory.length})
          </button>
        </div>
      </div>

      <div className='flex-1 p-2 overflow-auto scrollbar'>
        {tab === 'working' ? (
          <table className='w-full text-left text-xs'>
            <thead className='text-[10px] text-text-dim uppercase border-b border-border font-mono'>
              <tr>
                <th className='pb-2 pl-2'>Symbol</th>
                <th className='pb-2'>Type</th>
                <th className='pb-2 text-right'>Qty</th>
                <th className='pb-2 text-right'>Price</th>
                <th className='pb-2 text-right pr-2'>Action</th>
              </tr>
            </thead>
            <tbody className='font-mono'>
              {workingOrders.length === 0 ? (
                <tr>
                  <td colSpan={5} className='text-center py-6 text-text-dim italic text-xs'>
                    No active working orders
                  </td>
                </tr>
              ) : (
                workingOrders.map((ord) => (
                  <tr key={ord.id} className='hover:bg-hover group border-b border-border/30 text-[11px]'>
                    <td className='py-2 pl-2'>
                      <span className='text-white font-bold'>{ord.symbol}</span>
                      <span className={`text-[9px] px-1 rounded ml-1 font-bold ${ord.side === 'BUY' ? 'text-accent-green bg-emerald-950/40' : 'text-accent-red bg-red-950/40'}`}>
                        {ord.side}
                      </span>
                    </td>
                    <td className='py-2'>
                      <span className='text-accent-blue text-[9px] font-bold bg-accent-blue/10 px-1 py-0.5 rounded'>
                        {ord.orderType}
                      </span>
                    </td>
                    <td className='py-2 text-right text-text-secondary'>{ord.qty}</td>
                    <td className='py-2 text-right text-white'>${ord.price.toFixed(2)}</td>
                    <td className='py-2 text-right pr-2'>
                      <button
                        onClick={() => cancelOrder(ord.id)}
                        className='text-accent-red hover:bg-accent-red/20 p-1 rounded transition-colors inline-flex items-center gap-0.5 text-[9px] font-bold'
                        title='Cancel Order'
                      >
                        <X size={12} /> Cancel
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        ) : (
          <table className='w-full text-left text-xs'>
            <thead className='text-[10px] text-text-dim uppercase border-b border-border font-mono'>
              <tr>
                <th className='pb-2 pl-2'>Symbol</th>
                <th className='pb-2'>Side</th>
                <th className='pb-2 text-right'>Qty</th>
                <th className='pb-2 text-right'>Fill Price</th>
                <th className='pb-2 text-right pr-2'>Venue</th>
              </tr>
            </thead>
            <tbody className='font-mono'>
              {tradeHistory.length === 0 ? (
                <tr>
                  <td colSpan={5} className='text-center py-6 text-text-dim italic text-xs'>
                    No executed trades recorded
                  </td>
                </tr>
              ) : (
                tradeHistory.map((t) => (
                  <tr key={t.id} className='hover:bg-hover group border-b border-border/30 text-[11px]'>
                    <td className='py-2 pl-2 text-white font-bold'>{t.symbol}</td>
                    <td className='py-2'>
                      <span className={`text-[9px] px-1 rounded font-bold ${t.side === 'BUY' ? 'text-accent-green bg-emerald-950/40' : 'text-accent-red bg-red-950/40'}`}>
                        {t.side}
                      </span>
                    </td>
                    <td className='py-2 text-right text-text-secondary'>{t.qty}</td>
                    <td className='py-2 text-right text-white'>${t.price.toFixed(2)}</td>
                    <td className='py-2 text-right pr-2 text-accent-blue text-[9px] font-semibold'>
                      {(t as any).routedExchange || 'Robin Pools'}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}