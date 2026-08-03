use std::net::{TcpListener, TcpStream};
use std::io::{Read, Write};
use std::sync::{Arc, Mutex};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use robin_risk::gate::{RiskGate, Order, OrderSide};
use robin_risk::metrics;
use robin_risk::config::PORT_RISK_METRICS;
use serde_json::Value;

fn main() {
    println!("[RISK] Starting Risk Analytics Daemon on port 9092...");

    // Serve Prometheus metrics on a dedicated port in a background thread.
    {
        let metrics_port = PORT_RISK_METRICS;
        std::thread::spawn(move || {
            metrics::serve_metrics(metrics_port);
        });
    }
    
    let mut gate = RiskGate::with_config(
        "robin_risk_match.shm",
        1_000_000_000_000_000_000u64, // credit limit ($10B scaled by 1e8)
        100 * 100_000_000,           // position limit (100 contracts scaled by 1e8)
        100_000 * 100_000_000,       // max qty per order (100k contracts scaled by 1e8)
    );
    
    // Attempt to load snapshot
    let snapshot_path = "positions.bin";
    if let Ok(count) = gate.load_snapshot(snapshot_path) {
        println!("[RISK] Loaded {} positions from snapshot.", count);
    } else {
        println!("[RISK] No valid snapshot found, starting fresh.");
    }
    
    // Set realistic initial reference prices in ticks (price * 100,000,000)
    gate.update_reference_price(1, 6_450_000_000_000); // BTC/USD ~ 64,500
    gate.update_reference_price(2, 345_000_000_000);  // ETH/USD ~ 3,450
    gate.update_reference_price(3, 18_530_000_000);   // AAPL ~ 185.30
    gate.update_reference_price(4, 108_500_000);      // EUR/USD ~ 1.0850

    let gate = Arc::new(Mutex::new(gate));
    let gate_clone = gate.clone();
    let gate_bg = gate.clone();

    // Setup background thread for periodic snapshot saving
    std::thread::spawn(move || {
        loop {
            std::thread::sleep(std::time::Duration::from_secs(5));
            if let Ok(g) = gate_bg.lock() {
                let _ = g.save_snapshot(snapshot_path);
            }
        }
    });

    // Setup Ctrl-C handler for snapshot saving and graceful shutdown
    let running = Arc::new(AtomicBool::new(true));
    let r = running.clone();
    ctrlc::set_handler(move || {
        println!("\n[RISK] Shutdown signal received. Saving snapshot...");
        r.store(false, Ordering::Relaxed);
        if let Ok(g) = gate_clone.lock() {
            if let Err(e) = g.save_snapshot(snapshot_path) {
                eprintln!("[RISK] Error saving snapshot: {}", e);
            } else {
                println!("[RISK] Snapshot saved successfully.");
            }
        }
    }).expect("Error setting Ctrl-C handler");

    let listener = TcpListener::bind("127.0.0.1:9092").unwrap();
    listener.set_nonblocking(true).expect("Cannot set non-blocking");

    let active_connections = Arc::new(AtomicU64::new(0));
    const MAX_CONNECTIONS: u64 = 64;

    // Use monotonically increasing atomic sequence for unique order IDs
    let initial_seq = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_nanos() as u64;
    let order_seq = Arc::new(AtomicU64::new(initial_seq));

    while running.load(Ordering::Relaxed) {
        match listener.accept() {
            Ok((stream, _)) => {
                stream.set_nonblocking(false).expect("Cannot set blocking");
                let gate = gate.clone();
                let active = active_connections.clone();
                let order_seq = order_seq.clone();
                if active.load(Ordering::Relaxed) >= MAX_CONNECTIONS {
                    eprintln!("[RISK] Max connections ({}) reached, dropping client", MAX_CONNECTIONS);
                    continue;
                }
                active.fetch_add(1, Ordering::Relaxed);
                std::thread::spawn(move || {
                    handle_client(stream, gate, order_seq);
                    active.fetch_sub(1, Ordering::Relaxed);
                });
            }
            Err(ref e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                std::thread::sleep(std::time::Duration::from_millis(10));
                continue;
            }
            Err(e) => eprintln!("[RISK] Connection failed: {}", e),
        }
    }

    println!("[RISK] Shutting down gracefully, waiting for active connections...");
    while active_connections.load(Ordering::Relaxed) > 0 {
        std::thread::sleep(std::time::Duration::from_millis(50));
    }
    println!("[RISK] All connections closed. Exiting.");
}

