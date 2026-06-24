use std::collections::HashMap;

#[derive(Clone, Copy, Debug)]
pub struct Position {
    pub symbol: &'static str,
    pub qty: i64,
    pub avg_price: f64,
}

#[derive(Clone, Copy, Debug)]
pub struct HedgeOrder {
    pub symbol: &'static str,
    pub side: HedgeSide,
    pub qty: u64,
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub enum HedgeSide {
    Buy,
    Sell,
}

pub struct HedgingEngine {
    positions: HashMap<&'static str, Position>,
    beta_sensitivities: HashMap<&'static str, f64>,
}

impl Default for HedgingEngine {
    fn default() -> Self {
        Self::new()
    }
}

impl HedgingEngine {
    pub fn new() -> Self {
        Self {
            positions: HashMap::new(),
            beta_sensitivities: HashMap::new(),
        }
    }

    pub fn update_position(&mut self, pos: Position) {
        self.positions.insert(pos.symbol, pos);
    }

    pub fn set_beta(&mut self, symbol: &'static str, beta: f64) {
        self.beta_sensitivities.insert(symbol, beta);
    }

    pub fn execute_hedge(&self, index_price: f64) -> Vec<HedgeOrder> {
        let mut orders = Vec::new();
        for (symbol, pos) in &self.positions {
            if let Some(beta) = self.beta_sensitivities.get(symbol) {
                let market_exposure = pos.qty as f64 * pos.avg_price;
                let hedge_notional = market_exposure * beta;
                if hedge_notional.abs() > 1000.0 {
                    let hedge_qty = (hedge_notional / index_price).abs() as u64;
                    let side = if hedge_notional > 0.0 {
                        HedgeSide::Sell
                    } else {
                        HedgeSide::Buy
                    };
                    if hedge_qty > 0 {
                        orders.push(HedgeOrder {
                            symbol,
                            side,
                            qty: hedge_qty,
                        });
                    }
                }
            }
        }
        orders
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_delta_neutral_hedge() {
        let mut engine = HedgingEngine::new();
        engine.update_position(Position {
            symbol: "AAPL",
            qty: 1000,
            avg_price: 150.0,
        });
        engine.set_beta("AAPL", 1.2);
        let orders = engine.execute_hedge(5000.0);
        assert_eq!(orders.len(), 1);
        assert_eq!(orders[0].symbol, "AAPL");
        assert_eq!(orders[0].side, HedgeSide::Sell);
    }

    #[test]
    fn test_no_hedge_for_small_position() {
        let mut engine = HedgingEngine::new();
        engine.update_position(Position {
            symbol: "AAPL",
            qty: 1,
            avg_price: 150.0,
        });
        engine.set_beta("AAPL", 1.2);
        let orders = engine.execute_hedge(5000.0);
        assert!(orders.is_empty());
    }
}
