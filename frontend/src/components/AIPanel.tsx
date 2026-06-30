import React, { useState, useEffect, useRef } from 'react';
import { BrainCircuit, Send, Play, Square, Loader2 } from 'lucide-react';
import { useTerminalStore } from '../store/useTerminalStore';

export default function AIPanel() {
  const [messages, setMessages] = useState<{ role: 'user' | 'ai', content: string }[]>([
    { role: 'ai', content: 'Hello! I am your AI Trading Assistant. Ask me anything about the market or enable Auto Trading.' }
  ]);
  const [input, setInput] = useState('');
  const [isTyping, setIsTyping] = useState(false);
  
  const [autoTradeEnabled, setAutoTradeEnabled] = useState(false);
  const [autoTradeStatus, setAutoTradeStatus] = useState<string>('Inactive');

  const messagesEndRef = useRef<HTMLDivElement>(null);

  // Store selections for context
  const selectedSymbol = useTerminalStore((state) => state.selectedSymbol);
  const assets = useTerminalStore((state) => state.assets);
  const orderBook = useTerminalStore((state) => state.orderBook);
  const submitOrder = useTerminalStore((state) => state.submitOrder);
  const showNotification = useTerminalStore((state) => state.showNotification);

  // Scroll to bottom of chat
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  // Chat Submission
  const handleChatSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!input.trim()) return;

    const userMessage = input;
    setInput('');
    setMessages(prev => [...prev, { role: 'user', content: userMessage }]);
    setIsTyping(true);

    try {
      const res = await fetch('http://localhost:8000/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: userMessage })
      });
      const data = await res.json();
      setMessages(prev => [...prev, { role: 'ai', content: data.reply || 'No response.' }]);
    } catch (err: any) {
      setMessages(prev => [...prev, { role: 'ai', content: `Error connecting to AI: ${err.message}` }]);
    } finally {
      setIsTyping(false);
    }
  };

  // Auto Trading Loop
  useEffect(() => {
    let interval: NodeJS.Timeout;

    if (autoTradeEnabled) {
      setAutoTradeStatus('Analyzing market...');
      interval = setInterval(async () => {
        try {
          const currentAsset = assets.find(a => a.symbol === selectedSymbol);
          if (!currentAsset) return;

          const context = `Symbol: ${selectedSymbol}, Current Price: ${currentAsset.currentPrice}, Daily Change: ${currentAsset.dailyChangePct}%
Bids: ${JSON.stringify(orderBook.bids.slice(0, 3))}
Asks: ${JSON.stringify(orderBook.asks.slice(0, 3))}`;

          setAutoTradeStatus('Requesting trade decision...');
          
          const res = await fetch('http://localhost:8000/trade_decision', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ market_context: context })
          });
          
          const decision = await res.json();
          
          if (decision.action === 'BUY' || decision.action === 'SELL') {
            setAutoTradeStatus(`Executing ${decision.action} ${decision.qty} @ ${decision.price}`);
            submitOrder(decision.symbol || selectedSymbol, decision.action, decision.price, decision.qty, false);
            showNotification(`AI placed ${decision.action} order for ${decision.symbol}`, 'success');
            
            // Log it into chat context
            setMessages(prev => [...prev, { role: 'ai', content: `Auto-Trade Action: ${decision.action} ${decision.qty} ${decision.symbol} @ ${decision.price}\nReason: ${decision.reasoning}` }]);
          } else {
            setAutoTradeStatus('Holding position...');
          }
        } catch (err: any) {
          setAutoTradeStatus(`Error: ${err.message}`);
          setAutoTradeEnabled(false);
          showNotification('Auto trading disabled due to error', 'error');
        }
      }, 10000); // Check every 10 seconds
    } else {
      setAutoTradeStatus('Inactive');
    }

    return () => {
      if (interval) clearInterval(interval);
    };
  }, [autoTradeEnabled, selectedSymbol, assets, orderBook, submitOrder, showNotification]);

  return (
    <div className='bg-panel border border-border rounded-lg h-full flex flex-col overflow-hidden shadow-lg'>
      <div className='h-8 border-b border-border bg-card px-3 flex justify-between items-center gap-2'>
        <div className='flex items-center gap-2'>
          <BrainCircuit size={14} className='text-accent-purple' />
          <span className='text-xs font-bold text-white uppercase tracking-wider'>AI Agent</span>
        </div>
        <div className='flex items-center gap-3'>
          <span className='text-[10px] text-text-secondary truncate max-w-[120px]'>{autoTradeStatus}</span>
          <button 
            onClick={() => setAutoTradeEnabled(!autoTradeEnabled)}
            className={`flex items-center gap-1 text-[10px] px-2 py-1 rounded font-bold uppercase tracking-wider transition-colors ${
              autoTradeEnabled ? 'bg-accent-red/20 text-accent-red hover:bg-accent-red/30' : 'bg-accent-green/20 text-accent-green hover:bg-accent-green/30'
            }`}
          >
            {autoTradeEnabled ? <Square size={10} fill="currentColor" /> : <Play size={10} fill="currentColor" />}
            {autoTradeEnabled ? 'Stop Auto' : 'Start Auto'}
          </button>
        </div>
      </div>
      
      {/* Chat Messages Area */}
      <div className='flex-1 p-4 flex flex-col gap-3 overflow-y-auto bg-bg-base/50'>
        {messages.map((msg, idx) => (
          <div key={idx} className={`flex flex-col ${msg.role === 'user' ? 'items-end' : 'items-start'}`}>
            <span className='text-[10px] text-text-dim mb-1 font-bold tracking-widest uppercase'>{msg.role === 'user' ? 'You' : 'Agent'}</span>
            <div className={`p-2 rounded text-xs leading-relaxed max-w-[85%] whitespace-pre-wrap ${
              msg.role === 'user' ? 'bg-accent-blue/20 text-accent-blue-dim border border-accent-blue/30' : 'bg-card border border-border text-text-secondary'
            }`}>
              {msg.content}
            </div>
          </div>
        ))}
        {isTyping && (
          <div className='flex items-center gap-2 text-text-dim text-xs'>
            <Loader2 size={12} className='animate-spin' /> AI is analyzing...
          </div>
        )}
        <div ref={messagesEndRef} />
      </div>

      {/* Chat Input Area */}
      <form onSubmit={handleChatSubmit} className='p-3 bg-card border-t border-border flex items-center gap-2'>
        <input 
          type='text' 
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder='Ask for analysis or instructions...' 
          className='flex-1 bg-panel border border-border rounded px-3 py-1.5 text-xs text-white focus:outline-none focus:border-accent-purple/50 transition-colors'
        />
        <button 
          type='submit' 
          disabled={!input.trim() || isTyping}
          className='p-1.5 rounded bg-accent-purple/20 text-accent-purple hover:bg-accent-purple/30 disabled:opacity-50 transition-colors'
        >
          <Send size={14} />
        </button>
      </form>
    </div>
  );
}