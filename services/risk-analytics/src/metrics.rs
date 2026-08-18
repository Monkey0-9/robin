// ============================================================================
// Robin Risk Analytics — Prometheus Metrics Exporter
// ============================================================================
// Exposes key risk gate metrics in Prometheus text exposition format.
// The metrics are served via a simple HTTP endpoint on PORT_RISK_METRICS.
//
// To scrape with Prometheus, add:
//   - job_name: 'robin-risk'
//     static_configs:
//       - targets: ['<host>:9096']
// ============================================================================

use std::sync::atomic::{AtomicU64, Ordering};

/// All risk gate Prometheus metrics, stored atomically.
pub static ORDERS_PROCESSED: AtomicU64 = AtomicU64::new(0);
pub static ORDERS_REJECTED: AtomicU64 = AtomicU64::new(0);
pub static LATENCY_SUM_NS: AtomicU64 = AtomicU64::new(0);
pub static LATENCY_MAX_NS: AtomicU64 = AtomicU64::new(0);
pub static LATENCY_COUNT: AtomicU64 = AtomicU64::new(0);
pub static KILL_SWITCH_TRIPS: AtomicU64 = AtomicU64::new(0);
pub static CIRCUIT_BREAKER_TRIPS: AtomicU64 = AtomicU64::new(0);
pub static DUPLICATE_REJECTIONS: AtomicU64 = AtomicU64::new(0);
pub static VELOCITY_REJECTIONS: AtomicU64 = AtomicU64::new(0);
pub static POSITION_REJECTIONS: AtomicU64 = AtomicU64::new(0);
pub static CREDIT_REJECTIONS: AtomicU64 = AtomicU64::new(0);
pub static PRICE_COLLAR_REJECTIONS: AtomicU64 = AtomicU64::new(0);
pub static CONCENTRATION_REJECTIONS: AtomicU64 = AtomicU64::new(0);
pub static CORRELATION_REJECTIONS: AtomicU64 = AtomicU64::new(0);
pub static STRESS_MARGIN_REJECTIONS: AtomicU64 = AtomicU64::new(0);

// Analytics gauges (f64 stored as raw bits; decode with f64::from_bits).
pub static PORTFOLIO_VALUE: AtomicU64 = AtomicU64::new(0);
pub static VAR_95: AtomicU64 = AtomicU64::new(0);
pub static VAR_99: AtomicU64 = AtomicU64::new(0);
pub static CVAR_95: AtomicU64 = AtomicU64::new(0);
pub static STRESS_MAX_LOSS: AtomicU64 = AtomicU64::new(0);
pub static SHARPE_RATIO: AtomicU64 = AtomicU64::new(0);
pub static POSITIONS_TRACKED: AtomicU64 = AtomicU64::new(0);

// Latency histogram buckets (cumulative)
pub static LATENCY_LE_100: AtomicU64 = AtomicU64::new(0);
pub static LATENCY_LE_500: AtomicU64 = AtomicU64::new(0);
pub static LATENCY_LE_1000: AtomicU64 = AtomicU64::new(0);
pub static LATENCY_LE_5000: AtomicU64 = AtomicU64::new(0);
pub static LATENCY_LE_10000: AtomicU64 = AtomicU64::new(0);

/// Flush thread-local counters to globals. Called periodically.
pub fn flush() {
    // No-op: using direct atomics now
}

/// Record a processed order with its gate latency.
#[inline]
pub fn record_order(latency_ns: u64, approved: bool) {
    ORDERS_PROCESSED.fetch_add(1, Ordering::Relaxed);
    if !approved {
        ORDERS_REJECTED.fetch_add(1, Ordering::Relaxed);
    }
    LATENCY_SUM_NS.fetch_add(latency_ns, Ordering::Relaxed);
    LATENCY_COUNT.fetch_add(1, Ordering::Relaxed);

    if latency_ns <= 100 {
        LATENCY_LE_100.fetch_add(1, Ordering::Relaxed);
    }
    if latency_ns <= 500 {
        LATENCY_LE_500.fetch_add(1, Ordering::Relaxed);
    }
    if latency_ns <= 1000 {
        LATENCY_LE_1000.fetch_add(1, Ordering::Relaxed);
    }
    if latency_ns <= 5000 {
        LATENCY_LE_5000.fetch_add(1, Ordering::Relaxed);
    }
    if latency_ns <= 10000 {
        LATENCY_LE_10000.fetch_add(1, Ordering::Relaxed);
    }

    // Update max latency (global CAS — rare, acceptable for observability)
    let mut cur = LATENCY_MAX_NS.load(Ordering::Relaxed);
    while latency_ns > cur {
        match LATENCY_MAX_NS.compare_exchange_weak(
            cur,
            latency_ns,
            Ordering::Relaxed,
            Ordering::Relaxed,
        ) {
            Ok(_) => break,
            Err(v) => cur = v,
        }
    }
}

