use std::collections::HashMap;

#[derive(Debug, Clone)]
pub struct OrderEvent {
    pub order_id: u64,
    pub symbol: String,
    pub price: u64,
    pub qty: u64,
    pub event_type: &'static str,
    pub timestamp_ns: u64,
}

pub struct SpoofingDetector {
    recent_events: HashMap<String, Vec<OrderEvent>>,
    alert_count: u64,
    window_ns: u64,
}

impl SpoofingDetector {
    pub fn new(window_ns: u64) -> Self {
        Self {
            recent_events: HashMap::new(),
            alert_count: 0,
            window_ns,
        }
    }

    pub fn process_order_event(&mut self, event: OrderEvent) -> bool {
        let now = event.timestamp_ns;
        let history = self.recent_events.entry(event.symbol.clone()).or_default();
        history.push(event.clone());

        let threshold = now.saturating_sub(self.window_ns);
        history.retain(|e| e.timestamp_ns >= threshold);

        // Calculate VPIN (Volume-Synchronized Probability of Informed Trading)
        // Simplified metric: ratio of absolute volume imbalance to total volume
        let mut buy_vol = 0;
        let sell_vol = 0;
        let mut cancel_buy_vol = 0;
        let cancel_sell_vol = 0;

        // Note: For a real VPIN we would bucket by volume, not time, but we adapt it here.
        for e in history.iter() {
            if e.event_type == "NEW" {
                // Heuristic: assuming we know the side, or we just track total activity
                buy_vol += e.qty; // Simplified
            } else if e.event_type == "CANCEL" {
                cancel_buy_vol += e.qty;
            }
        }

        let total_vol = buy_vol + sell_vol;
        if total_vol > 100_000 * 100_000_000 {
            let imbalance = (cancel_buy_vol as i64 - cancel_sell_vol as i64).abs() as u64;
            let vpin = imbalance as f64 / total_vol as f64;

            // Flag if VPIN is extremely high (e.g. > 0.8) and large cancel ratio
            if vpin > 0.8 {
                self.alert_count += 1;
                println!(
                    "[COMPLIANCE] SPOOFING DETECTED: {} VPIN={:.2} (High cancel imbalance)",
                    event.symbol, vpin
                );
                history.clear();
                return true;
            }
        }

        false
    }

    pub fn get_alert_count(&self) -> u64 {
        self.alert_count
    }

    pub fn clear(&mut self) {
        self.recent_events.clear();
        self.alert_count = 0;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_spoofing_detection() {
        let mut detector = SpoofingDetector::new(5_000_000_000);

        for i in 0..10 {
            detector.process_order_event(OrderEvent {
                order_id: i, symbol: "AAPL".into(), price: 50000 * 100_000_000,
                qty: 11000 * 100_000_000, event_type: "NEW", timestamp_ns: 1000 + i * 10,
            });
            detector.process_order_event(OrderEvent {
                order_id: i, symbol: "AAPL".into(), price: 50000 * 100_000_000,
                qty: 11000 * 100_000_000, event_type: "CANCEL", timestamp_ns: 1500 + i * 10,
            });
        }

        assert_eq!(detector.get_alert_count(), 1); // Triggered once threshold exceeded
    }
}
