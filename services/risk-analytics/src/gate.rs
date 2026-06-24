use crate::circuit_breaker::RiskCircuitBreaker;
use crate::gpio_kill_switch::HardwareKillSwitch;
use crate::hedging::HedgingEngine;
use crate::pre_trade::PreTradeRiskEvaluator;
use crate::risk_gate_fast::{ComplianceThresholds, RiskGateFast};
use crate::shm_bridge::ShmBridge;
use crate::tax_engine::TaxEngine;
use core::sync::atomic::{AtomicU64, Ordering};

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum OrderSide {
    Bid = 0,
    Ask = 1,
}

/// Pre-trade risk gate — zero-allocation hot path.
///
/// HARD BLOCKS (checked on every order, no exceptions):
///   1. Kill switch (hardware GPIO) — immediate halt
///   2. Circuit breaker (daily drawdown limit exceeded) — halt until reset
///   3. Order size limit (fat-finger: qty > 1,000,000) — reject
///   4. Order value limit (price × qty > credit_limit) — reject
///   5. Symbol restriction (blocked/unlisted securities) — reject
///   6. Duplicate order detection (same id within 1ms window) — reject
///   7. Price collar (±5% from last trade price) — reject
///
/// SOFT BLOCKS (configurable, logged):
///   1. Position limits (per-symbol net position)
///   2. Velocity limits (max orders per sliding 1-second window)
///   3. Concentration limits (could be added)
pub struct RiskGate {
    pre_trade: PreTradeRiskEvaluator,
    circuit_breaker: RiskCircuitBreaker,
    kill_switch: HardwareKillSwitch,
    fast_gate: RiskGateFast,
    shm: Option<ShmBridge>,
    #[allow(dead_code)]
    hedging: HedgingEngine,
    #[allow(dead_code)]
    tax_engine: TaxEngine,
    orders_processed: AtomicU64,
    credit_limit: u64, // Hard block: max order value in price-units
    duplicate_window_ns: u64,
    recent_orders: Box<[(u64, u64)]>, // (order_id, timestamp_ns) ring
    positions: Box<[i64]>,            // Per-instrument net position
    /// Circular buffer for velocity rate limiting.
    /// Stores the timestamp_ns of the last VELOCITY_WINDOW_SIZE approved orders.
    velocity_ring: Box<[u64]>,
    velocity_head: usize,
    velocity_window_ns: u64, // Time window in nanoseconds (default 1 second)
    max_velocity: usize,     // Max orders allowed within velocity_window_ns
    position_limit: i64,
}

/// Size of the velocity ring buffer.
/// Must be > max_velocity to hold an entire window worth of timestamps.
const VELOCITY_RING_SIZE: usize = 512;

impl RiskGate {
    pub fn new(shm_path: &str) -> Self {
        Self {
            pre_trade: PreTradeRiskEvaluator::new(
                10_000_000_000, // credit_limit
                1_000_000,      // max_qty
                1_000_000,      // max_price (placeholder)
                1,              // min_price
            ),
            circuit_breaker: RiskCircuitBreaker::new(0.10),
            kill_switch: HardwareKillSwitch::new(),
            fast_gate: RiskGateFast::new(ComplianceThresholds {
                max_order_value: 10_000_000_000,
                max_order_qty: 1_000_000,
                price_collar_bps: 500,
                reference_price: 50_000,
                restricted_list: [0u32; 128],
                restricted_count: 0,
            }),
            shm: ShmBridge::new(shm_path, true).ok(),
            hedging: HedgingEngine::new(),
            tax_engine: TaxEngine::new(Vec::new()),
            orders_processed: AtomicU64::new(0),
            credit_limit: 10_000_000_000,   // 10 billion price-units
            duplicate_window_ns: 1_000_000, // 1 ms
            recent_orders: vec![(0u64, 0u64); 4096].into_boxed_slice(),
            positions: vec![0i64; 4096].into_boxed_slice(),
            velocity_ring: vec![0u64; VELOCITY_RING_SIZE].into_boxed_slice(),
            velocity_head: 0,
            velocity_window_ns: 1_000_000_000, // 1 second
            max_velocity: 100,                 // max 100 orders per second
            position_limit: 100_000,
        }
    }

