use opentelemetry::trace::TracerProvider as _;
use robin_risk::config::PORT_RISK_METRICS;
use robin_risk::gate::{Order, OrderSide, RiskError, RiskGate};
use robin_risk::metrics;
use robin_risk::shm_bridge::{ShmBridge, ShmMessage};
use serde_json::{json, Value};
use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use tracing_subscriber::prelude::*;

/// Map a risk rejection reason onto the metric label used by
/// `robin_risk_rejections_by_type`.
fn rejection_reason(e: &RiskError) -> &'static str {
    match e {
        RiskError::KillSwitchActive => "kill_switch",
        RiskError::CircuitBreakerTripped => "circuit_breaker",
        RiskError::FatFinger => "fat_finger",
        RiskError::PriceCollar => "price_collar",
        RiskError::DuplicateOrder => "duplicate",
        RiskError::PositionLimit => "position",
        RiskError::VelocityLimit => "velocity",
        RiskError::SymbolRestricted => "symbol_restricted",
        RiskError::RegShoRestriction => "reg_sho",
        RiskError::CreditLimit => "credit",
        RiskError::ConcentrationLimit => "concentration",
        RiskError::CorrelationRisk => "correlation",
        RiskError::StressMarginLimit => "stress_margin",
    }
}

fn main() {
    println!("[RISK] Starting Risk Analytics Daemon on port 9092...");

    // Initialize OpenTelemetry Tracing
    let tracer = opentelemetry_otlp::new_pipeline()
        .tracing()
        .with_exporter(opentelemetry_otlp::new_exporter().tonic())
        .install_batch(opentelemetry_sdk::runtime::Tokio)
        .unwrap_or_else(|_| {
            opentelemetry_sdk::trace::TracerProvider::builder()
                .build()
                .tracer("robin-risk-gate")
        });
    let telemetry = tracing_opentelemetry::layer().with_tracer(tracer);
    let subscriber = tracing_subscriber::Registry::default().with(telemetry);
    tracing::subscriber::set_global_default(subscriber).ok();

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
        100 * 100_000_000,            // position limit (100 contracts scaled by 1e8)
        100_000 * 100_000_000,        // max qty per order (100k contracts scaled by 1e8)
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
    gate.update_reference_price(2, 345_000_000_000); // ETH/USD ~ 3,450
    gate.update_reference_price(3, 18_530_000_000); // AAPL ~ 185.30
    gate.update_reference_price(4, 108_500_000); // EUR/USD ~ 1.0850

    let gate = Arc::new(Mutex::new(gate));
    let gate_clone = gate.clone();
    let gate_bg = gate.clone();

    // Setup background thread for periodic snapshot saving
    std::thread::spawn(move || loop {
        std::thread::sleep(std::time::Duration::from_secs(5));
        if let Ok(g) = gate_bg.lock() {
            let _ = g.save_snapshot(snapshot_path);
        }
    });

    let gate_ai = gate.clone();
    std::thread::spawn(move || {
        println!("[RISK] AI Agent SHM consumer thread started on robin_ai_oms.shm");
        let mut ai_shm = match ShmBridge::new("robin_ai_oms.shm", true) {
            Ok(shm) => shm,
            Err(e) => {
                eprintln!("[RISK] Failed to create AI SHM bridge: {}", e);
                return;
            }
        };
        let mut msg = ShmMessage {
            msg_type: 0,
            client_id: 0,
            instrument_id: 0,
            price: 0,
            qty: 0,
            side: 0,
            flags: 0,
            order_id: 0,
            cl_order_id: 0,
            timestamp_ns: 0,
            _pad: [0; 13],
        };
        loop {
            if ai_shm.pop(&mut msg) {
                if let Ok(mut g) = gate_ai.lock() {
                    let side = if msg.side == 1 {
                        OrderSide::Bid
                    } else {
                        OrderSide::Ask
                    };
                    let order = Order {
                        id: msg.order_id,
                        cl_order_id: msg.cl_order_id,
                        instrument_id: msg.instrument_id,
                        symbol: *b"BTC/USD ",
                        price: msg.price,
                        qty: msg.qty,
                        side,
                        timestamp: msg.timestamp_ns,
                        account_id: 0,
                        client_id: msg.client_id,
                        strategy_id: 0,
                        entry_time_ns: msg.timestamp_ns,
                    };
                    let _ = g.check_order(&order);
                }
            } else {
                std::thread::sleep(std::time::Duration::from_millis(1));
            }
        }
    });

    // Periodic portfolio analytics: VaR / CVaR / stress / Sharpe / positions
    // published to the Prometheus endpoint so the gateway circuit breaker and
    // monitoring stack observe live risk posture without touching the hot path.
    {
        let gate_analytics = gate.clone();
        std::thread::spawn(move || loop {
            std::thread::sleep(std::time::Duration::from_secs(5));
            if let Ok(g) = gate_analytics.lock() {
                let mut portfolio_value = 0.0f64;
                let mut positions_tracked = 0u64;
                for i in 0..4096u32 {
                    let pos = g.get_position(i);
                    if pos != 0 {
                        let last = g.last_trade_price(i);
                        if last > 0 {
                            portfolio_value += (pos as f64).abs() * (last as f64 / 100_000_000.0);
                            positions_tracked += 1;
                        }
                    }
                }
                let vol = 0.20; // portfolio annualized vol proxy (20%)
                let var_p = g.calculate_var_parametric(portfolio_value.max(1.0), vol, 1.0);
                let var_mc = g.calculate_var_monte_carlo(portfolio_value.max(1.0), vol, 1.0);
                let var_95 = var_p.var_95.max(var_mc.var_95);
                let var_99 = var_p.var_99.max(var_mc.var_99);
                let cvar_95 = var_p.cvar_95.max(var_mc.cvar_95);
                let stress = g.stress_test(portfolio_value.max(1.0), 1.0);
                let worst_loss = stress
                    .iter()
                    .map(|(_, _, impact)| impact.abs())
                    .fold(0.0f64, f64::max);
                let sharpe = g.compute_sharpe(0);
                metrics::record_analytics(
                    portfolio_value,
                    var_95,
                    var_99,
                    cvar_95,
                    worst_loss,
                    sharpe,
                    positions_tracked,
                );
            }
        });
    }

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
    })
    .expect("Error setting Ctrl-C handler");

    let listener = TcpListener::bind("127.0.0.1:9092").unwrap();
    listener
        .set_nonblocking(true)
        .expect("Cannot set non-blocking");

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
                    eprintln!(
                        "[RISK] Max connections ({}) reached, dropping client",
                        MAX_CONNECTIONS
                    );
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

