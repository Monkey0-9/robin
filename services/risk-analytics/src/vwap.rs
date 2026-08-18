use std::sync::atomic::{AtomicU64, Ordering};

pub struct VWAPCalculator {
    cumulative_pv: AtomicU64, // Σ(price * volume)
    cumulative_v: AtomicU64,  // Σ(volume)
}

impl VWAPCalculator {
    pub fn new() -> Self {
        Self {
            cumulative_pv: AtomicU64::new(0),
            cumulative_v: AtomicU64::new(0),
        }
    }

    pub fn update(&self, price: u64, volume: u64) {
        let pv = price.saturating_mul(volume);
        self.cumulative_pv.fetch_add(pv, Ordering::Relaxed);
        self.cumulative_v.fetch_add(volume, Ordering::Relaxed);
    }

    pub fn vwap(&self) -> Option<u64> {
        let pv = self.cumulative_pv.load(Ordering::Relaxed);
        let v = self.cumulative_v.load(Ordering::Relaxed);
        if v == 0 {
            return None;
        }
        Some(pv / v)
    }

    pub fn reset(&self) {
        self.cumulative_pv.store(0, Ordering::Relaxed);
        self.cumulative_v.store(0, Ordering::Relaxed);
    }
}
