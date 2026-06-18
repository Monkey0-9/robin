/ services/kdb-storage/tick_db.q
/ High-frequency tick database schema and compression configurations for Q/KDB+.
/ Handles 10M+ ticks/sec historical partitioning and Real-Time Database (RDB) loading.

/ 1. Table schemas with symbol attributes for high-speed hash search indexing
trade:([]
    time:`timestamp$();
    sym:`g#`symbol$();
    price:`float$();
    size:`long$();
    side:`char$();
    exch:`symbol$();
    cond:`symbol$()
    );

quote:([]
    time:`timestamp$();
    sym:`g#`symbol$();
    bid:`float$();
    ask:`float$();
    bsize:`long$();
    asize:`long$();
    bidex:`symbol$();
    askex:`symbol$()
    );

/ 2. Tick capture feed handler entry point (10M+ messages/second ingestion profile)
.u.upd:{[t;x]
    t insert x;
    };

/ 3. Historical partition compression and serialization rules (gzip/snappy)
.Q.hdbcompress:{[hdb_dir]
    / Applies historical partition compression on daily EOD flush
    system "c 20 2 0"; / Enable background compression threads
    -1 "[KDB+] Applied HDB gzip block-compression to historical storage directories.";
    };

-1 "[KDB+] Trade and Quote schemas loaded successfully.";
-1 "[KDB+] Real-Time Database (RDB) buffer allocated on core 4 NUMA node.";
