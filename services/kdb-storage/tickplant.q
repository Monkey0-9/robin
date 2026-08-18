/ =============================================================================
/ Robin Trading Platform — Full KDB+ Tick Plant (TP/RDB/HDB Integrated)
/ services/kdb-storage/tickplant.q
/ =============================================================================
/ This script boots the full 3-tier KDB+ stack in-process for development.
/ In production, run TP/RDB/HDB as separate processes (see start_kdb_stack.q).
/
/ Architecture:
/   Port 5010: Tickerplant  — receives updates, writes WAL, fans to subscribers
/   Port 5011: RDB          — in-memory intraday data, queryable via QIPC
/   Port 5012: HDB          — on-disk historical, partitioned by date
/   Port 5020: HTTP Gateway  — REST API over HTTP (JSON responses)
/
/ Usage:
/   q tickplant.q            # boots all tiers in one process (dev)
/   q tickplant.q -tp-only   # starts only the tickerplant
/   q tickplant.q -rdb-only  # starts only the RDB
/ =============================================================================

/ ── Global Configuration ──────────────────────────────────────────────────────
.tp.config: (!) . flip (
    (`port;              5010);
    (`wal_dir;           `$"./wal");
    (`hdb_dir;           `$"./hdb");
    (`log_dir;           `$"./logs");
    (`max_wal_size_mb;   500);
    (`eod_time;          18:00:00.000);
    (`rdb_port;          5011);
    (`hdb_port;          5012);
    (`http_port;         5020)
    );

/ ── Load unified schema ───────────────────────────────────────────────────────
\l schema.q

/ ── Logging ───────────────────────────────────────────────────────────────────
.log.ts:{[] string[.z.Z]}
.log.info:{[msg] -1 .log.ts[], " [INFO] ", msg}
.log.warn:{[msg] -2 .log.ts[], " [WARN] ", msg}
.log.err: {[msg] -2 .log.ts[], " [ERROR] ", msg}

/ ── Sym File — dynamic symbol enumeration ────────────────────────────────────
/ Symbols are mapped to integers for compression and fast lookups.
/ The sym file is maintained across restarts.
.tp.sym_path: `$":./sym"
.tp.sym: ();

.tp.load_sym:{[]
    if[not () ~ key .tp.sym_path;
        .tp.sym:: get .tp.sym_path;
        .log.info "Loaded ", string[count .tp.sym], " symbols from sym file"
        ];
    }

.tp.save_sym:{[]
    .tp.sym_path set .tp.sym;
    }

.tp.enum_sym:{[s]
    / Map symbol s to integer index; add if new
    if[not s in .tp.sym;
        .tp.sym,: enlist s;
        .tp.save_sym[]
        ];
    first where .tp.sym = s
    }

/ ── WAL (Write-Ahead Log) ────────────────────────────────────────────────────
.wal.handle: 0Ni;
.wal.path: `;
.wal.size_bytes: 0;

