import { useState, useEffect } from 'react'

function App() {
  const [stats, setStats] = useState(null)
  const [connected, setConnected] = useState(false)

  useEffect(() => {
    // Connect to WebSocket
    const ws = new WebSocket('ws://localhost:8080/ws', ['token', 'test-secret'])
    
    ws.onopen = () => setConnected(true)
    ws.onclose = () => setConnected(false)
    ws.onmessage = (event) => {
      console.log('Trade received:', event.data)
    }

    return () => ws.close()
  }, [])

  return (
    <div className="container">
      <header>
        <h1>Robin Trading Console</h1>
        <span className={`status ${connected ? 'online' : 'offline'}`}>
          {connected ? '● LIVE' : '○ OFFLINE'}
        </span>
      </header>

      <main>
        <section className="dashboard-card">
          <h2>Market Activity</h2>
          <p>Listening to live trade stream via WebSocket...</p>
        </section>
        
        <section className="dashboard-card">
          <h2>System Stats</h2>
          {stats ? (
             <pre>{JSON.stringify(stats, null, 2)}</pre>
          ) : (
             <p>Loading stats...</p>
          )}
        </section>
      </main>
    </div>
  )
}

export default App
