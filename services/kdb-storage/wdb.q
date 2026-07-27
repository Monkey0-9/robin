/ =============================================================================
/ Robin Trading Platform — KDB+ Write-Direct Database (WDB)
/ =============================================================================
/ Provides intraday crash recovery by writing to disk on every batch.
/ Also subscribes to TP for redundancy and maintains on-disk state.
/
/ Port: 5013
/
/ Difference from RDB: RDB keeps everything in memory until EOD.
/   WDB writes every N seconds to disk, providing intraday durability.
/ =============================================================================

/ Load unified schema
\l schema.q

\p 5013

/ --- Flush interval ---
.robin.flush_interval: 30;  / flush to disk every 30 seconds

/ --- TP connection ---
.robin.tp_connect:{[]
    host: .z.getenv `ROBIN_TP_HOST;
    if[count[host] = 0; host: "localhost"];
    port: .z.getenv `ROBIN_TP_PORT;
    if[count[port] = 0; port: "5010"];
    h: hopen `$host, ":", port;
    {h (`.u.sub; x; `)} each `trade`quote`order`exec_report`signal`position;
    h
    };

.robin.tp_handle: .robin.tp_connect[];

/ --- TP message handler ---
.u.upd:{[t;x]
    t insert x;
    };

/ --- Periodic flush to disk ---
.robin.flush_buffer:{[]
    tbls: `trade`quote`order`exec_report`signal`position;
    now_dt: .z.D;
    path: .robin.hdb_path;
    / Ensure today's partition exists
    target_dir: ` sv (path; `$string now_dt);
    if[not () ~ key target_dir; @[system; "mkdir \"", (1_string target_dir), "\""; {[e] -1 "[WDB] mkdir error: ", e}]];
    / Save each table that has data
    {[t]
        if[0 < count value t;
            .robin.eod_save[now_dt; t];
            ];
        } each tbls;
    -1 "[WDB] Intraday flush at ", string .z.p, " (", string[now_dt], ")";
    };

/ Timer: flush to disk every .robin.flush_interval seconds
.z.ts:{[x] .robin.flush_buffer[]};
\t .robin.flush_interval * 1000

/ --- EOD handler ---
.u.end:{[dt]
    .robin.flush_buffer[];
    .robin.eod_reset each `trade`quote`order`exec_report`signal`position;
    -1 "[WDB] EOD reset for ", string dt;
    };

/ --- Connection lifecycle ---
.z.pc:{[h]
    if[h = .robin.tp_handle;
        -1 "[WDB] TP disconnected. Reconnecting...";
        system "sleep 5";
        .robin.tp_handle:: .robin.tp_connect[];
        ];
    };

/ --- Auth ---
.robin.password: .z.getenv `ROBIN_KDB_PASSWORD;
if[0 < count .robin.password; .z.pw:{[u;p] p ~ `$.robin.password}];

-1 "[WDB] Robin Write-Direct Database v2.0 running on port 5013";
-1 "[WDB] Flush interval: ", string .robin.flush_interval, "s";
