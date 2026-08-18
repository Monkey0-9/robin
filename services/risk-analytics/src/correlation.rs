//! EWMA cross-asset correlation monitor.
//!
//! Implements the classic RiskMetrics variance-covariance estimator using
//! exponentially weighted moving averages on log-returns of last-trade
//! prices. Unlike a rolling-window Pearson implementation it needs no
//! per-window history buffer — each update only requires the previous
//! EWMA state and the latest log-return, which makes it O(1) per pair and
//! ideally suited for a periodically invoked monitor inside a hot gate.
//!
//! Arithmetic: for log-return series r_a, r_b we maintain zero-mean EWMA
//! quantities
//!     var_a  <- μ·var_a + (1-μ)·r_a²
//!     var_b  <- μ·var_b + (1-μ)·r_b²
//!     cov_ab <- μ·cov_ab + (1-μ)·r_a·r_b
//! and report correlation = cov_ab / sqrt(var_a·var_b), clamped to [-1,1].
//! Zero-mean EWMA is the standard variance-covariance (RiskMetrics) form;
//! the mean is volatility-mean enough for correlation screening since a
//! constant-drift contamination only appears as a second-order bias.
//!
//! Three decaying horizons are tracked per pair (5 minute, 1 hour, 1 day)
//! so concentration / VaR / stress logic can discriminate between short-lived
//! correlations (flash moves) and structural co-movement.

use std::collections::HashMap;

/// Pair key storage. Pairs are canonicalised so (a,b) == (b,a).
#[inline]
fn pair_key(a: u32, b: u32) -> (u32, u32) {
    if a < b {
        (a, b)
    } else {
        (b, a)
    }
}

/// Decay factors per horizon. lambda is the smoothing constant applied to
/// the previous state; (1 - lambda) weights the newest sample.
const LAMBDA_5M: f64 = 0.60;
const LAMBDA_1H: f64 = 0.90;
const LAMBDA_1D: f64 = 0.99;

/// Upper bound on the number of tracked pairs. Prevents unbounded growth when
/// the universe of instrument ids is sparse/skewed. On overflow the
/// least-recently-updated pair is evicted.
const MAX_TRACKED_PAIRS: usize = 4096;

/// Tunable decay parameters (kept public so ops can calibrate per market).
#[derive(Clone, Copy)]
pub struct EwmaParams {
    pub lambda_5m: f64,
    pub lambda_1h: f64,
    pub lambda_1d: f64,
}

impl Default for EwmaParams {
    fn default() -> Self {
        Self {
            lambda_5m: LAMBDA_5M,
            lambda_1h: LAMBDA_1H,
            lambda_1d: LAMBDA_1D,
        }
    }
}

/// EWMA accumulator for a single horizon over a single pair.
#[derive(Clone, Copy, Debug, Default)]
struct EwmaHorizon {
    var_a: f64,
    var_b: f64,
    cov: f64,
}

#[derive(Clone, Copy, Debug)]
struct PairState {
    a: u32,
    b: u32,
    h5m: EwmaHorizon,
    h1h: EwmaHorizon,
    h1d: EwmaHorizon,
    /// ns of last update — used for staleness reporting and eviction.
    updated_at_ns: u64,
}

/// Snapshot of the correlation for a single pair.
#[derive(Clone, Copy, Debug)]
pub struct CorrelationSnapshot {
    pub instrument_a: u32,
    pub instrument_b: u32,
    pub correlation_5m: f64,
    pub correlation_1h: f64,
    pub correlation_1d: f64,
    pub updated_at_ns: u64,
}

pub struct EwmaCorrelationMonitor {
    params: EwmaParams,
    /// Most recent log-return per instrument, used as the cross term when a
    /// pairing partner ticks.
    last_logreturn: HashMap<u32, f64>,
    last_price: HashMap<u32, f64>,
    pairs: HashMap<(u32, u32), PairState>,
}

impl EwmaCorrelationMonitor {
    pub fn new() -> Self {
        Self::with_params(EwmaParams::default())
    }

    pub fn with_params(params: EwmaParams) -> Self {
        Self {
            params,
            last_logreturn: HashMap::new(),
            last_price: HashMap::new(),
            pairs: HashMap::with_capacity(MAX_TRACKED_PAIRS),
        }
    }

