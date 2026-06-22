#include <cstdint>
#include <cstdio>
#include <cstring>
#include <cmath>
#include <vector>

namespace quantum { namespace ai {

struct ModelInput {
    float price_features[64];
    float volume_features[64];
    float order_book_features[32];
    float timestamp_features[8];
};

struct ModelOutput {
    float alpha_signal;
    float volatility_estimate;
    float spread_estimate;
    float confidence;
};

class OnnxInferenceEngine {
public:
    OnnxInferenceEngine() noexcept : model_loaded_(false) {}

    bool load_model(const char* model_path) noexcept {
        std::snprintf(model_path_, sizeof(model_path_), "%s", model_path);
        model_loaded_ = true;
        return true;
    }

    ModelOutput infer(ModelInput& input) noexcept {
        if (!model_loaded_) return ModelOutput{0.0f, 0.0f, 0.0f, 0.0f};

        float price_mean = 0.0f;
        float volume_mean = 0.0f;
        float book_imbalance = 0.0f;
        float max_confidence = 0.0f;

        constexpr size_t PRICE_FEATURES = 64;
        for (size_t i = 0; i < PRICE_FEATURES; i++) {
            price_mean += input.price_features[i];
            if (input.price_features[i] > max_confidence)
                max_confidence = input.price_features[i];
        }
        price_mean /= static_cast<float>(PRICE_FEATURES);

        constexpr size_t VOLUME_FEATURES = 64;
        for (size_t i = 0; i < VOLUME_FEATURES; i++) {
            volume_mean += input.volume_features[i];
        }
        volume_mean /= static_cast<float>(VOLUME_FEATURES);

        constexpr size_t OB_FEATURES = 32;
        float bid_volume = 0.0f, ask_volume = 0.0f;
        for (size_t i = 0; i < OB_FEATURES / 2; i++) {
            bid_volume += input.order_book_features[i * 2];
            ask_volume += input.order_book_features[i * 2 + 1];
        }
        book_imbalance = (bid_volume + ask_volume > 0.0f)
            ? (bid_volume - ask_volume) / (bid_volume + ask_volume)
            : 0.0f;

        float alpha = price_mean * 0.4f + book_imbalance * 0.3f + volume_mean * 0.2f
                      + input.timestamp_features[0] * 0.1f;
        float volatility = std::fabs(price_mean - input.price_features[0])
                           / (input.price_features[0] + 0.001f);
        float spread = (price_mean > 0.0f) ? (1.0f / price_mean) * 100.0f : 0.0f;
        float confidence = std::fmin(max_confidence, 1.0f);

        return ModelOutput{alpha, volatility, spread, confidence};
    }

    bool is_loaded() const noexcept { return model_loaded_; }

private:
    char model_path_[256];
    bool model_loaded_;
};

}} // namespace quantum::ai

int main() {
    using namespace quantum::ai;

    OnnxInferenceEngine engine;
    if (!engine.load_model("models/alpha_model.onnx")) {
        std::printf("[AI] Failed to load model\n");
        return 1;
    }

    ModelInput input;
    std::memset(&input, 0, sizeof(input));

    for (size_t i = 0; i < 64; i++) {
        input.price_features[i] = 50000.0f + static_cast<float>(i) * 10.0f;
        input.volume_features[i] = 1000.0f + static_cast<float>(i) * 100.0f;
        if (i < 14) input.order_book_features[i] = 500.0f;
    }
    input.timestamp_features[0] = 0.5f;

    ModelOutput output = engine.infer(input);
    std::printf("[AI] Alpha=%.4f Volatility=%.4f Spread=%.4f Confidence=%.4f\n",
                output.alpha_signal, output.volatility_estimate,
                output.spread_estimate, output.confidence);

    return 0;
}
