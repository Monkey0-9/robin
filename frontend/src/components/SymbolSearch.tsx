import React, { useState, useRef, useEffect } from 'react';
import { Search, Check } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';

export default function SymbolSearch() {
  const assets = useTerminalStore((s) => s.assets);
  const selectedSymbol = useTerminalStore((s) => s.selectedSymbol);
  const setSelectedSymbol = useTerminalStore((s) => s.setSelectedSymbol);

  const [query, setQuery] = useState('');
  const [isOpen, setIsOpen] = useState(false);
  const wrapperRef = useRef<HTMLDivElement>(null);

  // Close dropdown on click outside
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (wrapperRef.current && !wrapperRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    }
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const filteredAssets = assets.filter(
    (a) =>
      a.symbol.toLowerCase().includes(query.toLowerCase()) ||
      (a.name && a.name.toLowerCase().includes(query.toLowerCase()))
  ).slice(0, 8);

  const handleSelect = (symbol: string) => {
    setSelectedSymbol(symbol);
    setQuery('');
    setIsOpen(false);
  };

  return (
    <div ref={wrapperRef} className="relative z-50">
      <div className="flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-hover border border-border text-xs focus-within:border-accent-blue transition-colors">
        <Search size={13} className="text-text-dim" />
        <input
          type="text"
          value={query}
          onFocus={() => setIsOpen(true)}
          onChange={(e) => {
            setQuery(e.target.value);
            setIsOpen(true);
          }}
          placeholder="Search Symbol (e.g. AAPL, BTC)..."
          className="bg-transparent text-white text-xs w-44 focus:outline-none placeholder:text-text-dim/60 font-mono"
        />
      </div>

      {isOpen && (
        <div className="absolute left-0 mt-1 w-64 bg-panel border border-border rounded-md shadow-2xl overflow-hidden z-50 max-h-60 overflow-y-auto">
          <div className="px-2 py-1 bg-card border-b border-border/50 text-[10px] font-mono text-text-dim uppercase font-bold">
            Available Instruments ({filteredAssets.length})
          </div>
          {filteredAssets.length === 0 ? (
            <div className="px-3 py-2 text-xs text-text-dim italic">No matching symbol found.</div>
          ) : (
            filteredAssets.map((a) => {
              const isSelected = a.symbol === selectedSymbol;
              return (
                <button
                  key={a.symbol}
                  onClick={() => handleSelect(a.symbol)}
                  className={`w-full px-3 py-1.5 flex items-center justify-between text-xs hover:bg-hover transition-colors text-left font-mono ${
                    isSelected ? 'bg-accent-blue/15 text-accent-blue font-bold' : 'text-white'
                  }`}
                >
                  <div className="flex flex-col">
                    <span className="font-bold">{a.symbol}</span>
                    {a.name && <span className="text-[10px] text-text-dim truncate max-w-[150px]">{a.name}</span>}
                  </div>
                  {isSelected && <Check size={12} className="text-accent-blue" />}
                </button>
              );
            })
          )}
        </div>
      )}
    </div>
  );
}
