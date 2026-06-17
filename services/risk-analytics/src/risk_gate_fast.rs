// services/risk-analytics/src/risk_gate_fast.rs
// Rust-based pre-trade risk controls and SEC Rule 15c3-5 compliance validator.
// Replaces Python-based post-trade compliance checks with inline high-performance validation.

use crate::gate::{Order, OrderSide};

pub struct ComplianceThresholds {
    pub max_order_value: u64,       // Fat-finger check limit (e.g. $1,000,000)
    pub max_order_qty: u32,         // Fat-finger quantity limit
    pub price_collar_pct: f32,      // Max price band deviation (e.g. 5%)
    pub reference_price: u32,       // Current reference market price
    pub restricted_list: Vec<u32>,   // Restricted instruments list
}

pub struct RiskGateFast {
    thresholds: ComplianceThresholds,
    failed_checks_count: u64,
}

impl RiskGateFast {
    pub fn new(thresholds: ComplianceThresholds) -> Self {
        Self {
            thresholds,
            failed_checks_count: 0,
        }
    }

    #[inline(always)]
    pub fn validate_compliance(&mut self, order: &Order) -> bool {
        // 1. Restricted list checks
        if self.thresholds.restricted_list.contains(&order.instrument_id) {
            self.failed_checks_count += 1;
            return false;
        }

        // 2. Fat-finger limits (value and quantity)
        let order_value = (order.price as u64) * (order.qty as u64);
        if order_value > self.thresholds.max_order_value {
            self.failed_checks_count += 1;
            return false;
        }
        if order.qty > self.thresholds.max_order_qty {
            self.failed_checks_count += 1;
            return false;
        }

        // 3. Price Collar Check (SEC 15c3-5 requirement)
        let ref_p = self.thresholds.reference_price as f32;
        let order_p = order.price as f32;
        let collar_upper = ref_p * (1.0 + self.thresholds.price_collar_pct);
        let collar_lower = ref_p * (1.0 - self.thresholds.price_collar_pct);

        match order.side {
            OrderSide::Bid => {
                if order_p > collar_upper {
                    self.failed_checks_count += 1;
                    return false;
                }
            }
            OrderSide::Ask => {
                if order_p < collar_lower {
                    self.failed_checks_count += 1;
                    return false;
                }
            }
        }

        true // Passed all regulatory and compliance checks
    }

    pub fn get_failed_count(&self) -> u64 {
        self.failed_checks_count
    }
}
