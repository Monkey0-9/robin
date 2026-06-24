#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <vector>
#include <string>
#include <cmath>
#include <chrono>
#include <algorithm>
#include <fstream>

// ============================================================================
// ONNX Inference Engine — Real ONNX Runtime integration with fallback
// ============================================================================
//
// This implements proper ONNX Runtime C++ API integration using Ort::Session.
// When USE_ONNX_RUNTIME is defined and an .onnx model file is provided, it
// loads and runs the model via ONNX Runtime. Otherwise, it falls back to a
// linear signal model (the same algorithm as LinearSignalModel).
//
// Environment variables:
//   ROBIN_ONNX_MODEL_PATH   — path to .onnx model file (optional)
//   ROBIN_ONNX_USE_CPU      — set to "1" to force CPU execution provider
//
// Build with ONNX Runtime:
//   g++ -O3 onnx_inference.cpp -lonnxruntime -I/path/to/onnxruntime/include
//     -L/path/to/onnxruntime/lib -o onnx_inference
//
// Build without ONNX Runtime (linear fallback):
//   g++ -O3 onnx_inference.cpp -o onnx_inference
// ============================================================================

// Conditional ONNX Runtime headers
#ifdef USE_ONNX_RUNTIME
#include <onnxruntime_cxx_api.h>
#endif

// ============================================================================
// Model types
// ============================================================================

struct ModelInput {
    float price_features[64];
    float volume_features[64];
    float order_book_features[32];
    float timestamp_features[8];
    float macro_features[8];
};

struct ModelOutput {
    float alpha_signal;
    float volatility_estimate;
    float spread_estimate;
    float confidence;
};

// ============================================================================
// Linear fallback model (same algorithm as LinearSignalModel)
// ============================================================================

static ModelOutput linear_fallback(const ModelInput& input) {
    ModelOutput out;

    // Price momentum
    float price_sum = 0.0f, price_max = 0.0f;
    for (size_t i = 0; i < 64; i++) {
        price_sum += input.price_features[i];
        if (input.price_features[i] > price_max) price_max = input.price_features[i];
    }
    float price_mean = price_sum / 64.0f;
    float price_momentum = (price_mean > 0.0f && input.price_features[0] > 0.0f)
        ? (price_mean - input.price_features[0]) / (input.price_features[0] + 1e-6f)
        : 0.0f;

    // Volume pressure
    float vol_sum = 0.0f;
    for (size_t i = 0; i < 64; i++) vol_sum += input.volume_features[i];
    float volume_pressure = std::tanh((vol_sum / 64.0f) / (vol_sum / 64.0f + 1000.0f + 1e-6f));

    // Order book imbalance
    float bid_vol = 0.0f, ask_vol = 0.0f;
    for (size_t i = 0; i < 16; i++) {
        bid_vol += input.order_book_features[i * 2];
        ask_vol += input.order_book_features[i * 2 + 1];
    }
    float ob_imbalance = (bid_vol + ask_vol > 1e-6f)
        ? (bid_vol - ask_vol) / (bid_vol + ask_vol)
        : 0.0f;

    // Intraday component
    float intraday = input.timestamp_features[0] * 2.0f - 1.0f;

    // Composite signal
    const float w_p = 0.40f, w_ob = 0.30f, w_v = 0.20f, w_t = 0.10f;
    float raw_alpha = w_p * price_momentum + w_ob * ob_imbalance + w_v * volume_pressure + w_t * intraday;
    out.alpha_signal = std::fmax(-1.0f, std::fmin(1.0f, raw_alpha));
    out.volatility_estimate = (input.price_features[0] > 0.0f)
        ? std::fabs(price_max - input.price_features[0]) / input.price_features[0]
        : 0.0f;
    out.spread_estimate = (price_mean > 0.0f) ? (1.0f / price_mean) * 10000.0f : 0.0f;
    out.confidence = std::fmin(1.0f, std::fabs(out.alpha_signal) / (out.volatility_estimate + 0.01f));
    return out;
}