    /// Create with custom velocity and credit limits.
    pub fn with_config(
        shm_path: &str,
        credit_limit: u64,
        max_orders_per_second: usize,
        position_limit: i64,
    ) -> Self {
        let mut g = Self::new(shm_path);
        g.credit_limit = credit_limit;
        g.max_velocity = max_orders_per_second;
        g.position_limit = position_limit;
        g
    }

    /// Hot-path pre-trade check.
    /// Returns Ok(OrderStatus::Approved) or Err(RiskError::*).
    ///
    /// Latency target: <500ns p99 on warm CPU (no DPDK, no kernel bypass).
    pub fn check_order(&mut self, order: &Order) -> Result<OrderStatus, RiskError> {
        // HARD BLOCK 1: Hardware kill switch
        if self.kill_switch.is_active() {
            return Err(RiskError::KillSwitchActive);
        }

        // HARD BLOCK 2: Circuit breaker (daily drawdown)
        if self.circuit_breaker.is_tripped() {
            return Err(RiskError::CircuitBreakerTripped);
        }

        // HARD BLOCK 3: Basic pre-trade size/price range checks
        self.pre_trade
            .evaluate_order(order)
            .map_err(|_| RiskError::FatFinger)?;

        // HARD BLOCK 4: Order value limit (price × qty > credit_limit)
        let order_value = (order.price as u64).saturating_mul(order.qty as u64);
        if order_value > self.credit_limit {
            return Err(RiskError::CreditLimit);
        }

        // HARD BLOCK 5: Symbol restrictions + order value + qty limits via fast gate
        if !self.fast_gate.validate_compliance(order) {
            return Err(RiskError::SymbolRestricted);
        }

        // HARD BLOCK 6: Duplicate order detection (same order ID within 1ms)
        if self.check_duplicate(order) {
            return Err(RiskError::DuplicateOrder);
        }

        // HARD BLOCK 7: Price collar (±5% from last trade price)
        {
            let slot = (order.instrument_id & 4095) as usize;
            let last = LAST_TRADE_PRICES[slot].load(Ordering::Relaxed);
            if last > 0 {
                let min_price = (last * 95) / 100;
                let max_price = (last * 105) / 100;
                let p = order.price as u64;
                if p < min_price || p > max_price {
                    return Err(RiskError::PriceCollar);
                }
            }
        }

        // SOFT BLOCK 1: Position limit (net position per instrument)
        {
            let slot = (order.instrument_id & 4095) as usize;
            let current = self.positions[slot];
            let next = match order.side {
                OrderSide::Bid => current.saturating_add(order.qty as i64),
                OrderSide::Ask => current.saturating_sub(order.qty as i64),
            };
            if next.abs() > self.position_limit {
                return Err(RiskError::PositionLimit);
            }
            // Optimistically update position — if downstream rejects, caller must call rollback_position()
            self.positions[slot] = next;
        }

        // SOFT BLOCK 2: Velocity limit (sliding window, corrected)
        if self.check_velocity_limit(order.timestamp) {
            return Err(RiskError::VelocityLimit);
        }

        // All checks passed — record the order timestamp in velocity ring
        self.velocity_ring[self.velocity_head] = order.timestamp;
        self.velocity_head = (self.velocity_head + 1) % VELOCITY_RING_SIZE;

        // Update duplicate detection ring
        self.recent_orders[(order.id & 4095) as usize] = (order.id, order.timestamp);

        self.orders_processed.fetch_add(1, Ordering::Relaxed);

        // Forward via shared memory if bridge is available
        if let Some(ref mut shm) = self.shm {
            let _ = shm.forward_order(order);
        }

        Ok(OrderStatus::Approved)
    }

