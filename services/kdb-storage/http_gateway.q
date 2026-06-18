/ services/kdb-storage/http_gateway.q
/ HTTP server on port 5001 bridging to REST API, Redis cache, and WebSocket streaming.

\p 5001

/ Load real-time WebSocket bridge
\l ws_bridge.q

/ In-memory cache dictionary
.cache.dict:([path:`symbol$()] value:(); expiry:`timestamp$())
.cache.get:{[path]
  if[not null key:.cache.dict[`path]?`$path;
    if[.cache.dict[key;`expiry] > .z.p;
      :.cache.dict[key;`value]]];
  :(::)}
.cache.set:{[path;val;ttl]
  .cache.dict[`$path]:`value`expiry!(val;.z.p+ttl*1000000000);
  val}

/ REST routing table
.rest.routes:(`symbol$())!()
.rest.handle:{[req]
  path: first req;
  handler:.rest.routes[`$path];
  if[null handler; :"404 Not Found"];
  handler[req]}

/ Register default endpoints
.rest.routes[`/health]:{[req]"{\"status\":\"ok\"}"}
.rest.routes[`/trades]:{[req]"{\"trades\":[]}"}
.rest.routes[`/quotes]:{[req]"{\"quotes\":[]}"}

.z.ph:{[req]
    path: req[0];

    / Check cache first
    cachedResp: .cache.get[path];
    if[not null cachedResp; :cachedResp];

    / Process via REST API
    resp: .rest.handle[req];

    / Cache the response
    .cache.set[path; resp; 60]; / Cache for 60 seconds

    :resp
 };