// ============================================================================
// ONNX Runtime inference engine
// ============================================================================

class OnnxInferenceEngine {
public:
    OnnxInferenceEngine() : use_onnx_(false), inference_count_(0) {}

    bool load(const std::string& model_path) {
        model_path_ = model_path;

#ifdef USE_ONNX_RUNTIME
        if (model_path.empty()) {
            std::printf("[ONNX] No model path provided — using linear fallback\n");
            return false;
        }

        // Check file exists
        std::ifstream f(model_path);
        if (!f.good()) {
            std::printf("[ONNX] Model file not found: %s — using linear fallback\n", model_path.c_str());
            return false;
        }

        try {
            env_ = std::make_unique<Ort::Env>(ORT_LOGGING_LEVEL_WARNING, "robin-ai");

            Ort::SessionOptions session_options;
            session_options.SetIntraOpNumThreads(1);
            session_options.SetGraphOptimizationLevel(GraphOptimizationLevel::ORT_ENABLE_EXTENDED);

#ifdef _WIN32
            // Use CPU only on Windows for compatibility
            session_options.DisableCpuEpFallback();
#else
            const char* use_cpu = std::getenv("ROBIN_ONNX_USE_CPU");
            if (use_cpu && use_cpu[0] == '1') {
                session_options.DisableCpuEpFallback();
            }
#endif

            session_ = std::make_unique<Ort::Session>(*env_, model_path.c_str(), session_options);
            use_onnx_ = true;

            Ort::AllocatorWithDefaultOptions allocator;
            input_name_ = session_->GetInputNameAllocated(0, allocator).get();
            output_name_ = session_->GetOutputNameAllocated(0, allocator).get();

            auto input_type_info = session_->GetInputTypeInfo(0);
            auto input_tensor_info = input_type_info.GetTensorTypeAndShapeInfo();
            input_dims_ = input_tensor_info.GetShape();

            auto output_type_info = session_->GetOutputTypeInfo(0);
            auto output_tensor_info = output_type_info.GetTensorTypeAndShapeInfo();
            output_dims_ = output_tensor_info.GetShape();

            std::printf("[ONNX] Loaded model: %s\n", model_path.c_str());
            std::printf("[ONNX]   Input dims: ");
            for (auto d : input_dims_) std::printf("%ld ", d);
            std::printf("\n[ONNX]   Output dims: ");
            for (auto d : output_dims_) std::printf("%ld ", d);
            std::printf("\n");

            return true;
        } catch (const Ort::Exception& e) {
            std::fprintf(stderr, "[ONNX] Failed to load model: %s\n", e.what());
            use_onnx_ = false;
            return false;
        }
#else
        std::printf("[ONNX] ONNX Runtime not compiled in. Using linear fallback.\n");
        (void)model_path;
        return false;
#endif
    }

