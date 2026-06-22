const fs = require('fs');
const comps = {
  Header: `import React from 'react';
import { Activity, Shield, User, Globe } from 'lucide-react';
export default function Header() {
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
            LATENCY: 65ns
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
  TradingViewChart: `import React from 'react';
import { LineChart, Maximize2, Settings, Download } from 'lucide-react';
export default function TradingViewChart() {
  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-10 border-b border-border bg-card px-3 flex items-center justify-between'>
        <div className='flex items-center gap-2'>
          <LineChart size={14} className='text-accent-blue' />
          <span className='text-xs font-bold text-white uppercase tracking-wider'>Advanced Chart</span>
          <div className='flex gap-1 ml-4'>
            {['1m', '5m', '15m', '1H', '4H', '1D'].map(t => (
              <button key={t} className='px-2 py-0.5 text-[10px] rounded hover:bg-hover text-text-secondary hover:text-white transition-colors'>{t}</button>
            ))}
          </div>
        </div>
        <div className='flex gap-2 text-text-dim'>
          <button className='hover:text-white transition-colors'><Settings size={14} /></button>
          <button className='hover:text-white transition-colors'><Download size={14} /></button>
          <button className='hover:text-white transition-colors'><Maximize2 size={14} /></button>
        </div>
      </div>
      <div className='flex-1 relative bg-[#0a0a0c] bg-opacity-50 overflow-hidden flex flex-col justify-center items-center'>
        <div className='absolute inset-0 opacity-10' style={{backgroundImage: 'linear-gradient(#26262c 1px, transparent 1px), linear-gradient(90deg, #26262c 1px, transparent 1px)', backgroundSize: '20px 20px'}}></div>
        <LineChart size={48} className='text-border-light mb-4' />
        <span className='text-text-dim text-sm font-mono z-10'>Interactive Chart Loading...</span>
      </div>
    </div>
  );
}`,
  OrderBook: `import React from 'react';
import { List } from 'lucide-react';
export default function OrderBook() {
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
          {[...Array(8)].map((_, i) => (
            <div key={i} className='grid grid-cols-3 px-2 py-0.5 hover:bg-hover rounded relative overflow-hidden group cursor-pointer'>
              <div className='absolute right-0 top-0 bottom-0 bg-accent-red-dim opacity-20' style={{width: ((i+2)*10)+'%'}}></div>
              <span className='text-accent-red z-10'>64,4{80 - i*2}.50</span>
              <span className='text-right text-text-secondary z-10'>0.{8 - i}4</span>
              <span className='text-right text-white z-10'>{(1.5 + i*0.2).toFixed(2)}</span>
            </div>
          ))}
        </div>
        <div className='py-2 text-center border-y border-border my-1 flex justify-between items-center px-4 bg-hover/50 rounded'>
          <span className='text-accent-green font-bold text-sm'>64,466.14</span>
          <span className='text-text-dim'>Spread: $0.50</span>
        </div>
        <div className='flex-1 flex flex-col gap-0.5 mt-1'>
          {[...Array(8)].map((_, i) => (
            <div key={i} className='grid grid-cols-3 px-2 py-0.5 hover:bg-hover rounded relative overflow-hidden group cursor-pointer'>
              <div className='absolute right-0 top-0 bottom-0 bg-accent-green-dim opacity-20' style={{width: ((10-i)*10)+'%'}}></div>
              <span className='text-accent-green z-10'>64,4{60 - i*2}.00</span>
              <span className='text-right text-text-secondary z-10'>0.{2 + i}1</span>
              <span className='text-right text-white z-10'>{(0.5 + i*0.3).toFixed(2)}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}`,
  OrderEntry: `import React from 'react';
import { Send } from 'lucide-react';
export default function OrderEntry() {
  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-8 border-b border-border bg-card px-3 flex items-center'>
        <span className='text-xs font-bold text-white uppercase tracking-wider'>Order Ticket</span>
      </div>
      <div className='p-3 flex flex-col gap-3 flex-1 overflow-auto'>
        <div className='flex rounded bg-hover p-1'>
          <button className='flex-1 py-1 text-xs font-bold rounded bg-accent-green text-white shadow-md'>BUY</button>
          <button className='flex-1 py-1 text-xs font-bold rounded text-text-dim hover:text-white transition-colors'>SELL</button>
        </div>
        <div className='flex gap-2 text-[10px] font-bold uppercase'>
          <button className='flex-1 py-1 border border-accent-blue text-accent-blue rounded'>Limit</button>
          <button className='flex-1 py-1 border border-border text-text-dim rounded hover:border-text-secondary'>Market</button>
          <button className='flex-1 py-1 border border-border text-text-dim rounded hover:border-text-secondary'>Stop</button>
        </div>
        <div className='space-y-2'>
          <div className='flex flex-col gap-1'>
            <label className='text-[10px] text-text-dim uppercase'>Price (USD)</label>
            <input type='text' className='bg-card border border-border rounded px-2 py-1.5 text-sm font-mono text-white focus:outline-none focus:border-accent-blue' defaultValue='64,466.14' />
          </div>
          <div className='flex flex-col gap-1'>
            <label className='text-[10px] text-text-dim uppercase'>Size (BTC)</label>
            <input type='text' className='bg-card border border-border rounded px-2 py-1.5 text-sm font-mono text-white focus:outline-none focus:border-accent-blue' defaultValue='1.0' />
          </div>
        </div>
        <div className='mt-auto space-y-2'>
          <div className='flex justify-between text-[10px] text-text-secondary'>
            <span>Margin Req:</span>
            <span className='font-mono'>$3,223.30</span>
          </div>
          <button className='w-full py-2 bg-accent-green hover:bg-emerald-500 text-white font-bold text-xs rounded shadow-lg shadow-emerald-500/20 transition-all flex items-center justify-center gap-2'>
            <Send size={14} /> SUBMIT BUY ORDER
          </button>
        </div>
      </div>
    </div>
  );
}`,
  PositionsTable: `import React from 'react';
import { Briefcase } from 'lucide-react';
export default function PositionsTable() {
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
            <tr className='hover:bg-hover group cursor-pointer border-b border-border/30'>
              <td className='py-2 pl-2'><span className='text-white font-bold'>BTC/USD</span> <span className='text-[9px] text-accent-green bg-accent-green-dim px-1 rounded ml-1'>LONG</span></td>
              <td className='py-2 text-text-secondary'>1.5</td>
              <td className='py-2 text-text-secondary'>63,200.00</td>
              <td className='py-2 text-right pr-2 text-accent-green'>+$1,899.21</td>
            </tr>
            <tr className='hover:bg-hover group cursor-pointer'>
              <td className='py-2 pl-2'><span className='text-white font-bold'>ETH/USD</span> <span className='text-[9px] text-accent-red bg-accent-red-dim px-1 rounded ml-1'>SHORT</span></td>
              <td className='py-2 text-text-secondary'>10.0</td>
              <td className='py-2 text-text-secondary'>3,500.20</td>
              <td className='py-2 text-right pr-2 text-accent-red'>-$537.80</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  );
}`,
  OrdersTable: `import React from 'react';
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
}`,
  RiskMetrics: `import React from 'react';
