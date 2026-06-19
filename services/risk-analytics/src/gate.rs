use crate::pre_trade::PreTradeRiskEvaluator;
use crate::circuit_breaker::RiskCircuitBreaker;
use crate::gpio_kill_switch::HardwareKillSwitch;
use crate::risk_gate_fast::{RiskGateFast, ComplianceThresholds};
use crate::shm_bridge::ShmBridge;
use crate::hedging::HedgingEngine;
use crate::tax_engine::TaxEngine;
use core::sync::atomic::{AtomicU64, Ordering};
// No HashMap needed, using direct-mapped array

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum OrderSide {
    Bid = 0,
    Ask = 1,
}

/// Prototype pre-trade risk gate framework.
///
/// HARD BLOCKS (simulated/mocked checks):
/// - Order size limits (fat-finger protection stub)
/// - Price collars (±X% from last trade stub)
/// - Credit limits (pre-funded account balance stub)
/// - Symbol restrictions (blocked/unlisted securities stub)
/// - Duplicate order detection (simulated duplicate check)
///
/// SOFT BLOCKS (simulated/mocked checks):
/// - Position limits (per-symbol, per-account stub)
/// - Velocity limits (orders/sec stub)
/// - Concentration limits (% of portfolio stub)
/// - Strategy-specific filters stub
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
    duplicate_window_ns: u64,
    recent_orders: Box<[(u64, u64)]>,
    positions: Box<[i64]>,
    last_order_timestamps: Box<[u64]>,
    timestamp_idx: usize,
}

