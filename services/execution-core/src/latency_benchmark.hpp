// services/execution-core/src/latency_benchmark.hpp
// High-precision nanosecond-resolution latency benchmarking harness using TSC cycles and PTP.

#pragma once

#include <iostream>
#include <vector>
#include <algorithm>
#include <cmath>
#include <cstdint>

namespace quantum {
namespace benchmark {

class LatencyTracker {
public:
    explicit LatencyTracker(size_t max_samples) : max_samples_(max_samples), count_(0) {
        samples_ = new uint64_t[max_samples_];
    }

    ~LatencyTracker() {
        delete[] samples_;
    }

    // Direct RDTSCP inline asm implementation for timing ticks
    static inline uint64_t get_tsc() noexcept {
        uint64_t rax, rdx;
        __asm__ __volatile__("rdtscp" : "=a"(rax), "=d"(rdx) : : "rcx");
        return (rdx << 32) | rax;
    }

    inline void record_sample(uint64_t start_tsc, uint64_t end_tsc) noexcept {
        if (__builtin_expect(count_ < max_samples_, 1)) {
            samples_[count_++] = end_tsc - start_tsc;
        }
    }

    void print_stats(double tsc_freq_ghz) {
        if (count_ == 0) {
            std::cout << "[Benchmark] No samples recorded." << std::endl;
            return;
        }

        // Convert TSC cycles to nanoseconds
        std::vector<double> ns_samples;
        ns_samples.reserve(count_);
        for (size_t i = 0; i < count_; ++i) {
            ns_samples.push_back(static_cast<double>(samples_[i]) / tsc_freq_ghz);
        }

        std::sort(ns_samples.begin(), ns_samples.end());

        double p50 = ns_samples[static_cast<size_t>(count_ * 0.50)];
        double p99 = ns_samples[static_cast<size_t>(count_ * 0.99)];
        double p999 = ns_samples[static_cast<size_t>(count_ * 0.999)];
        double p9999 = ns_samples[static_cast<size_t>(count_ * 0.9999)];
        double max_lat = ns_samples[count_ - 1];

        std::cout << "====================================================" << std::endl;
        std::cout << "  LATENCY BENCHMARK RESULTS (TSC Freq: " << tsc_freq_ghz << " GHz)" << std::endl;
        std::cout << "====================================================" << std::endl;
        std::cout << "  Total Samples: " << count_ << std::endl;
        std::cout << "  p50 Latency:   " << p50 << " ns" << std::endl;
        std::cout << "  p99 Latency:   " << p99 << " ns" << std::endl;
        std::cout << "  p99.9 Latency: " << p999 << " ns" << std::endl;
        std::cout << "  p99.99 Latency:" << p9999 << " ns" << std::endl;
        std::cout << "  Max Latency:   " << max_lat << " ns" << std::endl;
        std::cout << "====================================================" << std::endl;
    }

private:
    size_t max_samples_;
    size_t count_;
    uint64_t* samples_;
};

} // namespace benchmark
} // namespace quantum
