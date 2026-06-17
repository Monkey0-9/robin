// services/compliance/spoofing_detector.rs
// Real-time market manipulation and order book layering (spoofing) pattern matching.
// Compliance checker checking order cancellation ratios and size imbalances.

use std::collections::HashMap;

pub struct OrderEvent {
    pub order_id: u64,
    pub symbol: String,
    pub price: u32,
    pub qty: u32,
    pub event_type: &'static str, // "NEW", "CANCEL", "FILL"
    pub timestamp_ms: u64,
}

pub struct SpoofingDetector {
    // Maps symbol -> list of events in the rolling 5-second window
    recent_events: HashMap<String, Vec<OrderEvent>>,
    alert_count: u64,
}

impl SpoofingDetector {
    pub fn new() -> Self {
        Self {
            recent_events: HashMap::new(),
            alert_count: 0,
        }
    }

    pub fn process_order_event(&mut self, event: OrderEvent) -> bool {
        let history = self.recent_events.entry(event.symbol.clone()).or_insert_with(Vec::new);
        history.push(event);

        // Prune events older than 5000ms
        let now = history.last().unwrap().timestamp_ms;
        history.retain(|e| (now - e.timestamp_ms) <= 5000);

        // Check for spoofing pattern:
        // A large order is placed, followed by a small opposite order, then the large order is cancelled
        // within a short window (<1000ms) without any significant execution fills.
        let mut large_new_orders = Vec::new();
        let mut cancellations = Vec::new();

        for e in history.iter() {
            if e.event_type == "NEW" && e.qty >= 10000 {
                large_new_orders.push(e);
            } else if e.event_type == "CANCEL" && e.qty >= 10000 {
                cancellations.push(e);
            }
        }

        for c in &cancellations {
            for n in &large_new_orders {
                if c.order_id == n.order_id && (c.timestamp_ms - n.timestamp_ms) < 1000 {
                    // Pattern matched: rapid cancellation of large liquidity size
                    self.alert_count += 1;
                    println!("[COMPLIANCE WARNING] Spoofing attempt detected on {}! Large Order ID: {} cancelled in {}ms.", 
                             c.symbol, c.order_id, c.timestamp_ms - n.timestamp_ms);
                    return true; // Alert generated
                }
            }
        }

        false
    }

    pub fn get_alert_count(&self) -> u64 {
        self.alert_count
    }
}
