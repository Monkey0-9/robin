// ============================================================================
// Sharded Concurrent Risk Gate (services/risk-analytics/src/sharded_gate.rs)
// ============================================================================
// Resolves the &mut self bottleneck in gate.rs by sharding order checking
// across 16 independent gate instances, selected by (account_id % 16).
//
// Each shard is an Arc<Mutex<RiskGate>> and lives behind its own cache line
// to eliminate false sharing.  All 16 shards can check orders concurrently.
//
// Usage:
//   let sg = ShardedRiskGate::new("/robin_ingest_risk");
//   let result = sg.check_order(&order);   // selects shard by account_id
// ============================================================================

use crate::gate::{Order, OrderStatus, RiskError, RiskGate};
use std::sync::{Arc, Mutex};

const NUM_SHARDS: usize = 16;

/// One cache-line-padded shard wrapper to prevent false sharing.
#[repr(align(128))]
struct Shard {
    gate: Mutex<RiskGate>,
}

impl Shard {
    fn new(shm_path: &str) -> Self {
        Self {
            gate: Mutex::new(RiskGate::new(shm_path)),
        }
    }
}

/// Sharded risk gate: NUM_SHARDS independent gates, routed by account_id.
pub struct ShardedRiskGate {
    shards: Arc<Vec<Shard>>,
}

impl ShardedRiskGate {
    /// Construct with NUM_SHARDS independent gates, all sharing `shm_path`.
    pub fn new(shm_path: &str) -> Self {
        let mut shards = Vec::with_capacity(NUM_SHARDS);
        for _ in 0..NUM_SHARDS {
            shards.push(Shard::new(shm_path));
        }
        Self {
            shards: Arc::new(shards),
        }
    }

    /// Build with custom credit/position limits per shard.
    pub fn with_config(shm_path: &str, credit_limit: u64, position_limit: i64) -> Self {
        let mut shards = Vec::with_capacity(NUM_SHARDS);
        for _ in 0..NUM_SHARDS {
            shards.push(Shard {
                gate: Mutex::new(RiskGate::with_config(
                    shm_path,
                    credit_limit,
                    position_limit,
                    u64::MAX,
                )),
            });
        }
        Self {
            shards: Arc::new(shards),
        }
    }

    /// Route to the appropriate shard and perform the risk check.
    /// Contention is O(1/NUM_SHARDS) vs a single gate.
    ///
    /// # Returns
    /// - `Ok(OrderStatus::Approved)` if all checks pass.
    /// - `Err(RiskError::*)` if any hard/soft block fires.
    pub fn check_order(&self, order: &Order) -> Result<OrderStatus, RiskError> {
        let shard_idx = (order.account_id as usize) % NUM_SHARDS;
        let shard = &self.shards[shard_idx];
        let mut gate = shard.gate.lock().map_err(|_| RiskError::KillSwitchActive)?;
        gate.check_order(order)
    }

    /// Notify the appropriate shard of a fill (confirms/releases reservation).
    pub fn on_fill(
        &self,
        order_id: u64,
        instrument_id: u32,
        account_id: u32,
        fill_price: u64,
        fill_qty: u64,
        side: crate::gate::OrderSide,
    ) {
        let shard_idx = (account_id as usize) % NUM_SHARDS;
        if let Ok(mut gate) = self.shards[shard_idx].gate.lock() {
            gate.confirm_reservation(order_id);
            gate.update_pnl(instrument_id, account_id, fill_price, fill_qty, side);
        }
    }

    /// Notify a shard of a rejection so the optimistic reservation is released.
    pub fn on_reject(&self, order_id: u64, account_id: u32) {
        let shard_idx = (account_id as usize) % NUM_SHARDS;
        if let Ok(mut gate) = self.shards[shard_idx].gate.lock() {
            gate.on_reject(order_id);
        }
    }

