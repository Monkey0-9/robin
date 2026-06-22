const fs = require('fs');

const comps = {
  Header: `import React from 'react';
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
}`,
  OrderBook: `import React from 'react';
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
}`,
  OrderEntry: `import React, { useState } from 'react';
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
            className={\`flex-1 py-1 text-xs font-bold rounded shadow-md transition-colors \${side === 'BUY' ? 'bg-accent-green text-white' : 'text-text-dim hover:text-white'}\`}>BUY</button>
          <button 
            onClick={() => setSide('SELL')}
            className={\`flex-1 py-1 text-xs font-bold rounded shadow-md transition-colors \${side === 'SELL' ? 'bg-accent-red text-white' : 'text-text-dim hover:text-white'}\`}>SELL</button>
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
            <span className='font-mono'>$\${balance.toFixed(2)}</span>
          </div>
          <div className='flex justify-between text-[10px] text-text-secondary'>
            <span>Margin Req:</span>
            <span className='font-mono text-white'>$\${marginReq.toFixed(2)}</span>
          </div>
          <button 
            onClick={handleSubmit}
            className={\`w-full py-2 text-white font-bold text-xs rounded shadow-lg transition-all flex items-center justify-center gap-2 \${side === 'BUY' ? 'bg-accent-green hover:bg-emerald-500 shadow-emerald-500/20' : 'bg-accent-red hover:bg-rose-500 shadow-rose-500/20'}\`}>
            <Send size={14} /> SUBMIT {side} ORDER
          </button>
        </div>
      </div>
    </div>
  );
}`,
  PositionsTable: `import React from 'react';
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
                  <span className={\`text-[9px] px-1 rounded ml-1 \${p.side === 'LONG' ? 'text-accent-green bg-accent-green-dim' : 'text-accent-red bg-accent-red-dim'}\`}>{p.side}</span>
                </td>
                <td className='py-2 text-text-secondary'>{p.size}</td>
                <td className='py-2 text-text-secondary'>{p.entryPrice.toFixed(2)}</td>
                <td className={\`py-2 text-right pr-2 \${p.unrealizedPnL >= 0 ? 'text-accent-green' : 'text-accent-red'}\`}>
                  {p.unrealizedPnL >= 0 ? '+' : ''}\${p.unrealizedPnL.toFixed(2)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}`,
  RiskMetrics: `import React from 'react';
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
          <div className='flex justify-between text-xs'><span className='text-text-secondary'>Net Equity</span><span className='font-mono font-bold text-accent-green'>$\${equity.toFixed(2)}</span></div>
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
}`
};

for (const [name, content] of Object.entries(comps)) {
  fs.writeFileSync('src/components/' + name + '.tsx', content, 'utf8');
}
