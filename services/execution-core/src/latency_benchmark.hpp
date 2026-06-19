#pragma once

#include <iostream>
#include <algorithm>
#include <cmath>
#include <cstdint>
#include <array>

namespace quantum {
namespace benchmark {

template <size_t MaxSamples = 1000000>
class LatencyTracker {
public:
    LatencyTracker() : count_(0) {}

    static inline uint64_t get_tsc() noexcept {
        uint64_t rax, rdx;
        __asm__ __volatile__("rdtscp" : "=a"(rax), "=d"(rdx) : : "rcx");
        return (rdx << 32) | rax;
    }

    inline void record_sample(uint64_t start_tsc, uint64_t end_tsc) noexcept {
        if (__builtin_expect(count_ < MaxSamples, 1)) {
            samples_[count_++] = end_tsc - start_tsc;
        }
    }

    void print_stats(double tsc_freq_ghz) {
        if (count_ == 0) {
            std::cout << "[Benchmark] No samples recorded." << std::endl;
            return;
        }

        std::sort(samples_.begin(), samples_.begin() + count_);

        double p50 = static_cast<double>(samples_[static_cast<size_t>(count_ * 0.50)]) / tsc_freq_ghz;
        double p99 = static_cast<double>(samples_[static_cast<size_t>(count_ * 0.99)]) / tsc_freq_ghz;
        double p999 = static_cast<double>(samples_[static_cast<size_t>(count_ * 0.999)]) / tsc_freq_ghz;
        double p9999 = static_cast<double>(samples_[static_cast<size_t>(count_ * 0.9999)]) / tsc_freq_ghz;
        double max_lat = static_cast<double>(samples_[count_ - 1]) / tsc_freq_ghz;

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
    size_t count_;
    std::array<uint64_t, MaxSamples> samples_;
};

} // namespace benchmark
} // namespace quantum
