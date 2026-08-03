// KDB+ Real-Time Database (rdb.q)
// Subscribes to the Tickerplant and holds today's data in memory
// At EOD, it saves to disk (HDB) and clears memory.

// Start with: q rdb.q localhost:5010 localhost:5012 -p 5011

upd:insert;

.u.rep:{[x;y]
  // Initialize tables from schema and replay log
  (.[;();:;].)each x;
  if[null first y;:()];
  -11!y;
 }

end:{[d]
  // End of day: save to HDB and empty tables
  hdb:hopen `$":",.z.x[1];
  {
    path:`$":",.z.x[1],"/",string[.z.d],"/",string[x],"/";
    path set .Q.en[`$":",.z.x[1]] value x;
    delete from x;
  } each tables[];
  hdb "system\"l .\"";
  hclose hdb;
 }

// Connect to TP and subscribe
.u.init:{
  tp:hopen `$":",.z.x[0];
  .u.rep tp"(.u.w;`.u.l)";
  tp"(.u.add;`;`)";
 }

if[not "w"=first string .z.o;.u.init[]]
