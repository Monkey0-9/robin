#include <cstdint>
#include <cstdio>
#include <cstring>
#include <cmath>
#include <vector>
#include <array>
#include <fstream>
#include <string>
#include <sstream>

// ============================================================================
// LinearSignalModel — Feature-based trading signal generator
// ============================================================================
// Institutional-grade feature engineering (mirrors robin_signal_model.py):
//   - Price momentum: MACD-style EMA12/EMA26 crossover on log prices
//   - Volume pressure: z-score of volume vs rolling average, bounded by tanh
//   - Order book imbalance: depth-weighted (price-level decay)
//   - Intraday time-of-day component
//   - Features standardized online (running mean/std) to comparable scales
//   - Confidence derived from |alpha| relative to recent realized volatility
//
// Model weights are loadable from a key=value config file (defaults shown).

namespace quantum { namespace ai {

struct ModelInput {
    float price_features[64];    // Rolling price window
    float volume_features[64];   // Rolling volume window
    float order_book_features[32]; // [bid_vol0, ask_vol0, bid_vol1, ask_vol1, ...]
    float timestamp_features[8]; // Time-of-day encoding (sin/cos)
};

struct ModelOutput {
    float alpha_signal;        // Primary directional signal [-1, 1]
    float volatility_estimate; // Estimated realized volatility [0, ∞)
    float spread_estimate;     // Estimated bid-ask spread in bps
    float confidence;          // Signal confidence [0, 1]
};

// Model weights (hardcoded defaults; loadable from config file)
struct LinearWeights {
    float price_momentum_w  = 0.40f;
    float ob_imbalance_w    = 0.30f;
    float volume_pressure_w = 0.20f;
    float intraday_w        = 0.10f;
};

class LinearSignalModel {
public:
    LinearSignalModel() noexcept : weights_(), model_name_("LinearSignalModel_v1") {}

    // Load model config from a weights file (key=value lines).
    bool load(const char* config_path) noexcept {
        std::snprintf(config_path_, sizeof(config_path_), "%s", config_path);

        std::ifstream infile(config_path_);
        if (infile.is_open()) {
            std::string line;
            while (std::getline(infile, line)) {
                std::istringstream iss(line);
                std::string key;
                if (std::getline(iss, key, '=')) {
                    float value;
                    if (iss >> value) {
                        if (key == "price_momentum_w") weights_.price_momentum_w = value;
                        else if (key == "ob_imbalance_w") weights_.ob_imbalance_w = value;
                        else if (key == "volume_pressure_w") weights_.volume_pressure_w = value;
                        else if (key == "intraday_w") weights_.intraday_w = value;
                    }
                }
            }
            std::printf("[SIGNAL] Loaded custom weights from %s\n", config_path_);
        } else {
            std::printf("[SIGNAL] Failed to open %s. Using default weights.\n", config_path_);
        }

        return true;
    }

