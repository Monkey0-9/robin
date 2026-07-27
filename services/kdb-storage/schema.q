/ =============================================================================
/ Robin Trading Platform — KDB+ Unified Schema
/ =============================================================================
/ Shared schema loaded by all KDB+ components (TP, RDB, HDB, HTTP GW, WS GW).
/
/ Architecture:
/   TP  (port 5010) — receives publish, logs to WAL, distributes to subscribers
/   WDB (port 5013) — write-direct to disk for intraday crash recovery
/   RDB (port 5011) — subscribes to TP, holds today's data in-memory
/   HDB (port 5012) — mounts on-disk date partitions, handles history queries
/
/ Tables:
/   trade         — Matched trades from matching engine
/   quote         — Top-of-book bid/ask from market data feeds
/   order         — Full order lifecycle (NEW, PARTIAL, FILLED, CANCELED)
/   exec_report   — Execution reports with both sides
/   signal        — Strategy signals from ML/classic engines
/   position      — Current positions after each trade
/ =============================================================================

/ --- Sym file initialization ---
/ Load or create sym enumeration for symbol compression (1 byte storage vs full string)
@[{`sym set get hsym `$"sym"}; (); {`sym set `symbol$()}];

/ --- Compression configuration ---
/ Use kdb+ 4.1+ IPC and disk compression for all tables
/ gzip level 6 (good ratio/speed tradeoff)
.q.compress.set .z.zd: (17; 2; 6);  / block-level gzip compression

/ =============================================================================
/ Table Definitions
/ =============================================================================

/ Trade table: `g# on sym for O(1) group-by
trade:([]
    time:  `timestamp$();
    sym:   `g#`symbol$();
    price: `float$();
    size:  `long$();
    side:  `char$();             / B=buy, S=sell
    exch:  `symbol$();
    cond:  `symbol$()            / trade condition codes
    );

/ Quote table: top-of-book
quote:([]
    time:   `timestamp$();
    sym:    `g#`symbol$();
    bid:    `float$();
    ask:    `float$();
    bsize:  `long$();
    asize:  `long$();
    bidex:  `symbol$();          / bid exchange
    askex:  `symbol$()           / ask exchange
    );

/ Order table: full lifecycle
order:([]
    time:        `timestamp$();
    sym:         `g#`symbol$();
    order_id:    `long$();
    cl_order_id: `long$();       / client-assigned order ID
    price:       `float$();
    qty:         `long$();
    leaves_qty:  `long$();       / remaining unfilled qty
    filled_qty:  `long$();       / cumulatively filled qty
    side:        `char$();       / B=bid, A=ask
    order_type:  `char$();       / L=limit M=market I=IOC K=FOK
    tif:         `char$();       / time-in-force: D=day I=IOC G=GTC F=GTD
    status:      `char$();       / N=new W=working P=partial F=filled X=cancelled R=rejected
    account_id:  `long$();
    strategy_id: `int$();
    latency_us:  `int$()         / time from receive to acknowledge (microseconds)
    );

/ Execution report: matched trades with both sides
exec_report:([]
    time:          `timestamp$();
    trade_id:      `long$();
    buy_order_id:  `long$();
    sell_order_id: `long$();
    sym:           `g#`symbol$();
    price:         `float$();
    qty:           `long$();
    buy_account:   `long$();
    sell_account:  `long$();
    exch:          `symbol$()
    );

/ Signal table: strategy outputs for monitoring and audit
signal:([]
    time:          `timestamp$();
    sym:           `g#`symbol$();
    strategy_id:   `int$();       / 1=MR, 2=MOM, 3=VWAP, 4=ML, 255=Composite
    side:          `char$();      / B=buy S=sell H=hold
    confidence:    `real$();      / [0.0, 1.0]
    alpha:         `real$();      / predicted return
    kelly_pct:     `real$();      / Kelly fraction of capital
    price:         `float$();     / price at signal time
    reason:        `symbol$()     / human-readable signal reason
    );

/ Position table: current holdings snapshot
position:([]
    time:          `timestamp$();
    sym:           `g#`symbol$();
    account_id:    `long$();
    qty:           `long$();       / current position (positive=long, negative=short)
    avg_price:     `float$();      / average entry price
    unrealized_pnl:`float$();
    realized_pnl:  `float$();
    m2m_value:     `float$()       / mark-to-market value
    );

/ =============================================================================
/ End-of-Day handler for all tables
/ =============================================================================
/ Saves each table as a date-partitioned splayed table on disk.
/ Uses `p# parted attribute on sym column for fast date+sym lookups.
/ .z.zd compression is applied at disk level.

.robin.hdb_path: `$":./hdb";

.robin.eod_save:{[dt;tbl]
    path: .robin.hdb_path;
    target: ` sv (path; `$string dt; tbl);
    if[not ` sv path,`$string dt ~ key ` sv path, enlist `$string dt;
        system "mkdir \"", (1_string ` sv path,`$string dt), "\"";
        ];
    (` sv target,`) set .Q.en[path;] value tbl;
    / Apply parted attribute on sym for fast HDB queries
    @[target; `sym; `p#];
    -1 "[KDB+EOD] Saved ", string[tbl], " for ", string dt;
    };

.robin.eod_reset:{[tbl]
    @[tbl; (); 0#value tbl];
    };

/ =============================================================================
/ Utility: load all date partitions into HDB
/ =============================================================================
.robin.hdb_load:{[]
    parts: key .robin.hdb_path;
    parts: parts where 10h = type each parts;   / only symbol-type directories (dates)
    {system "l ", (1_string .robin.hdb_path), "/", string x} each parts;
    -1 "[KDB+HDB] Loaded ", string[count parts], " date partitions";
    };

/ =============================================================================
/ Feed simulation for development and testing
/ =============================================================================
.robin.sym_list: `AAPL`MSFT`GOOGL`AMZN`NVDA`TSLA`META`JPM`V`SPY`QQQ`IWM`GLD`BTCUSD`ETHUSD;

.robin.gen_tick:{[]
    sym: rand .robin.sym_list;
    base: ?[sym in `BTCUSD`ETHUSD; 50000; ?[sym in `AAPL`MSFT`GOOGL`AMZN`NVDA; 200; 100]];
    price: base + (rand 1000 - 500) % 100;
    size: 1 + rand 1000;
    side: $[1 = rand 2; "B"; "S"];
    exch: $[1 = rand 2; `BINANCE; `ALPACA];
    (`trade; `time`sym`price`size`side`exch`cond!(.z.p; sym; price; size; side; exch; `));
    };

.robin.gen_quote:{[]
    sym: rand .robin.sym_list;
    base: ?[sym in `BTCUSD`ETHUSD; 50000; ?[sym in `AAPL`MSFT`GOOGL`AMZN`NVDA; 200; 100]];
    spread: base * 0.0001 * (1 + rand 10);
    bid: base - spread % 2;
    ask: base + spread % 2;
    bsize: 100 * 1 + rand 100;
    asize: 100 * 1 + rand 100;
    (`quote; `time`sym`bid`ask`bsize`asize`bidex`askex!(.z.p; sym; bid; ask; bsize; asize; `NYSE; `NASDAQ));
    };

-1 "[KDB+SCHEMA] Robin Unified Schema v2.0 loaded";
-1 "[KDB+SCHEMA] Tables: trade, quote, order, exec_report, signal, position";
-1 "[KDB+SCHEMA] Compression: gzip level 6";
-1 "[KDB+SCHEMA] Sym enum: auto-initialize at ", string hsym `$"sym";
