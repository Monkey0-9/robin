import React, { useState, useEffect } from 'react';
import { Cpu, Activity, TrendingUp, TrendingDown, RefreshCw, BarChart2, PlayCircle, StopCircle, Zap } from 'lucide-react';

export default function AIAutonomousPanel() {
  const [status, setStatus] = useState({
    enabled: false,
    macro_pulse: 0.0,
    optimal_weights: {} as Record<string, number>,
    recent_trades: [] as any[]
  });

  const fetchStatus = async () => {
    try {
      const res = await fetch('http://localhost:8000/autonomous/status');
      if (res.ok) {
        const data = await res.json();
        setStatus(data);
      }
    } catch (e) {
      console.error("AI service unreachable", e);
    }
  };

  useEffect(() => {
    fetchStatus();
    const interval = setInterval(fetchStatus, 2000);
    return () => clearInterval(interval);
  }, []);

  const handleToggle = async () => {
    try {
      const res = await fetch('http://localhost:8000/autonomous/toggle', { method: 'POST' });
      if (res.ok) {
        const data = await res.json();
        setStatus(s => ({ ...s, enabled: data.autonomous_enabled }));
      }
    } catch (e) {
      console.error("Failed to toggle AI", e);
    }
  };

  return (
    <div className="flex flex-col h-full gap-4 p-4 overflow-y-auto text-primary">
      <div className="flex items-center gap-2 mb-2">
        <Cpu className="w-5 h-5 text-accent-blue" />
        <h2 className="text-xl font-bold tracking-tight">Institutional AI Autonomous Agent</h2>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {/* Core Control Panel */}
        <div className={`p-4 rounded border ${status.enabled ? 'bg-accent-blue/10 border-accent-blue' : 'bg-card border-border'} shadow-sm flex flex-col justify-between`}>
          <div>
            <div className="flex justify-between items-center mb-4">
              <h3 className="font-mono text-sm font-bold text-muted-foreground">24/7 TRADING ENGINE</h3>
              {status.enabled ? (
                <div className="flex items-center gap-1 text-accent-green text-xs font-bold animate-pulse">
                  <Activity size={14} /> ACTIVE
                </div>
              ) : (
                <div className="text-muted-foreground text-xs font-bold">STANDBY</div>
              )}
            </div>
            <p className="text-xs mb-4">Autonomous execution of mean-reversion strategies derived from 100-year deep learning backtests.</p>
          </div>
          
          <button
            onClick={handleToggle}
            className={`w-full py-2 px-3 flex items-center justify-center gap-2 text-xs uppercase tracking-wider rounded font-bold transition-colors ${
              status.enabled 
                ? 'bg-accent-red hover:bg-accent-red/80 text-void' 
                : 'bg-accent-green hover:bg-accent-green/80 text-void'
            }`}
          >
            {status.enabled ? <><StopCircle size={16} /> Halt AI Agent</> : <><PlayCircle size={16} /> Engage AI Agent</>}
          </button>
        </div>

        {/* Global Macro Sentiment */}
        <div className="p-4 rounded border bg-card border-border shadow-sm flex flex-col justify-between">
          <div>
            <div className="flex justify-between items-center mb-4">
              <h3 className="font-mono text-sm font-bold text-muted-foreground">MICROSECOND MACRO PULSE</h3>
              <Zap className="w-5 h-5 text-accent-orange" />
            </div>
            <p className="text-xs mb-2 text-muted-foreground">Global real-time NLP sentiment aggregation.</p>
            
            <div className="flex items-end gap-3 mt-4">
              <div className={`text-4xl font-bold ${status.macro_pulse > 0 ? 'text-accent-green' : 'text-accent-red'}`}>
                {status.macro_pulse > 0 ? '+' : ''}{status.macro_pulse.toFixed(2)}
              </div>
              <div className="mb-1">
                {status.macro_pulse > 0.5 ? (
                  <span className="text-xs font-bold text-accent-green flex items-center"><TrendingUp size={14} className="mr-1"/> EXTREME BULLISH</span>
                ) : status.macro_pulse < -0.5 ? (
                  <span className="text-xs font-bold text-accent-red flex items-center"><TrendingDown size={14} className="mr-1"/> EXTREME BEARISH</span>
                ) : (
                  <span className="text-xs font-bold text-muted-foreground flex items-center"><RefreshCw size={14} className="mr-1"/> NEUTRAL</span>
                )}
              </div>
            </div>
          </div>
        </div>

        {/* Portfolio Optimizer */}
        <div className="p-4 rounded border bg-card border-border shadow-sm">
          <div className="flex justify-between items-center mb-4">
            <h3 className="font-mono text-sm font-bold text-muted-foreground">SMART PORTFOLIO OPTIMIZER</h3>
            <BarChart2 className="w-5 h-5 text-accent-blue" />
          </div>
          <p className="text-xs mb-3 text-muted-foreground">Real-time MPT rebalancing for Max Sharpe Ratio.</p>
          
          <div className="space-y-2">
            {Object.keys(status.optimal_weights).length > 0 ? (
              Object.entries(status.optimal_weights).map(([symbol, weight]) => (
                <div key={symbol} className="flex items-center justify-between text-xs font-mono">
                  <span>{symbol}</span>
                  <div className="flex-1 mx-3 h-1.5 bg-muted rounded overflow-hidden">
                    <div className="h-full bg-accent-blue" style={{ width: `${weight * 100}%` }}></div>
                  </div>
                  <span>{(weight * 100).toFixed(1)}%</span>
                </div>
              ))
            ) : (
              <div className="text-xs text-muted-foreground/50 italic">Awaiting AI initialization...</div>
            )}
          </div>
        </div>
      </div>

      {/* Autonomous Action Log */}
      <div className="mt-4 border border-border rounded bg-card flex-1 flex flex-col overflow-hidden">
        <div className="p-3 border-b border-border/50 bg-muted/20">
          <h3 className="font-mono text-xs font-bold text-muted-foreground uppercase">Autonomous Decision Log</h3>
        </div>
        <div className="p-4 overflow-y-auto font-mono text-[11px] flex flex-col gap-2">
          {status.recent_trades.length > 0 ? (
            [...status.recent_trades].reverse().map((trade, i) => (
              <div key={i} className="flex gap-4 items-start border-b border-border/30 pb-2">
                <span className={`px-2 py-0.5 rounded font-bold ${trade.action === 'BUY' ? 'bg-accent-green/20 text-accent-green' : 'bg-accent-red/20 text-accent-red'}`}>
                  {trade.action}
                </span>
                <span className="font-bold w-16">{trade.symbol}</span>
                <span className="text-muted-foreground flex-1">{trade.reason}</span>
              </div>
            ))
          ) : (
            <div className="text-muted-foreground/50 italic py-4 text-center">No autonomous actions executed yet. Engage the AI Engine to begin.</div>
          )}
        </div>
      </div>
    </div>
  );
}
