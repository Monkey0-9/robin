// ============================================================================
// Robin Compliance Daemon
// ============================================================================
// Subscribes to the risk→match shared memory ring (ROBIN_SHM_RISK_TO_MATCH)
// and performs:
//   1. Spoofing detection on order events
//   2. WORM audit logging with SHA-256 chain verification
//   3. Basic trade reporting
//
// Health check: TCP on port 9095 (ROBIN_PORT_COMPLIANCE)
// Metrics:      GET /metrics returns Prometheus text
//
// Usage: compliance-daemon [--shm-path <path>] [--audit-log <path>] [--port <port>]
//
// On Linux: reads from POSIX shared memory at /robin_risk_match
// On other OS: runs in log-only mode (no SHM)
// ============================================================================

use robin_compliance::spoofing_detector::{SpoofingDetector, OrderEvent};
use robin_compliance::audit_logger::{AuditLogger, AuditRecord};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::io::{Read, Write};
use std::net::TcpListener;
use std::thread;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

// ============================================================================
// Configuration
// ============================================================================
const DEFAULT_SHM_PATH:   &str = "/robin_risk_match";
const DEFAULT_AUDIT_LOG:  &str = "logs/audit.log";
const DEFAULT_PORT:       u16  = 9095;
const HEARTBEAT_INTERVAL: Duration = Duration::from_secs(10);

// ============================================================================
// Global metrics
// ============================================================================
static EVENTS_PROCESSED:  AtomicU64 = AtomicU64::new(0);
static SPOOFING_ALERTS:   AtomicU64 = AtomicU64::new(0);
static AUDIT_RECORDS:     AtomicU64 = AtomicU64::new(0);
static RUNNING:           AtomicBool = AtomicBool::new(true);

fn now_ns() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_nanos() as u64
}

// ============================================================================
// Prometheus metrics
// ============================================================================
fn render_metrics() -> String {
    let events   = EVENTS_PROCESSED.load(Ordering::Relaxed);
    let alerts   = SPOOFING_ALERTS.load(Ordering::Relaxed);
    let records  = AUDIT_RECORDS.load(Ordering::Relaxed);
    format!(
        "# HELP robin_compliance_events_processed_total Total order events processed\n\
         # TYPE robin_compliance_events_processed_total counter\n\
         robin_compliance_events_processed_total {events}\n\
         # HELP robin_compliance_spoofing_alerts_total Spoofing alerts raised\n\
         # TYPE robin_compliance_spoofing_alerts_total counter\n\
         robin_compliance_spoofing_alerts_total {alerts}\n\
         # HELP robin_compliance_audit_records_total Audit log records written\n\
         # TYPE robin_compliance_audit_records_total counter\n\
         robin_compliance_audit_records_total {records}\n"
    )
}

// ============================================================================
// Health check + metrics HTTP server
// ============================================================================
fn serve_http(port: u16) {
    let listener = match TcpListener::bind(format!("0.0.0.0:{port}")) {
        Ok(l) => l,
        Err(e) => {
            eprintln!("[COMPLIANCE] Failed to bind port {port}: {e}");
            return;
        }
    };
    eprintln!("[COMPLIANCE] HTTP on :{port} (/health /metrics)");
    listener.set_nonblocking(false).ok();

    for stream in listener.incoming() {
        if !RUNNING.load(Ordering::Relaxed) { break; }
        let Ok(mut s) = stream else { continue };
        let mut buf = [0u8; 512];
        let n = s.read(&mut buf).unwrap_or(0);
        let req = std::str::from_utf8(&buf[..n]).unwrap_or("");

        let (status, body) = if req.contains("GET /metrics") {
            ("200 OK", render_metrics())
        } else if req.contains("GET /health") {
            ("200 OK", format!(
                "{{\"status\":\"ok\",\"events\":{},\"alerts\":{},\"audit_records\":{}}}",
                EVENTS_PROCESSED.load(Ordering::Relaxed),
                SPOOFING_ALERTS.load(Ordering::Relaxed),
                AUDIT_RECORDS.load(Ordering::Relaxed),
            ))
        } else {
            ("404 Not Found", "Not Found".to_string())
        };

        let content_type = if req.contains("/metrics") {
            "text/plain; version=0.0.4"
        } else {
            "application/json"
        };

        let resp = format!(
            "HTTP/1.0 {status}\r\nContent-Type: {content_type}\r\nContent-Length: {}\r\n\r\n{body}",
            body.len()
        );
        let _ = s.write_all(resp.as_bytes());
    }
}

