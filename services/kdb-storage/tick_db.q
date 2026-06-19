/ WARM PATH: Post-trade tick capture
/ NOTE: This is a basic schema. Production deployment requires:
/   - sym enumeration for symbol compression
/   - par.txt for HDB partitioning
/   - .u.init for tickerplant registration
/   - TP (Tickerplant) / RDB (Real-time DB) / HDB (Historical DB) gateway architecture

trade:([]
    time:`timestamp$();
    sym:`symbol$();
    price:`float$();
    size:`long$();
    side:`char$();
    exch:`symbol$();
    cond:`symbol$()
    );

quote:([]
    time:`timestamp$();
    sym:`symbol$();
    bid:`float$();
    ask:`float$();
    bsize:`long$();
    asize:`long$();
    bidex:`symbol$();
    askex:`symbol$()
    );

order:([]
    time:`timestamp$();
    sym:`symbol$();
    order_id:`long$();
    price:`float$();
    qty:`long$();
    side:`char$();
    order_type:`char$();
    status:`char$()
    );

.u.upd:{[t;x] t insert x};

system "c 20 2 0";

-1 "[KDB+] Trade, Quote, and Order schemas loaded (WARM PATH - prototype schema).";
-1 "[KDB+] NOTE: Not a production database - missing sym enum, par.txt, TP/RDB/HDB architecture.";
