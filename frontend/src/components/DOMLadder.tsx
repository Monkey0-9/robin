import React, { useEffect, useState } from 'react';
import { useTerminalStore } from '../store/useTerminalStore';

export interface DOMLevel {
  price: number;
  bidSize: number;
  askSize: number;
  bidOrders?: number;
  askOrders?: number;
}

interface DOMLadderProps {
  symbol?: string;
  levels?: DOMLevel[];
  currentPrice?: number;
  onOrderClick?: (side: 'BUY' | 'SELL', price: number) => void;
}

export const DOMLadder: React.FC<DOMLadderProps> = ({
  symbol: symbolProp,
  levels: levelsProp,
  currentPrice: currentPriceProp,
  onOrderClick,
}) => {
  const selectedSymbol = useTerminalStore(s => s.selectedSymbol);
  const orderBook = useTerminalStore(s => s.orderBook);
  const assets = useTerminalStore(s => s.assets);

  const symbol = symbolProp || selectedSymbol;
  const asset = assets.find(a => a.symbol === symbol);
  const currentPrice = currentPriceProp ?? asset?.currentPrice ?? 0;

  const [domData, setDomData] = useState<DOMLevel[]>([]);

  // Merge the live order book (bids + asks) into ladder levels keyed by price.
  useEffect(() => {
    if (levelsProp && levelsProp.length > 0) {
      setDomData(levelsProp);
      return;
    }
    const byPrice = new Map<number, DOMLevel>();
    for (const b of orderBook.bids) {
      const entry = byPrice.get(b.price) || { price: b.price, bidSize: 0, askSize: 0 };
      entry.bidSize = b.size;
      byPrice.set(b.price, entry);
    }
    for (const a of orderBook.asks) {
      const entry = byPrice.get(a.price) || { price: a.price, bidSize: 0, askSize: 0 };
      entry.askSize = a.size;
      byPrice.set(a.price, entry);
    }
    setDomData(Array.from(byPrice.values()));
  }, [levelsProp, orderBook]);

  const maxSize = Math.max(
    1,
    ...domData.map((l) => Math.max(l.bidSize, l.askSize))
  );

  return (
    <div className="bg-slate-900 border border-slate-800 rounded-lg p-3 text-xs font-mono select-none">
      <div className="flex justify-between items-center mb-2 pb-2 border-b border-slate-800">
        <span className="font-bold text-slate-200 uppercase tracking-wider">{symbol} DOM Ladder</span>
        <span className="text-emerald-400 bg-emerald-950/50 px-2 py-0.5 rounded border border-emerald-800/50">
          Last: {currentPrice > 0 ? `$${currentPrice.toFixed(2)}` : '—'}
        </span>
      </div>

      <div className="grid grid-cols-5 gap-1 px-2 py-1 text-slate-400 font-semibold text-[11px] border-b border-slate-800">
        <span className="text-left">Bid Vol</span>
        <span className="text-center">Bid#</span>
        <span className="text-center">Price</span>
        <span className="text-center">Ask#</span>
        <span className="text-right">Ask Vol</span>
      </div>

      <div className="max-h-80 overflow-y-auto space-y-0.5 mt-1">
        {domData.length === 0 && (
          <div className="py-8 text-center text-slate-500 text-[11px]">
            Awaiting live order book data...
          </div>
        )}
        {domData.map((level) => {
          const isCurrentPrice = Math.abs(level.price - currentPrice) < 0.25;
          const bidWidth = (level.bidSize / maxSize) * 100;
          const askWidth = (level.askSize / maxSize) * 100;

          return (
            <div
              key={level.price.toFixed(2)}
              className={`grid grid-cols-5 gap-1 px-2 py-1 items-center rounded cursor-pointer transition-colors ${
                isCurrentPrice ? 'bg-amber-500/20 border border-amber-500/40 font-bold' : 'hover:bg-slate-800/60'
              }`}
              onClick={() => {
                if (onOrderClick) {
                  onOrderClick(level.bidSize > 0 ? 'BUY' : 'SELL', level.price);
                }
              }}
            >
              {/* Bid Volume */}
              <div className="relative flex items-center h-5">
                {level.bidSize > 0 && (
                  <div
                    className="absolute left-0 top-0 bottom-0 bg-emerald-500/30 rounded-sm"
                    style={{ width: `${bidWidth}%` }}
                  />
                )}
                <span className="relative z-10 text-emerald-400 pl-1">
                  {level.bidSize > 0 ? level.bidSize : ''}
                </span>
              </div>

              {/* Bid Orders */}
              <span className="text-center text-slate-500">{level.bidOrders || ''}</span>

              {/* Price */}
              <span
                className={`text-center font-semibold ${
                  isCurrentPrice ? 'text-amber-400' : 'text-slate-200'
                }`}
              >
                {level.price.toFixed(2)}
              </span>

              {/* Ask Orders */}
              <span className="text-center text-slate-500">{level.askOrders || ''}</span>

              {/* Ask Volume */}
              <div className="relative flex items-center justify-end h-5">
                {level.askSize > 0 && (
                  <div
                    className="absolute right-0 top-0 bottom-0 bg-rose-500/30 rounded-sm"
                    style={{ width: `${askWidth}%` }}
                  />
                )}
                <span className="relative z-10 text-rose-400 pr-1">
                  {level.askSize > 0 ? level.askSize : ''}
                </span>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default DOMLadder;
