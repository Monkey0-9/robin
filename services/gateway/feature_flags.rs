// services/gateway/feature_flags.rs
// Low-latency dynamic feature flagging framework.
// Allows safe toggle of components (e.g. Dark pool access, HFT mode, compliance bounds) on the fly.

use std::collections::HashMap;
use std::sync::RwLock;

lazy_static::lazy_static! {
    static ref FEATURE_REGISTRY: RwLock<HashMap<String, bool>> = {
        let mut m = HashMap::new();
        m.insert("hft_execution_engine".to_string(), true);
        m.insert("pre_trade_risk_checks".to_string(), true);
        m.insert("alternative_data_streams".to_string(), false);
        m.insert("dark_pool_routing".to_string(), false);
        RwLock::new(m)
    };
}

pub struct FeatureFlagManager;

impl FeatureFlagManager {
    #[inline(always)]
    pub fn is_enabled(feature_name: &str) -> bool {
        let registry = FEATURE_REGISTRY.read().unwrap();
        *registry.get(feature_name).unwrap_or(&false)
    }

    pub fn set_feature(feature_name: &str, status: bool) {
        let mut registry = FEATURE_REGISTRY.write().unwrap();
        registry.insert(feature_name.to_string(), status);
        println!("[Feature Flag Update] Feature '{}' changed status to: {}", feature_name, status);
    }
}

fn main() {
    let check = FeatureFlagManager::is_enabled("hft_execution_engine");
    println!("[Feature Flags] HFT Engine Enabled: {}", check);
    
    FeatureFlagManager::set_feature("dark_pool_routing", true);
    println!("[Feature Flags] Dark Pool Routing Enabled: {}", FeatureFlagManager::is_enabled("dark_pool_routing"));
}
