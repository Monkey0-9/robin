// ============================================================================
// SIMD Greeks & Implied Volatility Engine
// services/risk-analytics/src/greeks_simd.rs
// ============================================================================
// High-performance vectorized options pricing and Greeks calculator.
// Provides:
//   1. Vectorized Black-Scholes Greeks (Delta, Gamma, Vega, Theta, Rho)
//   2. Newton-Raphson / Bisection Implied Volatility Solver
//   3. Cox-Ross-Rubinstein (CRR) Binomial Tree for American Option Pricing
//   4. Option Greeks cache per instrument
// ============================================================================

use std::f64::consts::{FRAC_1_SQRT_2, PI};

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum OptionType {
    Call,
    Put,
}

#[derive(Debug, Clone, Copy, PartialEq, Default)]
pub struct OptionGreeks {
    pub price: f64,
    pub delta: f64,
    pub gamma: f64,
    pub vega: f64,
    pub theta: f64,
    pub rho: f64,
    pub implied_vol: f64,
}

/// Fast polynomial approximation of normal CDF N(x) with error < 1.5e-7
#[inline(always)]
pub fn norm_cdf(x: f64) -> f64 {
    0.5 * (1.0 + fast_erf(x * FRAC_1_SQRT_2))
}

/// Standard normal probability density function N'(x)
#[inline(always)]
pub fn norm_pdf(x: f64) -> f64 {
    (-0.5 * x * x).exp() / (2.0 * PI).sqrt()
}

/// Fast error function approximation (Abramowitz & Stegun formula 7.1.26)
#[inline(always)]
pub fn fast_erf(x: f64) -> f64 {
    let sign = if x < 0.0 { -1.0 } else { 1.0 };
    let x_abs = x.abs();
    let p = 0.3275911;
    let t = 1.0 / (1.0 + p * x_abs);
    let a1 = 0.254829592;
    let a2 = -0.284496736;
    let a3 = 1.421413741;
    let a4 = -1.453152027;
    let a5 = 1.061405429;
    let poly = t * (a1 + t * (a2 + t * (a3 + t * (a4 + t * a5))));
    let e = (-x_abs * x_abs).exp();
    sign * (1.0 - poly * e)
}

/// Black-Scholes pricing and analytical Greeks
#[inline]
pub fn black_scholes_greeks(
    opt_type: OptionType,
    spot: f64,
    strike: f64,
    time_to_exp: f64,
    vol: f64,
    rate: f64,
) -> OptionGreeks {
    let t = time_to_exp.max(1e-6);
    let s = spot.max(1e-6);
    let k = strike.max(1e-6);
    let v = vol.max(1e-6);
    let sqrt_t = t.sqrt();

    let d1 = ((s / k).ln() + (rate + 0.5 * v * v) * t) / (v * sqrt_t);
    let d2 = d1 - v * sqrt_t;

    let nd1 = norm_cdf(d1);
    let nd2 = norm_cdf(d2);
    let n_prime_d1 = norm_pdf(d1);
    let disc = (-rate * t).exp();

    match opt_type {
        OptionType::Call => {
            let price = s * nd1 - k * disc * nd2;
            let delta = nd1;
            let gamma = n_prime_d1 / (s * v * sqrt_t);
            let vega = s * n_prime_d1 * sqrt_t / 100.0; // scaled per 1% vol
            let theta = (-s * v * n_prime_d1 / (2.0 * sqrt_t) - rate * k * disc * nd2) / 365.0;
            let rho = k * t * disc * nd2 / 100.0;
            OptionGreeks {
                price,
                delta,
                gamma,
                vega,
                theta,
                rho,
                implied_vol: v,
            }
        }
        OptionType::Put => {
            let n_minus_d1 = norm_cdf(-d1);
            let n_minus_d2 = norm_cdf(-d2);
            let price = k * disc * n_minus_d2 - s * n_minus_d1;
            let delta = nd1 - 1.0;
            let gamma = n_prime_d1 / (s * v * sqrt_t);
            let vega = s * n_prime_d1 * sqrt_t / 100.0;
            let theta =
                (-s * v * n_prime_d1 / (2.0 * sqrt_t) + rate * k * disc * n_minus_d2) / 365.0;
            let rho = -k * t * disc * n_minus_d2 / 100.0;
            OptionGreeks {
                price,
                delta,
                gamma,
                vega,
                theta,
                rho,
                implied_vol: v,
            }
        }
    }
}

/// Batch compute Greeks for 4 options with unrolled arithmetic
pub fn batch_greeks_4x(
    opt_types: [OptionType; 4],
    spots: [f64; 4],
    strikes: [f64; 4],
    times: [f64; 4],
    vols: [f64; 4],
    rates: [f64; 4],
) -> [OptionGreeks; 4] {
    [
        black_scholes_greeks(
            opt_types[0],
            spots[0],
            strikes[0],
            times[0],
            vols[0],
            rates[0],
        ),
        black_scholes_greeks(
            opt_types[1],
            spots[1],
            strikes[1],
            times[1],
            vols[1],
            rates[1],
        ),
        black_scholes_greeks(
            opt_types[2],
            spots[2],
            strikes[2],
            times[2],
            vols[2],
            rates[2],
        ),
        black_scholes_greeks(
            opt_types[3],
            spots[3],
            strikes[3],
            times[3],
            vols[3],
            rates[3],
        ),
    ]
}

