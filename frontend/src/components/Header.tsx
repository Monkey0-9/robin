'use client';
import React, { useState, useEffect, useMemo } from 'react';
import { Activity, Clock, User, Shield, Zap, Bird, TrendingUp, TrendingDown, Wifi, WifiOff, LogOut, Bell } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';
import { useAuthStore } from '../store/useAuthStore';
import SymbolSearch from './SymbolSearch';
import { useRouter } from 'next/navigation';

function useLiveClock() {
  const [time, setTime] = useState(new Date());
  useEffect(() => {
    const id = setInterval(() => setTime(new Date()), 1000);
    return () => clearInterval(id);
  }, []);
  return time;
}

function useSessionTimer() {
  const [seconds, setSeconds] = useState(0);
  useEffect(() => {
    const id = setInterval(() => setSeconds(s => s + 1), 1000);
    return () => clearInterval(id);
  }, []);
  const h = Math.floor(seconds / 3600).toString().padStart(2, '0');
  const m = Math.floor((seconds % 3600) / 60).toString().padStart(2, '0');
  const s = (seconds % 60).toString().padStart(2, '0');
  return `${h}:${m}:${s}`;
}

export default function Header() {
  const router = useRouter();
  const { logout } = useAuthStore();
  const systemHealth = useTerminalStore(s => s.systemHealth);
  const equity = useTerminalStore(s => s.equity);
  const balance = useTerminalStore(s => s.balance);
  const positions = useTerminalStore(s => s.positions);
  const workingOrders = useTerminalStore(s => s.workingOrders);
  const [showUserMenu, setShowUserMenu] = useState(false);
  const [showAlerts, setShowAlerts] = useState(false);

  const now = useLiveClock();
  const sessionTime = useSessionTimer();

  const totalUnrealized = positions.reduce((s, p) => s + p.unrealizedPnL, 0);
  const pnlPct = balance > 0 ? ((equity - balance) / balance) * 100 : 0;
  const isConnected = systemHealth.healthy > 0 || systemHealth.degraded > 0;

  const latencyLabel = useMemo(() => {
    const ns = systemHealth.latencyNs;
    if (ns === 0) return '—';
    if (ns < 1000) return `${ns}ns`;
    if (ns < 1_000_000) return `${(ns / 1000).toFixed(1)}μs`;
    return `${(ns / 1_000_000).toFixed(1)}ms`;
  }, [systemHealth.latencyNs]);

  const nyTime = now.toLocaleString('en-US', { timeZone: 'America/New_York', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
  const londonTime = now.toLocaleString('en-GB', { timeZone: 'Europe/London', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
  const tokyoTime = now.toLocaleString('ja-JP', { timeZone: 'Asia/Tokyo', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });

  const handleLogout = () => {
    logout();
    router.push('/login');
  };

  return (
    <header className="bg-panel border-b border-border h-14 px-3 flex items-center justify-between z-40 select-none shadow-md relative">
      {/* Left: Brand + Status */}
      <div className="flex items-center gap-3">
        <div className="flex items-center gap-2">
          <div className="w-7 h-7 rounded-lg overflow-hidden border border-[#C8102E]/30 shadow-lg shadow-[#C8102E]/20 flex-shrink-0">
            <img src="/robin_logo.png" alt="Robin Logo" className="w-full h-full object-cover"
              onError={e => { (e.target as HTMLImageElement).style.display = 'none'; }} />
          </div>
          <span className="font-black text-base tracking-tight text-white">
            ROBIN<span className="text-[#C8102E]">PRO</span>
          </span>
          <span className="text-[9px] font-mono text-text-dim border border-border px-1.5 py-0.5 rounded">v1.5.0</span>
        </div>

        <div className="h-5 w-px bg-border mx-1" />

        {/* System Status */}
        <div className="flex items-center gap-1.5">
          <div className={`flex items-center gap-1 text-[10px] font-mono px-2 py-0.5 rounded border ${isConnected
            ? 'text-accent-green border-accent-green/20 bg-accent-green/8'
            : 'text-accent-red border-accent-red/20 bg-accent-red/8'
            }`}>
            {isConnected ? <Wifi size={9} /> : <WifiOff size={9} />}
            <span className="font-bold">{isConnected ? 'LIVE' : 'OFFLINE'}</span>
          </div>

          <div className="flex items-center gap-1 text-[10px] font-mono px-2 py-0.5 rounded border border-border text-text-secondary">
            <Activity size={9} className="text-accent-purple" />
            <span>{latencyLabel}</span>
          </div>

          {systemHealth.failed > 0 && (
            <div className="flex items-center gap-1 text-[10px] font-mono px-2 py-0.5 rounded border border-accent-red/30 text-accent-red bg-accent-red/8">
              <span>{systemHealth.failed} FAILED</span>
            </div>
          )}
        </div>

        <div className="h-5 w-px bg-border mx-1" />

        {/* P&L Summary */}
        {equity > 0 && (
          <div className="flex items-center gap-2 text-[10px] font-mono">
            <span className="text-text-dim">Equity:</span>
            <span className="text-white font-bold">${equity.toLocaleString(undefined, { minimumFractionDigits: 0, maximumFractionDigits: 0 })}</span>
            <span className={`font-bold flex items-center gap-0.5 ${totalUnrealized >= 0 ? 'text-accent-green' : 'text-accent-red'}`}>
              {totalUnrealized >= 0 ? <TrendingUp size={9} /> : <TrendingDown size={9} />}
              {totalUnrealized >= 0 ? '+' : ''}${totalUnrealized.toFixed(0)} ({pnlPct >= 0 ? '+' : ''}{pnlPct.toFixed(2)}%)
            </span>
            {positions.length > 0 && (
              <span className="text-text-dim">{positions.length} pos</span>
            )}
            {workingOrders.length > 0 && (
              <span className="text-accent-amber font-bold">{workingOrders.length} working</span>
            )}
          </div>
        )}
      </div>

      {/* Center: World Clocks */}
      <div className="flex items-center gap-3 text-[10px] font-mono absolute left-1/2 -translate-x-1/2">
        {[
          { city: 'NY', time: nyTime },
          { city: 'LN', time: londonTime },
          { city: 'TY', time: tokyoTime },
        ].map(({ city, time }) => (
          <div key={city} className="flex items-center gap-1 text-text-dim">
            <span className="text-text-secondary">{city}</span>
            <span className="text-white">{time}</span>
          </div>
        ))}
        <div className="h-4 w-px bg-border" />
        <div className="flex items-center gap-1 text-text-dim">
          <Clock size={9} className="text-accent-blue" />
          <span className="text-accent-blue/80">Session: {sessionTime}</span>
        </div>
      </div>

      {/* Right: Controls */}
      <div className="flex items-center gap-2">
        <SymbolSearch />

        <a
          href="http://localhost:3001"
          target="_blank"
          rel="noreferrer"
          className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-md bg-hover border border-border text-text-secondary hover:bg-hover-light hover:text-white transition-colors text-[10px] font-mono"
          title="Robin AI Engine"
        >
          <Bird size={12} className="text-[#C8102E]" />
          <span className="hidden lg:inline">Engine</span>
        </a>

        {/* Alerts Bell */}
        <div className="relative">
          <button
            onClick={() => setShowAlerts(a => !a)}
            className="flex items-center gap-1 px-2.5 py-1.5 rounded-md bg-hover border border-border text-text-secondary hover:text-white transition-colors"
          >
            <Bell size={12} />
            {workingOrders.length > 0 && (
              <span className="absolute -top-1 -right-1 w-4 h-4 rounded-full bg-accent-amber text-[8px] text-black font-black flex items-center justify-center">
                {workingOrders.length}
              </span>
            )}
          </button>
          {showAlerts && (
            <div className="absolute right-0 top-full mt-1 w-72 bg-panel border border-border rounded-lg shadow-2xl z-50 overflow-hidden">
              <div className="bg-card px-3 py-2 border-b border-border text-[10px] font-bold text-white uppercase tracking-wider flex items-center justify-between">
                <span>Active Alerts</span>
                <button onClick={() => setShowAlerts(false)} className="text-text-dim hover:text-white">×</button>
              </div>
              {workingOrders.length === 0 ? (
                <div className="p-4 text-center text-text-dim text-[10px] font-mono">No active alerts</div>
              ) : (
                <div className="max-h-48 overflow-y-auto">
                  {workingOrders.map(o => (
                    <div key={o.id} className="px-3 py-2 border-b border-border/40 text-[10px] font-mono flex items-center justify-between">
                      <div>
                        <span className={`font-bold mr-1 ${o.side === 'BUY' ? 'text-accent-green' : 'text-accent-red'}`}>{o.side}</span>
                        <span className="text-white">{o.qty} {o.symbol}</span>
                        <span className="text-text-dim ml-1">@ ${o.price.toFixed(2)}</span>
                      </div>
                      <span className="text-accent-amber">{o.status}</span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>

        {/* User Menu */}
        <div className="relative">
          <button
            onClick={() => setShowUserMenu(m => !m)}
            className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-md bg-hover border border-border text-text-secondary hover:text-white transition-colors text-[10px]"
          >
            <User size={12} className="text-accent-blue" />
            <span className="hidden md:inline font-mono">Account</span>
          </button>

          {showUserMenu && (
            <div className="absolute right-0 top-full mt-1 w-52 bg-panel border border-border rounded-lg shadow-2xl z-50 overflow-hidden">
              <div className="bg-card px-3 py-2 border-b border-border">
                <div className="text-[10px] font-bold text-white">Trader Account</div>
                <div className="text-[9px] text-text-dim font-mono mt-0.5">ROLE: TRADER</div>
              </div>
              <div className="p-2 space-y-0.5">
                {[
                  { icon: <Shield size={11} />, label: 'Risk Profile: BALANCED' },
                  { icon: <Activity size={11} />, label: `Session: ${sessionTime}` },
                  { icon: <Zap size={11} />, label: `Latency: ${latencyLabel}` },
                ].map(item => (
                  <div key={item.label} className="flex items-center gap-2 px-2 py-1.5 rounded text-[10px] text-text-secondary font-mono">
                    <span className="text-text-dim">{item.icon}</span>
                    {item.label}
                  </div>
                ))}
                <div className="border-t border-border mt-1 pt-1">
                  <button
                    onClick={handleLogout}
                    className="w-full flex items-center gap-2 px-2 py-1.5 rounded text-[10px] text-accent-red hover:bg-accent-red/10 font-mono transition-colors"
                  >
                    <LogOut size={11} />
                    Sign Out
                  </button>
                </div>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Click-away overlay for dropdowns */}
      {(showUserMenu || showAlerts) && (
        <div
          className="fixed inset-0 z-40"
          onClick={() => { setShowUserMenu(false); setShowAlerts(false); }}
        />
      )}
    </header>
  );
}