fn handle_client(
    mut client_stream: TcpStream,
    gate: Arc<Mutex<RiskGate>>,
    order_seq: Arc<AtomicU64>,
) {
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
                // Command routing (analytics / market data / Reg SHO seeding)
                if let Some(cmd) = v.get("cmd").and_then(|c| c.as_str()) {
                    handle_command(&mut client_stream, &gate, &v, cmd);
                    continue;
                }

                let price = v["price"].as_u64().unwrap_or(0);
                let qty = v["qty"].as_u64().unwrap_or(0);
                let side_str = v["side"].as_str().unwrap_or("BUY");
                let side = if side_str.eq_ignore_ascii_case("SELL")
                    || side_str.eq_ignore_ascii_case("ASK")
                {
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
                    timestamp: std::time::SystemTime::now()
                        .duration_since(std::time::UNIX_EPOCH)
                        .unwrap()
                        .as_nanos() as u64,
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

                // Emit OpenTelemetry span
                let span = tracing::span!(
                    tracing::Level::INFO,
                    "risk_check",
                    order_id = order.id,
                    instrument = order.instrument_id
                );
                let _enter = span.enter();

                if let Err(e) = approved {
                    metrics::record_rejection(rejection_reason(&e));
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

fn now_ns() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_nanos() as u64
}

/// Handle control-plane commands on the same TCP port as order submission:
///  - quote            feed a last-trade price tick (Reg SHO / P&L / correlation)
///  - previous_close   seed the Reg SHO Rule 201 reference price
///  - analytics        full risk snapshot (P&L, VaR, stress, correlation, Reg SHO)
///  - pnl              per-strategy P&L detail
///  - var              Monte Carlo Value-at-Risk on demand
fn handle_command(
    client_stream: &mut TcpStream,
    gate: &Arc<Mutex<RiskGate>>,
    v: &Value,
    cmd: &str,
) {
    let instrument_id = v["instrument_id"].as_u64().unwrap_or(0) as u32;
    let response = match cmd {
        "quote" => {
            let price = v["price"].as_u64().unwrap_or(0);
            if price == 0 {
                json!({"status":"error","error":"price required"})
            } else if let Ok(mut g) = gate.lock() {
                g.set_market_data(instrument_id, price);
                json!({
                    "status": "ok",
                    "instrument_id": instrument_id,
                    "last_trade_price": price,
                    "reg_sho_active": g.short_sale_cb_active(instrument_id, now_ns())
                })
            } else {
                json!({"status":"error","error":"gate busy"})
            }
        }
        "previous_close" => {
            let price = v["price"].as_u64().unwrap_or(0);
            if price == 0 {
                json!({"status":"error","error":"price required"})
            } else if let Ok(mut g) = gate.lock() {
                g.set_previous_close(instrument_id, price);
                json!({"status":"ok","instrument_id":instrument_id,"previous_close":price})
            } else {
                json!({"status":"error","error":"gate busy"})
            }
        }
        "analytics" => {
            if let Ok(g) = gate.lock() {
                analytics_snapshot(&g)
            } else {
                json!({"status":"error","error":"gate busy"})
            }
        }
        "pnl" => {
            let account_id = v["account_id"].as_u64().unwrap_or(0) as u32;
            if let Ok(g) = gate.lock() {
                let pnl = g.get_pnl(account_id);
                json!({
                    "status": "ok",
                    "account_id": account_id,
                    "realized_pnl": pnl.realized_pnl,
                    "unrealized_pnl": pnl.unrealized_pnl,
                    "total_pnl": pnl.total_pnl,
                    "trades": pnl.trades_count,
                    "win_count": pnl.win_count,
                    "loss_count": pnl.loss_count,
                    "max_drawdown": pnl.max_drawdown,
                    "sharpe_ratio": pnl.sharpe_ratio
                })
            } else {
                json!({"status":"error","error":"gate busy"})
            }
        }
        "var" => {
            let pv = v["portfolio_value"].as_f64().unwrap_or(0.0);
            let vol = v["volatility"].as_f64().unwrap_or(0.20);
            let days = v["days"].as_f64().unwrap_or(1.0);
            if let Ok(g) = gate.lock() {
                let r = g.calculate_var_monte_carlo(pv.max(1.0), vol, days);
                json!({
                    "status": "ok",
                    "var_95": r.var_95,
                    "var_99": r.var_99,
                    "cvar_95": r.cvar_95,
                    "portfolio_value": r.portfolio_value,
                    "volatility_annual": r.volatility_annual,
                    "method": r.method
                })
            } else {
                json!({"status":"error","error":"gate busy"})
            }
        }
        _ => json!({"status":"error","error":format!("unknown command: {cmd}")}),
    };
    let mut out = response.to_string();
    out.push('\n');
    let _ = client_stream.write_all(out.as_bytes());
}

/// Build a live analytics snapshot from the gate state.
fn analytics_snapshot(g: &RiskGate) -> Value {
    let mut portfolio_value = 0.0f64;
    let mut positions: Vec<Value> = Vec::new();
    for i in 0..4096u32 {
        let pos = g.get_position(i);
        if pos != 0 {
            let last = g.last_trade_price(i);
            if last > 0 {
                portfolio_value += (pos as f64).abs() * (last as f64 / 100_000_000.0);
            }
            positions.push(json!({"instrument_id": i, "qty": pos}));
        }
    }
    let vol = 0.20;
    let var_p = g.calculate_var_parametric(portfolio_value.max(1.0), vol, 1.0);
    let var_mc = g.calculate_var_monte_carlo(portfolio_value.max(1.0), vol, 1.0);
    let stress = g.stress_test(portfolio_value.max(1.0), 1.0);
    let stress_vec: Vec<Value> = stress
        .into_iter()
        .map(|(name, value, impact)| {
            json!({"scenario": name, "stressed_value": value, "pnl_impact": impact})
        })
        .collect();
    let now = now_ns();
    let reg_sho: Vec<Value> = positions
        .iter()
        .filter_map(|p| {
            let id = p["instrument_id"].as_u64().unwrap_or(0) as u32;
            let prev_close = g.previous_close(id);
            if prev_close == 0 {
                return None;
            }
            Some(json!({
                "instrument_id": id,
                "previous_close": prev_close,
                "short_sale_breaker_active": g.short_sale_cb_active(id, now),
            }))
        })
        .collect();
    json!({
        "status": "ok",
        "portfolio_value": portfolio_value,
        "positions": positions,
        "var_95": var_mc.var_95.max(var_p.var_95),
        "var_99": var_mc.var_99.max(var_p.var_99),
        "cvar_95": var_mc.cvar_95.max(var_p.cvar_95),
        "volatility_annual": var_p.volatility_annual,
        "stress_test": stress_vec,
        "reg_sho": reg_sho,
        "sharpe_ratio": g.compute_sharpe(0)
    })
}