impl RiskGate {
    pub fn new(shm_path: &str) -> Self {
        Self {
            pre_trade: PreTradeRiskEvaluator::new(
                10_000_000_000,
                1_000_000,
                1_000_000,
                1,
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
            duplicate_window_ns: 1_000_000,
            recent_orders: vec![(0, 0); 4096].into_boxed_slice(),
            positions: vec![0i64; 4096].into_boxed_slice(),
            last_order_timestamps: vec![0u64; 128].into_boxed_slice(),
            timestamp_idx: 0,
        }
    }

    /// Prototype pre-trade check.
    /// Latency: Simulated targets (<200ns p50, <500ns p99)
    pub fn check_order(&mut self, order: &Order) -> Result<OrderStatus, RiskError> {
        // HARD BLOCK 1: Kill switch (hardware GPIO)
        if self.kill_switch.is_active() {
            return Err(RiskError::KillSwitchActive);
        }

        // HARD BLOCK 2: Circuit breaker
        if self.circuit_breaker.is_tripped() {
            return Err(RiskError::CircuitBreakerTripped);
        }

        // HARD BLOCK 3: Basic pre-trade checks
        self.pre_trade.evaluate_order(order).map_err(|_| RiskError::SymbolRestricted)?;

        // HARD BLOCK 4: Fast path compliance (restricted symbols, limits)
        if !self.fast_gate.validate_compliance(order) {
            return Err(RiskError::SymbolRestricted);
        }

        // HARD BLOCK 5: Duplicate detection
        if self.check_duplicate(order) {
            return Err(RiskError::DuplicateOrder);
        }

        // SOFT BLOCKS: Configurable risk limits
        self.check_soft_blocks(order)?;

        // Update positions on approval
        let slot = (order.instrument_id & 4095) as usize;
        let current_pos = self.positions[slot];
        let next_pos = match order.side {
            OrderSide::Bid => current_pos + (order.qty as i64),
            OrderSide::Ask => current_pos - (order.qty as i64),
        };
        self.positions[slot] = next_pos;

        // Update velocity timestamp circular buffer
        self.last_order_timestamps[self.timestamp_idx] = order.timestamp;
        self.timestamp_idx = (self.timestamp_idx + 1) % 128;

        // Audit trail
        self.orders_processed.fetch_add(1, Ordering::Relaxed);

        // Forward via shared memory if bridge is available
        if let Some(ref mut shm) = self.shm {
            let _ = shm.forward_order(order);
        }

        Ok(OrderStatus::Approved)
    }

    fn check_duplicate(&mut self, order: &Order) -> bool {
        let key = order.id;
        let slot = (key & 4095) as usize;
        let (old_id, old_ts) = self.recent_orders[slot];
        if old_id == key && order.timestamp.wrapping_sub(old_ts) < self.duplicate_window_ns {
            return true;
        }
        self.recent_orders[slot] = (key, order.timestamp);
        false
    }

    fn check_soft_blocks(&self, order: &Order) -> Result<(), RiskError> {
        // 1. Fat finger protection
        if (order.qty as u64) > 1_000_000 {
            return Err(RiskError::FatFinger);
        }

        // 2. Price collar
        let last = LAST_TRADE_PRICE.load(Ordering::Relaxed);
        if last > 0 {
            let min_price = (last * 95) / 100;
            let max_price = (last * 105) / 100;
            if (order.price as u64) < min_price || (order.price as u64) > max_price {
                return Err(RiskError::PriceCollar);
            }
        }

        // 3. Position limit (net position limit, e.g., 100,000 shares)
        let position_limit = 100_000i64;
        let slot = (order.instrument_id & 4095) as usize;
        let current_pos = self.positions[slot];
        let next_pos = match order.side {
            OrderSide::Bid => current_pos + (order.qty as i64),
            OrderSide::Ask => current_pos - (order.qty as i64),
        };
        if next_pos.abs() > position_limit {
            return Err(RiskError::PositionLimit);
        }

        // 4. Velocity rate limit (max 100 orders in a 1-second sliding window)
        let rate_limit = 100;
        let check_idx = (self.timestamp_idx + 128 - rate_limit) % 128;
        let old_ts = self.last_order_timestamps[check_idx];
        if old_ts > 0 && order.timestamp.saturating_sub(old_ts) < 1_000_000_000 {
            return Err(RiskError::VelocityLimit);
        }

        Ok(())
    }

    pub fn get_orders_processed(&self) -> u64 {
        self.orders_processed.load(Ordering::Relaxed)
    }
}


static LAST_TRADE_PRICE: AtomicU64 = AtomicU64::new(0);

pub fn update_last_trade_price(price: u64) {
    LAST_TRADE_PRICE.store(price, Ordering::Relaxed);
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_approve_valid_order() {
        let mut gate = RiskGate::new("/tmp/test_shm");
        let order = Order {
            id: 1, cl_order_id: 1001, instrument_id: 1,
            symbol: *b"AAPL    ", price: 15000, qty: 100,
            side: OrderSide::Bid, timestamp: 1000, account_id: 1,
            client_id: 42, strategy_id: 1, entry_time_ns: 0,
        };
        assert_eq!(gate.check_order(&order), Ok(OrderStatus::Approved));
    }

    #[test]
    fn test_reject_duplicate() {
        let mut gate = RiskGate::new("/tmp/test_shm");
        let order = Order {
            id: 1, cl_order_id: 1001, instrument_id: 1,
            symbol: *b"AAPL    ", price: 15000, qty: 100,
            side: OrderSide::Bid, timestamp: 1000, account_id: 1,
            client_id: 42, strategy_id: 1, entry_time_ns: 0,
        };
        assert!(gate.check_order(&order).is_ok());
        let order2 = Order {
            id: 1, cl_order_id: 1001, instrument_id: 1,
            symbol: *b"AAPL    ", price: 15000, qty: 100,
            side: OrderSide::Bid, timestamp: 1001, account_id: 1,
            client_id: 42, strategy_id: 1, entry_time_ns: 0,
        };
        assert_eq!(gate.check_order(&order2), Err(RiskError::DuplicateOrder));
    }

    #[test]
    fn test_reject_fat_finger() {
        let mut gate = RiskGate::new("/tmp/test_shm");
        let order = Order {
            id: 2, cl_order_id: 1002, instrument_id: 1,
            symbol: *b"AAPL    ", price: 15000, qty: 2_000_000,
            side: OrderSide::Bid, timestamp: 1000, account_id: 1,
            client_id: 42, strategy_id: 1, entry_time_ns: 0,
        };
        assert!(matches!(gate.check_order(&order), Err(_)));
    }

    #[test]
    fn test_reject_position_limit() {
        let mut gate = RiskGate::new("/tmp/test_shm");
        let o1 = Order {
            id: 10, cl_order_id: 1010, instrument_id: 5,
            symbol: *b"MSFT    ", price: 50000, qty: 80_000,
            side: OrderSide::Bid, timestamp: 1000, account_id: 1,
            client_id: 42, strategy_id: 1, entry_time_ns: 0,
        };
        assert_eq!(gate.check_order(&o1), Ok(OrderStatus::Approved));

        let o2 = Order {
            id: 11, cl_order_id: 1011, instrument_id: 5,
            symbol: *b"MSFT    ", price: 50000, qty: 30_000,
            side: OrderSide::Bid, timestamp: 2000, account_id: 1,
            client_id: 42, strategy_id: 1, entry_time_ns: 0,
        };
        assert_eq!(gate.check_order(&o2), Err(RiskError::PositionLimit));

        // Sell order should pass since it reduces position
        let o3 = Order {
            id: 12, cl_order_id: 1012, instrument_id: 5,
            symbol: *b"MSFT    ", price: 50000, qty: 20_000,
            side: OrderSide::Ask, timestamp: 3000, account_id: 1,
            client_id: 42, strategy_id: 1, entry_time_ns: 0,
        };
        assert_eq!(gate.check_order(&o3), Ok(OrderStatus::Approved));
    }

    #[test]
    fn test_reject_velocity_limit() {
        let mut gate = RiskGate::new("/tmp/test_shm");
        for i in 0..100 {
            let order = Order {
                id: i + 100, cl_order_id: i + 1000, instrument_id: 1,
                symbol: *b"AAPL    ", price: 50000, qty: 1,
                side: OrderSide::Bid, timestamp: 1_000_000 + i as u64 * 1000,
                account_id: 1, client_id: 42, strategy_id: 1, entry_time_ns: 0,
            };
            assert_eq!(gate.check_order(&order), Ok(OrderStatus::Approved));
        }

        // 101st order within 1 second should fail
        let o_fail = Order {
            id: 201, cl_order_id: 2001, instrument_id: 1,
            symbol: *b"AAPL    ", price: 50000, qty: 1,
            side: OrderSide::Bid, timestamp: 1_000_000 + 99_000,
            account_id: 1, client_id: 42, strategy_id: 1, entry_time_ns: 0,
        };
        assert_eq!(gate.check_order(&o_fail), Err(RiskError::VelocityLimit));
    }
}