    /// Roll back a position update if the order was later rejected downstream.
    pub fn rollback_position(&mut self, order: &Order) {
        let slot = (order.instrument_id & 4095) as usize;
        match order.side {
            OrderSide::Bid => {
                self.positions[slot] = self.positions[slot].saturating_sub(order.qty as i64)
            }
            OrderSide::Ask => {
                self.positions[slot] = self.positions[slot].saturating_add(order.qty as i64)
            }
        }
    }

    fn check_duplicate(&self, order: &Order) -> bool {
        let (old_id, old_ts) = self.recent_orders[(order.id & 4095) as usize];
        old_id == order.id && order.timestamp.wrapping_sub(old_ts) < self.duplicate_window_ns
    }

    /// Correct sliding-window velocity check.
    ///
    /// Looks back `max_velocity` entries in the velocity ring (circular buffer).
    /// If the oldest of those entries falls within `velocity_window_ns` of the
    /// current order's timestamp, we have exceeded the rate limit.
    ///
    /// This properly handles wrap-around and is O(1).
    fn check_velocity_limit(&self, now_ns: u64) -> bool {
        if self.max_velocity == 0 {
            return false; // Rate limiting disabled
        }
        // Index of the entry that was approved (max_velocity) orders ago
        let lookback_idx =
            (self.velocity_head + VELOCITY_RING_SIZE - self.max_velocity) % VELOCITY_RING_SIZE;
        let oldest_ts = self.velocity_ring[lookback_idx];

        // If the oldest entry in the window is non-zero and within the time window,
        // we have hit max_velocity orders within velocity_window_ns.
        oldest_ts > 0 && now_ns.saturating_sub(oldest_ts) < self.velocity_window_ns
    }

    pub fn get_orders_processed(&self) -> u64 {
        self.orders_processed.load(Ordering::Relaxed)
    }

    pub fn get_position(&self, instrument_id: u32) -> i64 {
        self.positions[(instrument_id & 4095) as usize]
    }

    /// Update the last trade price (called after each matched trade).
    pub fn update_reference_price(&self, instrument_id: u32, price: u64) {
        let slot = (instrument_id & 4095) as usize;
        LAST_TRADE_PRICES[slot].store(price, Ordering::Relaxed);
        self.fast_gate
            .update_reference_price(instrument_id, price as u32);
    }

    // -------------------------------------------------------------------------
    // Position persistence — crash recovery (Gap 6)
    // -------------------------------------------------------------------------
    //
    // Format: [magic: u64][count: u64][i64 × count]
    // Magic: 0x524F42494E504F53 ("ROBINPOS")
    // On startup: call load_snapshot() before accepting orders.
    // On SIGTERM/shutdown: call save_snapshot().
    // This is NOT on the hot path; it is only called at startup/shutdown.
    // -------------------------------------------------------------------------

    const SNAPSHOT_MAGIC: u64 = 0x524F42494E504F53; // "ROBINPOS"

    /// Save all 4096 instrument positions to a binary snapshot file.
    pub fn save_snapshot(&self, path: &str) -> std::io::Result<()> {
        use std::io::Write;
        // Write to a temp file then atomically rename to avoid partial writes
        let tmp_path = format!("{}.tmp", path);
        let mut f = std::fs::OpenOptions::new()
            .write(true)
            .create(true)
            .truncate(true)
            .open(&tmp_path)?;

        // Header
        f.write_all(&Self::SNAPSHOT_MAGIC.to_le_bytes())?;
        f.write_all(&(self.positions.len() as u64).to_le_bytes())?;

        // Position data
        for &pos in self.positions.iter() {
            f.write_all(&pos.to_le_bytes())?;
        }
        f.flush()?;
        drop(f);

        // Atomic rename
        std::fs::rename(&tmp_path, path)?;
        eprintln!(
            "[RISK] Position snapshot saved to {path} ({} instruments)",
            self.positions.len()
        );
        Ok(())
    }

