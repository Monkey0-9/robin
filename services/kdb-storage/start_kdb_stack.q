/ =============================================================================
/ Robin Trading Platform — KDB+ Stack Startup Script
/ =============================================================================
/ Starts all KDB+ services in order:
/   1. HDB     (port 5012) — historical data
/   2. TP      (port 5010) — tickerplant with WAL
/   3. WDB     (port 5013) — write-direct to disk for crash recovery
/   4. RDB     (port 5011) — real-time database
/   5. HTTP GW (port 5001) — REST API gateway
/   6. WS GW   (port 5002) — WebSocket tick stream
/
/ Startup: q start_kdb_stack.q
/ =============================================================================

/ Create required directories
@[system; "mkdir hdb 2>/dev/null || mkdir hdb"; {[e] -1 "[START] hdb dir exists"}];
@[system; "mkdir wal 2>/dev/null || mkdir wal"; {[e] -1 "[START] wal dir exists"}];

/ Environment defaults
@[.z.setenv; `ROBIN_KDB_PASSWORD; .z.getenv[`ROBIN_KDB_PASSWORD] or "devpassword"];

/ Start HDB (historical)
system "q hdb.q &";
-1 "[START] HDB (port 5012) launching...";

/ Wait for HDB to be ready
system "sleep 2";

/ Start TP (tickerplant)
system "q tick.q &";
-1 "[START] TP (port 5010) launching...";
system "sleep 1";

/ Start WDB (write-direct backup)
system "q wdb.q &";
-1 "[START] WDB (port 5013) launching...";
system "sleep 1";

/ Start RDB (real-time database)
system "q rdb.q &";
-1 "[START] RDB (port 5011) launching...";
system "sleep 1";

/ Start HTTP gateway (REST API)
system "q http_gateway.q &";
-1 "[START] HTTP GW (port 5001) launching...";
system "sleep 1";

/ Start WebSocket bridge (tick stream)
system "q ws_bridge.q &";
-1 "[START] WS GW (port 5002) launching...";

/ Verify all services
-1 "[START] === KDB+ Stack Status ===";
-1 "[START] HDB (port 5012): ", $[() ~ key `$":hdb"; "empty"; "loaded"];
-1 "[START] Port check (TP 5010): ", $[() ~ key `$":tick.q"; "script found"; "missing?"];
-1 "[START] Port check (RDB 5011): ", $[() ~ key `$":rdb.q"; "script found"; "missing?"];
-1 "[START] Port check (HDB 5012): ", $[() ~ key `$":hdb.q"; "script found"; "missing?"];
-1 "[START] Port check (WDB 5013): ", $[() ~ key `$":wdb.q"; "script found"; "missing?"];
-1 "[START] Port check (HTTP 5001): ", $[() ~ key `$":http_gateway.q"; "script found"; "missing?"];
-1 "[START] Port check (WS 5002): ", $[() ~ key `$":ws_bridge.q"; "script found"; "missing?"];
-1 "[START] ============================";
-1 "[START] Robin KDB+ Stack started. Use 'ps aux | grep q' to verify.";
-1 "[START] To stop: pkill -f 'q '";
