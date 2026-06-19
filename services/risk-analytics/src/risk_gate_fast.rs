use crate::gate::{Order, OrderSide};
use std::sync::atomic::{AtomicU64, Ordering};

pub struct ComplianceThresholds {
    pub max_order_value: u64,
    pub max_order_qty: u32,
    pub price_collar_bps: u32, // e.g., 500 = 5%
    pub reference_price: u32,
    pub restricted_list: [u32; 128],
    pub restricted_count: usize,
}

pub struct RiskGateFast {
    thresholds: ComplianceThresholds,
    failed_checks_count: AtomicU64,
    total_checks_count: AtomicU64,
}

impl RiskGateFast {
    pub fn new(thresholds: ComplianceThresholds) -> Self {
        Self {
            thresholds,
            failed_checks_count: AtomicU64::new(0),
            total_checks_count: AtomicU64::new(0),
        }
    }

    #[inline(always)]
    pub fn validate_compliance(&self, order: &Order) -> bool {
        self.total_checks_count.fetch_add(1, Ordering::Relaxed);

        for i in 0..core::cmp::min(self.thresholds.restricted_count, 128) {
            if self.thresholds.restricted_list[i] == order.instrument_id {
                self.failed_checks_count.fetch_add(1, Ordering::Relaxed);
                return false;
            }
        }

        let order_value = (order.price as u64) * (order.qty as u64);
        if order_value > self.thresholds.max_order_value {
            self.failed_checks_count.fetch_add(1, Ordering::Relaxed);
            return false;
        }

        if order.qty > self.thresholds.max_order_qty {
            self.failed_checks_count.fetch_add(1, Ordering::Relaxed);
            return false;
        }

        let ref_p = self.thresholds.reference_price as u64;
        let order_p = order.price as u64;
        let collar_bps = self.thresholds.price_collar_bps as u64;

        match order.side {
            OrderSide::Bid => {
                let max_allowed = ref_p.saturating_mul(10000 + collar_bps) / 10000;
                if order_p > max_allowed {
                    self.failed_checks_count.fetch_add(1, Ordering::Relaxed);
                    return false;
                }
            }
            OrderSide::Ask => {
                let min_allowed = ref_p.saturating_mul(10000 - collar_bps) / 10000;
                if order_p < min_allowed {
                    self.failed_checks_count.fetch_add(1, Ordering::Relaxed);
                    return false;
                }
            }
        }

        true
    }

    pub fn get_failed_count(&self) -> u64 {
        self.failed_checks_count.load(Ordering::Relaxed)
    }

    pub fn get_total_count(&self) -> u64 {
        self.total_checks_count.load(Ordering::Relaxed)
    }

    pub fn get_pass_rate(&self) -> f64 {
        let total = self.total_checks_count.load(Ordering::Relaxed);
        let failed = self.failed_checks_count.load(Ordering::Relaxed);
        if total == 0 {
            return 1.0;
        }
        1.0 - (failed as f64 / total as f64)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_compliance_validation() {
        let thresholds = ComplianceThresholds {
            max_order_value: 10_000_000_000,
            max_order_qty: 100000,
            price_collar_bps: 500, // 5%
            reference_price: 50000,
            restricted_list: [0u32; 128],
            restricted_count: 0,
        };

        let gate = RiskGateFast::new(thresholds);

        let valid = Order {
            id: 1, cl_order_id: 1001, instrument_id: 1,
            symbol: *b"AAPL    ", price: 51000, qty: 100, side: OrderSide::Bid,
            timestamp: 1000, account_id: 1,
            client_id: 42, strategy_id: 1, entry_time_ns: 0,
        };

        assert!(gate.validate_compliance(&valid));

        let invalid = Order {
            id: 2, cl_order_id: 1002, instrument_id: 1,
            symbol: *b"AAPL    ", price: 60000, qty: 100, side: OrderSide::Bid,
            timestamp: 1000, account_id: 1,
            client_id: 42, strategy_id: 1, entry_time_ns: 0,
        };

        assert!(!gate.validate_compliance(&invalid));
        assert_eq!(gate.get_failed_count(), 1);
    }
}
