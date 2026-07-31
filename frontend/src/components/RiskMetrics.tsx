import React from 'react';
import { ShieldAlert, TrendingDown, Activity, Gauge } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';

export default function RiskMetrics() {
  const { marginUtilization, equity, balance, systemHealth, positions, riskData } = useTerminalStore();

  const totalExposure = positions.reduce((sum, p) => sum + p.size * p.entryPrice, 0);
  const leverage = equity > 0 ? totalExposure / equity : 0;
  const unrealizedPnL = positions.reduce((sum, p) => sum + p.unrealizedPnL, 0);
  const pnlPct = equity > 0 ? (unrealizedPnL / (equity - unrealizedPnL)) * 100 : 0;
  const dailyReturn = equity > 0 ? ((equity - balance) / balance) * 100 : 0;
  
  const var95 = riskData?.var_95 || 0;
  const cvar95 = riskData?.cvar_95 || 0;

  const largestPosition = positions.length > 0
    ? Math.max(...positions.map(p => (p.size * p.entryPrice) / (equity || 1) * 100))
    : 0;

  const getColor = (val: number, warn: number, danger: number) =>
    val >= danger ? 'text-accent-red' : val >= warn ? 'text-accent-amber' : 'text-accent-green';

  const getBarColor = (val: number, warn: number, danger: number) =>
    val >= danger ? 'bg-accent-red' : val >= warn ? 'bg-accent-amber' : 'bg-accent-blue';

  const riskScore = Math.min(100,
    (marginUtilization > 80 ? 30 : marginUtilization > 50 ? 15 : 0) +
    (leverage > 4 ? 25 : leverage > 2 ? 10 : 0) +
    (largestPosition > 25 ? 25 : largestPosition > 10 ? 10 : 0) +
    (systemHealth.failed > 0 ? 20 : 0)
  );

  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-8 border-b border-border bg-card px-3 flex items-center gap-2 justify-between'>
        <div className='flex items-center gap-2'>
          <ShieldAlert size={14} className='text-accent-amber' />
          <span className='text-xs font-bold text-white uppercase tracking-wider'>Risk Diagnostics</span>
        </div>
        <span className={`text-[10px] font-bold px-2 py-0.5 rounded ${
          riskScore < 30 ? 'bg-accent-green/20 text-accent-green' :
          riskScore < 60 ? 'bg-accent-amber/20 text-accent-amber' :
          'bg-accent-red/20 text-accent-red'
        }`}>
          Risk Score: {riskScore}/100
        </span>
      </div>
      <div className='flex-1 p-3 grid grid-cols-2 gap-2 text-[11px] font-mono overflow-auto'>
        <div className='bg-card border border-border rounded p-2 space-y-1.5'>
          <div className='text-[9px] text-text-dim uppercase font-bold tracking-wider flex items-center gap-1'>
            <Activity size={10} /> Exposure
          </div>
          <div className='flex justify-between'>
            <span className='text-text-secondary'>Total Exposure</span>
            <span className='text-white font-bold'>${totalExposure.toFixed(2)}</span>
          </div>
          <div className='flex justify-between'>
            <span className='text-text-secondary'>Leverage</span>
            <span className={`font-bold ${getColor(leverage, 2, 4)}`}>{leverage.toFixed(2)}x</span>
          </div>
          <div className='h-1 w-full bg-hover rounded-full overflow-hidden'>
            <div className={`h-full rounded-full ${getBarColor(leverage, 2, 4)}`}
              style={{ width: Math.min((leverage / 5) * 100, 100) + '%' }} />
          </div>
        </div>

        <div className='bg-card border border-border rounded p-2 space-y-1.5'>
          <div className='text-[9px] text-text-dim uppercase font-bold tracking-wider flex items-center gap-1'>
            <TrendingDown size={10} /> P&L
          </div>
          <div className='flex justify-between'>
            <span className='text-text-secondary'>Unrealized P&L</span>
            <span className={`font-bold ${unrealizedPnL >= 0 ? 'text-accent-green' : 'text-accent-red'}`}>
              {unrealizedPnL >= 0 ? '+' : ''}${unrealizedPnL.toFixed(2)}
            </span>
          </div>
          <div className='flex justify-between'>
            <span className='text-text-secondary'>P&L %</span>
            <span className={`font-bold ${pnlPct >= 0 ? 'text-accent-green' : 'text-accent-red'}`}>
              {pnlPct >= 0 ? '+' : ''}{pnlPct.toFixed(2)}%
            </span>
          </div>
          <div className='flex justify-between'>
            <span className='text-text-secondary'>Daily Return</span>
            <span className={`font-bold ${dailyReturn >= 0 ? 'text-accent-green' : 'text-accent-red'}`}>
              {dailyReturn >= 0 ? '+' : ''}{dailyReturn.toFixed(2)}%
            </span>
          </div>
        </div>

        <div className='bg-card border border-border rounded p-2 space-y-1.5'>
          <div className='text-[9px] text-text-dim uppercase font-bold tracking-wider flex items-center gap-1'>
            <Gauge size={10} /> Margins
          </div>
          <div className='flex justify-between'>
            <span className='text-text-secondary'>Margin Util.</span>
            <span className={`font-bold ${getColor(marginUtilization, 50, 80)}`}>
              {marginUtilization.toFixed(1)}%
            </span>
          </div>
          <div className='h-1 w-full bg-hover rounded-full overflow-hidden'>
            <div className={`h-full rounded-full ${getBarColor(marginUtilization, 50, 80)}`}
              style={{ width: Math.min(marginUtilization, 100) + '%' }} />
          </div>
          <div className='flex justify-between'>
            <span className='text-text-secondary'>Net Equity</span>
            <span className='text-white font-bold'>${equity.toFixed(2)}</span>
          </div>
          <div className='flex justify-between'>
            <span className='text-text-secondary'>Cash Balance</span>
            <span className='text-white font-bold'>${balance.toFixed(2)}</span>
          </div>
        </div>

        <div className='bg-card border border-border rounded p-2 space-y-1.5'>
          <div className='text-[9px] text-text-dim uppercase font-bold tracking-wider'>Concentration & VaR</div>
          <div className='flex justify-between'>
            <span className='text-text-secondary'>Largest Pos.</span>
            <span className={`font-bold ${getColor(largestPosition, 10, 25)}`}>
              {largestPosition.toFixed(1)}%
            </span>
          </div>
          <div className='flex justify-between'>
            <span className='text-text-secondary'>95% VaR</span>
            <span className='text-accent-amber font-bold'>${var95.toFixed(2)}</span>
          </div>
          <div className='flex justify-between'>
            <span className='text-text-secondary'>95% CVaR</span>
            <span className='text-accent-red font-bold'>${cvar95.toFixed(2)}</span>
          </div>
        </div>

        <div className='bg-card border border-border rounded p-2 col-span-2 space-y-1.5'>
          <div className='text-[9px] text-text-dim uppercase font-bold tracking-wider'>System Health</div>
          <div className='grid grid-cols-3 gap-2'>
            <div className='flex flex-col items-center p-1 rounded bg-hover/50'>
              <span className='text-accent-green font-bold text-xs'>{systemHealth.healthy}</span>
              <span className='text-[9px] text-text-dim'>Healthy</span>
            </div>
            <div className='flex flex-col items-center p-1 rounded bg-hover/50'>
              <span className='text-accent-amber font-bold text-xs'>{systemHealth.degraded}</span>
              <span className='text-[9px] text-text-dim'>Degraded</span>
            </div>
            <div className='flex flex-col items-center p-1 rounded bg-hover/50'>
              <span className={`font-bold text-xs ${systemHealth.failed > 0 ? 'text-accent-red' : 'text-accent-green'}`}>
                {systemHealth.failed}
              </span>
              <span className='text-[9px] text-text-dim'>Failed</span>
            </div>
          </div>
          <div className='flex justify-between pt-1 border-t border-border/50'>
            <span className='text-text-secondary'>Gateway Latency</span>
            <span className='text-white font-bold'>
              {systemHealth.latencyNs > 0 ? (systemHealth.latencyNs / 1000000).toFixed(2) + 'ms' : 'Offline'}
            </span>
          </div>
        </div>
      </div>
    </div>
  );
}