    /// Feed a new last-trade price for an instrument. `now_ns` is the ingest
    /// timestamp so the monitor stays independent of the caller's clock.
    pub fn update(&mut self, instrument_id: u32, price: f64, now_ns: u64) {
        if !price.is_finite() || price <= 0.0 {
            return;
        }
        match self.last_price.get(&instrument_id) {
            Some(prev) if *prev > 0.0 => {
                let r = (price / *prev).ln();
                if r.is_finite() {
                    self.observe_pair_sweep(instrument_id, r, now_ns);
                }
                self.last_logreturn.insert(instrument_id, r);
            }
            _ => {
                // First observation warms the last price; no return yet.
                self.last_logreturn.insert(instrument_id, 0.0);
            }
        };
        self.last_price.insert(instrument_id, price);
    }

    /// Honest-extra sweep: once an instrument reports a new return, update the
    /// EWMA state of every pair (inst, other) we already have a return for.
    fn observe_pair_sweep(&mut self, instrument_id: u32, ret_inst: f64, now_ns: u64) {
        let lam5 = self.params.lambda_5m;
        let lam1 = self.params.lambda_1h;
        let lamd = self.params.lambda_1d;

        let others: Vec<u32> = self.last_logreturn.keys().copied().collect();
        for other in others {
            if other == instrument_id {
                continue;
            }
            let ret_other = match self.last_logreturn.get(&other) {
                Some(r) => *r,
                None => continue,
            };
            if ret_other == 0.0 {
                // Partner not warmed up yet: a single sample carries no
                // covariance information.
                continue;
            }

            let key = pair_key(instrument_id, other);
            if !self.pairs.contains_key(&key) {
                self.pairs.insert(
                    key,
                    PairState {
                        a: key.0,
                        b: key.1,
                        h5m: EwmaHorizon::default(),
                        h1h: EwmaHorizon::default(),
                        h1d: EwmaHorizon::default(),
                        updated_at_ns: now_ns,
                    },
                );
            }

            if let Some(p) = self.pairs.get_mut(&key) {
                update_horizon(&mut p.h5m, ret_inst, ret_other, lam5);
                update_horizon(&mut p.h1h, ret_inst, ret_other, lam1);
                update_horizon(&mut p.h1d, ret_inst, ret_other, lamd);
                p.updated_at_ns = now_ns;
            }

            // Bound the table: evict the least-recently-written pair.
            while self.pairs.len() > MAX_TRACKED_PAIRS {
                let victim = self
                    .pairs
                    .iter()
                    .min_by(|x, y| x.1.updated_at_ns.cmp(&y.1.updated_at_ns))
                    .map(|(k, _)| *k);
                match victim {
                    Some(v) => {
                        self.pairs.remove(&v);
                    }
                    None => break,
                }
            }
        }
    }

    /// Retrieve the EWMA correlation between two instruments. Returns None
    /// when the pair is not (yet) tracked — the caller may choose a
    /// conservative default (e.g. treat as maximally correlated).
    pub fn correlation(&self, a: u32, b: u32) -> Option<CorrelationSnapshot> {
        if a == b {
            return Some(CorrelationSnapshot {
                instrument_a: a,
                instrument_b: b,
                correlation_5m: 1.0,
                correlation_1h: 1.0,
                correlation_1d: 1.0,
                updated_at_ns: 0,
            });
        }
        let key = pair_key(a, b);
        let p = self.pairs.get(&key)?;
        Some(CorrelationSnapshot {
            instrument_a: p.a,
            instrument_b: p.b,
            correlation_5m: corr_coeff(p.h5m),
            correlation_1h: corr_coeff(p.h1h),
            correlation_1d: corr_coeff(p.h1d),
            updated_at_ns: p.updated_at_ns,
        })
    }

    /// Number of tracked pairs (mainly for observability).
    pub fn tracked_pairs(&self) -> usize {
        self.pairs.len()
    }

    /// Staleness helper for operators: the most recent pair update in ns.
    pub fn latest_update_ns(&self) -> u64 {
        self.pairs
            .values()
            .map(|p| p.updated_at_ns)
            .max()
            .unwrap_or(0)
    }
}

