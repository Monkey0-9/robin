#ifndef ROBIN_AI_ENGINE_ONNX_ADAPTER_HPP
#define ROBIN_AI_ENGINE_ONNX_ADAPTER_HPP

#include <cstddef>
#include <string>
#include <vector>
#include <cstdio>
#include <memory>

// Fallback linear model used when no .onnx file is available.
// Note: this is a lightweight header-only include of the signal model.
// In a real build you would #include "signal_model.hpp" instead.

namespace quantum { namespace ai {

class OnnxRuntimeAdapter {
public:
    OnnxRuntimeAdapter() = default;

    bool load_model(const std::string& onnx_path) {
        onnx_path_ = onnx_path;
#ifdef USE_ONNX_RUNTIME
        // TODO: Ort::SessionOptions session_options;
        // TODO: session_ = std::make_unique<Ort::Session>(env_, onnx_path.c_str(), session_options);
        // TODO: input_name_ = session_->GetInputName(0, allocator_);
        // TODO: output_name_ = session_->GetOutputName(0, allocator_);
        std::printf("[ONNX] Loading model from %s\n", onnx_path.c_str());
        return false; // Placeholder — replace when ONNX Runtime is linked
#else
        std::printf("[ONNX] ONNX Runtime not available. Use LinearSignalModel fallback.\n");
        return false;
#endif
    }

    bool is_loaded() const {
#ifdef USE_ONNX_RUNTIME
        return session_ != nullptr;
#else
        return false;
#endif
    }

    std::vector<float> run_inference(const std::vector<float>& input) {
#ifdef USE_ONNX_RUNTIME
        if (!session_) {
            std::fprintf(stderr, "[ONNX] No model loaded.\n");
            return {};
        }
        // TODO: Build Ort::Value from input, call session_->Run(...)
        // TODO: Extract and return float outputs
        std::printf("[ONNX] Inference stub — %zu floats in\n", input.size());
        return {}; // Placeholder
#else
        std::fprintf(stderr, "[ONNX] ONNX Runtime not compiled in.\n");
        return {};
#endif
    }

    const std::string& model_path() const { return onnx_path_; }

private:
    std::string onnx_path_;
#ifdef USE_ONNX_RUNTIME
    // Ort::Env env_;
    // std::unique_ptr<Ort::Session> session_;
    // Ort::AllocatorWithDefaultOptions allocator_;
    // std::string input_name_;
    // std::string output_name_;
#endif
};

}} // namespace quantum::ai

#endif // ROBIN_AI_ENGINE_ONNX_ADAPTER_HPP
