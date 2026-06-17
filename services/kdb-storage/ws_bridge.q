/ services/kdb-storage/ws_bridge.q
/ WebSocket server on port 5002 to push high-frequency tick updates to front-end clients.

\p 5002

/ Maintain a list of active websocket connection descriptors
activeConns:();

/ Connection opened callback
.z.wo:{[w]
    activeConns::activeConns, w;
    show "WebSocket Connection established from descriptor: ", string w;
    (neg w) "Welcome to Quantum Terminal Real-Time Feed.";
 };

/ Connection closed callback
.z.wc:{[w]
    activeConns::activeConns except w;
    show "WebSocket Connection closed for descriptor: ", string w;
 };

/ Message received callback
.z.ws:{[msg]
    / Simply echo back or parse client subscription requests
    show "Received: ", msg;
 };

/ Function to broadcast tick payload to all active websockets
broadcastTick:{[sym;price;size]
    payload: .j.j `sym`price`size!(sym;price;size);
    {[w;p] (neg w) p}[;payload] each activeConns;
 };

show "WebSocket Tick Bridge active on port 5002."
