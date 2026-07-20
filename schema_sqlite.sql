-- ============================================================================
-- Robin Trading Platform - Persistence Layer Schema (SQLite)
-- Institutional-Grade Schema v2.0
-- Compliant with: SEC 15c3-5, FINRA 3110, MiFID II RTS 22/25, SEC 17a-4
-- ============================================================================

-- 1. Order History (with CAT/MiFID fields)
CREATE TABLE IF NOT EXISTS orders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cl_order_id TEXT NOT NULL,
    instrument_id INTEGER NOT NULL,
    price INTEGER NOT NULL,
    qty INTEGER NOT NULL,
    side SMALLINT NOT NULL,                -- 0 = Bid, 1 = Ask
    status TEXT NOT NULL,                  -- NEW, PARTIAL, FILLED, CANCELED, REJECTED
    order_type TEXT NOT NULL DEFAULT 'LIMIT',
    account_id INTEGER NOT NULL,
    client_id INTEGER NOT NULL,
    strategy_id INTEGER NOT NULL,
    algo_id TEXT NOT NULL DEFAULT '',      -- MiFID II: algorithm identifier
    decision_maker TEXT NOT NULL DEFAULT '', -- MiFID II: human or algo
    liquidity_provision INTEGER NOT NULL DEFAULT 0, -- MiFID II flag
    fdid TEXT NOT NULL DEFAULT '',         -- CAT: Firm Designated ID
    rfid TEXT NOT NULL DEFAULT '',         -- CAT: Regulatory Firm ID
    manta TEXT NOT NULL DEFAULT '',        -- CAT: Manufacturer/Advising/Networking/Trading/Alliance
    exchange TEXT NOT NULL DEFAULT '',     -- Routed exchange
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL,
    entry_time_ns INTEGER NOT NULL DEFAULT 0, -- time order entered system
    first_route_ns INTEGER NOT NULL DEFAULT 0 -- time first routed
);

CREATE INDEX IF NOT EXISTS idx_orders_client_id ON orders(client_id);
CREATE INDEX IF NOT EXISTS idx_orders_instrument_id ON orders(instrument_id);
CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders(created_at_ns);
CREATE INDEX IF NOT EXISTS idx_orders_fdid ON orders(fdid);
CREATE INDEX IF NOT EXISTS idx_orders_strategy ON orders(strategy_id);

-- 2. Trade Ledgers
CREATE TABLE IF NOT EXISTS trades (
    trade_id INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id INTEGER NOT NULL REFERENCES orders(id),
    instrument_id INTEGER NOT NULL,
    execution_price INTEGER NOT NULL,
    execution_qty INTEGER NOT NULL,
    side SMALLINT NOT NULL,
    maker_taker TEXT NOT NULL,             -- MAKER or TAKER
    fee INTEGER NOT NULL DEFAULT 0,
    slippage_bps INTEGER NOT NULL DEFAULT 0, -- price improvement / slippage in bps
    executed_at_ns INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_trades_order_id ON trades(order_id);
CREATE INDEX IF NOT EXISTS idx_trades_instrument_id ON trades(instrument_id);
CREATE INDEX IF NOT EXISTS idx_trades_executed_at ON trades(executed_at_ns);

-- 3. Risk Positions (Snapshot state)
CREATE TABLE IF NOT EXISTS risk_positions (
    account_id INTEGER NOT NULL,
    instrument_id INTEGER NOT NULL,
    net_position INTEGER NOT NULL DEFAULT 0,
    realized_pnl INTEGER NOT NULL DEFAULT 0,
    updated_at_ns INTEGER NOT NULL,
    PRIMARY KEY (account_id, instrument_id)
);

-- 4. Audit Log (WORM-compliant trace) — SEC 17a-4 compliant
CREATE TABLE IF NOT EXISTS audit_log (
    sequence_id INTEGER PRIMARY KEY AUTOINCREMENT,
    sequence_monotonic INTEGER NOT NULL,   -- verified monotonic, gap detection
    timestamp_ns INTEGER NOT NULL,
    gps_time_ns INTEGER NOT NULL DEFAULT 0, -- hardware PTP timestamp (0 = OS clock)
    action TEXT NOT NULL,
    order_id INTEGER NOT NULL,
    client_id INTEGER NOT NULL,
    instrument_id INTEGER NOT NULL,
    price INTEGER NOT NULL,
    qty INTEGER NOT NULL,
    chain_hash TEXT NOT NULL,              -- SHA-256 hash chaining to previous record
    user_id INTEGER NOT NULL DEFAULT 0,
    ip_address TEXT NOT NULL DEFAULT '',
    retention_expires_at_ns INTEGER NOT NULL DEFAULT 0 -- 0 = permanent; 3yr min per 17a-4
);

CREATE INDEX IF NOT EXISTS idx_audit_log_timestamp ON audit_log(timestamp_ns);
CREATE INDEX IF NOT EXISTS idx_audit_log_sequence ON audit_log(sequence_monotonic);

-- Prevent modification of immutable audit fields (SEC 17a-4 WORM requirement)
CREATE TRIGGER IF NOT EXISTS trg_audit_log_no_update
BEFORE UPDATE ON audit_log
BEGIN
    SELECT RAISE(ABORT, 'audit_log records are immutable (SEC 17a-4 WORM requirement)');
END;

CREATE TRIGGER IF NOT EXISTS trg_audit_log_no_delete
BEFORE DELETE ON audit_log
WHEN (SELECT retention_expires_at_ns FROM audit_log WHERE sequence_id = OLD.sequence_id) > strftime('%s','now') * 1000000000
    OR (SELECT retention_expires_at_ns FROM audit_log WHERE sequence_id = OLD.sequence_id) = 0
BEGIN
    SELECT RAISE(ABORT, 'audit_log record within retention period cannot be deleted (SEC 17a-4)');
END;

-- 5. CEO Annual Certification (SEC 15c3-5 §(e)(2))
CREATE TABLE IF NOT EXISTS compliance_certifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    year INTEGER NOT NULL UNIQUE,
    ceo_name TEXT NOT NULL,
    ceo_title TEXT NOT NULL DEFAULT 'CEO',
    attested_at_ns INTEGER NOT NULL,
    review_notes TEXT NOT NULL DEFAULT '',
    systems_reviewed TEXT NOT NULL DEFAULT '',  -- JSON array of systems reviewed
    signature_hash TEXT NOT NULL,              -- SHA-256 of (year||ceo_name||attested_at||review_notes)
    next_review_due_ns INTEGER NOT NULL,       -- 1 year from attestation
    created_by TEXT NOT NULL DEFAULT ''        -- admin user who submitted
);

