import React, { useEffect, useRef, useState, useCallback } from 'react';
import {
  createChart,
  IChartApi,
  ISeriesApi,
  CandlestickSeries,
  LineSeries,
  AreaSeries,
  HistogramSeries,
  ColorType,
  CrosshairMode,
  LineStyle,
} from 'lightweight-charts';
import { LineChart, Activity } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';
import { useAuthStore } from '../store/useAuthStore';

type ChartType = 'candle' | 'line' | 'area';
type TimeframeKey = '1m' | '5m' | '15m' | '1H' | '4H' | '1D';

interface OHLCVBar {
  time: number;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
}

const GATEWAY_URL = process.env.NEXT_PUBLIC_GATEWAY_URL || 'http://localhost:8080';

async function fetchCandles(symbol: string, resolution: string, count = 200): Promise<OHLCVBar[]> {
  try {
    const token = useAuthStore.getState().getToken();
    const headers: Record<string, string> = {};
    if (token) {
      headers['Authorization'] = `Bearer ${token}`;
    }
    const res = await fetch(
      `${GATEWAY_URL}/api/candles?symbol=${encodeURIComponent(symbol)}&resolution=${resolution}&count=${count}`,
      { headers, signal: AbortSignal.timeout(3000) }
    );
    if (!res.ok) return [];
    const data = await res.json();
    if (!Array.isArray(data) || data.length === 0) return [];
    return data.map((c: any) => ({
      time: (c.time > 1e10 ? Math.floor(c.time / 1000) : c.time) as number,
      open: c.open,
      high: c.high,
      low: c.low,
      close: c.close,
      volume: c.volume || 0,
    }));
  } catch {
    return [];
  }
}

