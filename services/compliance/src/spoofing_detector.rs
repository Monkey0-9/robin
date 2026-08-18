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
        // VPIN = Sum(|BuyVol - SellVol|) / (TotalVol) over volume buckets
        let bucket_size = 50_000 * 100_000_000; // 50,000 shares per bucket
        let mut total_imbalance = 0_f64;
        let mut total_volume_in_buckets = 0_f64;

        let mut current_bucket_buy = 0;
        let mut current_bucket_sell = 0;
        let mut current_bucket_vol = 0;

        for e in history.iter() {
            // We proxy Buy/Sell based on event type since side isn't in OrderEvent.
            // In a real VPIN we classify by aggressor side (tick rule or direct feed).
            // For spoofing detection, we look at the imbalance of CANCELs vs NEWs
            // as an adapted VPIN-like metric for spoofing.
            let vol = e.qty;
            if e.event_type == "NEW" {
                current_bucket_buy += vol;
            } else if e.event_type == "CANCEL" {
                current_bucket_sell += vol; // Treat CANCEL as opposite side for spoofing pressure
            }
            current_bucket_vol += vol;

            if current_bucket_vol >= bucket_size {
                let imbalance =
                    (current_bucket_buy as i64 - current_bucket_sell as i64).unsigned_abs();
                total_imbalance += imbalance as f64;
                total_volume_in_buckets += current_bucket_vol as f64;

                current_bucket_buy = 0;
                current_bucket_sell = 0;
                current_bucket_vol = 0;
            }
        }

        if total_volume_in_buckets > 0.0 {
            let vpin = total_imbalance / total_volume_in_buckets;

            // Flag if VPIN-like imbalance is extremely high (e.g. > 0.8) meaning
            // 90% of activity was cancels compared to real orders (spoofing indicator)
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

        for i in 0..100 {
            // Small NEW order
            detector.process_order_event(OrderEvent {
                order_id: i,
                symbol: "AAPL".into(),
                price: 50000 * 100_000_000,
                qty: 500 * 100_000_000, // 500 qty
                event_type: "NEW",
                timestamp_ns: 1000 + i * 10,
            });
            // Massive CANCEL order (Spoofing)
            detector.process_order_event(OrderEvent {
                order_id: i,
                symbol: "AAPL".into(),
                price: 50000 * 100_000_000,
                qty: 9500 * 100_000_000, // 9,500 qty (95% cancel ratio)
                event_type: "CANCEL",
                timestamp_ns: 1500 + i * 10,
            });
        }

        assert!(detector.get_alert_count() >= 1); // Triggered once threshold exceeded
    }
}