import { ShieldAlert } from 'lucide-react';
export default function RiskMetrics() {
  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-8 border-b border-border bg-card px-3 flex items-center gap-2'>
        <ShieldAlert size={14} className='text-accent-amber' />
        <span className='text-xs font-bold text-white uppercase tracking-wider'>Risk Diagnostics</span>
      </div>
      <div className='flex-1 p-4 flex flex-col gap-4'>
        <div className='space-y-1'>
          <div className='flex justify-between text-xs'><span className='text-text-secondary'>Margin Utilization</span><span className='font-mono font-bold text-white'>12.4%</span></div>
          <div className='h-1.5 w-full bg-hover rounded-full overflow-hidden'><div className='h-full bg-accent-blue w-[12.4%]' style={{borderRadius: '9999px'}}></div></div>
        </div>
        <div className='space-y-1'>
          <div className='flex justify-between text-xs'><span className='text-text-secondary'>Daily Drawdown</span><span className='font-mono font-bold text-accent-green'>+1.2%</span></div>
          <div className='h-1.5 w-full bg-hover rounded-full overflow-hidden'><div className='h-full bg-accent-green w-[5%]' style={{borderRadius: '9999px'}}></div></div>
        </div>
        <div className='mt-auto bg-card border border-border p-3 rounded'>
          <div className='text-[10px] text-text-dim uppercase mb-1 font-bold'>System Health</div>
          <div className='text-xs font-mono text-white flex flex-col gap-1'>
            <div className='flex justify-between'><span>Matching Engine:</span><span className='text-accent-green'>OK</span></div>
            <div className='flex justify-between'><span>Risk Analytics:</span><span className='text-accent-green'>OK</span></div>
            <div className='flex justify-between'><span>Gateway Latency:</span><span>1ms</span></div>
          </div>
        </div>
      </div>
    </div>
  );
}`,
  AIPanel: `import React from 'react';
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
}`,
  NewsFeed: `import React from 'react';
import { Rss } from 'lucide-react';
export default function NewsFeed() {
  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-8 border-b border-border bg-card px-3 flex items-center gap-2'>
        <Rss size={14} className='text-accent-amber' />
        <span className='text-xs font-bold text-white uppercase tracking-wider'>Real-Time Macro Feed</span>
      </div>
      <div className='flex-1 overflow-auto flex flex-col'>
        {[
          { time: '10:42', text: 'Fed Chairman leaves rates unchanged, cites persistent inflation metrics.', impact: 'high' },
          { time: '10:15', text: 'ECB announces new bond purchasing parameters starting Q3.', impact: 'medium' },
          { time: '09:30', text: 'US Core CPI data matches consensus estimates at 3.2% YoY.', impact: 'medium' },
        ].map((news, i) => (
          <div key={i} className='p-3 border-b border-border/50 hover:bg-hover transition-colors'>
            <div className='flex justify-between text-[10px] mb-1'>
              <span className='font-mono text-accent-blue'>{news.time}</span>
              <span className={'uppercase font-bold ' + (news.impact === 'high' ? 'text-accent-red' : 'text-accent-amber')}>{news.impact} IMPACT</span>
            </div>
            <p className='text-xs text-white leading-relaxed'>{news.text}</p>
          </div>
        ))}
      </div>
    </div>
  );
}`
};

for (const [name, content] of Object.entries(comps)) {
  fs.writeFileSync('src/components/' + name + '.tsx', content, 'utf8');
}
