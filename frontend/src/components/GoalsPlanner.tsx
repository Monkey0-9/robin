// frontend/src/components/GoalsPlanner.tsx
import React, { useState } from 'react';

export default function GoalsPlanner() {
  const [targetCapital, setTargetCapital] = useState(500000);
  const [years, setYears] = useState(10);
  const [riskProfile, setRiskProfile] = useState('Moderate');

  const calculateRequiredMonthly = () => {
    // Basic compounding approximation: A = P * (1 + r)^n
    const rates: Record<string, number> = { 'Conservative': 0.04, 'Moderate': 0.07, 'Aggressive': 0.11 };
    const r = rates[riskProfile];
    const months = years * 12;
    const monthlyRate = r / 12;
    const required = targetCapital / (((Math.pow(1 + monthlyRate, months) - 1) / monthlyRate) * (1 + monthlyRate));
    return isNaN(required) ? 0 : required;
  };

  return (
    <div className="bg-panel border border-border rounded-lg flex flex-col h-full min-h-0 select-none">
      <div className="bg-card px-4 py-2 border-b border-border">
        <span className="font-bold text-xs uppercase tracking-wider text-white">Goals Planning System</span>
      </div>

      <div className="p-4 flex-1 flex flex-col justify-between text-xs space-y-3 font-mono">
        <div className="space-y-3">
          <div className="flex flex-col gap-1">
            <span className="text-text-secondary">Target Capital:</span>
            <input 
              type="number" 
              value={targetCapital} 
              onChange={(e) => setTargetCapital(Number(e.target.value))}
              className="bg-bg-base border border-border rounded px-2.5 py-1 text-white text-xs font-mono w-full"
            />
          </div>

          <div className="flex justify-between items-center">
            <span className="text-text-secondary">Time Horizon (Years):</span>
            <input 
              type="range" 
              min="1" 
              max="40" 
              value={years} 
              onChange={(e) => setYears(Number(e.target.value))}
              className="w-1/2 accent-accent-blue"
            />
            <span className="text-white font-bold">{years} yrs</span>
          </div>

          <div className="flex justify-between items-center">
            <span className="text-text-secondary">Risk Profile:</span>
            <select 
              value={riskProfile} 
              onChange={(e) => setRiskProfile(e.target.value)}
              className="bg-bg-base border border-border rounded text-[10px] px-2 py-0.5 text-white"
            >
              <option value="Conservative">Conservative (4% return)</option>
              <option value="Moderate">Moderate (7% return)</option>
              <option value="Aggressive">Aggressive (11% return)</option>
            </select>
          </div>
        </div>

        <div className="bg-bg-base/60 p-3 rounded border border-border/60 space-y-2">
          <div className="text-[11px] text-text-dim">Estimated Required Monthly Investment:</div>
          <div className="text-lg font-bold text-accent-blue">
            ${calculateRequiredMonthly().toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
          </div>
        </div>
      </div>
    </div>
  );
}
