/ Robin Tick Processor (TP)
/ Subscribes to SHM/network publishers and broadcasts to RDBs
\p 5010

trades:([] time:`timestamp$(); sym:`symbol$(); price:`float$(); size:`long$());
quotes:([] time:`timestamp$(); sym:`symbol$(); bid:`float$(); ask:`float$(); bsize:`long$(); asize:`long$());

upd:{[t;x] insert[t;x]; .u.pub[t;x]}

.u.init:{[] 
    .u.w:([] handle:`int$(); table:`symbol$());
    }

.u.sub:{[t]
    `.u.w insert (.z.w; t);
    (t; value t)
    }

.u.pub:{[t;x]
    h:exec handle from .u.w where table=t;
    {neg[x] (`upd; t; y)}[;x] each h;
    }

.u.init[]
show "Tick Processor started on port 5010"
