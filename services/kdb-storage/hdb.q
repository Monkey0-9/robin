/ =============================================================================
/ Robin Trading Platform — KDB+ Historical Database (HDB)
/ =============================================================================
/ Mounts date-partitioned on-disk historical data written by RDB at EOD.
/ Provides read-only access for backtesting, analytics, and queries.
/
/ Port: 5012
/
/ Partitioning: date-level: hdb/YYYY.MM.DD/trade/, quote/, etc.
/ Attributes: `p# on sym column for fast date+sym lookups
/ Compression: gzip level 6 (configured in schema.q)
/ =============================================================================

/ Load unified schema for table definitions
\l schema.q

\p 5012

/ Load all existing date partitions
.robin.hdb_load[];

/ --- Query endpoints via HTTP ---
.z.ph:{[req]
    path: first req;
    params: `$last req;
    $[path ~ "/health";
        "{\"status\":\"ok\",\"service\":\"hdb\",\"partitions\":" .j.j (string key .robin.hdb_path where 10h = type each key .robin.hdb_path), "}";
    path ~ "/symbols";
        "{\"symbols\":" .j.j distinct exec sym from trade, "}";
    path ~ "/trades";
        .j.j select[-100] time, sym, price, size, side from trade;
    path ~ "/quotes";
        .j.j select[-100] time, sym, bid, ask, bsize, asize from quote;
    path ~ "/orders";
        .j.j select[-100] time, sym, order_id, side, price, qty, status from order;
    path ~ "/signals";
        .j.j select[-100] time, sym, strategy_id, side, confidence, alpha, kelly_pct, reason from signal;
    path ~ "/positions";
        .j.j select from position where time = max time;
    path ~ "/correlation";
        / Compute rolling correlation between first two symbols in trade table
        syms: distinct exec sym from trade;
        $[2 <= count syms;
            [r0: exec price from trade where sym = first syms;
             r1: exec price from trade where sym = syms[1];
             c: (avg r0 * r1) - (avg r0) * (avg r1);
             s0: dev r0; s1: dev r1;
             corr: $[0 < s0 * s1; c % (s0 * s1); 0f];
             .j.j `sym0`sym1`correlation!(first syms; syms[1]; corr)];
            "{\"error\":\"insufficient symbols\"}"
            ];
    path ~ "/var";
        / Historical VaR for all symbols
        syms: distinct exec sym from trade;
        / Daily log returns
        ret: {[s] (0.0, 1_deltas log exec price from trade where sym=s)} each syms;
        var95: {[r] r[where r <= (quantile[r; 0.05])]}[;] each ret;
        .j.j `symbols`sym_var95!(syms; var95);
    / Time-series query: /timeseries?symbol=AAPL&limit=100
    enlist "timeseries" ~ 10#path;
        / Parse query string
        qs: path;
        sym: `$last "=" vs (first "&" vs qs);
        lim: "I"$last "=" vs (last "&" vs qs);
        if[null lim; lim: 100];
        t: select[-lim] time, price, size, side from trade where sym = sym;
        .j.j t;
    / History replay: /replay?date=2026.01.15&symbol=AAPL
    enlist "replay" ~ 6#path;
        qs: path;
        date: `$last "=" vs (first "&" vs qs);
        sym: `$last "=" vs (last "&" vs qs);
        if[null date; :"{\"error\":\"date required\"}"];
        trades: select time, price, size, side from trade where date = date, sym = sym;
        quotes: select time, bid, ask from quote where date = date, sym = sym;
        .j.j `trades`quotes!(trades; quotes);
        "HTTP/1.1 404 Not Found\r\nContent-Type: application/json\r\n\r\n{\"error\":\"not found\"}"
    ]
    };

/ --- Authentication ---
.robin.password: .z.getenv `ROBIN_KDB_PASSWORD;
if[0 < count .robin.password;
    .z.pw:{[u;p] p ~ `$.robin.password};
    -1 "[HDB] Auth enabled";
    ];

-1 "[HDB] Robin Historical Database v2.0 running on port 5012";
-1 "[HDB] Partitions: ", " " sv string key .robin.hdb_path where 10h = type each key .robin.hdb_path;
-1 "[HDB] Endpoints: /health /symbols /trades /quotes /orders /signals /positions /correlation /var";
