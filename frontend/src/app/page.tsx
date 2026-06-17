'use client';

import React, { useEffect, useState } from 'react';
import { useTerminalStore } from '../store/useTerminalStore';
import Header from '../components/Header';
import Watchlist from '../components/Watchlist';
import TradingViewChart from '../components/TradingViewChart';
import OrderBook from '../components/OrderBook';
import OrderEntry from '../components/OrderEntry';
import PositionsTable from '../components/PositionsTable';
import OrdersTable from '../components/OrdersTable';
import RiskMetrics from '../components/RiskMetrics';
import AIPanel from '../components/AIPanel';
import NewsFeed from '../components/NewsFeed';
import Disclaimers from '../components/Disclaimers';
import {
  Download,
  AlertTriangle,
  Activity,
  Award,
  BookOpen,
  PieChart,
  LayoutGrid,
  Heart
} from 'lucide-react';

export default function Dashboard() {
  const init = useTerminalStore((state) => state.init);
  const assets = useTerminalStore((state) => state.assets);
  const selectedSymbol = useTerminalStore((state) => state.selectedSymbol);
  const notification = useTerminalStore((state) => state.notification);
  const dismissNotification = useTerminalStore((state) => state.dismissNotification);
  const exportToCSV = useTerminalStore((state) => state.exportToCSV);
  const tradeHistory = useTerminalStore((state) => state.tradeHistory);
  const positions = useTerminalStore((state) => state.positions);
  const balance = useTerminalStore((state) => state.balance);
  const equity = useTerminalStore((state) => state.equity);

  const [activeTab, setActiveTab] = useState<'execution' | 'portfolio' | 'risk' | 'ai' | 'help'>('execution');

  // Trigger state init on mount
  useEffect(() => {
    init();
  }, [init]);

  // Handle toast timeout auto-dismiss
  useEffect(() => {
    if (notification) {
      const timer = setTimeout(() => {
        dismissNotification();
      }, 5000);
      return () => clearTimeout(timer);
    }
  }, [notification, dismissNotification]);



  // Format price decimals based on asset type
  const formatPrice = (symbol: string, p: number) => {
    if (symbol === "EUR/USD") return p.toFixed(4);
    if (p > 1000) return p.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
    return p.toFixed(2);
  };

  return (
    <div className="h-screen w-screen flex flex-col overflow-hidden bg-void text-primary">
      {/* Top Banner Disclaimer */}
      <div className="bg-amber-950/60 border-b border-amber-900/60 text-amber-300 px-4 py-1 text-center text-[10px] tracking-wide font-medium flex items-center justify-center gap-1.5 z-50">
        <AlertTriangle size={11} className="text-accent-amber animate-pulse" />
        <span>EDUCATIONAL DEMONSTRATION TERMINAL ONLY — SIMULATION MODE ENABLED. NO REAL CAPITAL IS DEPLOYED.</span>
      </div>

      {/* Header Panel */}
      <Header />

      {/* Ticker Tape */}
      <div className="bg-card border-b border-border/80 h-7 overflow-hidden flex items-center relative select-none z-30">
        <div className="flex gap-8 whitespace-nowrap animate-[marquee_50s_linear_infinite] hover:[animation-play-state:paused] px-4">
          {assets.map((asset, index) => {
            const isPos = asset.dailyChangePct >= 0;
            return (
              <div key={`${asset.symbol}-${index}`} className="flex items-center gap-1.5 font-mono text-[10px]">
                <span className={`font-bold ${isPos ? 'text-accent-green' : 'text-accent-red'}`}>{asset.symbol}</span>
                <span className={`${isPos ? 'text-accent-green/80' : 'text-accent-red/80'}`}>{formatPrice(asset.symbol, asset.currentPrice)}</span>
                <span className={`font-semibold ${isPos ? 'text-accent-green' : 'text-accent-red'}`}>
                  {isPos ? '▲' : '▼'} {isPos ? '+' : ''}{asset.dailyChangePct}%
                </span>
              </div>
            );
          })}
          {/* Duplicate for seamless scrolling marquee */}
          {assets.map((asset, index) => {
            const isPos = asset.dailyChangePct >= 0;
            return (
              <div key={`${asset.symbol}-dup-${index}`} className="flex items-center gap-1.5 font-mono text-[10px]">
                <span className={`font-bold ${isPos ? 'text-accent-green' : 'text-accent-red'}`}>{asset.symbol}</span>
                <span className={`${isPos ? 'text-accent-green/80' : 'text-accent-red/80'}`}>{formatPrice(asset.symbol, asset.currentPrice)}</span>
                <span className={`font-semibold ${isPos ? 'text-accent-green' : 'text-accent-red'}`}>
                  {isPos ? '▲' : '▼'} {isPos ? '+' : ''}{asset.dailyChangePct}%
                </span>
              </div>
            );
          })}
        </div>
      </div>

      {/* Main Grid Workspace Container */}
      <div className="flex-1 flex overflow-hidden min-h-0">
        {/* Navigation Sidebar */}
        <aside className="w-14 bg-panel border-r border-border flex flex-col items-center py-4 justify-between select-none z-30">
          <div className="flex flex-col gap-3 w-full px-2">
            {/* Execution tab */}
            <button
              onClick={() => setActiveTab('execution')}
              className={`flex flex-col items-center gap-1 py-2.5 rounded-lg text-text-dim hover:text-text-secondary transition-all w-full ${
                activeTab === 'execution' ? 'bg-accent-blue-dim text-accent-blue font-bold border-l-2 border-accent-blue rounded-l-none' : ''
              }`}
              title="Execution Terminal"
            >
              <LayoutGrid size={18} />
              <span className="text-[8px] uppercase tracking-wider font-semibold">Trade</span>
            </button>

            {/* Portfolio tab */}
            <button
              onClick={() => setActiveTab('portfolio')}
              className={`flex flex-col items-center gap-1 py-2.5 rounded-lg text-text-dim hover:text-text-secondary transition-all w-full ${
                activeTab === 'portfolio' ? 'bg-accent-blue-dim text-accent-blue font-bold border-l-2 border-accent-blue rounded-l-none' : ''
              }`}
              title="Portfolio Sync"
            >
              <PieChart size={18} />
              <span className="text-[8px] uppercase tracking-wider font-semibold">Port</span>
            </button>

            {/* Risk tab */}
            <button
              onClick={() => setActiveTab('risk')}
              className={`flex flex-col items-center gap-1 py-2.5 rounded-lg text-text-dim hover:text-text-secondary transition-all w-full ${
                activeTab === 'risk' ? 'bg-accent-blue-dim text-accent-blue font-bold border-l-2 border-accent-blue rounded-l-none' : ''
              }`}
              title="Risk Settings"
            >
              <Activity size={18} />
              <span className="text-[8px] uppercase tracking-wider font-semibold">Risk</span>
            </button>

            {/* AI Signal Pane */}
            <button
              onClick={() => setActiveTab('ai')}
              className={`flex flex-col items-center gap-1 py-2.5 rounded-lg text-text-dim hover:text-text-secondary transition-all w-full ${
                activeTab === 'ai' ? 'bg-accent-blue-dim text-accent-blue font-bold border-l-2 border-accent-blue rounded-l-none' : ''
              }`}
              title="AI Signals & News"
            >
              <Award size={18} />
              <span className="text-[8px] uppercase tracking-wider font-semibold">Signals</span>
            </button>
          </div>

          <div className="flex flex-col gap-3 w-full px-2">
            {/* Help/Disclaimer */}
            <button
              onClick={() => setActiveTab('help')}
              className={`flex flex-col items-center gap-1 py-2.5 rounded-lg text-text-dim hover:text-text-secondary transition-all w-full ${
                activeTab === 'help' ? 'bg-accent-blue-dim text-accent-blue font-bold border-l-2 border-accent-blue rounded-l-none' : ''
              }`}
              title="Education Guide"
            >
              <BookOpen size={18} />
              <span className="text-[8px] uppercase tracking-wider font-semibold">Guide</span>
            </button>
          </div>
        </aside>

        {/* View Workspace Contents */}
        <main className="flex-1 overflow-hidden min-h-0 bg-bg-base relative">
          
          {/* TAB 1: EXECUTION WORKSPACE */}
          {activeTab === 'execution' && (
            <div className="h-full p-2.5 grid grid-cols-12 gap-2.5 overflow-hidden">
              {/* Left Column: Watchlist (Grid col-span-3) */}
              <div className="col-span-12 lg:col-span-3 h-full min-h-0">
                <Watchlist />
              </div>

              {/* Center Column: Chart (top) & Active Positions/Orders (bottom) */}
              <div className="col-span-12 lg:col-span-6 h-full flex flex-col gap-2.5 min-h-0">
                <div className="flex-[3] min-h-0">
                  <TradingViewChart />
                </div>
                <div className="flex-[2] grid grid-cols-2 gap-2.5 min-h-0">
                  <div className="col-span-2 md:col-span-1 min-h-0">
                    <PositionsTable />
                  </div>
                  <div className="col-span-2 md:col-span-1 min-h-0">
                    <OrdersTable />
                  </div>
                </div>
              </div>

              {/* Right Column: Order Book (top) & Order Ticket (bottom) */}
              <div className="col-span-12 lg:col-span-3 h-full flex flex-col gap-2.5 min-h-0">
                <div className="flex-[3] min-h-0">
                  <OrderBook />
                </div>
                <div className="flex-[2] min-h-0">
                  <OrderEntry />
                </div>
              </div>
            </div>
          )}

          {/* TAB 2: PORTFOLIO WORKSPACE */}
          {activeTab === 'portfolio' && (
            <div className="h-full p-6 overflow-auto scrollbar space-y-6">
              <div className="flex items-center justify-between border-b border-border pb-3">
                <div>
                  <h2 className="text-lg font-bold text-white">Portfolio Management Synchronization</h2>
                  <p className="text-xs text-text-secondary">Simulated ledger accounting & capital distributions.</p>
                </div>
                {/* Download CSV button */}
                <button
                  onClick={exportToCSV}
                  disabled={tradeHistory.length === 0}
                  className="bg-accent-blue hover:bg-blue-600 disabled:opacity-40 disabled:cursor-not-allowed text-white font-bold text-xs px-3 py-1.5 rounded flex items-center gap-1.5 transition-all shadow-lg"
                >
                  <Download size={13} />
                  <span>Export Trade Log (CSV)</span>
                </button>
              </div>

              {/* Portfolio stats cards */}
              <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
                <div className="bg-panel border border-border rounded-lg p-4">
                  <span className="text-[10px] uppercase tracking-wider text-text-dim font-bold">Total Capital AUM</span>
                  <div className="font-mono text-xl font-bold text-white mt-1">
                    ${balance.toLocaleString(undefined, { minimumFractionDigits: 2 })}
                  </div>
                  <div className="text-[10px] text-text-secondary mt-1 flex items-center gap-1">
                    <span>Account initial: $100,000.00</span>
                  </div>
                </div>
                <div className="bg-panel border border-border rounded-lg p-4">
                  <span className="text-[10px] uppercase tracking-wider text-text-dim font-bold">Total Portfolio Equity</span>
                  <div className="font-mono text-xl font-bold text-accent-blue mt-1">
                    ${equity.toLocaleString(undefined, { minimumFractionDigits: 2 })}
                  </div>
                  <div className="text-[10px] text-accent-green mt-1">
                    Floating P&L: ${(equity - balance) >= 0 ? '+' : ''}${(equity - balance).toLocaleString(undefined, { minimumFractionDigits: 2 })}
                  </div>
                </div>
                <div className="bg-panel border border-border rounded-lg p-4">
                  <span className="text-[10px] uppercase tracking-wider text-text-dim font-bold">Leverage Used</span>
                  <div className="font-mono text-xl font-bold text-accent-purple mt-1">
                    {positions.length > 0 ? 'Dynamic' : '0.00x'}
                  </div>
                  <div className="text-[10px] text-text-secondary mt-1">
                    Open Positions: {positions.length}
                  </div>
                </div>
                <div className="bg-panel border border-border rounded-lg p-4">
                  <span className="text-[10px] uppercase tracking-wider text-text-dim font-bold">Export Logs Status</span>
                  <div className="font-mono text-xl font-bold text-accent-amber mt-1">
                    {tradeHistory.length} Trades
                  </div>
                  <div className="text-[10px] text-text-secondary mt-1">
                    Pending logs: {tradeHistory.filter((t: any) => t.realizedPnL === 0).length}
                  </div>
                </div>
              </div>

              {/* Holdings Section */}
              <div className="bg-panel border border-border rounded-lg overflow-hidden">
                <div className="bg-card px-4 py-2 border-b border-border font-bold text-xs uppercase tracking-wider">
                  Open Exposure Ledger
                </div>
                <div className="p-4">
                  {positions.length === 0 ? (
                    <div className="text-center py-6 text-text-dim font-mono text-xs">
                      No open holdings. Open trades in the Execution tab.
                    </div>
                  ) : (
                    <table className="w-full text-left text-xs border-collapse">
                      <thead>
                        <tr className="border-b border-border text-text-dim font-mono uppercase text-[10px]">
                          <th className="py-2">Symbol</th>
                          <th className="py-2">Side</th>
                          <th className="py-2 text-right">Size</th>
                          <th className="py-2 text-right">Entry Avg</th>
                          <th className="py-2 text-right">Required Margin</th>
                          <th className="py-2 text-right">Floating P&L</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-border/20 font-mono">
                        {positions.map((pos) => (
                          <tr key={pos.symbol}>
                            <td className="py-3 font-bold text-white">{pos.symbol}</td>
                            <td className="py-3">
                              <span className={`px-1.5 py-0.5 rounded font-bold text-[9px] ${
                                pos.side === 'LONG' ? 'text-accent-green bg-emerald-950/40 border border-emerald-900/60' : 'text-accent-red bg-red-950/40 border border-red-900/60'
                              }`}>{pos.side}</span>
                            </td>
                            <td className="py-3 text-right text-white">{pos.size}</td>
                            <td className="py-3 text-right text-text-secondary">${pos.entryPrice.toLocaleString()}</td>
                            <td className="py-3 text-right text-accent-purple">${pos.marginRequired.toLocaleString(undefined, { minimumFractionDigits: 2 })}</td>
                            <td className="py-3 text-right">
                              <span className={pos.unrealizedPnL >= 0 ? 'text-accent-green' : 'text-accent-red'}>
                                {pos.unrealizedPnL >= 0 ? '+' : ''}${pos.unrealizedPnL.toLocaleString()}
                              </span>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )}
                </div>
              </div>
            </div>
          )}

          {/* TAB 3: RISK METRICS */}
          {activeTab === 'risk' && (
            <div className="h-full p-6 overflow-auto scrollbar space-y-6">
              <div className="border-b border-border pb-3">
                <h2 className="text-lg font-bold text-white">Risk Management Controls</h2>
                <p className="text-xs text-text-secondary">Audit drawdown parameters and configure loss safeguards.</p>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
                <div className="md:col-span-1">
                  <RiskMetrics />
                </div>
                
                <div className="md:col-span-2 space-y-4">
                  <div className="bg-panel border border-border rounded-lg p-4 space-y-3">
                    <h3 className="text-sm font-bold text-white">Understanding Risk Gate Enforcement</h3>
                    <div className="space-y-2 text-xs text-text-secondary leading-relaxed">
                      <p>
                        ⚡ <strong className="text-white">Drawdown Limits:</strong> Max drawdown measures your peak account value versus the current equity level. Minimizing drawdown is the core metric used to assess institutional trader performance.
                      </p>
                      <p>
                        🛡️ <strong className="text-white">Margin Calls (50%):</strong> If your margin ratio approaches high thresholds and equity drops to 50% of the required margin cache, an automatic liquidation fires. The simulator immediately trigger the Emergency Kill Switch to close all open trades to prevent negative balance states.
                      </p>
                      <p>
                        🚨 <strong className="text-white">Daily Loss Safeguard:</strong> This customizable threshold will lock trading actions in the ticket panel if loss targets are breached. Feel free to adjust the gate value to simulate tighter constraints.
                      </p>
                    </div>
                  </div>

                  <div className="bg-panel border border-border rounded-lg p-4">
                    <h3 className="text-xs uppercase tracking-wider text-text-dim font-bold mb-2">Simulated Stress Tests</h3>
                    <div className="space-y-2 text-xs font-mono">
                      <div className="flex justify-between border-b border-border/40 py-1.5">
                        <span className="text-text-secondary">Crypto Market Shock (-15%)</span>
                        <span className="text-accent-red font-semibold">Simulated Margin Call</span>
                      </div>
                      <div className="flex justify-between border-b border-border/40 py-1.5">
                        <span className="text-text-secondary">Equity Index Decline (-5%)</span>
                        <span className="text-accent-amber font-semibold">Elevated Volatility Check</span>
                      </div>
                      <div className="flex justify-between border-b border-border/40 py-1.5">
                        <span className="text-text-secondary">USD Sudden Breakout (+2%)</span>
                        <span className="text-accent-green font-semibold">Exposure Balance Preserved</span>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          )}

          {/* TAB 4: AI SIGNAL & NEWS FEED */}
          {activeTab === 'ai' && (
            <div className="h-full p-2.5 grid grid-cols-12 gap-2.5 overflow-hidden">
              <div className="col-span-12 md:col-span-6 h-full min-h-0">
                <AIPanel />
              </div>
              <div className="col-span-12 md:col-span-6 h-full min-h-0">
                <NewsFeed />
              </div>
            </div>
          )}

          {/* TAB 5: HELP & DISCLAIMERS */}
          {activeTab === 'help' && (
            <div className="h-full p-6 overflow-auto scrollbar">
              <div className="max-w-3xl mx-auto">
                <Disclaimers />
              </div>
            </div>
          )}

        </main>
      </div>

      {/* Footer Bar */}
      <footer className="h-7 min-h-[28px] bg-panel border-t border-border flex items-center justify-between px-4 z-40 text-[10px] text-text-dim select-none">
        <div className="flex items-center gap-1">
          <span>Simulation Speed: </span>
          <span className="text-accent-blue font-bold font-mono">1.0s / Tick</span>
        </div>
        <div className="flex items-center gap-1">
          <span>Made with</span>
          <Heart size={10} className="text-accent-red fill-accent-red" />
          <span>for Educational Purposes Only</span>
        </div>
      </footer>

      {/* Floating Notifications (Toast) */}
      {notification && (
        <div className="fixed bottom-10 right-4 z-50 pointer-events-none max-w-sm w-full animate-toast-in">
          <div className={`p-3 rounded border shadow-xl flex items-start gap-2.5 pointer-events-auto bg-panel ${
            notification.type === 'success'
              ? 'border-accent-green/60 text-accent-green bg-emerald-950/20'
              : notification.type === 'error'
              ? 'border-accent-red/60 text-accent-red bg-red-950/20'
              : 'border-accent-blue/60 text-accent-blue bg-blue-950/20'
          }`}>
            <div className="flex-1 text-[11px] font-mono leading-relaxed">
              {notification.message}
            </div>
            <button
              onClick={dismissNotification}
              className="text-text-dim hover:text-white text-xs px-1 font-bold"
            >
              ×
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
