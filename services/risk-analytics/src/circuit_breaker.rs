use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::time::{SystemTime, UNIX_EPOCH};

pub struct RiskCircuitBreaker {
    tripped: AtomicBool,
    daily_drawdown_limit: f64,
    current_drawdown: AtomicU64,
    peak_equity: AtomicU64,
    current_equity: AtomicU64,
    trip_time_ns: AtomicU64,
    reset_count: AtomicU64,
    trip_count: AtomicU64,
}

impl RiskCircuitBreaker {
    pub fn new(daily_drawdown_limit: f64) -> Self {
        Self {
            tripped: AtomicBool::new(false),
            daily_drawdown_limit,
            current_drawdown: AtomicU64::new(0),
            peak_equity: AtomicU64::new(0),
            current_equity: AtomicU64::new(0),
            trip_time_ns: AtomicU64::new(0),
            reset_count: AtomicU64::new(0),
            trip_count: AtomicU64::new(0),
        }
    }

    #[inline(always)]
    pub fn check_drawdown(&self, peak_equity: f64, current_equity: f64) -> bool {
        if self.tripped.load(Ordering::Relaxed) {
            return true;
        }

        self.peak_equity.store(peak_equity.to_bits(), Ordering::Relaxed);
        self.current_equity.store(current_equity.to_bits(), Ordering::Relaxed);

        if peak_equity > 0.0 {
            let dd_bps = (((peak_equity - current_equity) / peak_equity) * 10000.0) as u64;
            self.current_drawdown.store(dd_bps, Ordering::Relaxed);
            if (dd_bps as f64 / 10000.0) >= self.daily_drawdown_limit {
                self.trip("DAILY_DRAWDOWN_LIMIT_EXCEEDED");
                return true;
            }
        }

        false
    }

    pub fn trip(&self, reason: &str) {
        self.tripped.store(true, Ordering::Release);
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_nanos() as u64;
        self.trip_time_ns.store(now, Ordering::Relaxed);
        self.trip_count.fetch_add(1, Ordering::Relaxed);
        println!("[CIRCUIT_BREAKER] TRIPPED: {} at {}", reason, now);
    }

    pub fn reset(&self) {
        self.tripped.store(false, Ordering::Release);
        self.trip_time_ns.store(0, Ordering::Relaxed);
        self.reset_count.fetch_add(1, Ordering::Relaxed);
        println!("[CIRCUIT_BREAKER] RESET");
    }

    #[inline(always)]
    pub fn is_tripped(&self) -> bool {
        self.tripped.load(Ordering::Acquire)
    }

    pub fn get_stats(&self) -> (u64, u64, u64) {
        (
            self.trip_count.load(Ordering::Relaxed),
            self.reset_count.load(Ordering::Relaxed),
            self.current_drawdown.load(Ordering::Relaxed),
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_circuit_breaker() {
        let cb = RiskCircuitBreaker::new(0.10);

        assert!(!cb.check_drawdown(1000.0, 950.0));
        assert!(!cb.is_tripped());

        assert!(cb.check_drawdown(1000.0, 850.0));
        assert!(cb.is_tripped());

        cb.reset();
        assert!(!cb.is_tripped());
    }
}