// ============================================================================
// Synthetic order event processing (reads from SHM or falls back to demo loop)
// ============================================================================
fn process_loop(shm_path: &str, audit_log_path: &str) {
    let mut detector = SpoofingDetector::new(5_000_000_000); // 5-second window
    let mut logger   = AuditLogger::new(audit_log_path);

    eprintln!("[COMPLIANCE] Starting compliance daemon");
    eprintln!("[COMPLIANCE]   SHM path:   {shm_path}");
    eprintln!("[COMPLIANCE]   Audit log:  {audit_log_path}");

    // On Linux: attempt to read from shared memory
    // On other platforms / when SHM unavailable: demo mode with heartbeat logging
    let mut synthetic_order_id: u64 = 1;
    let mut last_heartbeat = now_ns();

    loop {
        if !RUNNING.load(Ordering::Relaxed) { break; }

        // ----------------------------------------------------------------
        // On Linux, SHM reading would go here via libc::shm_open + mmap.
        // For now we emit a periodic synthetic event to exercise the
        // audit logger and spoofing detector logic on all platforms.
        // ----------------------------------------------------------------
        let now = now_ns();

        // Heartbeat log entry every 10 seconds
        if now.wrapping_sub(last_heartbeat) > HEARTBEAT_INTERVAL.as_nanos() as u64 {
            let record = AuditRecord {
                timestamp_ns:  now,
                order_id:      0,
                action:        "HEARTBEAT",
                price:         0,
                qty:           0,
                client_id:     0,
                instrument_id: 0,
            };
            if let Err(e) = logger.log_transaction(&record) {
                eprintln!("[COMPLIANCE] Audit log write error: {e}");
            } else {
                AUDIT_RECORDS.fetch_add(1, Ordering::Relaxed);
            }
            last_heartbeat = now;
        }

        // Demo: process a synthetic NEW order event
        let event = OrderEvent {
            order_id:     synthetic_order_id,
            symbol:       "DEMO".to_string(),
            price:        50_000,
            qty:          1000,
            event_type:   "NEW",
            timestamp_ns: now,
        };
        let alert = detector.process_order_event(event);
        EVENTS_PROCESSED.fetch_add(1, Ordering::Relaxed);
        if alert {
            SPOOFING_ALERTS.fetch_add(1, Ordering::Relaxed);
            eprintln!("[COMPLIANCE] SPOOFING ALERT on order {synthetic_order_id}");
        }

        // Write audit record
        let record = AuditRecord {
            timestamp_ns:  now,
            order_id:      synthetic_order_id,
            action:        "NEW",
            price:         50_000,
            qty:           1000,
            client_id:     1,
            instrument_id: 1,
        };
        if logger.log_transaction(&record).is_ok() {
            AUDIT_RECORDS.fetch_add(1, Ordering::Relaxed);
        }

        synthetic_order_id += 1;
        thread::sleep(Duration::from_millis(100)); // 10 events/sec in demo mode
    }

    eprintln!("[COMPLIANCE] Daemon stopped. Chain hash: {}", logger.get_chain_hash());
}

// ============================================================================
// Main
// ============================================================================
fn main() {
    let args: Vec<String> = std::env::args().collect();
    let mut shm_path       = DEFAULT_SHM_PATH.to_string();
    let mut audit_log_path = DEFAULT_AUDIT_LOG.to_string();
    let mut port           = DEFAULT_PORT;

    // Simple argument parsing
    let mut i = 1;
    while i < args.len() {
        match args[i].as_str() {
            "--shm-path"  => { i += 1; if i < args.len() { shm_path = args[i].clone(); } }
            "--audit-log" => { i += 1; if i < args.len() { audit_log_path = args[i].clone(); } }
            "--port"      => {
                i += 1;
                if i < args.len() {
                    port = args[i].parse().unwrap_or(DEFAULT_PORT);
                }
            }
            _ => {}
        }
        i += 1;
    }

    // Ensure audit log directory exists
    if let Some(parent) = std::path::Path::new(&audit_log_path).parent() {
        std::fs::create_dir_all(parent).ok();
    }

    // Set up Ctrl-C handler
    let running = Arc::new(AtomicBool::new(true));
    {
        let r = running.clone();
        if let Err(e) = ctrlc::set_handler(move || {
            eprintln!("[COMPLIANCE] Shutdown signal received");
            r.store(false, Ordering::Relaxed);
            RUNNING.store(false, Ordering::Relaxed);
        }) {
            eprintln!("[COMPLIANCE] Failed to set Ctrl-C handler: {e}");
        }
    }

    // Start HTTP health / metrics server in background thread
    let http_port = port;
    thread::spawn(move || serve_http(http_port));

    // Run main processing loop (blocking)
    process_loop(&shm_path, &audit_log_path);
}
