import React, { useState } from 'react';
import { Zap, Play } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';
import SkeletonLoader from './SkeletonLoader';

export default function StrategyRoller() {
  const { selectedSymbol, assets, submitOrder, showNotification } = useTerminalStore();
  const [spreadType, setSpreadType] = useState('Iron Condor');
  
  const currentAsset = assets.find(a => a.symbol === selectedSymbol);
  const spot = currentAsset && currentAsset.currentPrice > 0 ? currentAsset.currentPrice : (selectedSymbol === 'BTC/USD' ? 64500 : 185);

  const isCrypto = spot > 1000;
  const step = isCrypto ? Math.round(spot * 0.02) : Math.max(1, Math.round(spot * 0.02));
  const k2 = Math.round(spot / step) * step;
  const k1 = k2 - step;
  const k3 = k2 + step;
  const k4 = k3 + step;

  const leg1Prem = spot * 0.005;
  const leg2Prem = spot * 0.012;
  const leg3Prem = spot * 0.014;
  const leg4Prem = spot * 0.006;

  const netCredit = (leg2Prem + leg3Prem - leg1Prem - leg4Prem);
  const maxLoss = step - netCredit;

  const handleExecuteSpread = async () => {
    // Submit legs to store
    await submitOrder(selectedSymbol, 'SELL', k2, 1, false, 'LIMIT');
    await submitOrder(selectedSymbol, 'BUY', k1, 1, false, 'LIMIT');
    showNotification(`Multi-Leg ${spreadType} spread strategy submitted for ${selectedSymbol}!`, 'success');
  };

  const formatPrice = (p: number) => {
    if (p > 500) return p.toFixed(0);
    return p.toFixed(2);
  };

  return (
    <div className="bg-panel border border-border rounded-lg flex flex-col h-full min-h-0 select-none shadow-lg">
      <div className="bg-card px-4 py-2 border-b border-border flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Zap size={14} className="text-accent-amber" />
          <span className="font-bold text-xs uppercase tracking-wider text-white">Options Strategy Roller</span>
        </div>
        <select 
          value={spreadType} 
          onChange={(e) => setSpreadType(e.target.value)}
          className="bg-bg-base border border-border rounded text-[10px] px-2 py-0.5 text-text-secondary font-mono cursor-pointer"
        >
          <option value="Iron Condor">Iron Condor</option>
          <option value="Bull Put Spread">Bull Put Spread</option>
          <option value="Bear Call Spread">Bear Call Spread</option>
          <option value="Straddle">Long Straddle</option>
        </select>
      </div>

      {(!currentAsset || currentAsset.currentPrice === 0) ? (
        <div className="flex-1 p-4">
          <SkeletonLoader lines={5} height="h-full" />
        </div>
      ) : (
      <div className="p-4 flex-1 flex flex-col justify-between font-mono text-xs space-y-3">
        <div className="space-y-2">
          <div className="flex justify-between border-b border-border/40 pb-1">
            <span className="text-text-secondary">Selected Strategy:</span>
            <span className="text-accent-blue font-bold">{spreadType} ({selectedSymbol})</span>
          </div>
          <div className="flex justify-between border-b border-border/40 pb-1">
            <span className="text-text-secondary">Leg 1 (Long Put):</span>
            <span className="text-white">Strike ${formatPrice(k1)} | Prem ${formatPrice(leg1Prem)}</span>
          </div>
          <div className="flex justify-between border-b border-border/40 pb-1">
            <span className="text-text-secondary">Leg 2 (Short Put):</span>
            <span className="text-white">Strike ${formatPrice(k2)} | Prem ${formatPrice(leg2Prem)}</span>
          </div>
          <div className="flex justify-between border-b border-border/40 pb-1">
            <span className="text-text-secondary">Leg 3 (Short Call):</span>
            <span className="text-white">Strike ${formatPrice(k3)} | Prem ${formatPrice(leg3Prem)}</span>
          </div>
          <div className="flex justify-between border-b border-border/40 pb-1">
            <span className="text-text-secondary">Leg 4 (Long Call):</span>
            <span className="text-white">Strike ${formatPrice(k4)} | Prem ${formatPrice(leg4Prem)}</span>
          </div>
        </div>

        <div className="bg-bg-base/60 p-2.5 rounded border border-border/60 space-y-1.5">
          <div className="flex justify-between text-[11px]">
            <span className="text-text-dim">Net Credit Received:</span>
            <span className="text-accent-green font-bold">${formatPrice(netCredit)}</span>
          </div>
          <div className="flex justify-between text-[11px]">
            <span className="text-text-dim">Max Potential Loss:</span>
            <span className="text-accent-red font-bold">-${formatPrice(Math.max(0, maxLoss))}</span>
          </div>
        </div>

        <button 
          onClick={handleExecuteSpread}
          className="w-full bg-accent-blue hover:bg-blue-600 text-white font-bold text-xs py-2 rounded transition-all shadow-lg flex items-center justify-center gap-1.5"
        >
          <Play size={14} /> Execute {spreadType} Spread
        </button>
      </div>
      )}
    </div>
  );
}
