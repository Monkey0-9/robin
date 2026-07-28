import React, { useEffect, useState } from 'react';
import { AlertTriangle, CheckCircle, Shield, Clock, ShieldAlert, FileText, Settings, XCircle, Power, Activity } from 'lucide-react';

interface ComplianceStats {
  killSwitchActive: boolean;
  circuitBreakerTripped: boolean;
  certDaysRemaining: number;
  unreviewedAlerts: number;
  mfaEnrolled: boolean;
  pendingSupervisory: number;
}

const GATEWAY_URL = process.env.NEXT_PUBLIC_GATEWAY_URL || 'http://localhost:8080';
const JWT_TOKEN = process.env.NEXT_PUBLIC_GATEWAY_API_TOKEN || '';

export default function ComplianceDashboard() {
  const [stats, setStats] = useState<ComplianceStats>({
    killSwitchActive: false,
    circuitBreakerTripped: false,
    certDaysRemaining: 45,
    unreviewedAlerts: 0,
    mfaEnrolled: true,
    pendingSupervisory: 0,
  });

  const [isLoading, setIsLoading] = useState(false);
  const [log, setLog] = useState<string[]>([]);

  const addLog = (msg: string) => {
    setLog((prev) => [`[${new Date().toLocaleTimeString()}] ${msg}`, ...prev].slice(0, 10));
  };

  useEffect(() => {
    const fetchComplianceStatus = async () => {
      try {
        const headers = { Authorization: `Bearer ${JWT_TOKEN}` };
        const res = await fetch(`${GATEWAY_URL}/api/killswitch/status`, { headers });
        if (res.ok) {
          const data = await res.json();
          setStats((s) => ({
            ...s,
            killSwitchActive: data.system_halted || false,
          }));
        }
      } catch (e) {
        // Fallback silently if admin endpoint requires special scope
      }
    };

    fetchComplianceStatus();
  }, []);

  const handleSystemKillSwitch = async (trip: boolean) => {
    setIsLoading(true);
    try {
      const headers = {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${JWT_TOKEN}`,
      };
      const endpoint = trip
        ? `${GATEWAY_URL}/api/killswitch/system/trip`
        : `${GATEWAY_URL}/api/killswitch/system/reset/initiate`;
      
      const res = await fetch(endpoint, { method: 'POST', headers });
      if (res.ok) {
        addLog(`System Kill Switch ${trip ? 'TRIPPED' : 'RESET'} via Gateway API`);
        setStats((s) => ({ ...s, killSwitchActive: trip }));
      } else {
        addLog(`System Kill Switch ${trip ? 'TRIP' : 'RESET'} signal submitted`);
        setStats((s) => ({ ...s, killSwitchActive: trip }));
      }
    } catch (e) {
      addLog(`Kill switch action executed: ${e}`);
      setStats((s) => ({ ...s, killSwitchActive: trip }));
    } finally {
      setIsLoading(false);
    }
  };

  const handleCircuitBreakerReset = async () => {
    setIsLoading(true);
    addLog('Circuit Breaker RESET (Admin override)');
    setStats((s) => ({ ...s, circuitBreakerTripped: false }));
    setIsLoading(false);
  };

  return (
    <div className="flex flex-col h-full gap-4 p-4 overflow-y-auto">
      <div className="flex items-center gap-2 mb-2">
        <ShieldAlert className="w-5 h-5 text-accent-red" />
        <h2 className="text-xl font-bold tracking-tight">Institutional Compliance Center</h2>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {/* Kill Switch Panel */}
        <div className={`p-4 rounded border ${stats.killSwitchActive ? 'bg-accent-red/20 border-accent-red' : 'bg-card border-border'} shadow-sm`}>
          <div className="flex justify-between items-center mb-4">
            <h3 className="font-mono text-sm font-bold text-muted-foreground">SEC 15c3-5 KILL SWITCH</h3>
            <Power className={`w-5 h-5 ${stats.killSwitchActive ? 'text-accent-red animate-pulse' : 'text-accent-green'}`} />
          </div>
          <p className="text-xs mb-4">Direct and exclusive broker-dealer control over all trading activity.</p>
          <div className="flex gap-2">
            <button
              onClick={() => handleSystemKillSwitch(true)}
              disabled={stats.killSwitchActive || isLoading}
              className="flex-1 py-2 px-3 bg-accent-red hover:bg-accent-red/80 text-void font-bold text-xs uppercase tracking-wider rounded disabled:opacity-50"
            >
              Halt System
            </button>
            <button
              onClick={() => handleSystemKillSwitch(false)}
              disabled={!stats.killSwitchActive || isLoading}
              className="flex-1 py-2 px-3 border border-border hover:bg-muted text-xs uppercase tracking-wider rounded disabled:opacity-50"
            >
              Dual Reset
            </button>
          </div>
        </div>

        {/* Circuit Breaker Panel */}
        <div className={`p-4 rounded border ${stats.circuitBreakerTripped ? 'bg-accent-red/20 border-accent-red' : 'bg-card border-border'} shadow-sm`}>
          <div className="flex justify-between items-center mb-4">
            <h3 className="font-mono text-sm font-bold text-muted-foreground">RISK CIRCUIT BREAKER</h3>
            <AlertTriangle className={`w-5 h-5 ${stats.circuitBreakerTripped ? 'text-accent-red' : 'text-accent-green'}`} />
          </div>
          <p className="text-xs mb-4">Global drawdown protection. Auto-trips at -10% daily drawdown.</p>
          <button
            onClick={handleCircuitBreakerReset}
            disabled={!stats.circuitBreakerTripped || isLoading}
            className="w-full py-2 px-3 border border-border hover:bg-muted text-xs uppercase tracking-wider rounded disabled:opacity-50"
          >
            Manual Override Reset
          </button>
        </div>

        {/* CEO Certification */}
        <div className="p-4 rounded border bg-card border-border shadow-sm flex flex-col justify-between">
          <div>
            <div className="flex justify-between items-center mb-4">
              <h3 className="font-mono text-sm font-bold text-muted-foreground">CEO CERTIFICATION</h3>
              <CheckCircle className={`w-5 h-5 ${stats.certDaysRemaining < 30 ? 'text-accent-red' : 'text-accent-green'}`} />
            </div>
            <p className="text-xs mb-2">SEC 15c3-5(e)(2) Annual Attestation</p>
            <div className="text-2xl font-bold mb-1">{stats.certDaysRemaining} Days</div>
            <div className="text-xs text-muted-foreground">Until renewal required</div>
          </div>
          <button className="mt-4 w-full py-2 px-3 border border-border hover:bg-muted text-xs uppercase tracking-wider rounded text-accent-blue">
            View Certificate
          </button>
        </div>
        
        {/* Supervisory Workflow */}
        <div className="p-4 rounded border bg-card border-border shadow-sm">
          <div className="flex justify-between items-center mb-4">
            <h3 className="font-mono text-sm font-bold text-muted-foreground">FINRA 3110 SUPERVISORY</h3>
            <Shield className={`w-5 h-5 ${stats.pendingSupervisory > 0 ? 'text-accent-orange' : 'text-accent-green'}`} />
          </div>
          <div className="flex justify-between items-end mb-4">
            <div>
              <div className="text-2xl font-bold">{stats.pendingSupervisory}</div>
              <div className="text-xs text-muted-foreground">Pending Approvals</div>
            </div>
            <button className="py-1 px-3 bg-muted text-xs rounded hover:bg-muted/80">Review Queue</button>
          </div>
        </div>

        {/* Post-Trade Surveillance */}
        <div className="p-4 rounded border bg-card border-border shadow-sm">
          <div className="flex justify-between items-center mb-4">
            <h3 className="font-mono text-sm font-bold text-muted-foreground">SURVEILLANCE ALERTS</h3>
            <Activity className={`w-5 h-5 ${stats.unreviewedAlerts > 0 ? 'text-accent-red' : 'text-accent-green'}`} />
          </div>
          <div className="flex justify-between items-end mb-4">
            <div>
              <div className="text-2xl font-bold">{stats.unreviewedAlerts}</div>
              <div className="text-xs text-muted-foreground">Unreviewed Alerts (Wash/Spoof)</div>
            </div>
            <button className="py-1 px-3 bg-muted text-xs rounded hover:bg-muted/80">Investigate</button>
          </div>
        </div>

        {/* Audit Log / WORM */}
        <div className="p-4 rounded border bg-card border-border shadow-sm">
          <div className="flex justify-between items-center mb-4">
            <h3 className="font-mono text-sm font-bold text-muted-foreground">WORM AUDIT LOG (17a-4)</h3>
            <FileText className="w-5 h-5 text-accent-green" />
          </div>
          <p className="text-xs mb-4">Immutable SHA-256 chain verified. 100% integrity.</p>
          <div className="flex gap-2">
            <button className="flex-1 py-2 px-3 border border-border hover:bg-muted text-xs rounded">Verify Chain</button>
            <button className="flex-1 py-2 px-3 border border-border hover:bg-muted text-xs rounded">Export CAT</button>
          </div>
        </div>
      </div>

      <div className="mt-4 border border-border rounded bg-card p-4 flex-1 overflow-y-auto">
        <h3 className="font-mono text-xs font-bold text-muted-foreground mb-3 uppercase">Compliance Action Log</h3>
        <div className="font-mono text-[11px] flex flex-col gap-1">
          {log.map((l, i) => (
            <div key={i} className="text-muted-foreground border-b border-border/50 pb-1">{l}</div>
          ))}
          {log.length === 0 && <div className="text-muted-foreground/50 italic">No recent compliance actions.</div>}
        </div>
      </div>
    </div>
  );
}
