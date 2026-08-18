# Robin Quantitative Trading Platform — Disaster Recovery & High Availability
**Document ID:** SPEC-DR-202608-01  
**Classification:** SRE & High Availability Specification  

---

## 1. Recovery Objectives
* **Recovery Point Objective (RPO):** $< 1 \text{ second}$ (Continuous async WAL journaling).
* **Recovery Time Objective (RTO):** $< 50 \text{ milliseconds}$ for automated Raft failover; $< 5 \text{ seconds}$ for full cluster reboot.

---

## 2. Active-Active Clustering with Raft Consensus
* **3-Node Cluster:** Leader node handles active matching while two follower nodes continuously replicate the state machine log.
* **Heartbeat & Election:** 150ms timeout triggers candidate election; majority quorum (2 of 3) commits all order book operations.
* **State Machine Snapshots:** Periodic CRC-32 snapshots compact the log and accelerate follower catch-up upon network partition recovery.

---

## 3. Crash Recovery Procedure
```
1. Startup: Service boots in RESTORING mode.
2. Read Latest Snapshot: Load most recent risk_snapshot_*.bin and verify CRC-32 checksum.
3. WAL Replay: Replay uncompacted WAL entries from KDB+ or disk journal to reconstruct positions.
4. Reconciliation: Compare rehydrated positions against broker/exchange REST state.
5. State Verification: Ensure sum of open positions matches portfolio equity before accepting new orders.
6. Open Quoting: Transition service state to ACTIVE.
```
