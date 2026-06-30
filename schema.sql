-- ============================================================================
-- Robin Trading Platform - Persistence Layer Schema (PostgreSQL / TimescaleDB)
-- ============================================================================

-- Enable TimescaleDB extension if available
CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE;

-- 1. Order History
CREATE TABLE IF NOT EXISTS orders (
    id BIGSERIAL PRIMARY KEY,
    cl_order_id BIGINT NOT NULL,
    instrument_id INTEGER NOT NULL,
    price INTEGER NOT NULL,
    qty INTEGER NOT NULL,
    side SMALLINT NOT NULL, -- 0 = Bid, 1 = Ask
    status VARCHAR(20) NOT NULL, -- NEW, PARTIAL, FILLED, CANCELED, REJECTED
    account_id INTEGER NOT NULL,
    client_id INTEGER NOT NULL,
    strategy_id INTEGER NOT NULL,
    created_at_ns BIGINT NOT NULL,
    updated_at_ns BIGINT NOT NULL
);

CREATE INDEX idx_orders_client_id ON orders(client_id);
CREATE INDEX idx_orders_instrument_id ON orders(instrument_id);
CREATE INDEX idx_orders_created_at ON orders(created_at_ns);

-- 2. Trade Ledgers (hypertable if TimescaleDB)
CREATE TABLE IF NOT EXISTS trades (
    trade_id BIGSERIAL,
    order_id BIGINT NOT NULL REFERENCES orders(id),
    instrument_id INTEGER NOT NULL,
    execution_price INTEGER NOT NULL,
    execution_qty INTEGER NOT NULL,
    side SMALLINT NOT NULL,
    maker_taker VARCHAR(10) NOT NULL, -- MAKER or TAKER
    fee INTEGER NOT NULL DEFAULT 0,
    executed_at_ns BIGINT NOT NULL,
    PRIMARY KEY (trade_id, executed_at_ns)
);

-- Convert to hypertable for TimescaleDB
SELECT create_hypertable('trades', 'executed_at_ns', chunk_time_interval => 86400000000000, if_not_exists => TRUE);

CREATE INDEX idx_trades_order_id ON trades(order_id);
CREATE INDEX idx_trades_instrument_id ON trades(instrument_id);

-- 3. Risk Positions (Snapshot state)
CREATE TABLE IF NOT EXISTS risk_positions (
    account_id INTEGER NOT NULL,
    instrument_id INTEGER NOT NULL,
    net_position BIGINT NOT NULL DEFAULT 0,
    realized_pnl BIGINT NOT NULL DEFAULT 0,
    updated_at_ns BIGINT NOT NULL,
    PRIMARY KEY (account_id, instrument_id)
);

-- 4. Audit Log (WORM-compliant trace)
CREATE TABLE IF NOT EXISTS audit_log (
    sequence_id BIGSERIAL PRIMARY KEY,
    timestamp_ns BIGINT NOT NULL,
    action VARCHAR(50) NOT NULL,
    order_id BIGINT NOT NULL,
    client_id INTEGER NOT NULL,
    instrument_id INTEGER NOT NULL,
    price INTEGER NOT NULL,
    qty INTEGER NOT NULL,
    chain_hash VARCHAR(64) NOT NULL -- SHA-256 hash linking to previous record
);

CREATE INDEX idx_audit_log_timestamp ON audit_log(timestamp_ns);
