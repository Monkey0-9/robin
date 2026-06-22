import React from 'react';
import { Clock } from 'lucide-react';
export default function OrdersTable() {
  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-8 border-b border-border bg-card px-3 flex items-center gap-2'>
        <Clock size={14} className='text-accent-blue' />
        <span className='text-xs font-bold text-white uppercase tracking-wider'>Working Orders</span>
      </div>
      <div className='flex-1 p-2 overflow-auto flex items-center justify-center'>
        <span className='text-text-dim text-xs font-mono italic'>No active working orders</span>
      </div>
    </div>
  );
}