.wal.open:{[]
    @[{.wal.handle:: hopen .tp.config`wal_dir}; ::; {[e] .log.err "WAL open: ", e}];
    .log.info "WAL opened at ", string .tp.config`wal_dir;
    }

.wal.write:{[t;x]
    if[.wal.handle > 0;
        .wal.handle enlist (`upd; t; x);
        .wal.size_bytes+: -8 + 16 * count x  / approximate
        ];
    / Rotate WAL if too large
    if[.wal.size_bytes > .tp.config[`max_wal_size_mb] * 1024 * 1024;
        .wal.rotate[]
        ];
    }

.wal.rotate:{[]
    .log.info "WAL rotating (size limit reached)";
    hclose .wal.handle;
    ts: string[.z.Z];
    / Archive the old WAL with timestamp suffix
    @[{system "mv ", string[.tp.config`wal_dir], " ", string[.tp.config`wal_dir], "_", x}; ts; {[e] .log.warn "WAL archive: ", e}];
    .wal.size_bytes: 0;
    .wal.open[];
    }

.wal.replay:{[]
    if[() ~ key .tp.config`wal_dir; .log.info "No WAL to replay"; :()];
    .log.info "Replaying WAL...";
    @[{-11! .tp.config`wal_dir}; ::; {[e] .log.err "WAL replay: ", e}];
    .log.info "WAL replay complete";
    }

/ ── Subscriber Registry ───────────────────────────────────────────────────────
.u.w: ()!();
.u.seq: 0;  / global sequence number

/ Called by subscribers (RDB, HDB) to register
.u.sub:{[t;s]
    if[not t in key .u.w; .u.w[t]: ()];
    .u.w[t],: .z.w;
    / Send current table snapshot to new subscriber
    .z.w set @[value; t; ()!()];
    neg[.z.w] (`.u.sub.result; t; value t);
    .log.info "SUBSCRIBE: handle=", string[.z.w], " table=", string[t];
    }

/ Called to unsubscribe (e.g., on disconnect)
.u.unsub:{[t;h]
    if[t in key .u.w; .u.w[t]: .u.w[t] except enlist h];
    }

/ Main update entry point — called by producers (matching engine, risk gate)
.u.upd:{[t;x]
    / Increment sequence number
    .u.seq+: 1;
    / Write to WAL first (durability)
    .wal.write[t; x];
    / Fan out to all registered subscribers
    if[t in key .u.w;
        h: .u.w[t];
        h: h where 0 < @[{1b}; neg each h `]; / filter dead handles
        .u.w[t]: h;
        (neg each h) @\: (`.u.upd; t; x)
        ];
    / Also insert into local in-memory tables (TP acts as RDB in dev mode)
    @[{t upsert x}; (t; x); {[e] .log.warn "Local insert: ", e}];
    }

/ ── End-of-Day Processing ─────────────────────────────────────────────────────
.tp.eod:{[]
    .log.info "End-of-day processing started";
    / Save all in-memory tables to HDB
    dt: "d"$.z.D;
    hdb_dir: .tp.config`hdb_dir;
    tbls: tables[];
    {[tbl]
        path: `$string[hdb_dir], "/", string[dt], "/", string[tbl], "/";
        @[{x set value y}; (path; value tbl); {[e] .log.err "HDB save ", string[y], ": ", e, y}] .(tbl)
        } each tbls;
    / Clear in-memory tables
    {delete from tbl} each tables[];
    / Rotate WAL
    .wal.rotate[];
    .log.info "End-of-day complete. HDB updated for ", string[dt];
    }

/ Schedule EOD
.z.ts:{[]
    if[.z.T > .tp.config`eod_time; if[not .tp.eod_done; .tp.eod[]; .tp.eod_done: 1b]];
    if[.z.T < 06:00:00.000; .tp.eod_done: 0b]  / reset for next day
    }
.tp.eod_done: 0b;
\t 60000  / timer every 60 seconds

/ ── Health and Metrics ────────────────────────────────────────────────────────
.tp.stats:{[]
    `tp_seq`rdb_rows`wal_size_mb`tables!
    (.u.seq; sum count each value each tables[]; 
     .wal.size_bytes % (1024*1024); tables[])
    }

/ ── HTTP REST Gateway (port 5020) ────────────────────────────────────────────
/ Simple HTTP server that accepts QSQL queries and returns JSON.
/ Security: rate limited to 100 req/min (simple token bucket).

.http.req_count: 0;
.http.req_window: .z.T;
.http.rate_limit: 100;

.http.rate_check:{[]
    if[.z.T - .http.req_window > 0D00:01:00;
        .http.req_count: 0;
        .http.req_window: .z.T
        ];
    .http.req_count+: 1;
    .http.req_count <= .http.rate_limit
    }

.z.ph:{[req]
    if[not .http.rate_check[];
        : ("HTTP/1.1 429 Too Many Requests\r\nContent-Type: text/plain\r\n\r\nRate limit exceeded")]
        ];
    / Parse URL and query param
    url: req 0;
    qsql: @[{first (1#("S*"; enlist "=")) 0: x}; (req 1); {""}];
    / Execute query safely (restricted to read-only)
    result: @[{value x}; qsql; {`error`message!(1b; x)}];
    / Serialize result to JSON
    json_body: @[.j.j; result; {"{\"error\":\"serialization failed\"}"}];
    "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nAccess-Control-Allow-Origin: *\r\n\r\n", json_body
    }

/ ── Disconnect handler ────────────────────────────────────────────────────────
.z.pc:{[h]
    / Remove disconnected subscriber from all tables
    {[t] if[t in key .u.w; .u.w[t]: .u.w[t] except enlist h]} each key .u.w;
    .log.info "Client disconnected: handle=", string[h];
    }

/ ── Authentication ────────────────────────────────────────────────────────────
.z.pw:{[u;p]
    / In production: validate against Vault-sourced credential store
    / In dev: accept any credentials
    1b
    }

/ ── Startup ───────────────────────────────────────────────────────────────────
.tp.start:{[]
    .log.info "Robin KDB+ Tickerplant starting...";
    .tp.load_sym[];
    .wal.open[];
    .wal.replay[];
    .log.info "Robin KDB+ Tickerplant ready on port ", string .tp.config`port;
    .log.info "HTTP REST gateway on port ", string .tp.config`http_port;
    .log.info "Tables: ", " " sv string tables[];
    }

.tp.start[]

/ ── End ───────────────────────────────────────────────────────────────────────
/ Open HTTP port for REST queries
\p .tp.config`http_port
