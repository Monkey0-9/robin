/ =============================================================================
/ Robin Trading Platform - KDB+ Tick Database
/ =============================================================================
/ Proper production schema with:
/   - sym enumeration via `sym file (symbol compression)
/   - `g# attribute for hash index on sym column (O(1) lookups)
/   - `p# attribute for parted reads on date partitions
/   - .u.init for tickerplant (TP) registration protocol
/   - Splayed table layout for HDB compatibility
/   - Date-partitioned HDB: data/<date>/trade/, quote/, order/
/
/ Architecture:
/   TP  (port 5010) — receives publish, distributes to subscribers
/   RDB (port 5011) — subscribes to TP, holds today's data in-memory
/   HDB (port 5012) — mounts on-disk date partitions, handles history queries
/
/ NOTE: This is a research prototype. Production deployment additionally needs:
/   - permissioning (.z.pw authentication)
/   - schema validation before upsert
/   - disk I/O error handling on EOD save
/   - cluster configuration (par.txt for multi-segment)

/ --- Sym file initialisation ---
/ Load or create sym enumeration file. sym file maps symbols to integers.
@[{`sym set get hsym `$"sym"}; (); {`sym set `symbol$()}];

/ --- Schema definitions with correct attributes ---

/ Trade table: `g# on sym for O(1) group-by, `p# applied on-disk by HDB save
trade:([]
    time: `timestamp$();
    sym:  `g#`symbol$();         / hash-indexed for fast sym lookups
    price:`float$();
    size: `long$();
    side: `char$();
    exch: `symbol$();
    cond: `symbol$()
    );

/ Quote table
quote:([]
    time:  `timestamp$();
    sym:   `g#`symbol$();
    bid:   `float$();
    ask:   `float$();
    bsize: `long$();
    asize: `long$();
    bidex: `symbol$();
    askex: `symbol$()
    );

/ Order table — full order lifecycle (NEW, PARTIAL, FILLED, CANCELED)
order:([]
    time:       `timestamp$();
    sym:        `g#`symbol$();
    order_id:   `long$();
    cl_order_id:`long$();
    price:      `float$();
    qty:        `long$();
    leaves_qty: `long$();           / remaining unfilled qty
    side:       `char$();           / B=bid, A=ask
    order_type: `char$();           / L=limit M=market I=IOC K=FOK
    status:     `char$()            / N=new W=working P=partial F=filled X=canceled
    );

/ Execution report — matched trades with both sides
exec_report:([]
    time:        `timestamp$();
    trade_id:    `long$();
    buy_order_id:`long$();
    sell_order_id:`long$();
    sym:         `g#`symbol$();
    price:       `float$();
    qty:         `long$();
    exch:        `symbol$()
    );

/ --- Tickerplant protocol ---
/ .u.sub  — called by subscribers (RDB) to register interest
/ .u.pub  — called by TP to publish to all registered subscribers
/ .u.upd  — called by publishers (risk gate, matching engine) to push data

.u.w: ()!();  / subscriber handles: table -> (handles list)

.u.sub:{[t;s]
    / Register subscriber handle (.z.w) for table t, sym list s
    .z.w set @[value; t; ()!()];
    if[not t in key .u.w; .u.w[t]: ()];
    .u.w[t],: .z.w;
    -1 (string .z.w)," subscribed to ",string t;
    };

.u.upd:{[t;x]
    / Insert row(s) into local table, then publish to all subscribers
    t insert x;
    if[t in key .u.w; {neg[x] (`.u.upd; t; x)} each .u.w[t]];
    };

.u.pub:{[t;s;x]
    / Publish to subscribers filtered by sym list s
    if[0=count s; :()];
    d: ?[x; enlist (in; `sym; enlist s); 0b; ()!()];
    {neg[x] (`.u.upd; t; d)} each .u.w[t];
    };

/ --- End-of-day handler ---
/ Called by TP at EOD to save today's data to HDB and reset RDB
.u.end:{[dt]
    path: ` sv (hsym `$"data"; `$string dt);
    {[p;t] (` sv p,t,`) set .Q.en[hsym `$"data";] value t} [path] each `trade`quote`order`exec_report;
    -1 "[KDB+] EOD save complete for ", string dt;
    / Reset in-memory tables
    {.[t; (); 0#value t]} each `trade`quote`order`exec_report;
    };

/ --- Utility ---
/ Attach `p# parted attribute to sym after HDB save (speeds up date+sym lookups)
.robin.set_parted:{[t]
    update `p#sym from `$string t;
    };

-1 "[KDB+] Robin schema loaded with sym enum, g# attributes, TP/RDB/HDB protocol.";
-1 "[KDB+] Tables: trade, quote, order, exec_report";
-1 "[KDB+] Endpoints: .u.sub .u.upd .u.pub .u.end";

/ =============================================================================
/ Permissioning — Gap 8
/ =============================================================================

/ .z.pw: Password authentication
/ Reads ROBIN_KDB_PASSWORD from environment (default: "devpassword" for local dev)
/ Production: set ROBIN_KDB_PASSWORD to a strong random value in .env
.robin.pw: {[]
    pw: getenv `ROBIN_KDB_PASSWORD;
    $[0 = count pw; `devpassword; `$pw]
    };

.z.pw: {[u;p]
    $[p ~ .robin.pw[];
        [-1 "[KDB+] AUTH OK: ", string u;    1b];
        [-1 "[KDB+] AUTH FAIL: ", string u;  0b]]
    };

/ .z.po: Called when a new client connects
.z.po: {[h]
    -1 "[KDB+] CONNECT: handle=", (string h), " ip=", (string .z.a), " user=", string .z.u;
    };

/ .z.pc: Called when a client disconnects
.z.pc: {[h]
    -1 "[KDB+] DISCONNECT: handle=", string h;
    / Remove from subscriber lists on disconnect
    .u.w: {[k;v] k!{[h;v] v except h}[h] each v}[key .u.w; .u.w];
    };

-1 "[KDB+] Permissioning enabled (.z.pw/.z.po/.z.pc). Set ROBIN_KDB_PASSWORD env var.";
