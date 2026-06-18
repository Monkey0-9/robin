// services/risk-analytics/src/gate.rs
// Rust-based pre-trade risk controls and SEC Rule 15c3-5 compliance validator.
// Replaces Python-based post-trade compliance checks with inline high-performance validation.

use crate::shm_bridge::{ShmBridge, ShmMessage};
use crate::gpio_kill_switch::HardwareKillSwitch;
use crate::pre_trade::PreTradeRiskEvaluator;
use crate::circuit_breaker::RiskCircuitBreaker;

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum OrderSide {
    Bid = 0,
    Ask = 1,
}

#[derive(Debug, Clone)]
pub struct Order {
    pub id: u64,
    pub cl_order_id: u64,
    pub instrument_id: u32,
    pub price: u32,
    pub qty: u32,
    pub side: OrderSide,
    pub client_id: u32,
    pub strategy_id: u32,
    pub entry_time_ns: u64,
}

#[derive(Clone, Copy)]
pub struct PositionRecord {
    pub instrument_id: u32,
    pub position: i64,
    pub cost_basis: u64,
    pub realized_pnl: i64,
}

#[derive(Clone, Copy)]
pub struct OrderHistoryRecord {
    pub instrument_id: u32,
    pub price: u32,
    pub qty: u32,
    pub timestamp_ns: u64,
    pub order_id: u64,
}

pub struct RiskGateConfig {
    pub max_position: i64,
    pub initial_credit: u64,
    pub shm_path: String,
    pub max_order_value: u64,
    pub max_order_qty: u32,
    pub price_collar_pct: f64,
    pub max_order_rate: u32,
    pub max_cancel_rate: u32,
}

pub struct RiskGate {
    pub positions: [PositionRecord; 1024],
    pub position_count: usize,
    pub max_position: i64,
    pub available_credit: u64,
    pub shm: ShmBridge,
    pub kill_switch: HardwareKillSwitch,
    pub pre_trade: PreTradeRiskEvaluator,
    pub circuit_breaker: RiskCircuitBreaker,
    
    // Pre-allocated ring buffer for duplicate order check (SEC 15c3-5)
    pub recent_orders: [OrderHistoryRecord; 1024],
    pub recent_order_idx: usize,
    
    pub order_count: u64,
    pub reject_count: u64,
    
    // Rate limiters
    pub last_second_orders: u32,
    pub last_second_cancels: u32,
    pub last_second_reset_ns: u64,
    pub max_order_rate: u32,
    pub max_cancel_rate: u32,
}

impl RiskGate {
    pub fn new(config: RiskGateConfig) -> anyhow::Result<Self> {
        let mut kill_switch = HardwareKillSwitch::new();
        kill_switch.start_monitoring();

        let pre_trade = PreTradeRiskEvaluator::new(
            config.max_order_value,
            config.max_order_qty,
            (config.max_order_value as u32) * 2, // upper price bound
            1, // lower price bound
        );
        let circuit_breaker = RiskCircuitBreaker::new(0.10); // 10% daily drawdown trip threshold

        // Unlink old path first just in case
        let _ = ShmBridge::unlink(&config.shm_path);
        let shm = ShmBridge::new(&config.shm_path, true).map_err(|e| anyhow::anyhow!(e))?;

        let positions = [PositionRecord { instrument_id: 0, position: 0, cost_basis: 0, realized_pnl: 0 }; 1024];
        let recent_orders = [OrderHistoryRecord { instrument_id: 0, price: 0, qty: 0, timestamp_ns: 0, order_id: 0 }; 1024];

        Ok(Self {
            positions,
            position_count: 0,
            max_position: config.max_position,
            available_credit: config.initial_credit,
            shm,
            kill_switch,
            pre_trade,
            circuit_breaker,
            recent_orders,
            recent_order_idx: 0,
            order_count: 0,
            reject_count: 0,
            last_second_orders: 0,
            last_second_cancels: 0,
            last_second_reset_ns: 0,
            max_order_rate: config.max_order_rate,
            max_cancel_rate: config.max_cancel_rate,
        })
    }