    ModelOutput compute(const ModelInput& input) noexcept {
        constexpr size_t P = 64;
        constexpr size_t V = 64;

        // --- Price momentum: log-return based, MACD-style EMA crossover ---
        double log_prices[P];
        double pmin = 1e-9;
        for (size_t i = 0; i < P; i++) {
            double p = input.price_features[i];
            log_prices[i] = std::log(p > pmin ? p : pmin);
        }
        double ema12 = log_prices[0];
        double ema26 = log_prices[0];
        {
            const double k12 = 2.0 / (12.0 + 1.0);
            const double k26 = 2.0 / (26.0 + 1.0);
            for (size_t i = 1; i < P; i++) {
                ema12 = k12 * log_prices[i] + (1.0 - k12) * ema12;
                ema26 = k26 * log_prices[i] + (1.0 - k26) * ema26;
            }
        }
        double momentum = (std::fabs(ema26) > 1e-9) ? (ema12 - ema26) / ema26 : 0.0;

        // --- Volume pressure: z-score vs rolling average, tanh bounded ---
        constexpr size_t vol_window = 20;
        double vol_ma = 0.0;
        for (size_t i = P - vol_window; i < V; i++) vol_ma += input.volume_features[i];
        vol_ma /= static_cast<double>(vol_window);
        double vol_var = 0.0;
        for (size_t i = P - vol_window; i < V; i++) {
            double d = input.volume_features[i] - vol_ma;
            vol_var += d * d;
        }
        vol_var /= static_cast<double>(vol_window);
        double vol_std = std::sqrt(vol_var);
        double volume_z = (vol_std > 1e-10) ? (input.volume_features[V - 1] - vol_ma) / vol_std : 0.0;
        double volume_pressure = std::tanh(volume_z);

        // --- Order book imbalance: depth-weighted ---
        double bid_w = 0.0, ask_w = 0.0;
        constexpr size_t OB = 32;
        for (size_t i = 0; i < OB / 2; i++) {
            double weight = 1.0 / (1.0 + static_cast<double>(i));
            bid_w += input.order_book_features[i * 2] * weight;
            ask_w += input.order_book_features[i * 2 + 1] * weight;
        }
        double ob_imbalance = (bid_w + ask_w > 1e-6f)
            ? (bid_w - ask_w) / (bid_w + ask_w)
            : 0.0;

        // --- Intraday time-of-day component ---
        double intraday = (input.timestamp_features[0] * 2.0f - 1.0f);

        // --- Online feature standardization (running mean/std) ---
        const double raw[4] = {momentum, volume_pressure, ob_imbalance, intraday};
        double standardized[4];
        for (int f = 0; f < 4; f++) {
            double delta = raw[f] - feature_means_[f];
            feature_means_[f] += delta / static_cast<double>(n_obs_ + 1);
            double delta2 = raw[f] - feature_means_[f];
            feature_m2_[f] += delta * delta2;
            double var = (n_obs_ > 1) ? feature_m2_[f] / static_cast<double>(n_obs_) : 0.0;
            double sd = std::sqrt(var > 0.0 ? var : 1e-10);
            standardized[f] = (sd > 1e-10) ? (raw[f] - feature_means_[f]) / sd : 0.0;
        }
        n_obs_++;

        // --- Composite alpha (weighted linear combination) ---
        double raw_alpha = weights_.price_momentum_w  * standardized[0]
                         + weights_.volume_pressure_w * standardized[1]
                         + weights_.ob_imbalance_w    * standardized[2]
                         + weights_.intraday_w        * standardized[3];

        double alpha = std::fmax(-1.0, std::fmin(1.0, raw_alpha));

        // --- Volatility estimate: log-return realized vol ---
        double sum_sq = 0.0;
        for (size_t i = 1; i < P; i++) {
            double r = log_prices[i] - log_prices[i - 1];
            sum_sq += r * r;
        }
        double realized_vol = std::sqrt(sum_sq / static_cast<double>(P - 1));

        // --- Spread estimate in basis points (1/price proxy) ---
        double price_sum = 0.0;
        for (size_t i = 0; i < P; i++) price_sum += input.price_features[i];
        double price_mean = price_sum / static_cast<double>(P);
        double spread_bps = (price_mean > 0.0f)
            ? (1.0 / price_mean) * 10000.0
            : 0.0;

        // --- Confidence: |alpha| vs realized vol (non-circular) ---
        double confidence;
        if (realized_vol > 1e-9) {
            confidence = std::fabs(alpha) * 0.5 / (realized_vol + 1e-4);
        } else {
            confidence = std::fabs(alpha);
        }
        confidence = std::fmax(0.0, std::fmin(1.0, confidence));

        return ModelOutput{static_cast<float>(alpha),
                           static_cast<float>(realized_vol),
                           static_cast<float>(spread_bps),
                           static_cast<float>(confidence)};
    }

    const char* name() const noexcept { return model_name_; }

private:
    LinearWeights weights_;
    char config_path_[256] = {};
    const char* model_name_;

    // Online standardization state
    size_t n_obs_ = 0;
    double feature_means_[4] = {0.0, 0.0, 0.0, 0.0};
    double feature_m2_[4] = {0.0, 0.0, 0.0, 0.0};
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