/// Record a rejection categorized by risk reason. This keeps
/// `robin_risk_rejections_by_type` accurate per block type.
#[inline]
pub fn record_rejection(reason: &str) {
    match reason {
        "kill_switch" => KILL_SWITCH_TRIPS.fetch_add(1, Ordering::Relaxed),
        "circuit_breaker" => CIRCUIT_BREAKER_TRIPS.fetch_add(1, Ordering::Relaxed),
        "duplicate" => DUPLICATE_REJECTIONS.fetch_add(1, Ordering::Relaxed),
        "velocity" => VELOCITY_REJECTIONS.fetch_add(1, Ordering::Relaxed),
        "position" => POSITION_REJECTIONS.fetch_add(1, Ordering::Relaxed),
        "credit" => CREDIT_REJECTIONS.fetch_add(1, Ordering::Relaxed),
        "price_collar" => PRICE_COLLAR_REJECTIONS.fetch_add(1, Ordering::Relaxed),
        "concentration" => CONCENTRATION_REJECTIONS.fetch_add(1, Ordering::Relaxed),
        "correlation" => CORRELATION_REJECTIONS.fetch_add(1, Ordering::Relaxed),
        "stress_margin" => STRESS_MARGIN_REJECTIONS.fetch_add(1, Ordering::Relaxed),
        _ => ORDERS_REJECTED.fetch_add(1, Ordering::Relaxed),
    };
}

/// Publish the periodic portfolio analytics snapshot. f64 values are stored
/// as their raw bits and decoded at render time.
pub fn record_analytics(
    portfolio_value: f64,
    var_95: f64,
    var_99: f64,
    cvar_95: f64,
    stress_max_loss: f64,
    sharpe: f64,
    positions_tracked: u64,
) {
    PORTFOLIO_VALUE.store(portfolio_value.to_bits(), Ordering::Relaxed);
    VAR_95.store(var_95.to_bits(), Ordering::Relaxed);
    VAR_99.store(var_99.to_bits(), Ordering::Relaxed);
    CVAR_95.store(cvar_95.to_bits(), Ordering::Relaxed);
    STRESS_MAX_LOSS.store(stress_max_loss.to_bits(), Ordering::Relaxed);
    SHARPE_RATIO.store(sharpe.to_bits(), Ordering::Relaxed);
    POSITIONS_TRACKED.store(positions_tracked, Ordering::Relaxed);
}

