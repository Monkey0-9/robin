// KDB+ Tickerplant (tick.q)
// Logs incoming ticks to disk and broadcasts to subscribers

// Start with: q tick.q sym /path/to/tplog -p 5010

\d .u

init:{[t]
  // Initialize log file
  if[not system"t";system"t 1000"];
  .u.t:t;
  .u.l:hopen `$":",string[.u.log_path];
  .u.l "init";
  .u.w:t!(count t:tables[])#()
 }

pub:{[t;x]
  // Publish to subscribers
  {[t;x;w]if[count x:select from x where sym in w[1];(neg w[0])(upsert;t;x)]}[t;x]each .u.w[t];
 }

upd:{[t;x]
  // Write to log and publish
  if[not -16=type first first x;a:x[0];x[0]:x[1];x[1]:a];
  .u.l(insert;t;x);
  pub[t;x];
 }

add:{[t;s]
  // Add subscriber
  if[t in keys .u.w; .u.w[t],:enlist(.z.w;s)];
 }

\d .
