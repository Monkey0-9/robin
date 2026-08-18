# Robin Quantitative Trading Platform — Market Data & Feed Handlers
**Document ID:** SPEC-MD-202608-01  
**Classification:** Market Data Engineering Specification  

---

## 1. Direct Feed Ingestion Pipeline

```
┌────────────────────────────────────────────────────────┐
│               Exchange UDP Multicast A/B Lines         │
└───────────────────────────┬────────────────────────────┘
                            │
                            ▼
┌────────────────────────────────────────────────────────┐
│             DPDK 23.11 Kernel-Bypass PMD               │
│     (Dedicated RX Ring • 1024 x 2MB Hugepages)         │
└───────────────────────────┬────────────────────────────┘
                            │
                            ▼
┌────────────────────────────────────────────────────────┐
│            Zero-Copy Binary Protocol Parsers           │
│   • Nasdaq TotalView-ITCH 5.0    • NYSE Arca XDP       │
└───────────────────────────┬────────────────────────────┘
                            │
                            ▼
┌────────────────────────────────────────────────────────┐
│           Normalized Shared Memory SPSC Ring           │
│          (/robin_ingest_risk • 64-byte align)          │
└────────────────────────────────────────────────────────┘
```

---

## 2. Gap Detection & Failover Mechanics
1. **Monotonic Sequence Tracking:** Every exchange message carries a 32-bit or 64-bit sequence counter.
2. **Gap Detection:** If $\text{Seq}_{\text{new}} > \text{Seq}_{\text{expected}}$, an alert is dispatched and an immediate TCP retransmission request is initiated against the exchange replay feed.
3. **A/B Line Arbitrage:** Ingestion actively monitors primary (Line A) and secondary (Line B) multicast streams, seamlessly dropping duplicates and selecting whichever packet arrives first with zero queueing jitter.
