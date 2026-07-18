import React, { useState } from 'react';
import { useTerminalStore } from '../store/useTerminalStore';
import { Filter, ArrowUpRight } from 'lucide-react';

export default function Screener() {
  const { screenerAssets, setSelectedSymbol, selectedSymbol } = useTerminalStore();

  const [assetClass, setAssetClass] = useState<string>('ALL');
  const [country, setCountry] = useState<string>('ALL');
  const [maxPE, setMaxPE] = useState<number>(100);

  // Filter logic
  const filtered = screenerAssets.filter(asset => {
    if (assetClass !== 'ALL' && asset.asset_class.toUpperCase() !== assetClass) return false;
    if (country !== 'ALL' && asset.country.toUpperCase() !== country) return false;
    if (asset.pe_ratio > 0 && asset.pe_ratio > maxPE) return false;
    return true;
  });

  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-10 border-b border-border bg-card px-3 flex items-center justify-between'>
        <div className='flex items-center gap-2'>
          <Filter size={14} className='text-accent-blue' />
          <span className='text-xs font-bold text-white uppercase tracking-wider'>Global Asset Screener</span>
        </div>
      </div>

      {/* Filters Header Section */}
      <div className='p-3 border-b border-border/40 bg-card/30 grid grid-cols-3 gap-3 text-[10px]'>
        <div className='flex flex-col gap-1'>
          <label className='text-text-dim uppercase font-bold'>Asset Class</label>
          <select 
            value={assetClass} 
            onChange={e => setAssetClass(e.target.value)}
            className='bg-card border border-border rounded px-2 py-1 text-white focus:outline-none focus:border-accent-blue cursor-pointer'
          >
            <option value="ALL">All Classes</option>
            <option value="EQUITIES">Equities</option>
            <option value="CRYPTO">Crypto</option>
            <option value="FX">Foreign Exchange</option>
          </select>
        </div>

        <div className='flex flex-col gap-1'>
          <label className='text-text-dim uppercase font-bold'>Country</label>
          <select 
            value={country} 
            onChange={e => setCountry(e.target.value)}
            className='bg-card border border-border rounded px-2 py-1 text-white focus:outline-none focus:border-accent-blue cursor-pointer'
          >
            <option value="ALL">All Regions</option>
            <option value="US">United States</option>
            <option value="GLOBAL">Global / Decentralized</option>
            <option value="EU">European Union</option>
          </select>
        </div>

        <div className='flex flex-col gap-1'>
          <label className='text-text-dim uppercase font-bold'>Max P/E Ratio ({maxPE})</label>
          <input 
            type="range" 
            min="0" 
            max="100" 
            value={maxPE} 
            onChange={e => setMaxPE(parseInt(e.target.value))}
            className='h-1.5 bg-border rounded-lg appearance-none cursor-pointer accent-accent-blue'
          />
        </div>
      </div>

      {/* Screened Assets Table */}
      <div className='flex-1 overflow-auto p-3'>
        <table className='w-full text-left text-xs border-collapse'>
          <thead>
            <tr className='border-b border-border/40 text-text-dim font-mono uppercase text-[9px]'>
              <th className='py-1.5'>Symbol</th>
              <th className='py-1.5'>Name</th>
              <th className='py-1.5 text-right'>Price</th>
              <th className='py-1.5 text-right'>Market Cap</th>
              <th className='py-1.5 text-right'>P/E</th>
              <th className='py-1.5 text-right'>Div Yield</th>
            </tr>
          </thead>
          <tbody className='divide-y divide-border/20 font-mono text-[11px]'>
            {filtered.length === 0 ? (
              <tr>
                <td colSpan={6} className='text-center py-6 text-text-dim text-[10px]'>
                  No assets match current filter settings
                </td>
              </tr>
            ) : (
              filtered.map((asset) => {
                const isSelected = selectedSymbol === asset.symbol;
                return (
                  <tr 
                    key={asset.symbol}
                    onClick={() => setSelectedSymbol(asset.symbol)}
                    className={`hover:bg-hover cursor-pointer transition-colors group ${isSelected ? 'bg-accent-blue/10 border-l-2 border-accent-blue' : ''}`}
                  >
                    <td className='py-2.5 font-bold text-white pl-1 flex items-center gap-1 group-hover:text-accent-blue transition-colors'>
                      {asset.symbol}
                      <ArrowUpRight size={10} className='opacity-0 group-hover:opacity-100 transition-opacity' />
                    </td>
                    <td className='py-2.5 text-text-secondary'>{asset.name}</td>
                    <td className='py-2.5 text-right text-white'>
                      ${asset.price.toLocaleString(undefined, { minimumFractionDigits: asset.symbol === "EUR/USD" ? 4 : 2 })}
                    </td>
                    <td className='py-2.5 text-right text-text-secondary'>
                      {asset.market_cap_bill > 0 ? `$${asset.market_cap_bill.toFixed(1)}B` : '—'}
                    </td>
                    <td className='py-2.5 text-right text-accent-purple'>
                      {asset.pe_ratio > 0 ? asset.pe_ratio.toFixed(1) : '—'}
                    </td>
                    <td className='py-2.5 text-right text-accent-amber'>
                      {asset.div_yield > 0 ? `${asset.div_yield.toFixed(2)}%` : '—'}
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
