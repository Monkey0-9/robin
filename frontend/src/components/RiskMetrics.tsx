import React from 'react';
import { ShieldAlert } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';

export default function RiskMetrics() {
  const { marginUtilization, equity, balance, systemHealth } = useTerminalStore();

  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-8 border-b border-border bg-card px-3 flex items-center gap-2'>
        <ShieldAlert size={14} className='text-accent-amber' />
        <span className='text-xs font-bold text-white uppercase tracking-wider'>Risk Diagnostics</span>
      </div>
      <div className='flex-1 p-4 flex flex-col gap-4'>
        <div className='space-y-1'>
          <div className='flex justify-between text-xs'><span className='text-text-secondary'>Margin Utilization</span><span className='font-mono font-bold text-white'>{marginUtilization.toFixed(2)}%</span></div>
          <div className='h-1.5 w-full bg-hover rounded-full overflow-hidden'><div className='h-full bg-accent-blue rounded-full' style={{width: Math.min(marginUtilization, 100) + '%'}}></div></div>
        </div>
        <div className='space-y-1'>
          <div className='flex justify-between text-xs'><span className='text-text-secondary'>Net Equity</span><span className='font-mono font-bold text-accent-green'>${equity.toFixed(2)}</span></div>
        </div>
        <div className='mt-auto bg-card border border-border p-3 rounded'>
          <div className='text-[10px] text-text-dim uppercase mb-1 font-bold'>Gateway Health</div>
          <div className='text-xs font-mono text-white flex flex-col gap-1'>
            <div className='flex justify-between'><span>Healthy Services:</span><span className='text-accent-green'>{systemHealth.healthy}</span></div>
            <div className='flex justify-between'><span>Failed Services:</span><span className={systemHealth.failed > 0 ? 'text-accent-red' : 'text-accent-green'}>{systemHealth.failed}</span></div>
            <div className='flex justify-between'><span>Gateway Latency:</span><span>{systemHealth.latencyNs > 0 ? (systemHealth.latencyNs / 1000000).toFixed(2) + 'ms' : 'Offline'}</span></div>
          </div>
        </div>
      </div>
    </div>
  );
}