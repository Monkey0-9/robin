/ Robin Historical Database (HDB)
/ Mounts partitioned on-disk historical data
\p 5012

/ Assuming the data is stored in the ./db directory partitioned by date
system "l ./db"

show "HDB started on port 5012, mounted historical data"
