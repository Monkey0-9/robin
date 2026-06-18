use crate::gate::{Order, OrderSide};

pub struct ComplianceThresholds {
    pub max_order_value: u64,
    pub max_order_qty: u32,
    pub price_collar_pct: f64,
    pub reference_price: u32,
    pub restricted_list: [u32; 128],
    pub restricted_count: usize,
}

pub struct RiskGateFast {
    thresholds: ComplianceThresholds,
    failed_checks_count: u64,
    total_checks_count: u64,
}

impl RiskGateFast {
    pub fn new(thresholds: ComplianceThresholds) -> Self {
        Self {
            thresholds,
            failed_checks_count: 0,
            total_checks_count: 0,
        }
    }

    #[inline(always)]
    pub fn validate_compliance(&mut self, order: &Order) -> bool {
        self.total_checks_count += 1;

        for i in 0..self.thresholds.restricted_count {
            if self.thresholds.restricted_list[i] == order.instrument_id {
                self.failed_checks_count += 1;
                return false;
            }
        }

        let order_value = (order.price as u64) * (order.qty as u64);
        if order_value > self.thresholds.max_order_value {
            self.failed_checks_count += 1;
            return false;
        }

        if order.qty > self.thresholds.max_order_qty {
            self.failed_checks_count += 1;
            return false;
        }

        let ref_p = self.thresholds.reference_price as f64;
        let order_p = order.price as f64;
        let collar = self.thresholds.price_collar_pct;

        match order.side {
            OrderSide::Bid => {
                if order_p > ref_p * (1.0 + collar) {
                    self.failed_checks_count += 1;
                    return false;
                }
            }
            OrderSide::Ask => {
                if order_p < ref_p * (1.0 - collar) {
                    self.failed_checks_count += 1;
                    return false;
                }
            }
        }

        true
    }

    pub fn get_failed_count(&self) -> u64 {
        self.failed_checks_count
    }

    pub fn get_total_count(&self) -> u64 {
        self.total_checks_count
    }

    pub fn get_pass_rate(&self) -> f64 {
        if self.total_checks_count == 0 {
            return 1.0;
        }
        1.0 - (self.failed_checks_count as f64 / self.total_checks_count as f64)
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
            price_collar_pct: 0.05,
            reference_price: 50000,
            restricted_list: [0u32; 128],
            restricted_count: 0,
        };

        let mut gate = RiskGateFast::new(thresholds);

        let valid = Order {
            id: 1, cl_order_id: 1001, instrument_id: 1,
            price: 51000, qty: 100, side: OrderSide::Bid,
            client_id: 42, strategy_id: 1, entry_time_ns: 0,
        };

        assert!(gate.validate_compliance(&valid));

        let invalid = Order {
            id: 2, cl_order_id: 1002, instrument_id: 1,
            price: 60000, qty: 100, side: OrderSide::Bid,
            client_id: 42, strategy_id: 1, entry_time_ns: 0,
        };

        assert!(!gate.validate_compliance(&invalid));
        assert_eq!(gate.get_failed_count(), 1);
    }
}
