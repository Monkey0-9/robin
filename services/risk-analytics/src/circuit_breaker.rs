use std::sync::atomic::{AtomicBool, AtomicI64, AtomicU64, Ordering};
use std::sync::Mutex;

use coarsetime::Clock;

fn cached_time_ns() -> u64 {
    let now = Clock::now_since_epoch();
    now.as_secs() * 1_000_000_000 + now.as_nanos() % 1_000_000_000
}

/// Consistent (peak, current) equity pair.
///
/// Both values must be observed and updated atomically together, otherwise a
/// concurrent reader can see a new peak with a stale equity (exaggerated
/// drawdown -> spurious trip) or an old peak with a new equity (missed trip).
#[derive(Clone, Copy, Default)]
struct DrawdownState {
    peak_equity: f64,
    current_equity: f64,
}

pub struct RiskCircuitBreaker {
    tripped: AtomicBool,
    daily_drawdown_limit: f64,
    // Guarded by the mutex so peak + current are always updated/read as a pair.
    state: Mutex<DrawdownState>,
    current_drawdown: AtomicI64,
    trip_time_ns: AtomicU64,
    reset_count: AtomicU64,
    trip_count: AtomicU64,
}

impl RiskCircuitBreaker {
    pub fn new(daily_drawdown_limit: f64) -> Self {
        Self {
            tripped: AtomicBool::new(false),
            daily_drawdown_limit,
            state: Mutex::new(DrawdownState::default()),
            current_drawdown: AtomicI64::new(0),
            trip_time_ns: AtomicU64::new(0),
            reset_count: AtomicU64::new(0),
            trip_count: AtomicU64::new(0),
        }
    }

    #[inline]
    pub fn check_drawdown(&self, peak_equity: f64, current_equity: f64) -> bool {
        if self.tripped.load(Ordering::Acquire) {
            return true;
        }

        let mut st = match self.state.lock() {
            Ok(guard) => guard,
            Err(poisoned) => poisoned.into_inner(),
        };

        if peak_equity > st.peak_equity {
            st.peak_equity = peak_equity;
        }
        st.current_equity = current_equity;

        let final_peak = st.peak_equity;
        if final_peak > 0.0 {
            let dd_bps = (((final_peak - st.current_equity) / final_peak) * 10000.0) as i64;
            self.current_drawdown.store(dd_bps, Ordering::Release);
            if (dd_bps as f64 / 10000.0) >= self.daily_drawdown_limit {
                self.trip("DAILY_DRAWDOWN_LIMIT_EXCEEDED");
                return true;
            }
        }

        false
    }

    pub fn trip(&self, reason: &str) {
        self.tripped.store(true, Ordering::Release);
        let now = cached_time_ns();
        self.trip_time_ns.store(now, Ordering::Release);
        self.trip_count.fetch_add(1, Ordering::Release);
        println!("[CIRCUIT_BREAKER] TRIPPED: {} at {}", reason, now);
    }

    pub fn reset(&self) {
        self.tripped.store(false, Ordering::Release);
        self.trip_time_ns.store(0, Ordering::Release);
        self.reset_count.fetch_add(1, Ordering::Release);
        println!("[CIRCUIT_BREAKER] RESET");
    }

    #[inline(always)]
    pub fn is_tripped(&self) -> bool {
        self.tripped.load(Ordering::Acquire)
    }

    pub fn get_stats(&self) -> (u64, u64, u64) {
        (
            self.trip_count.load(Ordering::Acquire),
            self.reset_count.load(Ordering::Acquire),
            self.current_drawdown.load(Ordering::Acquire).max(0) as u64,
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

    #[test]
    fn test_peak_is_monotonic() {
        let cb = RiskCircuitBreaker::new(0.10);
        assert!(!cb.check_drawdown(1000.0, 950.0));
        // Lower "peak" must not lower the recorded peak (drawdown still from 1000).
        assert!(!cb.check_drawdown(900.0, 930.0));
        let (_, _, dd) = cb.get_stats();
        // Drawdown measured from 1000.0: (1000 - 930)/1000 = 700 bps
        assert!(dd == 700, "drawdown={} bps, expected 700", dd);
    }
}
