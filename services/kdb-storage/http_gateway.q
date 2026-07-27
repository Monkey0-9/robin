/ =============================================================================
/ Robin Trading Platform — KDB+ HTTP REST API Gateway
/ =============================================================================
/ HTTP server on port 5001 bridging to REST API, with:
/   - Bearer token authentication (.z.pw + Authorization header check)
/   - In-memory TTL cache
/   - REST routing table with /health, /trades, /quotes, /stats, /signals, /positions
/   - Prometheus-style /metrics endpoint with latency histograms
/ =============================================================================

/ Load unified schema for table access
\l schema.q

\p 5001

/ --- Authentication ---
.auth.token: .z.getenv `ROBIN_KDB_API_TOKEN;
if[0=count .auth.token;
    -1 "[HTTP_GW] WARNING: ROBIN_KDB_API_TOKEN not set. All requests will be rejected.";
    .auth.token: ""
    ];

.auth.extract_bearer:{[headers]
    pos: headers ss "Bearer ";
    if[0=count pos; :""];
    start: first[pos] + 7;
    rest: start _ headers;
    end: rest ss "\r\n";
    if[0=count end; :rest];
    :(first end) # rest
    };

.auth.check:{[req]
    headers: $[1 < count req; req[1]; ""];
    tok: .auth.extract_bearer headers;
    (tok ~ .auth.token) and 0 < count .auth.token
    };

/ --- In-memory cache ---
.cache.store: ()!();
.cache.get:{[path]
    if[not path in key .cache.store; :(::)];
    entry: .cache.store[path];
    if[.z.p < entry[1]; :entry[0]];
    .cache.store _: path;
    :(::)
    };
.cache.set:{[path; val; ttl_secs]
    .cache.store[path]: (val; .z.p + `long$ttl_secs * 1000000000);
    val
    };

/ --- Request metrics with latency histograms ---
.metrics.req_count: 0;
.metrics.req_auth_fail: 0;
.metrics.req_cache_hit: 0;
.metrics.latency_buckets: 0 1000 5000 10000 50000 100000 500000 1000000 5000000 10000000 50000000 100000000;
.metrics.latency_hist: {[b] b!count[b]#0};

/ Record a latency observation (ns) into histogram buckets
.metrics.record_latency:{[latency_ns]
    .metrics.latency_hist:: .metrics.latency_hist + {[b;l] $[l <= b; 1; 0]}[;latency_ns] each key .metrics.latency_hist;
    };

/ --- REST routing ---
.rest.routes: (`symbol$())!();

.rest.routes[`/health]:  {[req] "{\"status\":\"ok\",\"service\":\"kdb-gateway\"}"};
.rest.routes[`/trades]:  {[req] .j.j select[-100] time, sym, price, size, side, exch from trade};
.rest.routes[`/quotes]:  {[req] .j.j select[-100] time, sym, bid, ask, bsize, asize from quote};
.rest.routes[`/orders]:  {[req] .j.j select[-100] time, sym, order_id, side, price, qty, status from order};
.rest.routes[`/signals]: {[req] .j.j select[-100] time, sym, strategy_id, side, confidence, alpha, reason from signal};
.rest.routes[`/positions]:{[req]
    / Latest position snapshot
    .j.j select from position where time = max time
    };
.rest.routes[`/symbols]: {[req]
    .j.j distinct exec sym from trade
    };
.rest.routes[`/stats]:   {[req]
    .j.j `requests`auth_failures`cache_hits`latency_histogram!(.metrics.req_count; .metrics.req_auth_fail; .metrics.req_cache_hit; .metrics.latency_hist)
    };
.rest.routes[`/metrics]: {[req]
    / Prometheus text format with histogram
    "/# HELP robin_kdb_requests_total Total HTTP requests\n",
    "/# TYPE robin_kdb_requests_total counter\n",
    "robin_kdb_requests_total ", string .metrics.req_count, "\n",
    "/# HELP robin_kdb_auth_failures_total Auth rejection count\n",
    "/# TYPE robin_kdb_auth_failures_total counter\n",
    "robin_kdb_auth_failures_total ", string .metrics.req_auth_fail, "\n",
    "/# HELP robin_kdb_cache_hits_total Cache hit count\n",
    "/# TYPE robin_kdb_cache_hits_total counter\n",
    "robin_kdb_cache_hits_total ", string .metrics.req_cache_hit, "\n",
    "/# HELP robin_kdb_latency_ns Request latency histogram\n",
    "/# TYPE robin_kdb_latency_ns histogram\n",
    {[b] "robin_kdb_latency_ns_bucket{le=\"", string[b], "\"} ", string .metrics.latency_hist[b], "\n"} each key .metrics.latency_hist
    };

.rest.handle:{[req]
    path: `$first req;
    handler: .rest.routes path;
    if[null handler; :"HTTP/1.1 404 Not Found\r\nContent-Type: application/json\r\n\r\n{\"error\":\"not found\"}"];
    handler[req]
    };

/ --- Main HTTP handler ---
.z.ph:{[req]
    start: .z.p;
    .metrics.req_count +: 1;

    path: first req;
    if[path in ("/health"; "/metrics");
        resp: .rest.handle req;
        .metrics.record_latency[`long$.z.p - start];
        :resp
        ];

    if[not .auth.check req;
        .metrics.req_auth_fail +: 1;
        .metrics.record_latency[`long$.z.p - start];
        :"HTTP/1.1 401 Unauthorized\r\nContent-Type: application/json\r\n\r\n{\"error\":\"unauthorized\"}"
        ];

    cached: .cache.get path;
    if[not null cached;
        .metrics.req_cache_hit +: 1;
        .metrics.record_latency[`long$.z.p - start];
        :cached
        ];

    resp: .rest.handle req;
    if[not path ~ "/stats"; .cache.set[path; resp; 60]];
    .metrics.record_latency[`long$.z.p - start];
    resp
    };

/ --- Authentication ---
.ipc.pw: .z.getenv `ROBIN_KDB_IPC_PW;
.z.pw:{[u;p] (0=count .ipc.pw) or (p ~ .ipc.pw)};

-1 "[HTTP_GW] Robin KDB+ HTTP Gateway v2.0 running on port 5001";
-1 "[HTTP_GW] Auth: Bearer token required (except /health, /metrics)";
-1 "[HTTP_GW] Endpoints: /health /metrics /trades /quotes /orders /signals /positions /symbols /stats";
