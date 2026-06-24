// ============================================================================
// Robin Risk Analytics — Prometheus Metrics Exporter
// ============================================================================
// Exposes key risk gate metrics in Prometheus text exposition format.
// The metrics are served via a simple HTTP endpoint on PORT_RISK_HEALTH.
//
// To scrape with Prometheus, add:
//   - job_name: 'robin-risk'
//     static_configs:
//       - targets: ['<host>:9092']
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

/// Record a processed order with its gate latency.
#[inline]
pub fn record_order(latency_ns: u64, approved: bool) {
    ORDERS_PROCESSED.fetch_add(1, Ordering::Relaxed);
    if !approved {
        ORDERS_REJECTED.fetch_add(1, Ordering::Relaxed);
    }
    LATENCY_SUM_NS.fetch_add(latency_ns, Ordering::Relaxed);
    LATENCY_COUNT.fetch_add(1, Ordering::Relaxed);
    // Update max latency (non-atomic CAS loop — acceptable for observability)
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

    format!(
        "# HELP robin_risk_orders_processed_total Total orders through the risk gate\n\
         # TYPE robin_risk_orders_processed_total counter\n\
         robin_risk_orders_processed_total {processed}\n\
         # HELP robin_risk_orders_rejected_total Total orders rejected by the risk gate\n\
         # TYPE robin_risk_orders_rejected_total counter\n\
         robin_risk_orders_rejected_total {rejected}\n\
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
         robin_risk_rejections_by_type{{reason=\"credit\"}} {cred_rej}\n"
    )
}

/// Serve metrics over a simple HTTP/1.0 listener on the given port.
/// This is a blocking call — run in a background thread.
#[cfg(target_family = "unix")]
pub fn serve_metrics(port: u16) {
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

    for stream in listener.incoming() {
        match stream {
            Ok(mut s) => {
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
            Err(_) => {}
        }
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
    }

    #[test]
    fn test_record_order_updates_counters() {
        // Reset to a known state
        ORDERS_PROCESSED.store(0, Ordering::Relaxed);
        ORDERS_REJECTED.store(0, Ordering::Relaxed);
        record_order(1500, true);
        record_order(2500, false);
        assert_eq!(ORDERS_PROCESSED.load(Ordering::Relaxed), 2);
        assert_eq!(ORDERS_REJECTED.load(Ordering::Relaxed), 1);
        assert_eq!(LATENCY_MAX_NS.load(Ordering::Relaxed), 2500);
    }
}