CREATE INDEX IF NOT EXISTS idx_compliance_cert_year ON compliance_certifications(year);

-- 6. Compliance Annual Reviews (SEC 15c3-5 documentation requirement)
CREATE TABLE IF NOT EXISTS compliance_reviews (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    review_date_ns INTEGER NOT NULL,
    reviewer TEXT NOT NULL,
    reviewer_title TEXT NOT NULL DEFAULT '',
    findings TEXT NOT NULL DEFAULT '',         -- JSON array of findings
    remediation TEXT NOT NULL DEFAULT '',      -- JSON array of remediation actions
    controls_tested TEXT NOT NULL DEFAULT '',  -- JSON array of controls tested
    result TEXT NOT NULL DEFAULT 'PASS',       -- PASS, PASS_WITH_EXCEPTIONS, FAIL
    hash TEXT NOT NULL,                        -- SHA-256 of review record
    cert_id INTEGER REFERENCES compliance_certifications(id)
);

-- 7. CAT Reporting (FINRA CAT — complete order lifecycle)
CREATE TABLE IF NOT EXISTS cat_reports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id INTEGER NOT NULL REFERENCES orders(id),
    cat_event_type TEXT NOT NULL,              -- NEW, ROUTE, FILL, CANCEL, MODIFY
    event_timestamp_ns INTEGER NOT NULL,
    fdid TEXT NOT NULL,                        -- Firm Designated ID
    rfid TEXT NOT NULL,                        -- Regulatory Firm ID
    manta TEXT NOT NULL DEFAULT '',
    reporting_party TEXT NOT NULL DEFAULT 'ROBIN',
    exchange TEXT NOT NULL DEFAULT '',
    contra_party TEXT NOT NULL DEFAULT '',
    batch_id TEXT NOT NULL DEFAULT '',         -- submission batch identifier
    submitted_at_ns INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'PENDING'     -- PENDING, SUBMITTED, ACCEPTED, REJECTED
);

CREATE INDEX IF NOT EXISTS idx_cat_reports_order_id ON cat_reports(order_id);
CREATE INDEX IF NOT EXISTS idx_cat_reports_status ON cat_reports(status);
CREATE INDEX IF NOT EXISTS idx_cat_reports_batch ON cat_reports(batch_id);

-- 8. MiFID II RTS 22 Transaction Reports
CREATE TABLE IF NOT EXISTS mifid_reports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id INTEGER NOT NULL REFERENCES orders(id),
    transaction_ref TEXT NOT NULL,             -- UTI / transaction reference
    instrument_id_type TEXT NOT NULL DEFAULT 'ISIN',
    instrument_id TEXT NOT NULL DEFAULT '',
    price_currency TEXT NOT NULL DEFAULT 'USD',
    trading_venue TEXT NOT NULL DEFAULT '',    -- MIC code
    algo_id TEXT NOT NULL DEFAULT '',          -- DEA algorithm identifier
    decision_maker_id TEXT NOT NULL DEFAULT '',
    client_id_scheme TEXT NOT NULL DEFAULT 'CONCAT', -- CONCAT, LEI, etc.
    buyer_id TEXT NOT NULL DEFAULT '',
    seller_id TEXT NOT NULL DEFAULT '',
    report_status TEXT NOT NULL DEFAULT 'PENDING',
    reported_at_ns INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_mifid_reports_order ON mifid_reports(order_id);
CREATE INDEX IF NOT EXISTS idx_mifid_reports_status ON mifid_reports(report_status);