/// One EWMA step for a single horizon:
///
/// ```text
/// var_a  <- μ·var_a + (1-μ)·r_a²
/// var_b  <- μ·var_b + (1-μ)·r_b²
/// cov_ab <- μ·cov_ab + (1-μ)·r_a·r_b
/// ```
#[inline]
fn update_horizon(h: &mut EwmaHorizon, ra: f64, rb: f64, mu: f64) {
    let one_mu = 1.0 - mu;
    h.var_a = mu * h.var_a + one_mu * ra * ra;
    h.var_b = mu * h.var_b + one_mu * rb * rb;
    h.cov = mu * h.cov + one_mu * ra * rb;
}

#[inline]
fn corr_coeff(h: EwmaHorizon) -> f64 {
    let denom = (h.var_a * h.var_b).sqrt();
    if denom <= f64::EPSILON {
        return 0.0;
    }
    (h.cov / denom).clamp(-1.0, 1.0)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn perfect_positive_correlation() {
        let mut m = EwmaCorrelationMonitor::new();
        let mut px_a = 100.0;
        let mut px_b = 200.0;
        for i in 0..200u64 {
            px_a *= 1.0 + 0.001;
            px_b *= 1.0 + 0.001;
            m.update(1, px_a, i + 1);
            m.update(2, px_b, i + 1);
        }
        let c = m.correlation(1, 2).expect("pair tracked");
        assert!(c.correlation_1h > 0.95, "got {:.4}", c.correlation_1h);
        assert!(c.correlation_1d > 0.95, "got {:.4}", c.correlation_1d);
    }

    #[test]
    fn perfect_negative_correlation() {
        let mut m = EwmaCorrelationMonitor::new();
        let mut px_a = 100.0;
        let mut px_b = 200.0;
        for i in 0..200u64 {
            px_a *= 1.0 + 0.001;
            px_b *= 1.0 - 0.001;
            m.update(1, px_a, i + 1);
            m.update(2, px_b, i + 1);
        }
        let c = m.correlation(1, 2).expect("pair tracked");
        assert!(c.correlation_1h < -0.95, "corr {:.4}", c.correlation_1h);
    }

    #[test]
    fn uncorrelated_returns_sit_near_zero() {
        let mut m = EwmaCorrelationMonitor::new();
        let mut px_a = 100.0_f64;
        #[allow(unused_assignments)]
        let mut px_b = 200.0_f64;
        // b moves with a 2π/4.phase offset -> E[cos rate] ~ 0.
        let mut phase: f64 = 0.0;
        for i in 0..400u64 {
            px_a *= 1.0 + 0.002 * (i as f64 * 0.37).sin();
            px_b = 200.0 + 4.0 * (phase).sin();
            phase += 0.79;
            m.update(1, px_a, i + 1);
            m.update(2, px_b, i + 1);
        }
        let c = m.correlation(1, 2).expect("pair tracked");
        assert!(
            c.correlation_1h.abs() < 0.35,
            "corr {:.3}",
            c.correlation_1h
        );
    }

    #[test]
    fn unknown_pair_is_none() {
        let m = EwmaCorrelationMonitor::new();
        assert!(m.correlation(7, 9).is_none());
        assert_eq!(m.tracked_pairs(), 0);
    }

    #[test]
    fn same_instrument_is_one() {
        let m = EwmaCorrelationMonitor::new();
        let c = m.correlation(4, 4).expect("self pair");
        assert_eq!(c.correlation_5m, 1.0);
        assert_eq!(c.correlation_1h, 1.0);
        assert_eq!(c.correlation_1d, 1.0);
    }

    #[test]
    fn bounded_memory() {
        let mut m = EwmaCorrelationMonitor::new();
        #[allow(unused_assignments)]
        let mut px = 100.0;
        for i in 0..200u32 {
            let step = i as u64;
            px = 100.0 + step as f64 * 0.1;
            m.update(i, px, step + 1);
            // Alternate pairing tick to populate the sweep table.
            if i > 0 {
                m.update(i - 1, px * 1.001, step + 2);
            }
        }
        assert!(m.tracked_pairs() <= MAX_TRACKED_PAIRS);
    }
}
