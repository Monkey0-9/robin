# Robin Quantitative Trading Platform — Shared Memory IPC Protocol
**Document ID:** SPEC-IPC-202608-01  
**Classification:** Low-Latency IPC Specification  

---

## 1. Shared Memory Channels

| SHM Path | Producer | Consumer | Capacity | Slot Size |
| :--- | :--- | :--- | :--- | :--- |
| `/robin_ingest_risk` | C++ DPDK Ingestion | Rust Risk Gate | 65,536 slots | 64 bytes (Cacheline Aligned) |
| `/robin_risk_match` | Rust Risk Gate | C++ Matching Engine | 65,536 slots | 64 bytes (Cacheline Aligned) |
| `/robin_match_storage`| C++ Matching Engine | KDB+ Tickerplant / Audit | 65,536 slots | 64 bytes (Cacheline Aligned) |

---

## 2. 64-Byte Cacheline Message Layout

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                          Timestamp (ns)                       |
|                             (64-bit)                          |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                          Order ID                             |
|                             (64-bit)                          |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                          Client ID                            |
|                             (64-bit)                          |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|             Price (Ticks)     |         Quantity              |
|               (32-bit)        |         (32-bit)              |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
| Instrument ID |MsgType| Side  |OrdType|TIF    |    Reserved   |
|   (16-bit)    | (8-bit|(8-bit)|(8-bit)|(8-bit)|    (16-bit)   |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                     Padding to 64 Bytes                       |
|                          (128-bit)                            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

---

## 3. Concurrency & Memory Ordering
* **Lock-Free Ring:** Producer updates `write_idx` with `std::memory_order_release`.
* **Consumer Polling:** Consumer checks `read_idx` with `std::memory_order_acquire`.
* **Cacheline Isolation:** Producer and consumer indices are separated by 64 bytes of padding to eliminate false sharing.
