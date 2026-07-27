/ =============================================================================
/ Robin Trading Platform — KDB+ Tickerplant (TP)
/ =============================================================================
/ Receives data from publishers (matching engine, risk gate, market data),
/ writes to Write-Ahead Log (WAL), and fans out to subscribers (RDB, WDB).
/
/ Port: 5010
/
/ Data flow:
/   Publisher → .u.upd → TP (port 5010) → WAL (disk) → Subscribers (RDB:5011, WDB:5013)
/ =============================================================================

/ Load unified schema
\l schema.q

\p 5010

/ --- WAL (Write-Ahead Log) Setup ---
/ TP logs every update to disk for crash recovery
/ On restart, WAL is replayed to reconstruct state

.robin.wal_path: hsym `$"./wal";
.robin.wal_handle: 0Ni;

.robin.wal_open:{[]
    if[not () ~ key .robin.wal_path;
        / Remove stale lock file
        @[hclose; .robin.wal_handle; {[e] -1 "[TP] No stale WAL handle"}];
        ];
    .robin.wal_handle:: hopen .robin.wal_path;
    -1 "[TP] WAL opened: ", string .robin.wal_path;
    };

.robin.wal_write:{[t;x]
    .robin.wal_handle enlist (`upd; t; x);
    };

.robin.wal_replay:{[]
    if[() ~ key .robin.wal_path; :()];
    -1 "[TP] Replaying WAL...";
    / Read and execute all entries from WAL
    @[system; "l \"", (1_string .robin.wal_path), "\""; {[e] -1 "[TP] WAL replay error: ", e}];
    -1 "[TP] WAL replay complete";
    };

/ --- Subscriber registry ---
/ .u.w: table -> list of subscriber handles
/ Each entry: `trade`quote -> (handle1; handle2)
/ On disconnect, handle is automatically removed

.u.w: ()!();

/ --- Tickerplant protocol ---
/ .u.sub: called by subscribers (RDB, WDB) to register interest
.u.sub:{[t;s]
    if[not t in key .u.w; .u.w[t]: ()];
    .u.w[t],: .z.w;
    .z.w set @[value; t; ()!()];
    neg[.z.w] (`.u.sub.result; t; value t);
    -1 "[TP] SUBSCRIBE: handle=", string[.z.w], " table=", string[t];
    };

/ .u.upd: called by publishers to push data
/ Writes to WAL, inserts locally, and fans out to all subscribers
.u.upd:{[t;x]
    / Write to Write-Ahead Log
    .robin.wal_write[t; x];

    / Insert into local table
    t insert x;

    / Fan out to all subscribers
    if[t in key .u.w;
        {[h;t;x] neg[h] (`.u.upd; t; x)}[;t;x] each .u.w[t];
        ];
    };

/ --- End-of-Day handler ---
/ Closes WAL, opens new WAL for next day, notifies subscribers
.u.end:{[dt]
    -1 "[TP] End of day: ", string dt;

    / Close current WAL
    hclose .robin.wal_handle;

    / Archive WAL with date stamp
    old_name: `$"./wal_", string dt;
    @[system; "mv \"", (1_string .robin.wal_path), "\" \"", (1_string old_name), "\""; {[e] -1 "[TP] WAL archive error: ", e}];

    / Open new WAL for next day
    .robin.wal_open[];

    / Notify all subscribers to end their day
    {[h;d] neg[h] (`.u.end; d)}[;dt] each raze value .u.w;

    / Reset local tables
    .robin.eod_reset each `trade`quote`order`exec_report`signal`position;

    -1 "[TP] EOD complete for ", string dt;
    };

/ --- Connection lifecycle ---
.z.po:{[h]
    -1 "[TP] CONNECT: handle=", string[h], " ip=", string .z.a;
    };

.z.pc:{[h]
    -1 "[TP] DISCONNECT: handle=", string h;
    / Remove disconnected handle from all subscriber lists
    .u.w:: {[k;v] k!v except h}[; .u.w .] each key .u.w;
    };

/ --- Authentication ---
.robin.password: .z.getenv `ROBIN_KDB_PASSWORD;
if[0 < count .robin.password;
    .z.pw:{[u;p] p ~ `$.robin.password};
    -1 "[TP] Auth enabled: ROBIN_KDB_PASSWORD set";
    ];

/ --- Initialization ---
.robin.wal_replay[];
.robin.wal_open[];

-1 "[TP] Robin Tickerplant v2.0 running on port 5010";
-1 "[TP] Tables: trade, quote, order, exec_report, signal, position";
-1 "[TP] WAL: ", string .robin.wal_path;
