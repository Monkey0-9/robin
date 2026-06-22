use crate::gate::{Order, OrderSide};
use std::sync::atomic::{AtomicU32, AtomicU64, Ordering};

pub struct ComplianceThresholds {
    pub max_order_value:  u64,
    pub max_order_qty:    u32,
    pub price_collar_bps: u32,   // e.g. 500 = ±5%
    pub reference_price:  u32,   // Updated dynamically via update_reference_price()
    pub restricted_list:  [u32; 128],
    pub restricted_count: usize,
}

pub struct RiskGateFast {
    thresholds:           ComplianceThresholds,
    reference_price:      AtomicU32,   // Live-updated reference price
    failed_checks_count:  AtomicU64,
    total_checks_count:   AtomicU64,
}

impl RiskGateFast {
    pub fn new(thresholds: ComplianceThresholds) -> Self {
        let init_price = thresholds.reference_price;
        Self {
            thresholds,
            reference_price:     AtomicU32::new(init_price),
            failed_checks_count: AtomicU64::new(0),
            total_checks_count:  AtomicU64::new(0),
        }
    }

    /// Update the reference price used for price collar checks.
    /// Called after each matched trade to keep collars current.
    #[inline]
    pub fn update_reference_price(&self, price: u32) {
        self.reference_price.store(price, Ordering::Relaxed);
    }

    #[inline(always)]
    pub fn validate_compliance(&self, order: &Order) -> bool {
        self.total_checks_count.fetch_add(1, Ordering::Relaxed);

        // Check restricted list (linear scan — acceptable for ≤128 symbols)
        let n = self.thresholds.restricted_count.min(128);
        for i in 0..n {
            if self.thresholds.restricted_list[i] == order.instrument_id {
                self.failed_checks_count.fetch_add(1, Ordering::Relaxed);
                return false;
            }
        }

        // Order value check
        let order_value = (order.price as u64).saturating_mul(order.qty as u64);
        if order_value > self.thresholds.max_order_value {
            self.failed_checks_count.fetch_add(1, Ordering::Relaxed);
            return false;
        }

        // Quantity limit
        if order.qty > self.thresholds.max_order_qty {
            self.failed_checks_count.fetch_add(1, Ordering::Relaxed);
            return false;
        }

        // Price collar using live reference price
        let ref_p = self.reference_price.load(Ordering::Relaxed) as u64;
        if ref_p > 0 {
            let order_p        = order.price as u64;
            let collar_bps     = self.thresholds.price_collar_bps as u64;
            let max_allowed    = ref_p.saturating_mul(10000 + collar_bps) / 10000;
            let min_allowed    = ref_p.saturating_mul(10000u64.saturating_sub(collar_bps)) / 10000;

            let price_ok = match order.side {
                OrderSide::Bid => order_p <= max_allowed,
                OrderSide::Ask => order_p >= min_allowed,
            };

            if !price_ok {
                self.failed_checks_count.fetch_add(1, Ordering::Relaxed);
                return false;
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
        let total  = self.total_checks_count.load(Ordering::Relaxed);
        let failed = self.failed_checks_count.load(Ordering::Relaxed);
        if total == 0 { return 1.0; }
        1.0 - (failed as f64 / total as f64)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_gate() -> RiskGateFast {
        RiskGateFast::new(ComplianceThresholds {
            max_order_value:  10_000_000_000,
            max_order_qty:    100_000,
            price_collar_bps: 500,
            reference_price:  50_000,
            restricted_list:  [0u32; 128],
            restricted_count: 0,
        })
    }

    fn make_order(id: u64, instrument_id: u32, price: u32, qty: u32, side: OrderSide) -> Order {
        Order {
            id, cl_order_id: id + 1000, instrument_id,
            symbol: *b"AAPL    ", price, qty, side,
            timestamp: 1000, account_id: 1,
            client_id: 42, strategy_id: 1, entry_time_ns: 0,
        }
    }

    #[test]
    fn test_valid_order_passes() {
        let gate = make_gate();
        let order = make_order(1, 1, 51_000, 100, OrderSide::Bid);
        assert!(gate.validate_compliance(&order));
    }

    #[test]
    fn test_bid_above_collar_fails() {
        let gate = make_gate();
        // 50_000 * 1.05 = 52_500; anything above should fail for bids
        let order = make_order(2, 1, 53_000, 100, OrderSide::Bid);
        assert!(!gate.validate_compliance(&order));
        assert_eq!(gate.get_failed_count(), 1);
    }

    #[test]
    fn test_ask_below_collar_fails() {
        let gate = make_gate();
        // 50_000 * 0.95 = 47_500; ask below min should fail
        let order = make_order(3, 1, 47_000, 100, OrderSide::Ask);
        assert!(!gate.validate_compliance(&order));
    }

    #[test]
    fn test_restricted_symbol_fails() {
        let mut thresholds = ComplianceThresholds {
            max_order_value:  10_000_000_000,
            max_order_qty:    100_000,
            price_collar_bps: 500,
            reference_price:  50_000,
            restricted_list:  [0u32; 128],
            restricted_count: 1,
        };
        thresholds.restricted_list[0] = 42; // instrument_id 42 is restricted
        let gate = RiskGateFast::new(thresholds);
        let order = make_order(4, 42, 51_000, 100, OrderSide::Bid);
        assert!(!gate.validate_compliance(&order));
    }

    #[test]
    fn test_update_reference_price() {
        let gate = make_gate();
        // Update reference to 100_000 — collar is now ±5% of 100_000
        gate.update_reference_price(100_000);
        // 51_000 was valid at ref=50_000 but now 51_000 < 95_000 (ask min)
        // For bids: max = 100_000 * 1.05 = 105_000, so 51_000 bid should be valid
        let bid = make_order(5, 1, 51_000, 100, OrderSide::Bid);
        assert!(gate.validate_compliance(&bid));
        // Ask at 94_000 should fail (below 95_000 min)
        let ask = make_order(6, 1, 94_000, 100, OrderSide::Ask);
        assert!(!gate.validate_compliance(&ask));
    }

    #[test]
    fn test_pass_rate_calculation() {
        let gate = make_gate();
        let ok  = make_order(7, 1, 51_000, 100, OrderSide::Bid);
        let bad = make_order(8, 1, 60_000, 100, OrderSide::Bid);
        gate.validate_compliance(&ok);
        gate.validate_compliance(&bad);
        // 1 fail out of 2 = 50% pass rate
        assert!((gate.get_pass_rate() - 0.5).abs() < 1e-9);
    }
}