    /// Load instrument positions from a binary snapshot file.
    /// Returns Ok(count) with the number of positions restored, or Err on failure.
    pub fn load_snapshot(&mut self, path: &str) -> std::io::Result<usize> {
        use std::io::Read;
        let mut f = std::fs::File::open(path)?;

        // Validate magic
        let mut magic_buf = [0u8; 8];
        f.read_exact(&mut magic_buf)?;
        let magic = u64::from_le_bytes(magic_buf);
        if magic != Self::SNAPSHOT_MAGIC {
            return Err(std::io::Error::new(
                std::io::ErrorKind::InvalidData,
                format!("invalid snapshot magic: 0x{:016X}", magic),
            ));
        }

        // Read count
        let mut count_buf = [0u8; 8];
        f.read_exact(&mut count_buf)?;
        let count = u64::from_le_bytes(count_buf) as usize;
        let restore_count = count.min(self.positions.len());

        // Read positions
        let mut pos_buf = [0u8; 8];
        for i in 0..restore_count {
            f.read_exact(&mut pos_buf)?;
            self.positions[i] = i64::from_le_bytes(pos_buf);
        }

        eprintln!(
            "[RISK] Position snapshot loaded from {path} ({restore_count} instruments restored)"
        );
        Ok(restore_count)
    }
}

#[allow(clippy::declare_interior_mutable_const)]
static LAST_TRADE_PRICES: [AtomicU64; 4096] = {
    const INIT: AtomicU64 = AtomicU64::new(0);
    [INIT; 4096]
};

pub fn update_last_trade_price(instrument_id: u32, price: u64) {
    let slot = (instrument_id & 4095) as usize;
    LAST_TRADE_PRICES[slot].store(price, Ordering::Relaxed);
}

#[repr(C, align(64))]
pub struct Order {
    pub id: u64,
    pub cl_order_id: u64,
    pub instrument_id: u32,
    pub symbol: [u8; 8],
    pub price: u32,
    pub qty: u32,
    pub side: OrderSide,
    pub timestamp: u64,
    pub account_id: u32,
    pub client_id: u32,
    pub strategy_id: u32,
    pub entry_time_ns: u64,
}

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum OrderStatus {
    Approved,
    Rejected,
}

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum RiskError {
    KillSwitchActive,
    CircuitBreakerTripped,
    FatFinger,
    PriceCollar,
    DuplicateOrder,
    PositionLimit,
    VelocityLimit,
    SymbolRestricted,
    CreditLimit,
}

// ============================================================================
// Tests
// ============================================================================
#[cfg(test)]
mod tests {
    use super::*;

    fn make_order(id: u64, price: u32, qty: u32, side: OrderSide, ts: u64) -> Order {
        Order {
            id,
            cl_order_id: id + 1000,
            instrument_id: 1,
            symbol: *b"AAPL    ",
            price,
            qty,
            side,
            timestamp: ts,
            account_id: 1,
            client_id: 42,
            strategy_id: 1,
            entry_time_ns: 0,
        }
    }

    #[test]
    fn test_approve_valid_order() {
        let mut gate = RiskGate::new("/tmp/test_shm_valid");
        let order = make_order(1, 15000, 100, OrderSide::Bid, 1_000_000_000);
        assert_eq!(gate.check_order(&order), Ok(OrderStatus::Approved));
    }

    #[test]
    fn test_reject_duplicate() {
        let mut gate = RiskGate::new("/tmp/test_shm_dup");
        let o1 = make_order(1, 15000, 100, OrderSide::Bid, 1_000_000_000);
        assert!(gate.check_order(&o1).is_ok());
        // Same id, within duplicate_window_ns (1ms = 1_000_000 ns)
        let o2 = make_order(1, 15000, 100, OrderSide::Bid, 1_000_500_000);
        assert_eq!(gate.check_order(&o2), Err(RiskError::DuplicateOrder));
    }

