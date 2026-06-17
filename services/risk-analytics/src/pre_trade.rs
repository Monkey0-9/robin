// services/risk-analytics/src/pre_trade.rs
// Pre-trade risk validation gate executing in <500 microseconds.

use crate::gate::{Order, OrderSide};

pub struct PreTradeRiskEvaluator {
    pub max_notional_limit: u64,
    pub max_qty_per_order: u32,
    pub price_bound_upper: u32,
    pub price_bound_lower: u32,
}

impl PreTradeRiskEvaluator {
    pub fn new(max_notional_limit: u64, max_qty_per_order: u32, price_bound_upper: u32, price_bound_lower: u32) -> Self {
        Self {
            max_notional_limit,
            max_qty_per_order,
            price_bound_upper,
            price_bound_lower,
        }
    }

    #[inline(always)]
    pub fn evaluate_order(&self, order: &Order) -> Result<(), &'static str> {
        // 1. Notional Limit Check
        let notional = (order.price as u64) * (order.qty as u64);
        if notional > self.max_notional_limit {
            return Err("NOTIONAL_LIMIT_EXCEEDED");
        }

        // 2. Order Quantity Check
        if order.qty > self.max_qty_per_order {
            return Err("ORDER_QTY_LIMIT_EXCEEDED");
        }

        // 3. Price Collar Check
        if order.price > self.price_bound_upper || order.price < self.price_bound_lower {
            return Err("PRICE_COLLAR_COLLISION");
        }

        Ok(())
    }
}
