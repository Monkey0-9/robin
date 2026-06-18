# KDB+ Tick Database — WARM PATH ONLY

## WARNING
KDB+ operates on the **WARM PATH** (1-10ms acceptable latency).
It MUST NOT block the hot path (<500ns tick-to-trade).

## Architecture
- Aeron IPC subscriber receives trade data from hot path
- Real-Time Database (RDB) maintains in-memory state
- Historical Database (HDB) for on-disk partitioned storage
- WebSocket bridge for frontend tick streaming

## Data Flow
Shared Memory Ring Buffer (hot path) → Aeron IPC → KDB+ RDB → HDB