-- 9. Post-Trade Surveillance Alerts
CREATE TABLE IF NOT EXISTS surveillance_alerts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    alert_type TEXT NOT NULL,                  -- WASH_TRADE, LAYERING, MARKING_CLOSE, MOMENTUM_IGNITION, SPOOFING
    client_id INTEGER NOT NULL,
    order_ids TEXT NOT NULL DEFAULT '',        -- JSON array of related order IDs
    detected_at_ns INTEGER NOT NULL,
    evidence_json TEXT NOT NULL DEFAULT '{}',  -- evidence fields as JSON
    status TEXT NOT NULL DEFAULT 'UNREVIEWED', -- UNREVIEWED, REVIEWED_CLEAR, ESCALATED, REPORTED
    reviewer_id INTEGER NOT NULL DEFAULT 0,
    reviewed_at_ns INTEGER NOT NULL DEFAULT 0,
    review_notes TEXT NOT NULL DEFAULT '',
    chain_hash TEXT NOT NULL DEFAULT ''        -- links to audit chain
);

CREATE INDEX IF NOT EXISTS idx_surveillance_client ON surveillance_alerts(client_id);
CREATE INDEX IF NOT EXISTS idx_surveillance_type ON surveillance_alerts(alert_type);
CREATE INDEX IF NOT EXISTS idx_surveillance_status ON surveillance_alerts(status);
CREATE INDEX IF NOT EXISTS idx_surveillance_detected ON surveillance_alerts(detected_at_ns);

-- 10. User Credentials & MFA (2FA/TOTP)
CREATE TABLE IF NOT EXISTS user_credentials (
    user_id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    bcrypt_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'viewer',       -- viewer, trader, admin
    totp_secret_enc TEXT NOT NULL DEFAULT '',  -- AES-256-GCM encrypted TOTP secret
    totp_enabled INTEGER NOT NULL DEFAULT 0,
    created_at_ns INTEGER NOT NULL,
    last_login_ns INTEGER NOT NULL DEFAULT 0,
    failed_attempts INTEGER NOT NULL DEFAULT 0,
    locked_until_ns INTEGER NOT NULL DEFAULT 0,
    must_change_password INTEGER NOT NULL DEFAULT 0,
    password_changed_at_ns INTEGER NOT NULL DEFAULT 0
);

-- 11. Access Logs (tamper-evident with chain hash)
CREATE TABLE IF NOT EXISTS access_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    username TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL,                      -- LOGIN, LOGOUT, ORDER_SUBMIT, CONFIG_CHANGE, KILL_SWITCH_TRIP, etc.
    resource TEXT NOT NULL DEFAULT '',
    timestamp_ns INTEGER NOT NULL,
    ip_address TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    result TEXT NOT NULL DEFAULT 'SUCCESS',    -- SUCCESS, FAILURE, BLOCKED
    details TEXT NOT NULL DEFAULT '',          -- JSON extra context
    chain_hash TEXT NOT NULL DEFAULT ''        -- SHA-256 chain for tamper evidence
);

CREATE INDEX IF NOT EXISTS idx_access_log_user ON access_log(user_id);
CREATE INDEX IF NOT EXISTS idx_access_log_timestamp ON access_log(timestamp_ns);
CREATE INDEX IF NOT EXISTS idx_access_log_action ON access_log(action);

-- 12. Kill Switch State Persistence
CREATE TABLE IF NOT EXISTS kill_switch_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    level TEXT NOT NULL,                       -- SYSTEM, ALGO, TRADER
    target_id TEXT NOT NULL DEFAULT '',        -- algo_id or trader_id; empty for SYSTEM
    action TEXT NOT NULL,                      -- TRIP, RESET
    reason TEXT NOT NULL DEFAULT '',
    tripped_by TEXT NOT NULL DEFAULT '',
    reset_by TEXT NOT NULL DEFAULT '',
    secondary_approver TEXT NOT NULL DEFAULT '', -- dual-person integrity on reset
    tripped_at_ns INTEGER NOT NULL DEFAULT 0,
    reset_at_ns INTEGER NOT NULL DEFAULT 0,
    chain_hash TEXT NOT NULL DEFAULT ''
);

-- 13. Supervisory Approval Audit Trail (FINRA 3110 digital signatures)
CREATE TABLE IF NOT EXISTS supervisory_decisions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id INTEGER NOT NULL,
    notional REAL NOT NULL,
    symbol TEXT NOT NULL DEFAULT '',
    decision TEXT NOT NULL,                    -- APPROVED, REJECTED, AUTO_APPROVED
    principal_id INTEGER NOT NULL DEFAULT 0,
    principal_name TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    decided_at_ns INTEGER NOT NULL,
    expires_at_ns INTEGER NOT NULL DEFAULT 0,  -- TTL for pending approvals
    signature_hash TEXT NOT NULL DEFAULT ''    -- SHA-256 of decision record
);

CREATE INDEX IF NOT EXISTS idx_supervisory_order ON supervisory_decisions(order_id);
CREATE INDEX IF NOT EXISTS idx_supervisory_principal ON supervisory_decisions(principal_id);
