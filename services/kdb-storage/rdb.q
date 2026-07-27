/ =============================================================================
/ Robin Trading Platform — KDB+ Real-Time Database (RDB)
/ =============================================================================
/ Subscribes to TP (port 5010), maintains intraday in-memory state.
/ At End-of-Day, writes tables to HDB directory and resets.
/
/ Port: 5011
/
/ Tables: trade, quote, order, exec_report, signal, position
/ =============================================================================

/ Load unified schema
\l schema.q

\p 5011

/ --- Subscription to TP ---
/ Connect to TP and subscribe to all tables

.robin.tp_host: `$".z.getenv[`ROBIN_TP_HOST];  / default: localhost
.robin.tp_port: 5010;

/ Attempt to read TP host from env, fall back to localhost
.robin.tp_connect:{[]
    host: .z.getenv `ROBIN_TP_HOST;
    if[count[host] = 0; host: "localhost"];
    port: .z.getenv `ROBIN_TP_PORT;
    if[count[port] = 0; port: "5010"];
    h: hopen `$host, ":", port;
    / Subscribe to all tables
    {h (`.u.sub; x; `)} each `trade`quote`order`exec_report`signal`position;
    h
    };

.robin.tp_handle: .robin.tp_connect[];
-1 "[RDB] Connected to TP at ", string .robin.tp_handle;

/ --- TP message handlers ---
/ .u.upd: called by TP to push new data
.u.upd:{[t;x]
    t insert x;
    };

/ Signal aggregation: compute per-symbol statistics every 10 seconds
.robin.agg_signal:{[]
    / Last signal per symbol from composite engine (strategy_id=255)
    s: select last confidence, last alpha, last side, last price by sym from signal where strategy_id=255;
    -1 "[RDB] Signal snapshot: ", .j.j s;
    };

/ Run aggregation every 10 seconds
.z.ts:{[x]
    if[100 < count signal; .robin.agg_signal[]];
    };
\t 10000

/ --- End-of-Day handler ---
/ Called by TP at EOD. Writes all tables to HDB, then resets.
.u.end:{[dt]
    -1 "[RDB] End of day: ", string dt;

    / Write each table to HDB partition
    .robin.eod_save[dt;] each `trade`quote`order`exec_report`signal`position;

    / Reset in-memory tables
    .robin.eod_reset each `trade`quote`order`exec_report`signal`position;

    / Run garbage collector
    .Q.gc[];

    -1 "[RDB] EOD complete for ", string dt;
    };

/ --- Connection lifecycle ---
.z.pc:{[h]
    if[h = .robin.tp_handle;
        -1 "[RDB] TP disconnected. Reconnecting in 5s...";
        system "sleep 5";
        .robin.tp_handle:: .robin.tp_connect[];
        ];
    };

/ --- HTTP health endpoint ---
.z.ph:{[req]
    path: first req;
    $[path ~ "/health";
        "{\"status\":\"ok\",\"service\":\"rdb\",\"tables\":\"trade,quote,order,exec_report,signal,position\",\"symbols\":" .j.j exec distinct sym from trade, "}";
        "HTTP/1.1 404 Not Found\r\nContent-Type: application/json\r\n\r\n{\"error\":\"not found\"}"
        ]
    };

/ --- Authentication ---
.robin.password: .z.getenv `ROBIN_KDB_PASSWORD;
if[0 < count .robin.password;
    .z.pw:{[u;p] p ~ `$.robin.password};
    ];

-1 "[RDB] Robin Real-Time Database v2.0 running on port 5011";
-1 "[RDB] Tables: trade, quote, order, exec_report, signal, position";
-1 "[RDB] Connected to TP: ", string .robin.tp_handle;