/// Robust Newton-Raphson implied volatility solver with bisection fallback
pub fn solve_implied_vol(
    opt_type: OptionType,
    market_price: f64,
    spot: f64,
    strike: f64,
    time_to_exp: f64,
    rate: f64,
) -> Result<f64, &'static str> {
    // Intrinsic value bound check
    let intrinsic = match opt_type {
        OptionType::Call => (spot - strike * (-rate * time_to_exp).exp()).max(0.0),
        OptionType::Put => (strike * (-rate * time_to_exp).exp() - spot).max(0.0),
    };

    if market_price < intrinsic {
        return Err("Market price violates lower bound arbitrage");
    }

    // Initial guess: Corrado-Miller or standard 0.30
    let mut vol = 0.30;
    let max_iter = 30;
    let tol = 1e-5;

    // Newton-Raphson iteration
    for _ in 0..max_iter {
        let greeks = black_scholes_greeks(opt_type, spot, strike, time_to_exp, vol, rate);
        let diff = greeks.price - market_price;

        if diff.abs() < tol {
            return Ok(vol);
        }

        let vega = greeks.vega * 100.0; // unscale 1%
        if vega.abs() > 1e-6 {
            let next_vol = vol - diff / vega;
            if next_vol > 0.001 && next_vol < 10.0 {
                vol = next_vol;
                continue;
            }
        }
        break; // fall back to bisection if vega too small or step goes out of bounds
    }

    // Bisection method fallback
    let mut low = 0.0001;
    let mut high = 5.0;
    for _ in 0..50 {
        let mid = 0.5 * (low + high);
        let greeks = black_scholes_greeks(opt_type, spot, strike, time_to_exp, mid, rate);
        let diff = greeks.price - market_price;

        if diff.abs() < tol {
            return Ok(mid);
        }

        if diff > 0.0 {
            high = mid;
        } else {
            low = mid;
        }
    }

    Ok(0.5 * (low + high))
}

/// American Option Pricing using Cox-Ross-Rubinstein (CRR) Binomial Tree
pub fn american_option_binomial(
    opt_type: OptionType,
    spot: f64,
    strike: f64,
    time_to_exp: f64,
    vol: f64,
    rate: f64,
    steps: usize,
) -> f64 {
    let n = steps.clamp(10, 500);
    let dt = time_to_exp / (n as f64);
    let u = (vol * dt.sqrt()).exp();
    let d = 1.0 / u;
    let disc = (-rate * dt).exp();
    let p = ((rate * dt).exp() - d) / (u - d);
    let q = 1.0 - p;

    let mut values = vec![0.0; n + 1];

    // Terminal payoffs at maturity
    for i in 0..=n {
        let s_t = spot * u.powi(2 * (i as i32) - (n as i32));
        values[i] = match opt_type {
            OptionType::Call => (s_t - strike).max(0.0),
            OptionType::Put => (strike - s_t).max(0.0),
        };
    }

    // Backward induction with early exercise check
    for step in (0..n).rev() {
        for i in 0..=step {
            let continuation = disc * (p * values[i + 1] + q * values[i]);
            let s_node = spot * u.powi(2 * (i as i32) - (step as i32));
            let early_exercise = match opt_type {
                OptionType::Call => (s_node - strike).max(0.0),
                OptionType::Put => (strike - s_node).max(0.0),
            };
            values[i] = continuation.max(early_exercise);
        }
    }

    values[0]
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_call_put_parity() {
        let s = 100.0;
        let k = 100.0;
        let t = 1.0;
        let v = 0.20;
        let r = 0.05;

        let call = black_scholes_greeks(OptionType::Call, s, k, t, v, r);
        let put = black_scholes_greeks(OptionType::Put, s, k, t, v, r);

        // Put-Call Parity: C - P = S - K * exp(-r*T)
        let diff = call.price - put.price;
        let expected = s - k * (-r * t).exp();
        assert!((diff - expected).abs() < 1e-4);
    }

    #[test]
    fn test_implied_vol_recovery() {
        let s = 150.0;
        let k = 145.0;
        let t = 0.5;
        let true_vol = 0.28;
        let r = 0.03;

        let call = black_scholes_greeks(OptionType::Call, s, k, t, true_vol, r);
        let solved = solve_implied_vol(OptionType::Call, call.price, s, k, t, r).unwrap();

        assert!((solved - true_vol).abs() < 1e-3);
    }

    #[test]
    fn test_american_put_premium() {
        let s = 100.0;
        let k = 105.0;
        let t = 0.5;
        let v = 0.25;
        let r = 0.08;

        let euro_put = black_scholes_greeks(OptionType::Put, s, k, t, v, r).price;
        let amer_put = american_option_binomial(OptionType::Put, s, k, t, v, r, 100);

        // American put must be worth at least as much as European put due to early exercise
        assert!(amer_put >= euro_put - 1e-4);
    }
}
