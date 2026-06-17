import React, { useEffect, useRef, useState } from 'react';
import { useTerminalStore, Asset } from '../store/useTerminalStore';
import { TrendingUp, TrendingDown } from 'lucide-react';

interface WatchlistItemProps {
  asset: Asset;
  isSelected: boolean;
  onSelect: () => void;
}

function WatchlistItem({ asset, isSelected, onSelect }: WatchlistItemProps) {
  const { symbol, name, currentPrice, dailyChangePct, type } = asset;
  const [flashClass, setFlashClass] = useState<'green' | 'red' | null>(null);
  const prevPriceRef = useRef(currentPrice);

  useEffect(() => {
    if (currentPrice > prevPriceRef.current) {
      setFlashClass('green');
      const t = setTimeout(() => setFlashClass(null), 600);
      return () => clearTimeout(t);
    } else if (currentPrice < prevPriceRef.current) {
      setFlashClass('red');
      const t = setTimeout(() => setFlashClass(null), 600);
      return () => clearTimeout(t);
    }
    prevPriceRef.current = currentPrice;
  }, [currentPrice]);

  const changeIsPositive = dailyChangePct >= 0;
  
  // Format price decimals based on asset type
  const formatPrice = (p: number) => {
    if (symbol === "EUR/USD") return p.toFixed(4);
    if (p > 1000) return p.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
    return p.toFixed(2);
  };

  // Asset type badges styling
  const getTypeBadgeStyle = (t: string) => {
    switch (t) {
      case 'crypto': return 'bg-purple-950/40 border border-purple-900/60 text-purple-400';
      case 'equity': return 'bg-blue-950/40 border border-blue-900/60 text-blue-400';
      case 'index': return 'bg-amber-950/40 border border-amber-900/60 text-amber-400';
      case 'fx': return 'bg-emerald-950/40 border border-emerald-900/60 text-emerald-400';
      default: return 'bg-slate-900 border border-slate-700 text-slate-400';
    }
  };

  return (
    <div
      onClick={onSelect}
      className={`ticker-row flex items-center justify-between px-3 py-2 border-b border-border/40 cursor-pointer transition-all hover:bg-bg-hover relative select-none ${
        isSelected ? 'bg-accent-blue-dim border-l-2 border-l-accent-blue' : ''
      } ${
        flashClass === 'green' ? 'animate-flash-green' : flashClass === 'red' ? 'animate-flash-red' : ''
      }`}
    >
      <div className="flex flex-col gap-0.5">
        <div className="flex items-center gap-1.5">
          <span className={`font-mono font-bold text-xs ${changeIsPositive ? 'text-accent-green' : 'text-accent-red'}`}>{symbol}</span>
          <span className={`text-[8px] uppercase tracking-wide px-1 py-0.2 rounded font-bold ${getTypeBadgeStyle(type)}`}>
            {type}
          </span>
        </div>
        <span className="text-[10px] text-text-dim truncate max-w-[130px]">{name}</span>
      </div>

      <div className="flex flex-col items-end gap-0.5">
        <span className={`font-mono font-semibold text-xs ${changeIsPositive ? 'text-accent-green' : 'text-accent-red'}`}>
          {formatPrice(currentPrice)}
        </span>
        <div className={`flex items-center gap-0.5 font-mono text-[10px] font-medium ${changeIsPositive ? 'text-accent-green' : 'text-accent-red'}`}>
          {changeIsPositive ? <TrendingUp size={10} /> : <TrendingDown size={10} />}
          <span>{changeIsPositive ? '+' : ''}{dailyChangePct}%</span>
        </div>
      </div>
    </div>
  );
}

export default function Watchlist() {
  const { assets, selectedSymbol, setSelectedSymbol } = useTerminalStore();

  return (
    <div className="bg-panel border border-border rounded-lg overflow-hidden flex flex-col h-full">
      <div className="bg-card px-3 py-2 border-b border-border flex items-center justify-between">
        <h3 className="text-xs font-bold uppercase tracking-wider text-secondary flex items-center gap-1.5">
          <span className="w-1.5 h-1.5 bg-accent-blue rounded-full" />
          Market Watch
        </h3>
        <span className="font-mono text-[9px] text-text-dim">
          {assets.length} Active Instruments
        </span>
      </div>
      
      <div className="flex-1 overflow-auto divide-y divide-border/20 scrollbar">
        {assets.map((asset) => (
          <WatchlistItem
            key={asset.symbol}
            asset={asset}
            isSelected={asset.symbol === selectedSymbol}
            onSelect={() => setSelectedSymbol(asset.symbol)}
          />
        ))}
      </div>
    </div>
  );
}
