/ services/kdb-storage/http_gateway.q
/ HTTP server on port 5001 bridging to REST API, Redis cache, and WebSocket streaming.

\p 5001

/ Load REST and Cache APIs
\l rest_api.q
\l redis_cache.q

/ Load real-time WebSocket bridge
\l ws_bridge.q

.z.ph: {[req]
    path: req[0];
    
    / Check cache first
    cachedResp: .redis.get[path];
    if[not null cachedResp; :cachedResp];
    
    / Process via REST API
    resp: .rest.handle[req];
    
    / Cache the response
    .redis.set[path; resp; 60]; / Cache for 60 seconds
    
    :resp
 };