export default function TradingViewChart() {
  const { selectedSymbol, assets, indicators } = useTerminalStore();
  const asset = assets.find(a => a.symbol === selectedSymbol);
  const currentPrice = asset?.currentPrice ?? 0;
  const dailyChangePct = asset?.dailyChangePct ?? 0;

  const containerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const priceSeriesRef = useRef<ISeriesApi<'Candlestick' | 'Line' | 'Area'> | null>(null);
  const volumeSeriesRef = useRef<ISeriesApi<'Histogram'> | null>(null);
  const smaSeriesRef = useRef<ISeriesApi<'Line'> | null>(null);
  const emaSeriesRef = useRef<ISeriesApi<'Line'> | null>(null);
  const bbUpperRef = useRef<ISeriesApi<'Line'> | null>(null);
  const bbLowerRef = useRef<ISeriesApi<'Line'> | null>(null);
  const vwapSeriesRef = useRef<ISeriesApi<'Line'> | null>(null);

  const [chartType, setChartType] = useState<ChartType>('candle');
  const [activeInterval, setActiveInterval] = useState<TimeframeKey>('1m');
  const [candleData, setCandleData] = useState<OHLCVBar[]>([]);
  const [loading, setLoading] = useState(false);
  const candleCache = useRef<Map<string, OHLCVBar[]>>(new Map());

  const cacheKey = `${selectedSymbol}::${activeInterval}`;

  useEffect(() => {
    setLoading(true);
    const cached = candleCache.current.get(cacheKey);
    if (cached && cached.length > 0) {
      setCandleData(cached);
      setLoading(false);
      return;
    }
    fetchCandles(selectedSymbol, activeInterval).then(bars => {
      if (bars.length > 0) {
        candleCache.current.set(cacheKey, bars);
        setCandleData(bars);
      }
      setLoading(false);
    });
  }, [selectedSymbol, activeInterval, cacheKey]);

  const recreateOverlays = useCallback((chart: IChartApi) => {
    if (smaSeriesRef.current) { chart.removeSeries(smaSeriesRef.current); smaSeriesRef.current = null; }
    if (emaSeriesRef.current) { chart.removeSeries(emaSeriesRef.current); emaSeriesRef.current = null; }
    if (bbUpperRef.current) { chart.removeSeries(bbUpperRef.current); bbUpperRef.current = null; }
    if (bbLowerRef.current) { chart.removeSeries(bbLowerRef.current); bbLowerRef.current = null; }
    if (vwapSeriesRef.current) { chart.removeSeries(vwapSeriesRef.current); vwapSeriesRef.current = null; }

    const inds = indicators[selectedSymbol];
    if (!inds || candleData.length === 0) return;

    const closes = candleData.map(c => c.close);
    const computeSMA = (period: number): { time: number; value: number }[] => {
      const result: { time: number; value: number }[] = [];
      for (let i = period - 1; i < closes.length; i++) {
        let sum = 0;
        for (let j = i - period + 1; j <= i; j++) sum += closes[j];
        result.push({ time: candleData[i].time, value: sum / period });
      }
      return result;
    };
    const computeEMA = (period: number): { time: number; value: number }[] => {
      const result: { time: number; value: number }[] = [];
      const k = 2 / (period + 1);
      let ema = closes[0];
      for (let i = 0; i < closes.length; i++) {
        if (i > 0) ema = closes[i] * k + ema * (1 - k);
        if (i >= period - 1) result.push({ time: candleData[i].time, value: ema });
      }
      return result;
    };

    const smaData = computeSMA(20);
    if (smaData.length > 0) {
      smaSeriesRef.current = chart.addSeries(LineSeries, {
        color: '#3b82f6',
        lineWidth: 1,
        lineStyle: LineStyle.Solid,
        lastValueVisible: true,
        priceLineVisible: false,
      });
      smaSeriesRef.current.setData(smaData as any);
    }

    const emaData = computeEMA(50);
    if (emaData.length > 0) {
      emaSeriesRef.current = chart.addSeries(LineSeries, {
        color: '#8b5cf6',
        lineWidth: 1,
        lineStyle: LineStyle.Dashed,
        lastValueVisible: true,
        priceLineVisible: false,
      });
      emaSeriesRef.current.setData(emaData as any);
    }

    if (inds.upperBand && inds.lowerBand && smaData.length > 0) {
      const bbUpperData = smaData.map(d => ({ ...d, value: inds.upperBand }));
      const bbLowerData = smaData.map(d => ({ ...d, value: inds.lowerBand }));
      bbUpperRef.current = chart.addSeries(LineSeries, {
        color: '#ec4899',
        lineWidth: 1,
        lineStyle: LineStyle.Dotted,
        lastValueVisible: true,
        priceLineVisible: false,
      });
      bbUpperRef.current.setData(bbUpperData as any);
      bbLowerRef.current = chart.addSeries(LineSeries, {
        color: '#ec4899',
        lineWidth: 1,
        lineStyle: LineStyle.Dotted,
        lastValueVisible: true,
        priceLineVisible: false,
      });
      bbLowerRef.current.setData(bbLowerData as any);
    }
  }, [indicators, selectedSymbol, candleData]);

  useEffect(() => {
    if (!containerRef.current) return;

    const chart = createChart(containerRef.current, {
      layout: {
        background: { type: ColorType.Solid, color: '#060608' },
        textColor: '#6b7280',
        fontFamily: "'Inter', 'Roboto Mono', monospace",
        fontSize: 11,
      },
      grid: {
        vertLines: { color: 'rgba(255,255,255,0.03)' },
        horzLines: { color: 'rgba(255,255,255,0.03)' },
      },
      crosshair: {
        mode: CrosshairMode.Normal,
        vertLine: { color: 'rgba(255,255,255,0.15)', style: 1 },
        horzLine: { color: 'rgba(255,255,255,0.15)', style: 1 },
      },
      rightPriceScale: {
        borderColor: 'rgba(255,255,255,0.06)',
        scaleMargins: { top: 0.05, bottom: 0.25 },
      },
      timeScale: {
        borderColor: 'rgba(255,255,255,0.06)',
        timeVisible: true,
        secondsVisible: false,
        barSpacing: 6,
      },
      handleScale: { mouseWheel: true, pinch: true },
      handleScroll: { mouseWheel: true, pressedMouseMove: true },
    });

    chartRef.current = chart;

    const ro = new ResizeObserver(entries => {
      for (const entry of entries) {
        chart.resize(entry.contentRect.width, entry.contentRect.height);
      }
    });
    ro.observe(containerRef.current);

    return () => {
      ro.disconnect();
      chart.remove();
      chartRef.current = null;
      priceSeriesRef.current = null;
      volumeSeriesRef.current = null;
      smaSeriesRef.current = null;
      emaSeriesRef.current = null;
      bbUpperRef.current = null;
      bbLowerRef.current = null;
      vwapSeriesRef.current = null;
    };
  }, []);

  useEffect(() => {
    const chart = chartRef.current;
    if (!chart) return;

    if (priceSeriesRef.current) chart.removeSeries(priceSeriesRef.current);
    if (volumeSeriesRef.current) chart.removeSeries(volumeSeriesRef.current);

    if (chartType === 'candle') {
      priceSeriesRef.current = chart.addSeries(CandlestickSeries, {
        upColor: '#10b981',
        downColor: '#ef4444',
        borderUpColor: '#10b981',
        borderDownColor: '#ef4444',
        wickUpColor: '#10b981',
        wickDownColor: '#ef4444',
        priceFormat: { type: 'price' },
      });
    } else if (chartType === 'line') {
      priceSeriesRef.current = chart.addSeries(LineSeries, {
        color: '#3b82f6',
        lineWidth: 2,
        crosshairMarkerVisible: true,
        crosshairMarkerRadius: 4,
      });
    } else {
      priceSeriesRef.current = chart.addSeries(AreaSeries, {
        lineColor: '#3b82f6',
        topColor: 'rgba(37, 99, 235, 0.3)',
        bottomColor: 'rgba(37, 99, 235, 0.0)',
        lineWidth: 2,
      });
    }

    volumeSeriesRef.current = chart.addSeries(HistogramSeries, {
      color: '#26a69a',
      priceFormat: { type: 'volume' },
      priceScaleId: 'volume',
    });
    chart.priceScale('volume').applyOptions({
      scaleMargins: { top: 0.8, bottom: 0 },
    });

    recreateOverlays(chart);
  }, [chartType, recreateOverlays]);

  const computeVWAP = (bars: OHLCVBar[]): { time: number; value: number }[] => {
    let cumPV = 0, cumVol = 0;
    const result: { time: number; value: number }[] = [];
    for (const bar of bars) {
      const tp = (bar.high + bar.low + bar.close) / 3;
      cumPV += tp * bar.volume;
      cumVol += bar.volume;
      if (cumVol > 0) result.push({ time: bar.time, value: cumPV / cumVol });
    }
    return result;
  };

  useEffect(() => {
    const chart = chartRef.current;
    const priceSeries = priceSeriesRef.current;
    const volumeSeries = volumeSeriesRef.current;
    if (!chart || !priceSeries || !volumeSeries) return;

    const bars = candleData;
    if (bars.length === 0) return;

    try {
      if (chartType === 'candle') {
        (priceSeries as ISeriesApi<'Candlestick'>).setData(
          bars.map(c => ({ time: c.time as any, open: c.open, high: c.high, low: c.low, close: c.close }))
        );
      } else {
        (priceSeries as ISeriesApi<'Line' | 'Area'>).setData(
          bars.map(c => ({ time: c.time as any, value: c.close }))
        );
      }

      volumeSeries.setData(
        bars.map(c => ({
          time: c.time as any,
          value: c.volume,
          color: c.close >= c.open ? 'rgba(38, 166, 154, 0.5)' : 'rgba(239, 83, 80, 0.5)',
        }))
      );

      chart.timeScale().fitContent();

      if (vwapSeriesRef.current) chart.removeSeries(vwapSeriesRef.current);
      const vwapData = computeVWAP(bars);
      if (vwapData.length > 0) {
        vwapSeriesRef.current = chart.addSeries(LineSeries, {
          color: '#f97316',
          lineWidth: 1,
          lineStyle: LineStyle.Solid,
          lastValueVisible: true,
          priceLineVisible: false,
        });
        vwapSeriesRef.current.setData(vwapData as any);
      }

      recreateOverlays(chart);
    } catch {
      // swallow
    }
  }, [candleData, currentPrice, selectedSymbol, chartType, recreateOverlays]);

  const isUp = dailyChangePct >= 0;

  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-10 border-b border-border bg-card px-3 flex items-center justify-between z-20 flex-shrink-0'>
        <div className='flex items-center gap-2 overflow-x-auto no-scrollbar py-1'>
          <LineChart size={14} className='text-accent-blue flex-shrink-0' />
          <span className='text-xs font-bold text-white uppercase tracking-wider flex-shrink-0'>
            {selectedSymbol}
          </span>
          {currentPrice > 0 && (
            <>
              <span className='text-xs font-mono text-white font-semibold flex-shrink-0'>
                {currentPrice.toLocaleString(undefined, {
                  minimumFractionDigits: 2,
                  maximumFractionDigits: currentPrice < 10 ? 5 : 2,
                })}
              </span>
              <span className={`text-[10px] font-bold flex-shrink-0 ${isUp ? 'text-green-400' : 'text-red-400'}`}>
                {isUp ? '▲' : '▼'} {Math.abs(dailyChangePct).toFixed(2)}%
              </span>
            </>
          )}
          <div className='h-3 w-px bg-border/80 mx-1 flex-shrink-0' />
          <div className='flex rounded bg-hover p-0.5 text-[9px] font-semibold flex-shrink-0'>
            {(['candle', 'line', 'area'] as ChartType[]).map(type => (
              <button
                key={type}
                onClick={() => setChartType(type)}
                className={`px-1.5 py-0.5 rounded capitalize transition-all ${
                  chartType === type ? 'bg-accent-blue text-white' : 'text-text-dim hover:text-white'
                }`}
              >
                {type}
              </button>
            ))}
          </div>
        </div>
        <div className='flex gap-1 text-text-dim flex-shrink-0'>
          {(['1m', '5m', '15m', '1H', '4H', '1D'] as TimeframeKey[]).map(t => (
            <button
              key={t}
              onClick={() => setActiveInterval(t)}
              className={`px-1.5 py-0.5 text-[9px] rounded font-bold transition-all ${
                activeInterval === t ? 'bg-hover text-white' : 'hover:text-white'
              }`}
            >
              {t}
            </button>
          ))}
        </div>
      </div>
      <div ref={containerRef} className='flex-1 w-full min-h-0 relative'>
        {(!currentPrice || currentPrice === 0) && (
          <div className='absolute inset-0 flex flex-col items-center justify-center gap-2 text-text-dim pointer-events-none'>
            <Activity size={24} className='animate-pulse opacity-40' />
            <span className='text-[11px]'>Waiting for live price data…</span>
          </div>
        )}
        {loading && (
          <div className='absolute top-2 right-2 z-10'>
            <span className='text-[10px] text-text-dim bg-black/40 px-2 py-1 rounded animate-pulse'>
              Loading…
            </span>
          </div>
        )}
      </div>
      <div className='h-6 border-t border-border/50 px-3 flex items-center gap-3 text-[10px] text-text-dim flex-shrink-0 bg-card/50 overflow-x-auto no-scrollbar'>
        <span className='flex items-center gap-1 flex-shrink-0'>
          <span className='h-1.5 w-1.5 rounded-full bg-accent-green animate-pulse' />
          Live Feed
        </span>
        <span className='flex-shrink-0'>Interval: {activeInterval}</span>
        {indicators[selectedSymbol] && (
          <>
            <span className='text-blue-400 flex-shrink-0'>SMA20: {(indicators[selectedSymbol] as any).sma20?.toFixed(2) ?? indicators[selectedSymbol].sma20?.toFixed(2)}</span>
            <span className='text-purple-400 flex-shrink-0'>EMA50: {(indicators[selectedSymbol] as any).ema50?.toFixed(2) ?? '—'}</span>
            <span className={`flex-shrink-0 font-semibold ${
              (indicators[selectedSymbol] as any).rsi > 70 ? 'text-red-400' :
              (indicators[selectedSymbol] as any).rsi < 30 ? 'text-green-400' : 'text-yellow-400'
            }`}>RSI: {(indicators[selectedSymbol] as any).rsi?.toFixed(1) ?? indicators[selectedSymbol].rsi?.toFixed(1)}</span>
            <span className={`flex-shrink-0 ${
              ((indicators[selectedSymbol] as any).macd ?? indicators[selectedSymbol].macd) >= 0 ? 'text-accent-green' : 'text-accent-red'
            }`}>MACD: {((indicators[selectedSymbol] as any).macd ?? indicators[selectedSymbol].macd)?.toFixed(4)}</span>
            {(indicators[selectedSymbol] as any).atr > 0 && (
              <span className='text-orange-400 flex-shrink-0'>ATR: {(indicators[selectedSymbol] as any).atr?.toFixed(2)}</span>
            )}
            {(indicators[selectedSymbol] as any).stochK > 0 && (
              <span className='text-pink-400 flex-shrink-0'>Stoch: {(indicators[selectedSymbol] as any).stochK?.toFixed(1)}%K / {(indicators[selectedSymbol] as any).stochD?.toFixed(1)}%D</span>
            )}
          </>
        )}
        {currentPrice > 0 && (
          <span className='ml-auto font-mono text-white font-semibold flex-shrink-0'>
            ${currentPrice.toLocaleString(undefined, { minimumFractionDigits: 2 })}
          </span>
        )}
      </div>
    </div>
  );
}
