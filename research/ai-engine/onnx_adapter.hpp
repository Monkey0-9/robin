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
        try {
            Ort::SessionOptions session_options;
            session_options.SetIntraOpNumThreads(1);
            session_options.SetGraphOptimizationLevel(GraphOptimizationLevel::ORT_ENABLE_ALL);
            
            #if defined(_WIN32)
            std::wstring wide_path(onnx_path.begin(), onnx_path.end());
            session_ = std::make_unique<Ort::Session>(env_, wide_path.c_str(), session_options);
            #else
            session_ = std::make_unique<Ort::Session>(env_, onnx_path.c_str(), session_options);
            #endif

            auto input_name_ptr = session_->GetInputNameAllocated(0, allocator_);
            input_name_ = input_name_ptr.get();
            
            auto output_name_ptr = session_->GetOutputNameAllocated(0, allocator_);
            output_name_ = output_name_ptr.get();
            
            std::printf("[ONNX] Successfully loaded model from %s\n", onnx_path.c_str());
            return true;
        } catch (const std::exception& e) {
            std::fprintf(stderr, "[ONNX] Exception during load_model: %s\n", e.what());
            return false;
        }
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
        try {
            std::vector<int64_t> input_shape = {1, static_cast<int64_t>(input.size())};
            
            auto memory_info = Ort::MemoryInfo::CreateCpu(OrtArenaAllocator, OrtMemTypeDefault);
            Ort::Value input_tensor = Ort::Value::CreateTensor<float>(
                memory_info,
                const_cast<float*>(input.data()),
                input.size(),
                input_shape.data(),
                input_shape.size()
            );

            const char* input_names[] = {input_name_.c_str()};
            const char* output_names[] = {output_name_.c_str()};

            auto output_tensors = session_->Run(
                Ort::RunOptions{nullptr},
                input_names,
                &input_tensor,
                1,
                output_names,
                1
            );

            float* floatarr = output_tensors.front().GetTensorMutableData<float>();
            size_t output_count = output_tensors.front().GetTensorTypeAndShapeInfo().GetElementCount();
            
            std::vector<float> results(floatarr, floatarr + output_count);
            return results;
        } catch (const std::exception& e) {
            std::fprintf(stderr, "[ONNX] Exception during run_inference: %s\n", e.what());
            return {};
        }
#else
        std::fprintf(stderr, "[ONNX] ONNX Runtime not compiled in.\n");
        return {};
#endif
    }

    const std::string& model_path() const { return onnx_path_; }

private:
    std::string onnx_path_;
#ifdef USE_ONNX_RUNTIME
    Ort::Env env_{ORT_LOGGING_LEVEL_WARNING, "RobinONNX"};
    std::unique_ptr<Ort::Session> session_;
    Ort::AllocatorWithDefaultOptions allocator_;
    std::string input_name_;
    std::string output_name_;
#endif
};

}} // namespace quantum::ai

#endif // ROBIN_AI_ENGINE_ONNX_ADAPTER_HPP
