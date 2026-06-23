#pragma once

#include <iostream>
#include <algorithm>
#include <cmath>
#include <cstdint>
#include <vector>

namespace quantum {
namespace benchmark {

template <size_t MaxSamples = 1000000>
class LatencyTracker {
public:
    LatencyTracker() : count_(0), max_latency_(0) {
        std::fill(buckets_.begin(), buckets_.end(), 0);
    }

    static inline uint64_t get_tsc() noexcept {
        uint64_t rax, rdx;
        __asm__ __volatile__("rdtscp" : "=a"(rax), "=d"(rdx) : : "rcx");
        return (rdx << 32) | rax;
    }

    inline void record_sample(uint64_t start_tsc, uint64_t end_tsc) noexcept {
        uint64_t latency = end_tsc - start_tsc;
        count_++;
        if (latency > max_latency_) max_latency_ = latency;
        
        // Fast bucket index: scale down by 8 (to keep resolution tight at low end)
        // Max bucket = 65535, covering up to 65535 * 8 = 524280 TSC ticks
        size_t bucket = latency >> 3;
        if (bucket >= BUCKET_COUNT) {
            bucket = BUCKET_COUNT - 1;
        }
        buckets_[bucket]++;
    }

    void print_stats(double tsc_freq_ghz) {
        if (count_ == 0) {
            std::cout << "[Benchmark] No samples recorded." << std::endl;
            return;
        }

        size_t p50_idx = count_ * 0.50;
        size_t p99_idx = count_ * 0.99;
        size_t p999_idx = count_ * 0.999;
        size_t p9999_idx = count_ * 0.9999;

        uint64_t p50_lat = 0, p99_lat = 0, p999_lat = 0, p9999_lat = 0;
        size_t cumulative = 0;

        for (size_t i = 0; i < BUCKET_COUNT; i++) {
            cumulative += buckets_[i];
            if (p50_lat == 0 && cumulative >= p50_idx) p50_lat = i << 3;
            if (p99_lat == 0 && cumulative >= p99_idx) p99_lat = i << 3;
            if (p999_lat == 0 && cumulative >= p999_idx) p999_lat = i << 3;
            if (p9999_lat == 0 && cumulative >= p9999_idx) p9999_lat = i << 3;
        }

        double p50 = static_cast<double>(p50_lat) / tsc_freq_ghz;
        double p99 = static_cast<double>(p99_lat) / tsc_freq_ghz;
        double p999 = static_cast<double>(p999_lat) / tsc_freq_ghz;
        double p9999 = static_cast<double>(p9999_lat) / tsc_freq_ghz;
        double max_lat = static_cast<double>(max_latency_) / tsc_freq_ghz;

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
    static constexpr size_t BUCKET_COUNT = 65536;
    size_t count_;
    uint64_t max_latency_;
    std::array<uint32_t, BUCKET_COUNT> buckets_;
};

} // namespace benchmark
} // namespace quantum
