use crate::gate::Order;

#[derive(Clone)]
pub struct PreTradeRiskEvaluator {
    pub max_notional_limit: u64,
    pub max_qty_per_order: u32,
    pub price_bound_upper: u32,
    pub price_bound_lower: u32,
    pub restricted_instruments: [u32; 64],
    pub restricted_count: usize,
}

impl PreTradeRiskEvaluator {
    pub fn new(
        max_notional_limit: u64,
        max_qty_per_order: u32,
        price_bound_upper: u32,
        price_bound_lower: u32,
    ) -> Self {
        Self {
            max_notional_limit,
            max_qty_per_order,
            price_bound_upper,
            price_bound_lower,
            restricted_instruments: [0u32; 64],
            restricted_count: 0,
        }
    }

    pub fn add_restricted(&mut self, instrument_id: u32) {
        if self.restricted_count < 64 {
            self.restricted_instruments[self.restricted_count] = instrument_id;
            self.restricted_count += 1;
        }
    }

    #[inline(always)]
    pub fn evaluate_order(&self, order: &Order) -> Result<(), &'static str> {
        for i in 0..self.restricted_count {
            if self.restricted_instruments[i] == order.instrument_id {
                return Err("RESTRICTED_INSTRUMENT");
            }
        }

        if order.qty > self.max_qty_per_order {
            return Err("ORDER_QTY_LIMIT_EXCEEDED");
        }

        let notional = (order.price as u64) * (order.qty as u64);
        if notional > self.max_notional_limit {
            return Err("NOTIONAL_LIMIT_EXCEEDED");
        }

        if order.price > self.price_bound_upper || order.price < self.price_bound_lower {
            return Err("PRICE_COLLAR_COLLISION");
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_fat_finger_detection() {
        let evaluator = PreTradeRiskEvaluator::new(1_000_000_000, 10000, 100000, 1000);

        let order = Order {
            id: 1, cl_order_id: 1001, instrument_id: 1,
            symbol: *b"AAPL    ", price: 50000, qty: 999999, side: crate::gate::OrderSide::Bid,
            timestamp: 1000, account_id: 1,
            client_id: 42, strategy_id: 1, entry_time_ns: 0,
        };

        assert_eq!(evaluator.evaluate_order(&order), Err("ORDER_QTY_LIMIT_EXCEEDED"));
    }

    #[test]
    fn test_price_collar() {
        let evaluator = PreTradeRiskEvaluator::new(1_000_000_000, 10000, 100000, 50000);

        let order = Order {
            id: 1, cl_order_id: 1001, instrument_id: 1,
            symbol: *b"AAPL    ", price: 200000, qty: 100, side: crate::gate::OrderSide::Bid,
            timestamp: 1000, account_id: 1,
            client_id: 42, strategy_id: 1, entry_time_ns: 0,
        };

        assert_eq!(evaluator.evaluate_order(&order), Err("PRICE_COLLAR_COLLISION"));
    }
}
