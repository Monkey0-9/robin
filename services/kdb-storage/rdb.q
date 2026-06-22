/ Robin Real-Time Database (RDB)
/ Connects to TP, receives intraday ticks
\p 5011

trades:([] time:`timestamp$(); sym:`symbol$(); price:`float$(); size:`long$());
quotes:([] time:`timestamp$(); sym:`symbol$(); bid:`float$(); ask:`float$(); bsize:`long$(); asize:`long$());

upd:{[t;x] insert[t;x]}

h:hopen `:localhost:5010
h (`.u.sub; `trades)
h (`.u.sub; `quotes)

.z.ts:{[x] show "RDB heartbeat"}
\t 60000

show "RDB started on port 5011, subscribed to TP"
