// services/risk-analytics/src/gate.rs
// Rust pre-trade risk and compliance validator.

use std::collections::HashMap;
use crate::shm_bridge::ShmBridge;
use crate::gpio_kill_switch::HardwareKillSwitch;
use crate::pre_trade::PreTradeRiskEvaluator;
use crate::circuit_breaker::RiskCircuitBreaker;

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum OrderSide {
    Bid,
    Ask,
}

#[derive(Debug, Clone)]
pub struct Order {
    pub id: u64,
    pub instrument_id: u32,
    pub price: u32,
    pub qty: u32,
    pub side: OrderSide,
}

pub struct RiskGate {
    pub positions: HashMap<u32, i32>,
    pub max_position: i32,
    pub available_credit: u64,
    pub shm: ShmBridge,
    pub kill_switch: HardwareKillSwitch,
    pub pre_trade: PreTradeRiskEvaluator,
    pub circuit_breaker: RiskCircuitBreaker,
}

impl RiskGate {
    pub fn new(max_position: i32, initial_credit: u64, shm_path: &str) -> anyhow::Result<Self> {
        let mut kill_switch = HardwareKillSwitch::new();
        kill_switch.start_monitoring();

        let pre_trade = PreTradeRiskEvaluator::new(100000000, 50000, 1000000, 100);
        let circuit_breaker = RiskCircuitBreaker::new(0.10); // 10% daily drawdown trip threshold

        Ok(Self {
            positions: HashMap::new(),
            max_position,
            available_credit: initial_credit,
            shm: ShmBridge::new(shm_path)?,
            kill_switch,
            pre_trade,
            circuit_breaker,
        })
    }

    #[inline(always)]
    pub fn evaluate_and_route(&mut self, order: &Order) -> bool {
        // 1. Hardware Kill Switch Check
        if self.kill_switch.is_active() || self.circuit_breaker.is_tripped() {
            return false;
        }

        // 2. Pre-trade checks (notional, fat-finger, price bounds)
        if self.pre_trade.evaluate_order(order).is_err() {
            return false;
        }

        // 3. Position checks
        let current_pos = self.positions.get(&order.instrument_id).copied().unwrap_or(0);
        let delta = match order.side {
            OrderSide::Bid => order.qty as i32,
            OrderSide::Ask => -(order.qty as i32),
        };
        
        let projected_pos = current_pos + delta;
        if projected_pos.abs() > self.max_position {
            return false;
        }

        // 4. Update ledger and forward via SHM ring buffer
        self.positions.insert(order.instrument_id, projected_pos);
        let item_bytes = self.serialize_order(order);
        self.shm.push(&item_bytes)
    }

    fn serialize_order(&self, order: &Order) -> [u8; 32] {
        let mut bytes = [0u8; 32];
        bytes[0..8].copy_from_slice(&order.id.to_le_bytes());
        bytes[8..12].copy_from_slice(&order.instrument_id.to_le_bytes());
        bytes[12..16].copy_from_slice(&order.price.to_le_bytes());
        bytes[16..20].copy_from_slice(&order.qty.to_le_bytes());
        bytes[20] = match order.side {
            OrderSide::Bid => 0,
            OrderSide::Ask => 1,
        };
        bytes
    }
}
