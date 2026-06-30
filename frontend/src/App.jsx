import { useState, useEffect } from 'react'
import './index.css'

const JWT_TOKEN = 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJyb2Jpbi1zZXJ2aWNlcyIsImV4cCI6NDAwMDAwMDAwMCwiaXNzIjoicm9iaW4tZ2F0ZXdheSIsInJvbGUiOiJ0cmFkZXIifQ.5WK8fvJTxkWq7HuXky1UAjTsiK98SnR-5D3URf__GV4';

function App() {
  const [stats, setStats] = useState(null)
  const [connected, setConnected] = useState(false)
  const [pricing, setPricing] = useState(null)
  const [varResult, setVarResult] = useState(null)
  const [aiChat, setAiChat] = useState('')
  const [aiResponse, setAiResponse] = useState('')
  const [loadingAi, setLoadingAi] = useState(false)
  const [autoTrade, setAutoTrade] = useState(false)
  const [tradeLogs, setTradeLogs] = useState([])

  useEffect(() => {
    // Connect to WebSocket
    const ws = new WebSocket('ws://localhost:8080/ws', ['token', JWT_TOKEN])
    
    ws.onopen = () => setConnected(true)
    ws.onclose = () => setConnected(false)
    ws.onmessage = (event) => {
      console.log('Update received:', event.data)
      try {
        setStats(JSON.parse(event.data))
      } catch (e) {
        // Not json stats
      }
    }

    fetchPricing()
    fetchVar()

    return () => ws.close()
  }, [])

  useEffect(() => {
    let interval;
    if (autoTrade) {
      interval = setInterval(async () => {
        try {
          const marketContext = `Current Pricing: ${JSON.stringify(pricing)}, VaR: ${JSON.stringify(varResult)}`;
          const res = await fetch('http://127.0.0.1:8080/api/ai/trade_decision', {
            method: 'POST',
            headers: {
              'Content-Type': 'application/json',
              'Authorization': `Bearer ${JWT_TOKEN}`
            },
            body: JSON.stringify({ market_context: marketContext })
          });
          const decision = await res.json();
          
          const logEntry = `[${new Date().toLocaleTimeString()}] AI Decision: ${decision.action} ${decision.qty} ${decision.symbol} @ $${decision.price}\nReasoning: ${decision.reasoning}`;
          setTradeLogs(prev => [logEntry, ...prev].slice(0, 10));

          if (decision.action === 'BUY' || decision.action === 'SELL') {
            await fetch('http://127.0.0.1:8080/order', {
              method: 'POST',
              headers: {
                'Content-Type': 'application/json',
                'Authorization': `Bearer ${JWT_TOKEN}`
              },
              body: JSON.stringify({
                symbol: decision.symbol,
                side: decision.action,
                price: decision.price,
                qty: decision.qty,
                order_type: "LIMIT",
                cl_ord_id: `auto-${Date.now()}`
              })
            });
          }
        } catch (e) {
          console.error("Auto trade error:", e);
        }
      }, 15000); // every 15 seconds
    }
    return () => clearInterval(interval);
  }, [autoTrade, pricing, varResult]);

  const fetchPricing = async () => {
    try {
      const res = await fetch('http://127.0.0.1:8080/api/analytics/pricing', {
        method: 'POST',
        headers: { 
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${JWT_TOKEN}` 
        },
        body: JSON.stringify({
          spot: 64500.0,
          strike: 65000.0,
          vol: 0.65,
          rate: 0.05,
          time: 0.0833 // 30 days
        })
      })
      const data = await res.json()
      setPricing(data)
    } catch (e) {
      console.error('Pricing fetch failed:', e)
    }
  }

  const fetchVar = async () => {
    try {
      const res = await fetch('http://127.0.0.1:8080/api/analytics/var', {
        method: 'POST',
        headers: { 
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${JWT_TOKEN}` 
        },
        body: JSON.stringify({
          weights: { "BTC": 0.5, "ETH": 0.5 },
          confidence: 0.99
        })
      })
      const data = await res.json()
      setVarResult(data)
    } catch (e) {
      console.error('VaR fetch failed:', e)
    }
  }

  const handleAiSubmit = async (e) => {
    e.preventDefault()
    if (!aiChat) return
    setLoadingAi(true)
    try {
      const res = await fetch('http://127.0.0.1:8080/api/ai/chat', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${JWT_TOKEN}`
        },
        body: JSON.stringify({ message: aiChat })
      })
      const data = await res.json()
      setAiResponse(data.reply || data.response || "No response")
    } catch (e) {
      setAiResponse("Error reaching AI Advisor.")
    }
    setLoadingAi(false)
  }

  return (
    <div className="container" style={{ fontFamily: 'Inter, sans-serif', maxWidth: '1200px', margin: '0 auto', padding: '2rem' }}>
      <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', borderBottom: '1px solid #333', paddingBottom: '1rem', marginBottom: '2rem' }}>
        <h1 style={{ margin: 0, color: '#f0f0f0' }}>Robin Institutional Trading</h1>
        <div style={{ display: 'flex', gap: '1rem', alignItems: 'center' }}>
          <button 
            onClick={() => setAutoTrade(!autoTrade)}
            style={{ padding: '0.5rem 1rem', borderRadius: '4px', border: 'none', background: autoTrade ? '#ff3366' : '#00ff88', color: '#111', fontWeight: 'bold', cursor: 'pointer' }}
          >
            {autoTrade ? 'Disable Auto-Trade' : 'Enable Auto-Trade'}
          </button>
          <span style={{ color: connected ? '#00ff88' : '#ff3366', fontWeight: 'bold' }}>
            {connected ? '● LIVE' : '○ OFFLINE'}
          </span>
        </div>
      </header>

      <main style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '2rem' }}>
        
        {/* Analytics Section */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '2rem' }}>
          <section style={{ backgroundColor: '#1e1e24', padding: '1.5rem', borderRadius: '8px', border: '1px solid #333' }}>
            <h2 style={{ marginTop: 0, color: '#a0a0ff' }}>Quantitative Analytics</h2>
            
            <div style={{ marginBottom: '1.5rem' }}>
              <h3 style={{ color: '#ccc', fontSize: '1rem' }}>Options Pricing (BTC/USD)</h3>
              {pricing ? (
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem', background: '#2a2a35', padding: '1rem', borderRadius: '6px' }}>
                  <div><strong>Spot:</strong> ${pricing.spot_price}</div>
                  <div><strong>Strike:</strong> ${pricing.strike}</div>
                  <div><strong>IV:</strong> {(pricing.implied_vol * 100).toFixed(1)}%</div>
                  <div><strong>Call Price:</strong> <span style={{ color: '#00ff88' }}>${pricing.call_price.toFixed(2)}</span></div>
                  <div><strong>Put Price:</strong> <span style={{ color: '#ff3366' }}>${pricing.put_price.toFixed(2)}</span></div>
                  <div style={{ gridColumn: 'span 2' }}>
                    <small>Greeks: Delta {pricing.greeks?.delta.toFixed(3)} | Gamma {pricing.greeks?.gamma.toFixed(4)} | Theta {pricing.greeks?.theta.toFixed(2)} | Vega {pricing.greeks?.vega.toFixed(2)}</small>
                  </div>
                </div>
              ) : <p>Loading pricing...</p>}
            </div>

            <div>
              <h3 style={{ color: '#ccc', fontSize: '1rem' }}>Risk (Monte Carlo VaR)</h3>
              {varResult ? (
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1rem', background: '#2a2a35', padding: '1rem', borderRadius: '6px' }}>
                  <div><strong>95% VaR:</strong> <span style={{ color: '#ff9900' }}>${varResult.var_95.toFixed(2)}</span></div>
                  <div><strong>99% CVaR:</strong> <span style={{ color: '#ff3366' }}>${varResult.cvar_99.toFixed(2)}</span></div>
                  <div><strong>Simulations:</strong> {varResult.simulations}</div>
                  <div><strong>Time Horizon:</strong> {varResult.horizon_days} Days</div>
                </div>
              ) : <p>Loading VaR...</p>}
            </div>
          </section>

          <section style={{ backgroundColor: '#1e1e24', padding: '1.5rem', borderRadius: '8px', border: '1px solid #333' }}>
            <h2 style={{ marginTop: 0, color: '#a0a0ff' }}>System Stats</h2>
            {stats ? (
              <pre style={{ background: '#111', padding: '1rem', borderRadius: '4px', overflowX: 'auto', fontSize: '0.85rem' }}>
                {JSON.stringify(stats, null, 2)}
              </pre>
            ) : (
              <p style={{ color: '#888' }}>Waiting for websocket stream...</p>
            )}
          </section>

          <section style={{ backgroundColor: '#1e1e24', padding: '1.5rem', borderRadius: '8px', border: '1px solid #333' }}>
            <h2 style={{ marginTop: 0, color: '#ff9900' }}>Autonomous AI Trade Log</h2>
            <div style={{ background: '#111', borderRadius: '4px', padding: '1rem', minHeight: '150px', maxHeight: '300px', overflowY: 'auto' }}>
              {tradeLogs.length === 0 ? (
                <p style={{ color: '#888' }}>Auto-trade is disabled or waiting for next cycle...</p>
              ) : (
                tradeLogs.map((log, i) => (
                  <pre key={i} style={{ fontSize: '0.8rem', color: '#a0a0ff', borderBottom: '1px solid #333', paddingBottom: '0.5rem', whiteSpace: 'pre-wrap' }}>
                    {log}
                  </pre>
                ))
              )}
            </div>
          </section>
        </div>

        {/* AI Agent Section */}
        <section style={{ backgroundColor: '#1e1e24', padding: '1.5rem', borderRadius: '8px', border: '1px solid #333', display: 'flex', flexDirection: 'column' }}>
          <h2 style={{ marginTop: 0, color: '#a0a0ff' }}>AI Multi-Agent Advisor</h2>
          <p style={{ fontSize: '0.9rem', color: '#888' }}>Ask the research agent about risk metrics or portfolio optimization.</p>
          
          <div style={{ flexGrow: 1, background: '#111', borderRadius: '6px', padding: '1rem', marginBottom: '1rem', minHeight: '300px', display: 'flex', flexDirection: 'column', gap: '1rem', overflowY: 'auto' }}>
             {aiResponse && (
               <div style={{ background: '#2a2a35', padding: '1rem', borderRadius: '6px', color: '#e0e0e0', lineHeight: '1.5' }}>
                 <strong>Agent:</strong> {aiResponse}
               </div>
             )}
          </div>

          <form onSubmit={handleAiSubmit} style={{ display: 'flex', gap: '0.5rem' }}>
            <input 
              type="text" 
              value={aiChat}
              onChange={(e) => setAiChat(e.target.value)}
              placeholder="e.g., Analyze the gamma exposure of our options portfolio..."
              style={{ flexGrow: 1, padding: '0.75rem', borderRadius: '4px', border: '1px solid #444', background: '#222', color: '#fff' }}
            />
            <button 
              type="submit" 
              disabled={loadingAi}
              style={{ padding: '0.75rem 1.5rem', borderRadius: '4px', border: 'none', background: '#3b82f6', color: 'white', fontWeight: 'bold', cursor: 'pointer' }}
            >
              {loadingAi ? 'Thinking...' : 'Send'}
            </button>
          </form>
        </section>

      </main>
    </div>
  )
}

export default App