/// Render all metrics in Prometheus text exposition format.
/// Returns an owned String suitable for HTTP response body.
pub fn render_text() -> String {
    let processed = ORDERS_PROCESSED.load(Ordering::Relaxed);
    let rejected = ORDERS_REJECTED.load(Ordering::Relaxed);
    let lat_sum = LATENCY_SUM_NS.load(Ordering::Relaxed);
    let lat_cnt = LATENCY_COUNT.load(Ordering::Relaxed);
    let lat_max = LATENCY_MAX_NS.load(Ordering::Relaxed);
    let lat_avg = lat_sum.checked_div(lat_cnt).unwrap_or(0);
    let ks_trips = KILL_SWITCH_TRIPS.load(Ordering::Relaxed);
    let cb_trips = CIRCUIT_BREAKER_TRIPS.load(Ordering::Relaxed);
    let dup_rej = DUPLICATE_REJECTIONS.load(Ordering::Relaxed);
    let vel_rej = VELOCITY_REJECTIONS.load(Ordering::Relaxed);
    let pos_rej = POSITION_REJECTIONS.load(Ordering::Relaxed);
    let cred_rej = CREDIT_REJECTIONS.load(Ordering::Relaxed);
    let pc_rej = PRICE_COLLAR_REJECTIONS.load(Ordering::Relaxed);
    let conc_rej = CONCENTRATION_REJECTIONS.load(Ordering::Relaxed);
    let corr_rej = CORRELATION_REJECTIONS.load(Ordering::Relaxed);
    let stress_rej = STRESS_MARGIN_REJECTIONS.load(Ordering::Relaxed);

    let lat_100 = LATENCY_LE_100.load(Ordering::Relaxed);
    let lat_500 = LATENCY_LE_500.load(Ordering::Relaxed);
    let lat_1000 = LATENCY_LE_1000.load(Ordering::Relaxed);
    let lat_5000 = LATENCY_LE_5000.load(Ordering::Relaxed);
    let lat_10000 = LATENCY_LE_10000.load(Ordering::Relaxed);

    let pv = f64::from_bits(PORTFOLIO_VALUE.load(Ordering::Relaxed));
    let var95 = f64::from_bits(VAR_95.load(Ordering::Relaxed));
    let var99 = f64::from_bits(VAR_99.load(Ordering::Relaxed));
    let cvar95 = f64::from_bits(CVAR_95.load(Ordering::Relaxed));
    let stress = f64::from_bits(STRESS_MAX_LOSS.load(Ordering::Relaxed));
    let sharpe = f64::from_bits(SHARPE_RATIO.load(Ordering::Relaxed));
    let pos_tracked = POSITIONS_TRACKED.load(Ordering::Relaxed);

    format!(
        "# HELP robin_risk_orders_processed_total Total orders through the risk gate\n\
         # TYPE robin_risk_orders_processed_total counter\n\
         robin_risk_orders_processed_total {processed}\n\
         # HELP robin_risk_orders_rejected_total Total orders rejected by the risk gate\n\
         # TYPE robin_risk_orders_rejected_total counter\n\
         robin_risk_orders_rejected_total {rejected}\n\
         # HELP robin_risk_gate_latency_ns Gate latency histogram in nanoseconds\n\
         # TYPE robin_risk_gate_latency_ns histogram\n\
         robin_risk_gate_latency_ns_bucket{{le=\"100\"}} {lat_100}\n\
         robin_risk_gate_latency_ns_bucket{{le=\"500\"}} {lat_500}\n\
         robin_risk_gate_latency_ns_bucket{{le=\"1000\"}} {lat_1000}\n\
         robin_risk_gate_latency_ns_bucket{{le=\"5000\"}} {lat_5000}\n\
         robin_risk_gate_latency_ns_bucket{{le=\"10000\"}} {lat_10000}\n\
         robin_risk_gate_latency_ns_bucket{{le=\"+Inf\"}} {lat_cnt}\n\
         robin_risk_gate_latency_ns_sum {lat_sum}\n\
         robin_risk_gate_latency_ns_count {lat_cnt}\n\
         # HELP robin_risk_gate_latency_ns_avg Average gate latency in nanoseconds\n\
         # TYPE robin_risk_gate_latency_ns_avg gauge\n\
         robin_risk_gate_latency_ns_avg {lat_avg}\n\
         # HELP robin_risk_gate_latency_ns_max Maximum gate latency in nanoseconds\n\
         # TYPE robin_risk_gate_latency_ns_max gauge\n\
         robin_risk_gate_latency_ns_max {lat_max}\n\
         # HELP robin_risk_kill_switch_trips_total Kill switch activation count\n\
         # TYPE robin_risk_kill_switch_trips_total counter\n\
         robin_risk_kill_switch_trips_total {ks_trips}\n\
         # HELP robin_risk_circuit_breaker_trips_total Circuit breaker trip count\n\
         # TYPE robin_risk_circuit_breaker_trips_total counter\n\
         robin_risk_circuit_breaker_trips_total {cb_trips}\n\
         # HELP robin_risk_rejections_by_type Rejections broken down by reason\n\
         # TYPE robin_risk_rejections_by_type counter\n\
         robin_risk_rejections_by_type{{reason=\"duplicate\"}} {dup_rej}\n\
         robin_risk_rejections_by_type{{reason=\"velocity\"}} {vel_rej}\n\
         robin_risk_rejections_by_type{{reason=\"position\"}} {pos_rej}\n\
         robin_risk_rejections_by_type{{reason=\"credit\"}} {cred_rej}\n\
         robin_risk_rejections_by_type{{reason=\"price_collar\"}} {pc_rej}\n\
         robin_risk_rejections_by_type{{reason=\"concentration\"}} {conc_rej}\n\
         robin_risk_rejections_by_type{{reason=\"correlation\"}} {corr_rej}\n\
         robin_risk_rejections_by_type{{reason=\"stress_margin\"}} {stress_rej}\n\
         # HELP robin_risk_portfolio_value Current marked portfolio value (USD)\n\
         # TYPE robin_risk_portfolio_value gauge\n\
         robin_risk_portfolio_value {pv}\n\
         # HELP robin_risk_var_95 1-day Value-at-Risk at 95% confidence (USD)\n\
         # TYPE robin_risk_var_95 gauge\n\
         robin_risk_var_95 {var95}\n\
         # HELP robin_risk_var_99 1-day Value-at-Risk at 99% confidence (USD)\n\
         # TYPE robin_risk_var_99 gauge\n\
         robin_risk_var_99 {var99}\n\
         # HELP robin_risk_cvar_95 1-day Conditional VaR at 95% confidence (USD)\n\
         # TYPE robin_risk_cvar_95 gauge\n\
         robin_risk_cvar_95 {cvar95}\n\
         # HELP robin_risk_stress_max_loss Worst historical shock scenario loss (USD)\n\
         # TYPE robin_risk_stress_max_loss gauge\n\
         robin_risk_stress_max_loss {stress}\n\
         # HELP robin_risk_sharpe_ratio Annualized Sharpe ratio of realized returns\n\
         # TYPE robin_risk_sharpe_ratio gauge\n\
         robin_risk_sharpe_ratio {sharpe}\n\
         # HELP robin_risk_positions_tracked Number of open instrument positions\n\
         # TYPE robin_risk_positions_tracked gauge\n\
         robin_risk_positions_tracked {pos_tracked}\n"
    )
}