    /// Sweep timed-out reservations across all shards.
    /// Call periodically from a maintenance thread (e.g., every 100ms).
    pub fn check_timeouts(&self, now_ns: u64, timeout_ns: u64) {
        for shard in self.shards.iter() {
            if let Ok(mut gate) = shard.gate.lock() {
                gate.check_timeouts(now_ns, timeout_ns);
            }
        }
    }

    /// Aggregate orders-processed across all shards.
    pub fn orders_processed(&self) -> u64 {
        self.shards
            .iter()
            .filter_map(|s| s.gate.lock().ok().map(|g| g.get_orders_processed()))
            .sum()
    }

    /// Number of shards (useful for Prometheus labels).
    pub fn num_shards() -> usize {
        NUM_SHARDS
    }
}

/// Allow cheap clones of the gate handle (backed by Arc).
impl Clone for ShardedRiskGate {
    fn clone(&self) -> Self {
        Self {
            shards: Arc::clone(&self.shards),
        }
    }
}

// ============================================================================
// Unit Tests
// ============================================================================
#[cfg(test)]
mod tests {
    use super::*;
    use crate::gate::{Order, OrderSide};
    use std::thread;

    fn make_order(account_id: u32, instrument_id: u32, qty: u64, price: u64) -> Order {
        Order {
            id: account_id as u64 * 1000 + instrument_id as u64,
            cl_order_id: account_id as u64 * 1000 + instrument_id as u64 + 1,
            account_id,
            instrument_id,
            symbol: *b"AAPL    ",
            qty,
            price,
            side: OrderSide::Bid,
            timestamp: 1_000_000_000,
            strategy_id: 0,
            client_id: 0,
            entry_time_ns: 1_000_000_000,
        }
    }

    #[test]
    fn test_sharded_gate_approves_valid_order() {
        let gate = ShardedRiskGate::new("/tmp/test_shm");
        let order = make_order(1, 42, 100, 50_000 * 100_000_000);
        let result = gate.check_order(&order);
        assert!(result.is_ok(), "Expected approval: {:?}", result);
    }

    #[test]
    fn test_different_accounts_route_to_different_shards() {
        let gate = ShardedRiskGate::new("/tmp/test_shm");
        // Accounts in different shard buckets
        for account_id in 0..NUM_SHARDS as u32 {
            let order = make_order(account_id, 1, 100, 50_000 * 100_000_000);
            let result = gate.check_order(&order);
            assert!(
                result.is_ok(),
                "Account {} rejected: {:?}",
                account_id,
                result
            );
        }
    }

    #[test]
    fn test_concurrent_checks_across_shards() {
        let gate = Arc::new(ShardedRiskGate::new("/tmp/test_shm_concurrent"));
        let mut handles = vec![];
        for i in 0..8u32 {
            let g = Arc::clone(&gate);
            handles.push(thread::spawn(move || {
                for j in 0..100u64 {
                    let order = Order {
                        id: (i as u64) * 10_000 + j,
                        cl_order_id: (i as u64) * 10_000 + j + 1,
                        account_id: i,
                        instrument_id: 1,
                        qty: 100,
                        price: 50_000 * 100_000_000,
                        side: OrderSide::Bid,
                        timestamp: 1_000_000_000 + j * 1_000_000,
                        strategy_id: 0,
                        client_id: 0,
                        symbol: *b"AAPL    ",
                        entry_time_ns: 1_000_000_000 + j * 1_000_000,
                    };
                    let _ = g.check_order(&order);
                }
            }));
        }
        for h in handles {
            h.join().unwrap();
        }
        // Should not deadlock or panic
        assert!(gate.orders_processed() > 0);
    }

    #[test]
    fn test_shard_isolation_fat_finger() {
        let gate = ShardedRiskGate::with_config("/tmp/test_shm_ff", u64::MAX, i64::MAX);
        // Fat finger: qty > 1_000_000
        let order = make_order(0, 1, 2_000_000 * 100_000_000, 100);
        let result = gate.check_order(&order);
        assert!(
            matches!(result, Err(RiskError::FatFinger)),
            "Expected FatFinger: {:?}",
            result
        );
    }
}
