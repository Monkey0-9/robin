/ WARM PATH: Post-trade tick capture, NOT on hot path.
/ Hot path uses shared memory ring buffers (<500ns).
/ KDB+ operates on Aeron IPC subscriber (1-10ms acceptable).

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

.u.upd:{[t;x]
    t insert x;
    };

system "c 20 2 0";

-1 "[KDB+] Trade and Quote schemas loaded successfully (WARM PATH).";
