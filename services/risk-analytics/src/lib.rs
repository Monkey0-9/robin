// services/risk-analytics/src/lib.rs
pub mod gate;
pub mod shm_bridge;
pub mod gpio_kill_switch;
pub mod risk_gate_fast;
pub mod hedging;
pub mod pre_trade;
pub mod circuit_breaker;

#[path = "TaxEngine.rs"]
pub mod tax_engine;