    #[inline(always)]
    pub fn evaluate_and_route(&mut self, order: &Order, current_time_ns: u64) -> bool {
        self.order_count += 1;

        // Reset rate limiter every second
        if current_time_ns - self.last_second_reset_ns >= 1_000_000_000 {
            self.last_second_orders = 0;
            self.last_second_cancels = 0;
            self.last_second_reset_ns = current_time_ns;
        }

        self.last_second_orders += 1;
        if self.last_second_orders > self.max_order_rate {
            self.reject_count += 1;
            return false;
        }

        // 1. Hardware Kill Switch Check
        if self.kill_switch.is_active() || self.circuit_breaker.is_tripped() {
            self.reject_count += 1;
            return false;
        }

        // 2. Pre-trade checks (notional, fat-finger, price bounds)
        if self.pre_trade.evaluate_order(order).is_err() {
            self.reject_count += 1;
            return false;
        }

        // 3. Duplicate Order Detection check (Hard Block)
        if self.check_duplicate(order, current_time_ns) {
            self.reject_count += 1;
            return false;
        }

        // 4. Credit Check
        let order_value = (order.price as u64) * (order.qty as u64);
        if order_value > self.available_credit {
            self.reject_count += 1;
            return false;
        }

        // 5. Position checks
        let mut current_pos = 0i64;
        let mut found = false;
        for i in 0..self.position_count {
            if self.positions[i].instrument_id == order.instrument_id {
                current_pos = self.positions[i].position;
                found = true;
                break;
            }
        }

        let delta = match order.side {
            OrderSide::Bid => order.qty as i64,
            OrderSide::Ask => -(order.qty as i64),
        };
        
        let projected = current_pos + delta;
        if projected.abs() > self.max_position {
            self.reject_count += 1;
            return false;
        }

        // Update ledger positions
        if found {
            for i in 0..self.position_count {
                if self.positions[i].instrument_id == order.instrument_id {
                    self.positions[i].position = projected;
                    break;
                }
            }
        } else if self.position_count < 1024 {
            self.positions[self.position_count] = PositionRecord {
                instrument_id: order.instrument_id,
                position: projected,
                cost_basis: order.price as u64,
                realized_pnl: 0,
            };
            self.position_count += 1;
        }

        self.available_credit -= order_value;
        self.record_order(order, current_time_ns);

        // Build binary shared memory message
        let shm_msg = ShmMessage {
            msg_type: 1,
            client_id: order.client_id,
            instrument_id: order.instrument_id,
            price: order.price,
            qty: order.qty,
            side: order.side as u8,
            flags: 0,
            order_id: order.id,
            cl_order_id: order.cl_order_id,
            timestamp_ns: current_time_ns,
            _pad: [0u8; 21],
        };

        self.shm.push(&shm_msg)
    }

    #[inline(always)]
    fn check_duplicate(&self, order: &Order, current_time_ns: u64) -> bool {
        for i in 0..1024 {
            let r = &self.recent_orders[i];
            if r.instrument_id == order.instrument_id
                && r.price == order.price
                && r.qty == order.qty
                && (current_time_ns - r.timestamp_ns) < 1_000_000 // 1 millisecond = 1,000,000 ns
            {
                return true;
            }
        }
        false
    }

    #[inline(always)]
    fn record_order(&mut self, order: &Order, current_time_ns: u64) {
        let idx = self.recent_order_idx;
        self.recent_orders[idx] = OrderHistoryRecord {
            instrument_id: order.instrument_id,
            price: order.price,
            qty: order.qty,
            timestamp_ns: current_time_ns,
            order_id: order.id,
        };
        self.recent_order_idx = (idx + 1) & 1023;
    }

    pub fn stats(&self) -> (u64, u64, u64) {
        (self.order_count, self.reject_count, self.shm.available())
    }

    pub fn update_credit(&mut self, credit: u64) {
        self.available_credit = credit;
    }

    pub fn update_position_limit(&mut self, limit: i64) {
        self.max_position = limit;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_risk_gate_basic() {
        let config = RiskGateConfig {
            max_position: 1000,
            initial_credit: 10_000_000_000,
            shm_path: "/robin_risk_gate_test".to_string(),
            max_order_value: 1_000_000_000,
            max_order_qty: 100000,
            price_collar_pct: 0.05,
            max_order_rate: 10000,
            max_cancel_rate: 5000,
        };

        let mut gate = RiskGate::new(config).unwrap();
        ShmBridge::unlink("/robin_risk_gate_test");

        let order = Order {
            id: 1,
            cl_order_id: 1001,
            instrument_id: 100,
            price: 50000,
            qty: 100,
            side: OrderSide::Bid,
            client_id: 42,
            strategy_id: 1,
            entry_time_ns: 1234567890,
        };

        let result = gate.evaluate_and_route(&order, 1234567890);
        assert!(result);
        assert_eq!(gate.order_count, 1);
    }

    #[test]
    fn test_duplicate_detection() {
        let config = RiskGateConfig {
            max_position: 1000,
            initial_credit: 10_000_000_000,
            shm_path: "/robin_dup_test".to_string(),
            max_order_value: 1_000_000_000,
            max_order_qty: 100000,
            price_collar_pct: 0.05,
            max_order_rate: 10000,
            max_cancel_rate: 5000,
        };

        let mut gate = RiskGate::new(config).unwrap();
        ShmBridge::unlink("/robin_dup_test");

        let order = Order {
            id: 1, cl_order_id: 1001, instrument_id: 100,
            price: 50000, qty: 100, side: OrderSide::Bid,
            client_id: 42, strategy_id: 1, entry_time_ns: 0,
        };

        assert!(gate.evaluate_and_route(&order, 100));
        let dup = Order { id: 2, ..order };
        assert!(!gate.evaluate_and_route(&dup, 150));
    }
}
