-- ============================================================================
-- Robin Trading Platform - Persistence Layer Schema (SQLite)
-- ============================================================================

-- 1. Order History
CREATE TABLE IF NOT EXISTS orders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cl_order_id TEXT NOT NULL,
    instrument_id INTEGER NOT NULL,
    price INTEGER NOT NULL,
    qty INTEGER NOT NULL,
    side SMALLINT NOT NULL, -- 0 = Bid, 1 = Ask
    status TEXT NOT NULL, -- NEW, PARTIAL, FILLED, CANCELED, REJECTED
    account_id INTEGER NOT NULL,
    client_id INTEGER NOT NULL,
    strategy_id INTEGER NOT NULL,
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_orders_client_id ON orders(client_id);
CREATE INDEX IF NOT EXISTS idx_orders_instrument_id ON orders(instrument_id);
CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders(created_at_ns);

-- 2. Trade Ledgers
CREATE TABLE IF NOT EXISTS trades (
    trade_id INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id INTEGER NOT NULL REFERENCES orders(id),
    instrument_id INTEGER NOT NULL,
    execution_price INTEGER NOT NULL,
    execution_qty INTEGER NOT NULL,
    side SMALLINT NOT NULL,
    maker_taker TEXT NOT NULL, -- MAKER or TAKER
    fee INTEGER NOT NULL DEFAULT 0,
    executed_at_ns INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_trades_order_id ON trades(order_id);
CREATE INDEX IF NOT EXISTS idx_trades_instrument_id ON trades(instrument_id);

-- 3. Risk Positions (Snapshot state)
CREATE TABLE IF NOT EXISTS risk_positions (
    account_id INTEGER NOT NULL,
    instrument_id INTEGER NOT NULL,
    net_position INTEGER NOT NULL DEFAULT 0,
    realized_pnl INTEGER NOT NULL DEFAULT 0,
    updated_at_ns INTEGER NOT NULL,
    PRIMARY KEY (account_id, instrument_id)
);

-- 4. Audit Log (WORM-compliant trace)
CREATE TABLE IF NOT EXISTS audit_log (
    sequence_id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp_ns INTEGER NOT NULL,
    action TEXT NOT NULL,
    order_id INTEGER NOT NULL,
    client_id INTEGER NOT NULL,
    instrument_id INTEGER NOT NULL,
    price INTEGER NOT NULL,
    qty INTEGER NOT NULL,
    chain_hash TEXT NOT NULL -- SHA-256 hash linking to previous record
);

CREATE INDEX IF NOT EXISTS idx_audit_log_timestamp ON audit_log(timestamp_ns);
