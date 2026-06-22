import React from 'react';
import { Activity, Shield, User, Globe } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';

export default function Header() {
  const systemHealth = useTerminalStore(s => s.systemHealth);
  
  return (
    <header className='bg-panel border-b border-border h-14 px-4 flex items-center justify-between z-40 select-none shadow-md backdrop-blur-sm bg-opacity-90'>
      <div className='flex items-center gap-4'>
        <div className='flex items-center gap-2'>
          <div className='w-6 h-6 rounded bg-accent-blue flex items-center justify-center shadow-lg shadow-blue-500/20'>
            <Globe size={14} className='text-white' />
          </div>
          <span className='font-bold text-lg tracking-tight text-white'>ROBIN<span className='text-accent-blue'>PRO</span></span>
        </div>
        <div className='h-4 w-px bg-border mx-2' />
        <div className='flex items-center gap-3 text-xs font-mono'>
          <span className='flex items-center gap-1.5 text-accent-green bg-accent-green-dim px-2 py-0.5 rounded border border-accent-green/20'>
            <div className='w-1.5 h-1.5 rounded-full bg-accent-green animate-pulse' />
            SYSTEM OPERATIONAL
          </span>
          <span className='flex items-center gap-1.5 text-text-secondary bg-hover px-2 py-0.5 rounded'>
            <Activity size={12} className='text-accent-purple' />
            LATENCY: {systemHealth.latencyNs}ns
          </span>
        </div>
      </div>
      <div className='flex items-center gap-3 text-xs'>
        <div className='flex items-center gap-2 px-3 py-1.5 rounded-md bg-hover border border-border text-text-secondary'>
          <Shield size={14} className='text-accent-amber' />
          <span>Risk Level: <span className='text-white font-bold'>BALANCED</span></span>
        </div>
        <div className='flex items-center gap-2 px-3 py-1.5 rounded-md bg-hover border border-border text-text-secondary'>
          <User size={14} className='text-accent-blue' />
          <span>Demo User</span>
        </div>
      </div>
    </header>
  );
}