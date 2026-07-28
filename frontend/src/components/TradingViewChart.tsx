import React, { useState, useEffect } from 'react';
import { LineChart, Maximize2, Settings, Download, Activity } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';

export default function TradingViewChart() {
  const { selectedSymbol, assets } = useTerminalStore();
  const currentPrice = assets.find(a => a.symbol === selectedSymbol)?.currentPrice || 0;

  const [chartType, setChartType] = useState<'candle' | 'line' | 'area' | 'heikin'>('candle');
  const [activeInterval, setActiveInterval] = useState('1m');
  const [indicators, setIndicators] = useState<{ [key: string]: boolean }>({
    sma50: true,
    sma200: false,
    bb: true,
    rsi: false,
  });

  const toggleIndicator = (ind: string) => {
    setIndicators(prev => ({ ...prev, [ind]: !prev[ind] }));
  };

  // Generate deterministic mock historical data based on current price
  const generateHistory = () => {
    const data = [];
    let price = currentPrice * 0.98;
    for (let i = 0; i < 40; i++) {
      const sinVal = Math.sin(i * 0.4) * (currentPrice * 0.008);
      const randomDrift = (i * 0.0005) * currentPrice;
      const close = price + sinVal + randomDrift;
      const open = i === 0 ? price : data[i - 1].close;
      const high = Math.max(open, close) + (currentPrice * 0.004);
      const low = Math.min(open, close) - (currentPrice * 0.004);

      data.push({ open, high, low, close });
      price = close;
    }
    // Set the last element close to exactly the current price to match the ticker tape
    data[data.length - 1].close = currentPrice;
    return data;
  };

  const data = generateHistory();

  // Find min/max for scaling
  const allPrices = data.flatMap(d => [d.low, d.high]);
  const maxPrice = Math.max(...allPrices) * 1.002;
  const minPrice = Math.min(...allPrices) * 0.998;
  const range = maxPrice - minPrice;

  // SVG dimensions
  const width = 600;
  const height = 240;

  const getX = (index: number) => (index / (data.length - 1)) * (width - 60) + 10;
  const getY = (price: number) => {
    if (range === 0) return height / 2;
    return height - ((price - minPrice) / range) * (height - 40) - 20;
  };

  // Calculate Simple Moving Average helper
  const getSMA = (index: number, period: number) => {
    if (index < period - 1) return null;
    let sum = 0;
    for (let i = index - period + 1; i <= index; i++) {
      sum += data[i].close;
    }
    return sum / period;
  };

  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      {/* Chart Top Bar Controls */}
      <div className='h-10 border-b border-border bg-card px-3 flex items-center justify-between z-20'>
        <div className='flex items-center gap-2 overflow-x-auto no-scrollbar py-1'>
          <LineChart size={14} className='text-accent-blue flex-shrink-0' />
          <span className='text-xs font-bold text-white uppercase tracking-wider flex-shrink-0'>{selectedSymbol}</span>
          
          <div className='h-3 w-px bg-border/80 mx-2 flex-shrink-0'></div>
          
          {/* Chart types */}
          <div className='flex rounded bg-hover p-0.5 text-[9px] font-semibold flex-shrink-0'>
            {(['candle', 'line', 'area', 'heikin'] as const).map(type => (
              <button
                key={type}
                onClick={() => setChartType(type)}
                className={`px-1.5 py-0.5 rounded capitalize transition-all ${chartType === type ? 'bg-accent-blue text-white' : 'text-text-dim hover:text-white'}`}
              >
                {type}
              </button>
            ))}
          </div>

          <div className='h-3 w-px bg-border/80 mx-2 flex-shrink-0'></div>

          {/* Indicators toggler */}
          <div className='flex gap-1.5 text-[9px] font-semibold flex-shrink-0'>
            <button
              onClick={() => toggleIndicator('sma50')}
              className={`px-1.5 py-0.5 rounded border transition-all ${indicators.sma50 ? 'border-orange-500 text-orange-400 bg-orange-950/20' : 'border-border text-text-dim hover:border-text-secondary'}`}
            >
              SMA 50
            </button>
            <button
              onClick={() => toggleIndicator('sma200')}
              className={`px-1.5 py-0.5 rounded border transition-all ${indicators.sma200 ? 'border-yellow-500 text-yellow-400 bg-yellow-950/20' : 'border-border text-text-dim hover:border-text-secondary'}`}
            >
              SMA 200
            </button>
            <button
              onClick={() => toggleIndicator('bb')}
              className={`px-1.5 py-0.5 rounded border transition-all ${indicators.bb ? 'border-indigo-500 text-indigo-400 bg-indigo-950/20' : 'border-border text-text-dim hover:border-text-secondary'}`}
            >
              B-Bands
            </button>
            <button
              onClick={() => toggleIndicator('rsi')}
              className={`px-1.5 py-0.5 rounded border transition-all ${indicators.rsi ? 'border-purple-500 text-purple-400 bg-purple-950/20' : 'border-border text-text-dim hover:border-text-secondary'}`}
            >
              RSI
            </button>
          </div>
        </div>

        <div className='flex gap-2 text-text-dim flex-shrink-0'>
          {['1m', '5m', '1D'].map(t => (
            <button
              key={t}
              onClick={() => setActiveInterval(t)}
              className={`px-1.5 py-0.5 text-[9px] rounded font-bold transition-all ${activeInterval === t ? 'bg-hover text-white' : 'hover:text-white'}`}
            >
              {t}
            </button>
          ))}
        </div>
      </div>

      {/* SVG Canvas Area */}
      <div className='flex-1 relative bg-[#060608] overflow-hidden flex flex-col p-2'>
        {/* Y Axis Gridlines and Labels */}
        <div className='absolute right-2 top-0 bottom-0 w-12 border-l border-border/10 flex flex-col justify-between py-6 text-[8px] font-mono text-text-dim select-none z-10'>
          <span>${maxPrice.toLocaleString(undefined, { maximumFractionDigits: selectedSymbol === 'EUR/USD' ? 4 : 2 })}</span>
          <span>${(minPrice + range * 0.75).toLocaleString(undefined, { maximumFractionDigits: selectedSymbol === 'EUR/USD' ? 4 : 2 })}</span>
          <span>${(minPrice + range * 0.5).toLocaleString(undefined, { maximumFractionDigits: selectedSymbol === 'EUR/USD' ? 4 : 2 })}</span>
          <span>${(minPrice + range * 0.25).toLocaleString(undefined, { maximumFractionDigits: selectedSymbol === 'EUR/USD' ? 4 : 2 })}</span>
          <span>${minPrice.toLocaleString(undefined, { maximumFractionDigits: selectedSymbol === 'EUR/USD' ? 4 : 2 })}</span>
        </div>

        {/* Dynamic Grid Background */}
        <div className='absolute inset-0 opacity-[0.03] pointer-events-none' style={{backgroundImage: 'linear-gradient(#ffffff 1px, transparent 1px), linear-gradient(90deg, #ffffff 1px, transparent 1px)', backgroundSize: '30px 25px'}}></div>

        <div className='flex-1 relative min-h-0 w-full pr-12'>
          <svg className='w-full h-full' viewBox={`0 0 ${width} ${height}`} preserveAspectRatio='none'>
            
            {/* Draw Bollinger Bands ( Indigo Area ) */}
            {indicators.bb && (
              <path
                d={`M ${data.map((d, i) => `${getX(i)},${getY(d.high - (d.high - d.low) * 0.1)}`).join(' L ')} 
                   L ${data.slice().reverse().map((d, i) => `${getX(data.length - 1 - i)},${getY(d.low + (d.high - d.low) * 0.1)}`).join(' L ')} Z`}
                fill='rgba(99, 102, 241, 0.05)'
                stroke='rgba(99, 102, 241, 0.2)'
                strokeWidth='0.5'
                strokeDasharray='2 2'
              />
            )}

            {/* Draw Area Fill (Under Line Chart) */}
            {chartType === 'area' && (
              <path
                d={`M ${getX(0)},${height} L ${data.map((d, i) => `${getX(i)},${getY(d.close)}`).join(' L ')} L ${getX(data.length - 1)},${height} Z`}
                fill='url(#chart-area-grad)'
              />
            )}

            {/* Area Gradient Defs */}
            <defs>
              <linearGradient id='chart-area-grad' x1='0' y1='0' x2='0' y2='1'>
                <stop offset='0%' stopColor='#2563eb' stopOpacity='0.25' />
                <stop offset='100%' stopColor='#2563eb' stopOpacity='0.0' />
              </linearGradient>
            </defs>

            {/* Draw Line Chart */}
            {chartType === 'line' && (
              <path
                d={data.map((d, i) => `${i === 0 ? 'M' : 'L'} ${getX(i)},${getY(d.close)}`).join(' ')}
                fill='none'
                stroke='#3b82f6'
                strokeWidth='1.5'
              />
            )}

            {/* Draw Heikin Ashi or standard Candlesticks */}
            {(chartType === 'candle' || chartType === 'heikin') && data.map((d, i) => {
              const isUp = d.close >= d.open;
              const x = getX(i);
              const openY = getY(d.open);
              const closeY = getY(d.close);
              const highY = getY(d.high);
              const lowY = getY(d.low);

              const color = isUp ? 'rgb(16, 185, 129)' : 'rgb(239, 68, 68)';
              const widthCol = 5;

              return (
                <g key={i}>
                  {/* Wick */}
                  <line x1={x} y1={highY} x2={x} y2={lowY} stroke={color} strokeWidth='1' />
                  {/* Body */}
                  <rect
                    x={x - widthCol / 2}
                    y={Math.min(openY, closeY)}
                    width={widthCol}
                    height={Math.max(Math.abs(openY - closeY), 1)}
                    fill={color}
                  />
                </g>
              );
            })}

            {/* Draw SMA 50 Overlay (Orange) */}
            {indicators.sma50 && (
              <path
                d={data
                  .map((_, i) => {
                    const sma = getSMA(i, 8);
                    return sma ? `${getX(i)},${getY(sma)}` : '';
                  })
                  .filter(p => p !== '')
                  .map((p, i) => `${i === 0 ? 'M' : 'L'} ${p}`)
                  .join(' ')}
                fill='none'
                stroke='#f97316'
                strokeWidth='1'
              />
            )}

            {/* Draw SMA 200 Overlay (Yellow) */}
            {indicators.sma200 && (
              <path
                d={data
                  .map((_, i) => {
                    const sma = getSMA(i, 16);
                    return sma ? `${getX(i)},${getY(sma)}` : '';
                  })
                  .filter(p => p !== '')
                  .map((p, i) => `${i === 0 ? 'M' : 'L'} ${p}`)
                  .join(' ')}
                fill='none'
                stroke='#eab308'
                strokeWidth='1'
              />
            )}
          </svg>
        </div>

        {/* Live Price Tag overlay */}
        <div className='absolute left-3 top-3 bg-panel/85 border border-border px-2 py-0.5 rounded text-[10px] font-mono z-10 flex items-center gap-1.5'>
          <span className='h-1.5 w-1.5 rounded-full bg-accent-green animate-pulse'></span>
          <span className='text-text-secondary'>Live Price:</span>
          <span className='text-white font-bold'>${currentPrice.toLocaleString(undefined, { minimumFractionDigits: selectedSymbol === 'EUR/USD' ? 4 : 2 })}</span>
        </div>

        {/* Dynamic RSI panel at bottom */}
        {indicators.rsi && (
          <div className='h-12 border-t border-border/20 mt-1 relative bg-void/30 p-1 flex flex-col justify-between'>
            <div className='flex justify-between text-[7px] font-mono text-text-dim px-2 select-none'>
              <span>RSI (14)</span>
              <span>Overbought (70)</span>
              <span>Oversold (30)</span>
            </div>
            <div className='flex-1 relative w-full pr-12 mt-1'>
              <svg className='w-full h-full' viewBox={`0 0 ${width} 40`} preserveAspectRatio='none'>
                {/* 30/70 reference lines */}
                <line x1='0' y1='12' x2={width} y2='12' stroke='rgba(239, 68, 68, 0.15)' strokeWidth='0.5' />
                <line x1='0' y1='28' x2={width} y2='28' stroke='rgba(16, 185, 129, 0.15)' strokeWidth='0.5' />
                
                {/* Simulated RSI path */}
                <path
                  d={data.map((_, i) => {
                    const rsi = 40 + Math.sin(i * 0.4) * 15 + Math.cos(i * 0.2) * 5;
                    // map 0..100 to 35..5
                    const rsiY = 40 - (rsi / 100) * 30 - 5;
                    return `${i === 0 ? 'M' : 'L'} ${getX(i)},${rsiY}`;
                  }).join(' ')}
                  fill='none'
                  stroke='#a855f7'
                  strokeWidth='1'
                />
              </svg>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}