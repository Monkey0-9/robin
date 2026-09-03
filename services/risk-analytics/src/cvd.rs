use std::sync::atomic::{AtomicI64, Ordering};

pub struct CVDTracker {
    cvd: AtomicI64,
}

impl Default for CVDTracker {
    fn default() -> Self {
        Self::new()
    }
}

impl CVDTracker {
    pub fn new() -> Self {
        Self {
            cvd: AtomicI64::new(0),
        }
    }

    pub fn on_trade(&self, price: u64, volume: u64, best_bid: u64, best_ask: u64) {
        // Aggressive buyer: price >= ask
        // Aggressive seller: price <= bid
        // Otherwise: mid-price classification
        let delta = if price >= best_ask {
            volume as i64
        } else if price <= best_bid {
            -(volume as i64)
        } else {
            // At mid — classify by which side of mid
            let mid = (best_bid + best_ask) / 2;
            if price > mid {
                volume as i64
            } else {
                -(volume as i64)
            }
        };
        self.cvd.fetch_add(delta, Ordering::Relaxed);
    }

    pub fn cvd(&self) -> i64 {
        self.cvd.load(Ordering::Relaxed)
    }
}
