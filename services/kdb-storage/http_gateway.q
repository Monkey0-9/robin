/ services/kdb-storage/http_gateway.q
/ HTTP server on port 5001 bridging to REST API, with:
/   - Bearer token authentication (.z.pw + Authorization header check)
/   - In-memory TTL cache
/   - REST routing table with /health, /trades, /quotes, /stats
/   - Prometheus-style /metrics endpoint

\p 5001

/ --- Authentication ---
/ Token loaded from env var ROBIN_KDB_API_TOKEN (never hardcoded)
.auth.token: .z.getenv `ROBIN_KDB_API_TOKEN;
if[0=count .auth.token;
    -1 "[HTTP_GW] WARNING: ROBIN_KDB_API_TOKEN not set. All requests will be rejected.";
    .auth.token: ""
    ];

/ Extract Bearer token from HTTP Authorization header string
.auth.extract_bearer:{[headers]
    / headers is a string like "Authorization: Bearer <token>\r\n..."
    pos: headers ss "Bearer ";
    if[0=count pos; :""];
    start: first[pos] + 7;  / 7 = length of "Bearer "
    rest: start _ headers;
    end: rest ss "\r\n";
    if[0=count end; :rest];
    :(first end) # rest
    };

/ Validate request — returns 1b if authorized
.auth.check:{[req]
    headers: $[1 < count req; req[1]; ""];
    tok: .auth.extract_bearer headers;
    (tok ~ .auth.token) and 0 < count .auth.token
    };

/ --- In-memory cache ---
.cache.store: ()!();  / path -> (value; expiry_ns)

.cache.get:{[path]
    if[not path in key .cache.store; :(::)];
    entry: .cache.store[path];
    if[.z.p < entry[1]; :entry[0]];  / TTL valid
    / Expired — evict
    .cache.store _: path;
    :(::)
    };

.cache.set:{[path; val; ttl_secs]
    .cache.store[path]: (val; .z.p + `long$ttl_secs * 1000000000);
    val
    };

/ --- Request metrics ---
.metrics.req_count: 0;
.metrics.req_auth_fail: 0;
.metrics.req_cache_hit: 0;

/ --- REST routing ---
.rest.routes: (`symbol$())!();

.rest.routes[`/health]:  {[req] "{\"status\":\"ok\",\"service\":\"kdb-gateway\"}"};
.rest.routes[`/trades]:  {[req] .j.j select[-100] from trade};
.rest.routes[`/quotes]:  {[req] .j.j select[-100] from quote};
.rest.routes[`/stats]:   {[req]
    .j.j `requests`auth_failures`cache_hits!(
        .metrics.req_count;
        .metrics.req_auth_fail;
        .metrics.req_cache_hit)
    };
.rest.routes[`/metrics]: {[req]
    / Prometheus text format
    "# HELP robin_kdb_requests_total Total HTTP requests\n",
    "# TYPE robin_kdb_requests_total counter\n",
    "robin_kdb_requests_total ", string .metrics.req_count, "\n",
    "# HELP robin_kdb_auth_failures_total Auth rejection count\n",
    "# TYPE robin_kdb_auth_failures_total counter\n",
    "robin_kdb_auth_failures_total ", string .metrics.req_auth_fail, "\n"
    };

.rest.handle:{[req]
    path: `$first req;
    handler: .rest.routes path;
    if[null handler; :"HTTP/1.1 404 Not Found\r\nContent-Type: application/json\r\n\r\n{\"error\":\"not found\"}"];
    handler[req]
    };

/ --- Main HTTP handler ---
.z.ph:{[req]
    .metrics.req_count +: 1;

    / Skip auth check for /health and /metrics (monitoring probes)
    path: first req;
    if[path in ("/health"; "/metrics");
        :.rest.handle req
        ];

    / Check Bearer token on all other endpoints
    if[not .auth.check req;
        .metrics.req_auth_fail +: 1;
        :"HTTP/1.1 401 Unauthorized\r\nContent-Type: application/json\r\n\r\n{\"error\":\"unauthorized\"}"
        ];

    / Check cache
    cached: .cache.get path;
    if[not null cached;
        .metrics.req_cache_hit +: 1;
        :cached
        ];

    / Process and cache (60s TTL, except /stats which is never cached)
    resp: .rest.handle req;
    if[not path ~ "/stats"; .cache.set[path; resp; 60]];
    resp
    };

/ --- Password-level auth for IPC connections (.z.pw) ---
/ Set ROBIN_KDB_IPC_PW env var to restrict IPC access
.ipc.pw: .z.getenv `ROBIN_KDB_IPC_PW;
.z.pw:{[u;p] (0=count .ipc.pw) or (p ~ .ipc.pw)};

-1 "[HTTP_GW] KDB+ HTTP Gateway running on port 5001";
-1 "[HTTP_GW] Auth: Bearer token required (except /health, /metrics)";
-1 "[HTTP_GW] Set ROBIN_KDB_API_TOKEN env var before starting";
