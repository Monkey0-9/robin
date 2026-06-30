use std::net::{TcpListener, TcpStream};
use std::io::{Read, Write};
use std::sync::{Arc, Mutex};
use robin_risk::gate::{RiskGate, Order, OrderSide};
use serde_json::Value;

fn main() {
    println!("[RISK] Starting Risk Analytics Daemon on port 9092...");
    
    let mut gate = RiskGate::with_config("robin_risk_match.shm", 10_000_000_000, 100, 100_000);
    
    // Attempt to load snapshot
    let snapshot_path = "positions.bin";
    if let Ok(count) = gate.load_snapshot(snapshot_path) {
        println!("[RISK] Loaded {} positions from snapshot.", count);
    } else {
        println!("[RISK] No valid snapshot found, starting fresh.");
    }
    
    // Set realistic initial reference prices in ticks (price * 10,000)
    gate.update_reference_price(1, 645_000_000); // BTC/USD ~ 64,500
    gate.update_reference_price(2, 34_500_000);  // ETH/USD ~ 3,450
    gate.update_reference_price(3, 1_853_000);   // AAPL ~ 185.30
    gate.update_reference_price(4, 10_850);      // EUR/USD ~ 1.0850

    let gate = Arc::new(Mutex::new(gate));
    let gate_clone = gate.clone();

    // Setup Ctrl-C handler for snapshot saving
    ctrlc::set_handler(move || {
        println!("\n[RISK] Shutdown signal received. Saving snapshot...");
        if let Ok(g) = gate_clone.lock() {
            if let Err(e) = g.save_snapshot(snapshot_path) {
                eprintln!("[RISK] Error saving snapshot: {}", e);
            } else {
                println!("[RISK] Snapshot saved successfully.");
            }
        }
        std::process::exit(0);
    }).expect("Error setting Ctrl-C handler");

    let listener = TcpListener::bind("127.0.0.1:9092").unwrap();

    for stream in listener.incoming() {
        match stream {
            Ok(stream) => {
                let gate = gate.clone();
                std::thread::spawn(move || {
                    handle_client(stream, gate);
                });
            }
            Err(e) => eprintln!("[RISK] Connection failed: {}", e),
        }
    }
}

fn handle_client(mut client_stream: TcpStream, gate: Arc<Mutex<RiskGate>>) {
    let mut buffer = [0; 4096];

    while let Ok(size) = client_stream.read(&mut buffer) {
        if size == 0 {
            break;
        }
        let request = String::from_utf8_lossy(&buffer[..size]).into_owned();
        let request = request.trim();

        if request == "health" {
            let _ = client_stream.write_all(b"{\"status\":\"ok\"}\n");
            continue;
        }

        // Parse JSON
        let parsed: Result<Value, _> = serde_json::from_str(&request);
        if let Ok(v) = parsed {
            let price = v["price"].as_f64().unwrap_or(0.0) as u32;
            let qty = v["qty"].as_f64().unwrap_or(0.0) as u32;
            let side_str = v["side"].as_str().unwrap_or("BUY");
            let side = if side_str.eq_ignore_ascii_case("SELL") || side_str.eq_ignore_ascii_case("ASK") {
                OrderSide::Ask
            } else {
                OrderSide::Bid
            };

            let mut order = Order {
                id: 1, // Dummy ID
                cl_order_id: 1,
                instrument_id: 1, // Default to 1
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
            
            // Generate basic unique ID based on timestamp
            order.id = (order.timestamp % 1000000000) as u64;

            let approved = {
                if let Ok(mut g) = gate.lock() {
                    g.check_order(&order)
                } else {
                    Err(robin_risk::gate::RiskError::KillSwitchActive)
                }
            };

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
                    let mut out_req = request.to_string();
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