    ModelOutput run_inference(const ModelInput& input) {
        inference_count_++;

        if (use_onnx_) {
#ifdef USE_ONNX_RUNTIME
            auto start = std::chrono::high_resolution_clock::now();

            try {
                // Prepare input tensor (flatten ModelInput to 1D float array)
                const float* input_data = reinterpret_cast<const float*>(&input);
                size_t total_input_size = sizeof(ModelInput) / sizeof(float);

                std::vector<int64_t> actual_input_dims = {1, static_cast<int64_t>(total_input_size)};
                Ort::MemoryInfo memory_info = Ort::MemoryInfo::CreateCpu(OrtArenaAllocator, OrtMemTypeDefault);
                Ort::Value input_tensor = Ort::Value::CreateTensor<float>(
                    memory_info, const_cast<float*>(input_data), total_input_size,
                    actual_input_dims.data(), actual_input_dims.size()
                );

                const char* input_names[] = {input_name_.c_str()};
                const char* output_names[] = {output_name_.c_str()};
                Ort::Value output_tensor = session_->Run(
                    Ort::RunOptions{nullptr},
                    input_names, &input_tensor, 1,
                    output_names, 1
                );

                float* output_data = output_tensor.GetTensorMutableData<float>();
                auto output_info = output_tensor.GetTensorTypeAndShapeInfo();
                size_t output_count = output_info.GetElementCount();

                if (output_count >= 4) {
                    auto end = std::chrono::high_resolution_clock::now();
                    auto latency = std::chrono::duration_cast<std::chrono::nanoseconds>(end - start).count();
                    if (inference_count_ % 1000 == 0) {
                        std::printf("[ONNX] %lu inferences. Last latency: %ld ns\n",
                                    inference_count_, latency);
                    }
                    return {output_data[0], output_data[1], output_data[2], output_data[3]};
                }
            } catch (const Ort::Exception& e) {
                std::fprintf(stderr, "[ONNX] Inference error: %s\n", e.what());
                // Fall through to linear fallback
            }
#endif
        }

        // Fallback to linear model
        auto start = std::chrono::high_resolution_clock::now();
        ModelOutput out = linear_fallback(input);
        auto end = std::chrono::high_resolution_clock::now();
        auto latency = std::chrono::duration_cast<std::chrono::nanoseconds>(end - start).count();
        if (inference_count_ % 10000 == 0) {
            std::printf("[AI] 10k inferences (linear fallback). Last latency: %ld ns\n", latency);
        }
        return out;
    }

    bool is_onnx_loaded() const { return use_onnx_; }
    uint64_t inference_count() const { return inference_count_; }

private:
    bool use_onnx_;
    std::string model_path_;
    uint64_t inference_count_;

#ifdef USE_ONNX_RUNTIME
    std::unique_ptr<Ort::Env> env_;
    std::unique_ptr<Ort::Session> session_;
    std::string input_name_;
    std::string output_name_;
    std::vector<int64_t> input_dims_;
    std::vector<int64_t> output_dims_;
#endif
};

int main() {
    std::printf("=== ONNX Inference Engine Demo ===\n");

    const char* model_path = std::getenv("ROBIN_ONNX_MODEL_PATH");
    if (!model_path) model_path = "";

    OnnxInferenceEngine engine;
    engine.load(model_path);

    if (engine.is_onnx_loaded()) {
        std::printf("[DEMO] Running with ONNX Runtime backend\n");
    } else {
        std::printf("[DEMO] Running with linear fallback backend\n");
    }

    ModelInput input;
    std::memset(&input, 0, sizeof(input));

    // Synthetic market data: rising price with buy-side volume pressure
    for (size_t i = 0; i < 64; i++) {
        input.price_features[i]  = 50000.0f + static_cast<float>(i) * 10.0f;
        input.volume_features[i] = 1000.0f  + static_cast<float>(i) * 100.0f;
    }
    for (size_t i = 0; i < 14; i++) {
        input.order_book_features[i * 2]     = 800.0f;  // bid
        input.order_book_features[i * 2 + 1] = 500.0f;  // ask
    }
    input.timestamp_features[0] = 0.5f;
    input.macro_features[0] = 0.02f; // VIX-like vol

    ModelOutput out = engine.run_inference(input);
    std::printf("[DEMO] Alpha=%.4f Vol=%.4f SpreadBps=%.4f Conf=%.4f\n",
                out.alpha_signal, out.volatility_estimate,
                out.spread_estimate, out.confidence);

    // Benchmark
    constexpr int ITERATIONS = 100000;
    auto start = std::chrono::high_resolution_clock::now();
    for (int i = 0; i < ITERATIONS; i++) {
        input.price_features[0] = 50000.0f + static_cast<float>(i % 1000) * 10.0f;
        engine.run_inference(input);
    }
    auto end = std::chrono::high_resolution_clock::now();
    auto total = std::chrono::duration_cast<std::chrono::nanoseconds>(end - start).count();
    std::printf("[DEMO] Benchmark: %d inferences in %.2f ms (avg %.0f ns/inference)\n",
                ITERATIONS, total / 1e6, static_cast<double>(total) / ITERATIONS);

    return 0;
}