    #[test]
    fn test_duplicate_after_window_passes() {
        let mut gate = RiskGate::new("/tmp/test_shm_dup2");
        let o1 = make_order(5, 15000, 100, OrderSide::Bid, 1_000_000_000);
        assert!(gate.check_order(&o1).is_ok());
        // Same id but 2ms later — outside the 1ms window
        let o2 = make_order(5, 15000, 100, OrderSide::Bid, 1_002_000_000);
        assert_eq!(gate.check_order(&o2), Ok(OrderStatus::Approved));
    }

    #[test]
    fn test_reject_fat_finger_qty() {
        let mut gate = RiskGate::new("/tmp/test_shm_ff");
        let order = make_order(2, 15000, 2_000_000, OrderSide::Bid, 1_000_000_000);
        assert!(matches!(gate.check_order(&order), Err(_)));
    }

    #[test]
    fn test_reject_credit_limit() {
        // Use a gate with credit_limit=1_000_000.
        // Order: price=2000, qty=1000 → notional=2_000_000 > credit_limit=1_000_000
        // qty=1000 < max_qty=1_000_000 (ok), price=2000 < price_bound_upper=1_000_000 (ok)
        // So pre_trade passes, but gate credit check fails.
        let mut gate = RiskGate::with_config("/tmp/test_shm_credit", 1_000_000, 100, 100_000);
        let order = make_order(3, 2_000, 1_000, OrderSide::Bid, 1_000_000_000);
        assert_eq!(gate.check_order(&order), Err(RiskError::CreditLimit));
    }

    #[test]
    fn test_reject_position_limit() {
        let mut gate = RiskGate::new("/tmp/test_shm_pos");
        let o1 = Order {
            instrument_id: 5,
            ..make_order(10, 50000, 80_000, OrderSide::Bid, 1000)
        };
        assert_eq!(gate.check_order(&o1), Ok(OrderStatus::Approved));

        let o2 = Order {
            instrument_id: 5,
            ..make_order(11, 50000, 30_000, OrderSide::Bid, 2000)
        };
        assert_eq!(gate.check_order(&o2), Err(RiskError::PositionLimit));

        // Sell reduces position — should pass
        let o3 = Order {
            instrument_id: 5,
            ..make_order(12, 50000, 20_000, OrderSide::Ask, 3000)
        };
        assert_eq!(gate.check_order(&o3), Ok(OrderStatus::Approved));
    }

    #[test]
    fn test_reject_velocity_limit() {
        let mut gate = RiskGate::new("/tmp/test_shm_vel");
        // Send exactly max_velocity (100) orders, spaced 1000ns each (well within 1s window)
        for i in 0..100u64 {
            let o = make_order(i + 100, 50000, 1, OrderSide::Bid, 1_000_000 + i * 1000);
            assert_eq!(
                gate.check_order(&o),
                Ok(OrderStatus::Approved),
                "order {} should pass",
                i
            );
        }
        // 101st order within the same second should fail
        let o_fail = make_order(201, 50000, 1, OrderSide::Bid, 1_000_000 + 100 * 1000);
        assert_eq!(gate.check_order(&o_fail), Err(RiskError::VelocityLimit));
    }

    #[test]
    fn test_velocity_resets_after_window() {
        let mut gate = RiskGate::new("/tmp/test_shm_vel2");
        // Fill up the velocity window
        for i in 0..100u64 {
            let o = make_order(i + 300, 50000, 1, OrderSide::Bid, 1_000_000 + i * 1000);
            assert!(gate.check_order(&o).is_ok());
        }
        // Jump 2 seconds ahead — velocity window should have expired
        let o_after = make_order(401, 50000, 1, OrderSide::Bid, 3_000_000_000);
        assert_eq!(gate.check_order(&o_after), Ok(OrderStatus::Approved));
    }

    #[test]
    fn test_position_rollback() {
        let mut gate = RiskGate::new("/tmp/test_shm_rollback");
        let order = make_order(1, 15000, 1000, OrderSide::Bid, 1_000_000_000);
        assert!(gate.check_order(&order).is_ok());
        assert_eq!(gate.get_position(1), 1000);
        gate.rollback_position(&order);
        assert_eq!(gate.get_position(1), 0);
    }
}