/// Serve metrics over a simple HTTP/1.0 listener on the given port.
/// This is a blocking call — run in a background thread.
/// On Windows the same listener code applies; we keep the body generic.
#[cfg(target_family = "unix")]
pub fn serve_metrics(port: u16) {
    serve_metrics_impl(port);
}

#[cfg(not(target_family = "unix"))]
pub fn serve_metrics(port: u16) {
    serve_metrics_impl(port);
}

fn serve_metrics_impl(port: u16) {
    use std::io::{Read, Write};
    use std::net::TcpListener;

    let listener = match TcpListener::bind(format!("0.0.0.0:{port}")) {
        Ok(l) => l,
        Err(e) => {
            eprintln!("[METRICS] Failed to bind port {port}: {e}");
            return;
        }
    };
    eprintln!("[METRICS] Serving Prometheus metrics on :{port}/metrics");

    for mut s in listener.incoming().flatten() {
        let mut buf = [0u8; 256];
        let _ = s.read(&mut buf);
        let body = render_text();
        let response = format!(
            "HTTP/1.0 200 OK\r\nContent-Type: text/plain; version=0.0.4\r\n\
             Content-Length: {}\r\n\r\n{}",
            body.len(),
            body
        );
        let _ = s.write_all(response.as_bytes());
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_render_text_contains_metric_names() {
        let text = render_text();
        assert!(text.contains("robin_risk_orders_processed_total"));
        assert!(text.contains("robin_risk_orders_rejected_total"));
        assert!(text.contains("robin_risk_gate_latency_ns_avg"));
        assert!(text.contains("robin_risk_rejections_by_type"));
        assert!(text.contains("robin_risk_portfolio_value"));
        assert!(text.contains("robin_risk_var_95"));
        assert!(text.contains("robin_risk_var_99"));
        assert!(text.contains("robin_risk_cvar_95"));
        assert!(text.contains("robin_risk_stress_max_loss"));
        assert!(text.contains("robin_risk_sharpe_ratio"));
        assert!(text.contains("robin_risk_positions_tracked"));
    }

    #[test]
    fn test_record_rejection_reasons() {
        DUPLICATE_REJECTIONS.store(0, Ordering::Relaxed);
        VELOCITY_REJECTIONS.store(0, Ordering::Relaxed);
        PRICE_COLLAR_REJECTIONS.store(0, Ordering::Relaxed);
        record_rejection("duplicate");
        record_rejection("velocity");
        record_rejection("price_collar");
        record_rejection("unknown");
        assert_eq!(DUPLICATE_REJECTIONS.load(Ordering::Relaxed), 1);
        assert_eq!(VELOCITY_REJECTIONS.load(Ordering::Relaxed), 1);
        assert_eq!(PRICE_COLLAR_REJECTIONS.load(Ordering::Relaxed), 1);
    }

    #[test]
    fn test_record_analytics_round_trip() {
        record_analytics(
            1_000_000.5,
            25_000.25,
            40_000.75,
            30_000.0,
            -200_000.0,
            1.25,
            7,
        );
        let text = render_text();
        assert!(text.contains("robin_risk_portfolio_value 1000000.5"));
        assert!(text.contains("robin_risk_var_95 25000.25"));
        assert!(text.contains("robin_risk_sharpe_ratio 1.25"));
        assert!(text.contains("robin_risk_positions_tracked 7"));
    }

    #[test]
    fn test_record_order_updates_counters() {
        // Reset to a known state
        ORDERS_PROCESSED.store(0, Ordering::Relaxed);
        ORDERS_REJECTED.store(0, Ordering::Relaxed);
        LATENCY_LE_1000.store(0, Ordering::Relaxed);
        LATENCY_LE_5000.store(0, Ordering::Relaxed);

        record_order(1500, true);
        record_order(2500, false);
        record_order(800, true);
        flush(); // Flush thread-local counters to globals

        assert_eq!(ORDERS_PROCESSED.load(Ordering::Relaxed), 3);
        assert_eq!(ORDERS_REJECTED.load(Ordering::Relaxed), 1);
        assert_eq!(LATENCY_MAX_NS.load(Ordering::Relaxed), 2500);
        assert_eq!(LATENCY_LE_1000.load(Ordering::Relaxed), 1);
        assert_eq!(LATENCY_LE_5000.load(Ordering::Relaxed), 3);
    }
}
