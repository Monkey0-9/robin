// services/risk-analytics/src/circuit_breaker.rs
// Real-time software circuit breaker monitor tracking drawdowns and daily losses.

use std::sync::atomic::{AtomicBool, Ordering};

pub struct RiskCircuitBreaker {
    tripped: AtomicBool,
    daily_drawdown_limit: f64,
    current_drawdown: f64,
}

impl RiskCircuitBreaker {
    pub fn new(daily_drawdown_limit: f64) -> Self {
        Self {
            tripped: AtomicBool::new(false),
            daily_drawdown_limit,
            current_drawdown: 0.0,
        }
    }

    #[inline(always)]
    pub fn check_drawdown(&mut self, peak_equity: f64, current_equity: f64) -> bool {
        if self.tripped.load(Ordering::Relaxed) {
            return true; // Already tripped
        }

        if peak_equity > 0.0 {
            self.current_drawdown = (peak_equity - current_equity) / peak_equity;
            if self.current_drawdown >= self.daily_drawdown_limit {
                self.trip("CRITICAL_DAILY_DRAWDOWN_BREACHED");
                return true;
            }
        }

        false
    }

    pub fn trip(&self, reason: &str) {
        self.tripped.store(true, Ordering::Release);
        println!("[CIRCUIT BREAKER TRIPPED] System halted. Reason: {}", reason);
    }

    pub fn reset(&self) {
        self.tripped.store(false, Ordering::Release);
        println!("[CIRCUIT BREAKER RESET] System trading operational.");
    }

    #[inline(always)]
    pub fn is_tripped(&self) -> bool {
        self.tripped.load(Ordering::Acquire)
    }
}
