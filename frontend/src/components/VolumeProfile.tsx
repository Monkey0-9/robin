'use client';
import React, { useEffect, useRef } from 'react';
import { useTerminalStore } from '../store/useTerminalStore';

interface VolumeProfileProps {
  symbol?: string;
}

/**
 * VolumeProfile — Horizontal histogram of volume traded at each price level.
 * Institutional standard (TradingView Pro, Sierra Chart, Bookmap all show this).
 * POC = Point of Control (highest volume price level)
 * VAH = Value Area High (70th percentile of volume)
 * VAL = Value Area Low (30th percentile of volume)
 */
export default function VolumeProfile({ symbol }: VolumeProfileProps) {
  const { selectedSymbol, volumeStats } = useTerminalStore();
  const sym = symbol || selectedSymbol;
  const stats = volumeStats?.[sym];
  const canvasRef = useRef<HTMLCanvasElement>(null);

  // Real per-price-level volume data arrives via the volume_profile
  // WebSocket event. No synthetic profile is rendered when it is absent.
  const profileData = React.useMemo(() => {
    if (!stats?.levels || stats.levels.length === 0) return null;
    return stats.levels;
  }, [stats]);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || !profileData) return;

    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const dpr = window.devicePixelRatio || 1;
    const width = canvas.offsetWidth;
    const height = canvas.offsetHeight;
    canvas.width = width * dpr;
    canvas.height = height * dpr;
    ctx.scale(dpr, dpr);

    ctx.clearRect(0, 0, width, height);

    const maxVol = Math.max(...profileData.map(l => l.volume));
    const barHeight = height / profileData.length;
    const maxBarWidth = width * 0.8;
    const pocIdx = profileData.reduce((best, l, i) =>
      l.volume > profileData[best].volume ? i : best, 0);

    profileData.forEach((level, i) => {
      const barWidth = (level.volume / maxVol) * maxBarWidth;
      const y = i * barHeight;

      // Color: POC = gold, otherwise gradient from green (bid-side) to transparent
      const isPOC = i === pocIdx;
      const isAboveVWAP = i < profileData.length / 2;

      ctx.fillStyle = isPOC
        ? 'rgba(250, 200, 60, 0.7)'
        : isAboveVWAP
        ? 'rgba(239, 68, 68, 0.35)'
        : 'rgba(34, 197, 94, 0.35)';

      ctx.fillRect(0, y + 1, barWidth, barHeight - 2);

      // Price label
      ctx.fillStyle = isPOC ? '#fbbf24' : 'rgba(156, 163, 175, 0.7)';
      ctx.font = '9px Inter, monospace';
      ctx.textAlign = 'right';
      ctx.fillText(level.price.toFixed(2), width - 2, y + barHeight - 3);
    });
  }, [profileData]);

  if (!stats) {
    return (
      <div className="flex items-center justify-center h-full text-text-dim text-xs">
        Waiting for volume data…
      </div>
    );
  }

  const cvd = stats.cvd ?? 0;
  const vwap = stats.vwap ?? 0;

  return (
    <div className="flex flex-col h-full bg-panel border border-border rounded-lg overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-1.5 border-b border-border bg-card flex-shrink-0">
        <span className="text-[10px] font-bold text-white uppercase tracking-wider">
          Volume Profile
        </span>
        <span className="text-[10px] text-text-dim">{sym}</span>
      </div>

      {/* Stats Row */}
      <div className="grid grid-cols-3 gap-1 px-3 py-2 flex-shrink-0">
        <div className="text-center">
          <div className="text-[9px] text-text-dim uppercase tracking-wider">VWAP</div>
          <div className="text-[11px] font-mono text-orange-400 font-semibold">
            {vwap > 0 ? vwap.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 }) : '—'}
          </div>
        </div>
        <div className="text-center">
          <div className="text-[9px] text-text-dim uppercase tracking-wider">CVD</div>
          <div className={`text-[11px] font-mono font-semibold ${cvd >= 0 ? 'text-green-400' : 'text-red-400'}`}>
            {cvd >= 0 ? '+' : ''}{cvd.toFixed(2)}
          </div>
        </div>
        <div className="text-center">
          <div className="text-[9px] text-text-dim uppercase tracking-wider">Vol</div>
          <div className="text-[11px] font-mono text-white font-semibold">
            {stats.volume > 1000
              ? `${(stats.volume / 1000).toFixed(1)}K`
              : stats.volume.toFixed(2)}
          </div>
        </div>
      </div>

      {/* Profile Canvas */}
      <div className="flex-1 relative min-h-0 px-1 pb-1">
        {profileData ? (
          <div className="flex items-start gap-1 h-full">
            {/* CVD bar on left */}
            <div className="w-2 h-full flex flex-col justify-end">
              <div
                className={`w-full rounded-sm transition-all duration-500 ${cvd >= 0 ? 'bg-green-500/60' : 'bg-red-500/60'}`}
                style={{
                  height: `${Math.min(100, Math.abs(cvd) / (stats.volume || 1) * 200)}%`,
                  marginTop: cvd >= 0 ? 'auto' : 0,
                  marginBottom: cvd < 0 ? 'auto' : 0,
                }}
              />
            </div>
            <canvas ref={canvasRef} className="flex-1 h-full" />
          </div>
        ) : (
          <div className="h-full flex items-center justify-center text-text-dim text-[10px]">
            Awaiting live per-price volume profile…
          </div>
        )}

        {/* POC + VAH / VAL labels */}
        <div className="absolute top-1 left-3 flex flex-col gap-0.5">
          <span className="text-[9px] text-yellow-400 font-bold">POC</span>
          <span className="text-[9px] text-red-400/70">VAH</span>
          <span className="text-[9px] text-green-400/70">VAL</span>
        </div>
      </div>

      {/* Legend */}
      <div className="flex items-center justify-between px-3 py-1 border-t border-border/50 text-[9px] text-text-dim flex-shrink-0">
        <span className="flex items-center gap-1">
          <span className="w-2 h-2 rounded-sm bg-yellow-400/70 inline-block" /> POC
        </span>
        <span className="flex items-center gap-1">
          <span className="w-2 h-2 rounded-sm bg-red-400/40 inline-block" /> Sell
        </span>
        <span className="flex items-center gap-1">
          <span className="w-2 h-2 rounded-sm bg-green-400/40 inline-block" /> Buy
        </span>
      </div>
    </div>
  );
}