fn handle_client(mut client_stream: TcpStream, gate: Arc<Mutex<RiskGate>>, order_seq: Arc<AtomicU64>) {
    let mut buffer = [0; 4096];
    let mut stream_buffer = String::new();

    while let Ok(size) = client_stream.read(&mut buffer) {
        if size == 0 {
            break;
        }
        let chunk = String::from_utf8_lossy(&buffer[..size]);
        stream_buffer.push_str(&chunk);

        while let Some(pos) = stream_buffer.find('\n') {
            let line = stream_buffer[..pos].trim().to_string();
            stream_buffer.drain(..=pos);

            if line.is_empty() {
                continue;
            }

            if line == "health" {
                let _ = client_stream.write_all(b"{\"status\":\"ok\"}\n");
                continue;
            }

            // Parse JSON
            let parsed: Result<Value, _> = serde_json::from_str(&line);
            if let Ok(v) = parsed {
                let price = v["price"].as_u64().unwrap_or(0);
                let qty = v["qty"].as_u64().unwrap_or(0);
                let side_str = v["side"].as_str().unwrap_or("BUY");
                let side = if side_str.eq_ignore_ascii_case("SELL") || side_str.eq_ignore_ascii_case("ASK") {
                    OrderSide::Ask
                } else {
                    OrderSide::Bid
                };

                let instrument_id = v["instrument_id"].as_u64().unwrap_or(1) as u32;

                let mut order = Order {
                    id: 1, // Dummy ID
                    cl_order_id: 1,
                    instrument_id,
                    symbol: *b"UNKNOWN ",
                    price,
                    qty,
                    side,
                    timestamp: std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap().as_nanos() as u64,
                    account_id: 0,
                    client_id: 0,
                    strategy_id: 0,
                    entry_time_ns: 0,
                };

                order.id = order_seq.fetch_add(1, Ordering::Relaxed);

                let approved = {
                    if let Ok(mut g) = gate.lock() {
                        g.check_order(&order)
                    } else {
                        Err(robin_risk::gate::RiskError::KillSwitchActive)
                    }
                };

                let gate_start = std::time::Instant::now();
                let latency_ns = gate_start.elapsed().as_nanos() as u64;
                metrics::record_order(latency_ns, approved.is_ok());

                if let Err(e) = approved {
                    let resp = format!(
                        "{{\"order_id\":0,\"instrument_id\":1,\"fill_price\":0,\"fill_qty\":0,\"status\":\"REJECTED\",\"success\":false,\"error\":\"{:?}\"}}\n",
                        e
                    );
                    let _ = client_stream.write_all(resp.as_bytes());
                    continue;
                }

                // Forward to execution core
                match TcpStream::connect("127.0.0.1:9091") {
                    Ok(mut engine_stream) => {
                        let mut out_req = line.to_string();
                        if !out_req.ends_with('\n') {
                            out_req.push('\n');
                        }
                        let _ = engine_stream.write_all(out_req.as_bytes());
                        let mut resp_buf = [0; 1024];
                        if let Ok(resp_size) = engine_stream.read(&mut resp_buf) {
                            let _ = client_stream.write_all(&resp_buf[..resp_size]);
                        }
                    }
                    Err(e) => {
                        eprintln!("[RISK] Failed to connect to matching engine: {}", e);
                        let resp = "{\"status\":\"REJECTED\",\"success\":false,\"error\":\"engine offline\"}\n";
                        let _ = client_stream.write_all(resp.as_bytes());
                    }
                }
            } else {
                let _ = client_stream.write_all(b"{\"error\":\"invalid json\"}\n");
            }
        }
    }
}
