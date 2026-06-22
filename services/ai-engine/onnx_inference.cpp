#include <cstdint>
#include <cstdio>
#include <cstring>
#include <cmath>
#include <vector>
#include <array>

// ============================================================================
// LinearSignalModel — Feature-based trading signal generator
// ============================================================================
// NOTE: This is a handcrafted linear model, NOT a neural network / ONNX model.
// It computes a weighted linear combination of market microstructure features:
//   - Price momentum (rolling mean of price feature window)
//   - Volume pressure (rolling mean of volume feature window)
//   - Order book imbalance (bid volume vs ask volume ratio)
//   - Time-of-day component (intraday seasonality proxy)
//
// To integrate a real ML model:
//   1. Add onnxruntime-c or onnxruntime-cxx as a dependency
//   2. Replace compute() with Ort::Session::Run() calls
//   3. Keep ModelInput/ModelOutput structs as the interface contract
//
// Previously mislabeled as OnnxInferenceEngine (misleading — no ONNX runtime
// was ever loaded or used).

namespace quantum { namespace ai {

struct ModelInput {
    float price_features[64];    // Rolling price window (normalized)
    float volume_features[64];   // Rolling volume window (normalized)
    float order_book_features[32]; // [bid_vol0, ask_vol0, bid_vol1, ask_vol1, ...]
    float timestamp_features[8]; // Time-of-day encoding (sin/cos)
};

struct ModelOutput {
    float alpha_signal;        // Primary directional signal [-1, 1]
    float volatility_estimate; // Estimated realized volatility [0, ∞)
    float spread_estimate;     // Estimated bid-ask spread in bps
    float confidence;          // Signal confidence [0, 1]
};

// Model weights (hardcoded; in production load from a config/binary file)
struct LinearWeights {
    float price_momentum_w  = 0.40f;
    float ob_imbalance_w    = 0.30f;
    float volume_pressure_w = 0.20f;
    float intraday_w        = 0.10f;
};

class LinearSignalModel {
public:
    LinearSignalModel() noexcept : weights_(), model_name_("LinearSignalModel_v1") {}

    // Load model config from a weights file (future: JSON or binary format)
    // Returns true always for the linear model since no file is needed.
    bool load(const char* config_path) noexcept {
        std::snprintf(config_path_, sizeof(config_path_), "%s", config_path);
        // TODO: parse config_path to override LinearWeights fields
        // For now the default weights are used unconditionally.
        (void)config_path;
        return true;
    }

    ModelOutput compute(const ModelInput& input) noexcept {
        // --- Price momentum: rolling mean of price feature window ---
        float price_sum = 0.0f;
        float price_max = 0.0f;
        constexpr size_t P = 64;
        for (size_t i = 0; i < P; i++) {
            price_sum += input.price_features[i];
            if (input.price_features[i] > price_max)
                price_max = input.price_features[i];
        }
        float price_mean = price_sum / static_cast<float>(P);
        float price_momentum = (price_mean > 0.0f && input.price_features[0] > 0.0f)
            ? (price_mean - input.price_features[0]) / (input.price_features[0] + 1e-6f)
            : 0.0f;

        // --- Volume pressure: rolling mean of volume window ---
        float vol_sum = 0.0f;
        constexpr size_t V = 64;
        for (size_t i = 0; i < V; i++) vol_sum += input.volume_features[i];
        float vol_mean = vol_sum / static_cast<float>(V);
        // Normalize volume pressure to [-1, 1] range
        float volume_pressure = std::tanh(vol_mean / (vol_mean + 1000.0f + 1e-6f));

        // --- Order book imbalance: (bid_vol - ask_vol) / (bid_vol + ask_vol) ---
        float bid_vol = 0.0f, ask_vol = 0.0f;
        constexpr size_t OB = 32;
        for (size_t i = 0; i < OB / 2; i++) {
            bid_vol += input.order_book_features[i * 2];
            ask_vol += input.order_book_features[i * 2 + 1];
        }
        float ob_imbalance = (bid_vol + ask_vol > 1e-6f)
            ? (bid_vol - ask_vol) / (bid_vol + ask_vol)
            : 0.0f;

        // --- Intraday time-of-day component ---
        float intraday = (input.timestamp_features[0] * 2.0f - 1.0f); // map [0,1] -> [-1,1]

        // --- Composite alpha signal (weighted linear combination) ---
        float raw_alpha = weights_.price_momentum_w  * price_momentum
                        + weights_.ob_imbalance_w    * ob_imbalance
                        + weights_.volume_pressure_w * volume_pressure
                        + weights_.intraday_w        * intraday;

        // Clip alpha to [-1, 1]
        float alpha = std::fmax(-1.0f, std::fmin(1.0f, raw_alpha));

        // --- Volatility estimate (price range / mean proxy) ---
        float volatility = std::fabs(price_max - input.price_features[0])
                         / (input.price_features[0] + 1e-6f);

        // --- Spread estimate in basis points (1/price * 10000 proxy) ---
        float spread_bps = (price_mean > 0.0f)
            ? (1.0f / price_mean) * 10000.0f
            : 0.0f;

        // --- Confidence: based on signal strength and volatility context ---
        float confidence = std::fmin(1.0f, std::fabs(alpha) / (volatility + 0.01f));

        return ModelOutput{alpha, volatility, spread_bps, confidence};
    }

    const char* name() const noexcept { return model_name_; }

private:
    LinearWeights weights_;
    char config_path_[256] = {};
    const char* model_name_;
};

}} // namespace quantum::ai

int main() {
    using namespace quantum::ai;

    LinearSignalModel model;
    model.load("config/signal_model.json"); // No-op for now; file path reserved

    std::printf("[SIGNAL] Model: %s\n", model.name());
    std::printf("[SIGNAL] Backend: Linear weighted combination (CPU, no ONNX runtime)\n");

    ModelInput input;
    std::memset(&input, 0, sizeof(input));

    // Synthetic market data: rising price with buy-side volume pressure
    for (size_t i = 0; i < 64; i++) {
        input.price_features[i]  = 50000.0f + static_cast<float>(i) * 10.0f;
        input.volume_features[i] = 1000.0f  + static_cast<float>(i) * 100.0f;
    }
    // Simulated order book: more bid volume than ask volume
    for (size_t i = 0; i < 14; i++) {
        input.order_book_features[i * 2]     = 800.0f;  // bid
        input.order_book_features[i * 2 + 1] = 500.0f;  // ask
    }
    input.timestamp_features[0] = 0.5f; // Mid-session

    ModelOutput out = model.compute(input);
    std::printf("[SIGNAL] Alpha=%.4f Volatility=%.4f SpreadBps=%.4f Confidence=%.4f\n",
                out.alpha_signal, out.volatility_estimate,
                out.spread_estimate, out.confidence);

    return 0;
}
