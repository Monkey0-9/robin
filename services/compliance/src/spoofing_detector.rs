use std::collections::HashMap;

#[derive(Debug, Clone)]
pub struct OrderEvent {
    pub order_id: u64,
    pub symbol: String,
    pub price: u32,
    pub qty: u32,
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
        let history = self.recent_events.entry(event.symbol.clone()).or_default();
        history.push(event);

        let now = history.last().unwrap().timestamp_ns;
        let threshold = now.saturating_sub(self.window_ns);

        history.retain(|e| e.timestamp_ns >= threshold);

        let large_news: Vec<&OrderEvent> = history.iter()
            .filter(|e| e.event_type == "NEW" && e.qty >= 10000)
            .collect();

        let large_cancels: Vec<&OrderEvent> = history.iter()
            .filter(|e| e.event_type == "CANCEL" && e.qty >= 10000)
            .collect();

        for cancel in &large_cancels {
            for new_order in &large_news {
                if cancel.order_id == new_order.order_id
                    && (cancel.timestamp_ns - new_order.timestamp_ns) < 1_000_000
                {
                    self.alert_count += 1;
                    println!(
                        "[COMPLIANCE] SPOOFING: {} OrderID={} cancelled in {}ns",
                        cancel.symbol, cancel.order_id,
                        cancel.timestamp_ns - new_order.timestamp_ns
                    );
                    return true;
                }
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

        detector.process_order_event(OrderEvent {
            order_id: 1, symbol: "AAPL".into(), price: 50000,
            qty: 50000, event_type: "NEW", timestamp_ns: 1000,
        });

        let detected = detector.process_order_event(OrderEvent {
            order_id: 1, symbol: "AAPL".into(), price: 50000,
            qty: 50000, event_type: "CANCEL", timestamp_ns: 1500,
        });

        assert!(detected);
        assert_eq!(detector.get_alert_count(), 1);
    }
}
