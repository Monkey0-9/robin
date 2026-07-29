/**
 * AIPanel — Quantitative Chat Assistant (Monitoring Mode)
 *
 * Architecture note (institutional standard):
 *   Auto-trading strategies MUST run on backend compute grids, not in the browser.
 *   Opening multiple tabs would otherwise spawn duplicate trading bots.
 *   Closing the laptop would silently halt execution with no audit trail.
 *
 *   This panel is a *read-only monitoring and chat interface only*.
 *   It forwards natural-language queries to the gateway's /api/ai/chat endpoint
 *   and displays AI responses. It never autonomously submits orders.
 *
 *   To enable/disable backend auto-trading strategies, use the dedicated
 *   Strategy Management panel (Phase 2 roadmap).
 */

import React, { useState, useEffect, useRef } from 'react';
import { BrainCircuit, Send, Loader2, Info } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';
import { useAuthStore } from '../store/useAuthStore';

const GATEWAY_URL = process.env.NEXT_PUBLIC_GATEWAY_URL || 'http://localhost:8080';


interface Message {
  role: 'user' | 'ai';
  content: string;
  timestamp: Date;
}

export default function AIPanel() {
  const [messages, setMessages] = useState<Message[]>([
    {
      role: 'ai',
      content:
        'Hello! I am your AI Trading Assistant. Ask me anything about the market, your positions, or risk exposure.\n\n⚠️ Auto-trading strategies run on the backend server. Use the Strategy Management panel to enable or disable them.',
      timestamp: new Date(),
    },
  ]);
  const [input, setInput] = useState('');
  const [isTyping, setIsTyping] = useState(false);
  const [wsStatus, setWsStatus] = useState<'connected' | 'disconnected'>('disconnected');

  const messagesEndRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // Store selectors — read-only
  const selectedSymbol = useTerminalStore(state => state.selectedSymbol);
  const positions = useTerminalStore(state => state.positions);
  const balance = useTerminalStore(state => state.balance);
  const equity = useTerminalStore(state => state.equity);

  // Poll backend gateway health to reflect WS status
  useEffect(() => {
    const check = async () => {
      try {
        const res = await fetch(`${GATEWAY_URL}/health`);
        setWsStatus(res.ok ? 'connected' : 'disconnected');
      } catch {
        setWsStatus('disconnected');
      }
    };
    check();
    const id = setInterval(check, 10_000);
    return () => clearInterval(id);
  }, []);

  // Auto-scroll to latest message
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  const handleChatSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const text = input.trim();
    if (!text) return;

    const userMsg: Message = { role: 'user', content: text, timestamp: new Date() };
    setInput('');
    setMessages(prev => [...prev, userMsg]);
    setIsTyping(true);

    try {
      // Build a concise, sanitized context — no raw order book blobs
      const context = [
        `Selected symbol: ${selectedSymbol}`,
        `Account balance: $${balance.toLocaleString(undefined, { maximumFractionDigits: 2 })}`,
        `Portfolio equity: $${equity.toLocaleString(undefined, { maximumFractionDigits: 2 })}`,
        positions.length > 0
          ? `Open positions: ${positions.map(p => `${p.symbol} ${p.side} x${p.size}`).join(', ')}`
          : 'No open positions.',
      ].join('\n');

      const res = await fetch(`${GATEWAY_URL}/api/ai/chat`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${useAuthStore.getState().getToken() || ''}`,
        },
        body: JSON.stringify({
          message: text,
          context, // sanitized — no raw order book data
        }),
        signal: AbortSignal.timeout(30_000),
      });

      if (!res.ok) {
        throw new Error(`Gateway returned ${res.status}`);
      }

      const data = await res.json();
      const reply = data.reply || data.message || 'No response from AI.';
      setMessages(prev => [...prev, { role: 'ai', content: reply, timestamp: new Date() }]);
    } catch (err: any) {
      setMessages(prev => [
        ...prev,
        {
          role: 'ai',
          content: `⚠️ Unable to reach AI backend: ${err.message || 'Unknown error'}`,
          timestamp: new Date(),
        },
      ]);
    } finally {
      setIsTyping(false);
      inputRef.current?.focus();
    }
  };

  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      {/* Header */}
      <div className='h-8 border-b border-border bg-card px-3 flex justify-between items-center gap-2 flex-shrink-0'>
        <div className='flex items-center gap-2'>
          <BrainCircuit size={14} className='text-accent-purple' />
          <span className='text-xs font-bold text-white uppercase tracking-wider'>AI Agent</span>
        </div>
        <div className='flex items-center gap-2'>
          <span
            className={`h-1.5 w-1.5 rounded-full ${wsStatus === 'connected' ? 'bg-accent-green' : 'bg-accent-red'} animate-pulse`}
          />
          <span className='text-[10px] text-text-secondary'>
            {wsStatus === 'connected' ? 'Backend Connected' : 'Backend Offline'}
          </span>
        </div>
      </div>

      {/* Architecture notice banner */}
      <div className='flex items-start gap-2 px-3 py-2 bg-amber-950/20 border-b border-amber-900/30 text-[10px] text-amber-400/80 flex-shrink-0'>
        <Info size={10} className='mt-0.5 flex-shrink-0' />
        <span>
          Auto-trading runs on the backend server. This panel is for monitoring and analysis only.
        </span>
      </div>

      {/* Chat Messages */}
      <div className='flex-1 p-3 flex flex-col gap-3 overflow-y-auto bg-bg-base/50 min-h-0'>
        {messages.map((msg, idx) => (
          <div key={idx} className={`flex flex-col ${msg.role === 'user' ? 'items-end' : 'items-start'}`}>
            <div className='flex items-center gap-1.5 mb-1'>
              <span className='text-[9px] text-text-dim font-bold tracking-widest uppercase'>
                {msg.role === 'user' ? 'You' : 'Agent'}
              </span>
              <span className='text-[9px] text-text-dim opacity-50'>
                {msg.timestamp.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
              </span>
            </div>
            <div
              className={`p-2.5 rounded text-xs leading-relaxed max-w-[88%] whitespace-pre-wrap ${
                msg.role === 'user'
                  ? 'bg-accent-blue/20 text-accent-blue-dim border border-accent-blue/30'
                  : 'bg-card border border-border text-text-secondary'
              }`}
            >
              {msg.content}
            </div>
          </div>
        ))}
        {isTyping && (
          <div className='flex items-center gap-2 text-text-dim text-xs'>
            <Loader2 size={12} className='animate-spin' />
            <span>AI is analyzing…</span>
          </div>
        )}
        <div ref={messagesEndRef} />
      </div>

      {/* Input */}
      <form onSubmit={handleChatSubmit} className='p-3 bg-card border-t border-border flex items-center gap-2 flex-shrink-0'>
        <input
          ref={inputRef}
          type='text'
          value={input}
          onChange={e => setInput(e.target.value)}
          placeholder='Ask about market conditions, risk, or positions…'
          className='flex-1 bg-panel border border-border rounded px-3 py-1.5 text-xs text-white focus:outline-none focus:border-accent-purple/50 transition-colors'
          disabled={isTyping}
        />
        <button
          type='submit'
          disabled={!input.trim() || isTyping}
          className='p-1.5 rounded bg-accent-purple/20 text-accent-purple hover:bg-accent-purple/30 disabled:opacity-50 transition-colors'
          aria-label='Send message'
        >
          <Send size={14} />
        </button>
      </form>
    </div>
  );
}