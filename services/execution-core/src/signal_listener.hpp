#pragma once

#include <string>
#include <vector>
#include <chrono>
#include <iostream>
#include <atomic>

namespace quantum {
namespace execution {

// Data structure representing a statistical signal computed by R
struct QuantSignal {
    uint32_t instrument_id = 0;
    double   target_volatility = 0.0;
    double   stat_arb_z_score = 0.0;
    double   fair_value = 0.0;
    uint64_t timestamp_ns = 0;
};

// Low-latency listener for statistical computing signals
class QuantSignalListener {
public:
    QuantSignalListener() : running_(false) {}

    void start() {
        running_ = true;
        std::cout << "[SIGNAL_LISTENER] High-speed C++ Signal Listener active. Ready for R statistical feed." << std::endl;
    }

    void stop() {
        running_ = false;
    }

    // Process incoming signal from R service
    void on_signal_received(const QuantSignal& signal) {
        latest_signal_ = signal;
        // Apply statistical guardrails directly to low-level order book / matching rules
        if (std::abs(signal.stat_arb_z_score) > 2.0) {
            std::cout << "[SIGNAL_LISTENER] StatArb Signal Triggered! Instrument=" 
                      << signal.instrument_id << " Z-Score=" << signal.stat_arb_z_score << std::endl;
        }
    }

    const QuantSignal& get_latest_signal() const { return latest_signal_; }

private:
    std::atomic<bool> running_;
    QuantSignal       latest_signal_;
};

} // namespace execution
} // namespace quantum
