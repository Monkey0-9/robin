/ =============================================================================
/ Robin Trading Platform — KDB+ Historical Replay Engine
/ =============================================================================
/ Reads HDB partitions and replays them through the TP at configurable speed.
/ Used for backtesting, system integration testing, and strategy validation.
/
/ Usage: q replay.q [speed_multiplier]
/   speed_multiplier: 1 = real-time, 10 = 10x, 0 = as-fast-as-possible (default)
/
/ Example: q replay.q 100  (replay at 100x speed)
/ =============================================================================

/ Load unified schema
\l schema.q

/ Parse speed multiplier from command line
.robin.speed: $[0 < count .z.x; "J"$first .z.x; 1];
.robin.speed: 0 | .robin.speed;  / clamp to non-negative

/ Connect to TP
.robin.tp_host: .z.getenv `ROBIN_TP_HOST;
if[count[.robin.tp_host] = 0; .robin.tp_host: "localhost"];
.robin.tp_port: .z.getenv `ROBIN_TP_PORT;
if[count[.robin.tp_port] = 0; .robin.tp_port: "5010"];

.robin.tp: hopen `$.robin.tp_host, ":", .robin.tp_port;
-1 "[REPLAY] Connected to TP at ", .robin.tp_host, ":", .robin.tp_port;

/ Load HDB data
.robin.hdb_load[];

/ Discover dates and symbols
.robin.dates: key .robin.hdb_path where 10h = type each key .robin.hdb_path;
.robin.symbols: distinct exec sym from trade;

-1 "[REPLAY] Loaded ", string[count .robin.dates], " partitions across ", string[count .robin.symbols], " symbols";

/ --- Replay configuration ---
/ Which tables to replay
.robin.replay_tables: `trade`quote`order`exec_report`signal;

/ --- Replay loop ---
.robin.replay_date:{[dt]
    -1 "[REPLAY] Replaying ", string dt;

    / Load the date partition
    hdb_path: ` sv (.robin.hdb_path; `$string dt);
    .robin.replay_tp:{[t; dt; path]
        / Check if table exists in this partition
        tbl_path: ` sv (path; t);
        if[not () ~ key tbl_path;
            data: get tbl_path;
            / Sort by time to maintain temporal order
            data: `time xasc data;
            count_data: count data;
            / Replay through TP
            .robin.replay_tick[t; data; count_data];
            ];
        }[; dt; hdb_path];

    .robin.replay_tp each .robin.replay_tables;
    };

.robin.replay_tick:{[t; data; n]
    if[n = 0; :()];
    / Batch size: send in chunks for efficiency
    .robin.batch_size: 1000;
    .robin.batches: 0 | ceil n % .robin.batch_size;
    .robin.pos: 0;

    do[.robin.batches;
        .robin.end: (.robin.pos + .robin.batch_size) & n;
        .robin.chunk: .robin.pos _ .robin.end _ data;
        neg[.robin.tp] (`.u.upd; t; .robin.chunk);
        .robin.pos: .robin.end;
        ];

    -1 "[REPLAY] ", string[n], " ", string[t], " ticks sent to TP";
    };

/ --- Main replay ---
/ If speed > 0, replay with sleep between dates
/ If speed = 0, replay as fast as possible
if[.robin.speed > 0;
    .robin.date_sleep: 1000 % .robin.speed;  / ms between dates
    {[dt]
        .robin.replay_date[dt];
        if[.robin.date_sleep > 0; system "sleep ", string .robin.date_sleep % 1000];
        } each .robin.dates;
    ];
/ As-fast-as-possible mode
.robin.replay_date each .robin.dates;

/ Close TP connection
hclose .robin.tp;

-1 "[REPLAY] Replay complete for ", string[count .robin.dates], " partitions";
exit 0;
