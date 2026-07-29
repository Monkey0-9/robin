'use client';

import React, { useState, useEffect } from 'react';
import { useRouter } from 'next/navigation';
import { useAuthStore } from '../../store/useAuthStore';
import { Shield, Lock, User, Eye, EyeOff, AlertTriangle, Activity } from 'lucide-react';

export default function LoginPage() {
  const router = useRouter();
  const { login, isAuthenticated, isLoading, error } = useAuthStore();

  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);

  // If already authenticated, redirect to home
  useEffect(() => {
    if (isAuthenticated()) {
      router.push('/');
    }
  }, [isAuthenticated, router]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const ok = await login(username, password);
    if (ok) {
      router.push('/');
    }
  };

  return (
    <div className="min-h-screen bg-void flex items-center justify-center relative overflow-hidden">
      {/* Animated background grid */}
      <div className="absolute inset-0 opacity-10"
        style={{
          backgroundImage: 'linear-gradient(rgba(59,130,246,0.3) 1px, transparent 1px), linear-gradient(90deg, rgba(59,130,246,0.3) 1px, transparent 1px)',
          backgroundSize: '48px 48px',
        }}
      />

      {/* Glow orbs */}
      <div className="absolute top-1/4 left-1/4 w-96 h-96 bg-accent-blue/5 rounded-full blur-3xl" />
      <div className="absolute bottom-1/4 right-1/4 w-96 h-96 bg-accent-purple/5 rounded-full blur-3xl" />

      <div className="relative z-10 w-full max-w-md mx-4">
        {/* Logo & Header */}
        <div className="text-center mb-8">
          <div className="flex items-center justify-center gap-2 mb-4">
            <div className="w-10 h-10 bg-accent-blue/20 rounded-lg flex items-center justify-center border border-accent-blue/40">
              <Activity className="w-6 h-6 text-accent-blue" />
            </div>
            <span className="text-2xl font-bold tracking-tight text-white">Robin Terminal</span>
          </div>
          <p className="text-text-dim text-sm font-mono">Institutional Trading Platform</p>
          <div className="flex items-center justify-center gap-1.5 mt-2">
            <span className="h-1.5 w-1.5 rounded-full bg-accent-green animate-pulse" />
            <span className="text-[10px] text-accent-green font-mono uppercase tracking-wider">System Online</span>
          </div>
        </div>

        {/* Login Card */}
        <div className="bg-panel border border-border rounded-xl shadow-2xl overflow-hidden">
          {/* Card header */}
          <div className="bg-card border-b border-border px-6 py-4 flex items-center gap-2">
            <Shield className="w-4 h-4 text-accent-blue" />
            <span className="text-sm font-bold text-white uppercase tracking-wider">Secure Access</span>
            <span className="ml-auto text-[10px] text-text-dim font-mono">RS256 · JWT · TLS</span>
          </div>

          <form onSubmit={handleSubmit} className="p-6 space-y-4">
            {/* Username */}
            <div>
              <label className="block text-[11px] text-text-dim font-mono uppercase tracking-wider mb-1.5">
                Username
              </label>
              <div className="relative">
                <User className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-text-dim" />
                <input
                  type="text"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder="admin"
                  autoComplete="username"
                  autoFocus
                  className="w-full bg-card border border-border rounded-lg pl-9 pr-4 py-2.5 text-sm text-white placeholder:text-text-dim/50 focus:outline-none focus:border-accent-blue/60 focus:ring-1 focus:ring-accent-blue/20 transition-all font-mono"
                  required
                />
              </div>
            </div>

            {/* Password */}
            <div>
              <label className="block text-[11px] text-text-dim font-mono uppercase tracking-wider mb-1.5">
                Password
              </label>
              <div className="relative">
                <Lock className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-text-dim" />
                <input
                  type={showPassword ? 'text' : 'password'}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="••••••••"
                  autoComplete="current-password"
                  className="w-full bg-card border border-border rounded-lg pl-9 pr-10 py-2.5 text-sm text-white placeholder:text-text-dim/50 focus:outline-none focus:border-accent-blue/60 focus:ring-1 focus:ring-accent-blue/20 transition-all font-mono"
                  required
                />
                <button
                  type="button"
                  onClick={() => setShowPassword(!showPassword)}
                  className="absolute right-3 top-1/2 -translate-y-1/2 text-text-dim hover:text-white transition-colors"
                >
                  {showPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                </button>
              </div>
            </div>

            {/* Error */}
            {error && (
              <div className="flex items-start gap-2 bg-accent-red/10 border border-accent-red/30 rounded-lg px-3 py-2.5">
                <AlertTriangle className="w-4 h-4 text-accent-red mt-0.5 flex-shrink-0" />
                <span className="text-xs text-accent-red font-mono">{error}</span>
              </div>
            )}

            {/* Submit */}
            <button
              type="submit"
              disabled={isLoading || !username || !password}
              className="w-full py-2.5 px-4 bg-accent-blue hover:bg-accent-blue/90 disabled:opacity-50 disabled:cursor-not-allowed text-white font-bold text-sm rounded-lg transition-all shadow-lg shadow-accent-blue/20 flex items-center justify-center gap-2"
            >
              {isLoading ? (
                <>
                  <span className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
                  <span>Authenticating...</span>
                </>
              ) : (
                <>
                  <Shield className="w-4 h-4" />
                  <span>Sign In</span>
                </>
              )}
            </button>
          </form>

          {/* Dev credentials hint */}
          <div className="px-6 pb-5">
            <div className="border border-border/60 rounded-lg px-3 py-2.5 bg-card/50">
              <p className="text-[10px] text-text-dim font-mono uppercase tracking-wider mb-1">Dev Credentials</p>
              <div className="flex gap-4 text-[11px] font-mono">
                <div>
                  <span className="text-text-secondary">Admin: </span>
                  <span className="text-white">admin / admin</span>
                </div>
                <div>
                  <span className="text-text-secondary">Trader: </span>
                  <span className="text-white">trader / trader</span>
                </div>
              </div>
            </div>
          </div>
        </div>

        <p className="text-center text-[10px] text-text-dim/50 font-mono mt-4">
          Robin Institutional Gateway v1.5 · All sessions are audited
        </p>
      </div>
    </div>
  );
}
