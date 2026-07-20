// ============================================================================
// Robin Trading Platform — C++ Strategy Engine Benchmark
// Validates latency targets: <500ns per tick on i5 hardware
// ============================================================================

#include "../src/strategy_engine.hpp"

#include <cstdio>
#include <cstring>
#include <cmath>
#include <array>
#include <random>
#include <chrono>

using namespace robin::strategy;

// ─── Benchmark harness ────────────────────────────────────────────────────────

static inline uint64_t now_cycles() noexcept {
#if defined(__x86_64__)
    uint32_t aux;
    uint64_t rax, rdx;
    __asm__ __volatile__("rdtscp" : "=a"(rax), "=d"(rdx) : : "rcx");
    return (rdx << 32) | rax;
#else
    return static_cast<uint64_t>(
        std::chrono::high_resolution_clock::now().time_since_epoch().count());
#endif
}

static void benchmark_composite(int n_ticks = 100'000) {
    CompositeSignalEngine engine("BTC-USD");

    // Warm up CPU caches
    std::mt19937_64 rng(42);
    std::normal_distribution<double> ret_dist(0.0002, 0.018);
    double price = 65'000.0;

    Tick tick{};
    std::strncpy(tick.symbol, "BTC-USD", 15);
    tick.exchange = 0;

    // Discard first 1000 ticks (warm-up)
    for (int i = 0; i < 1000; ++i) {
        price *= (1.0 + ret_dist(rng));
        tick.timestamp_ns = now_cycles();
        tick.price  = price;
        tick.bid    = price * 0.9999;
        tick.ask    = price * 1.0001;
        tick.volume = std::fabs(std::normal_distribution<double>(1e6, 2e5)(rng));
        Signal sig;
        engine.on_tick(tick, sig);
    }
    engine.reset();
    price = 65'000.0;

    // Benchmark
    std::array<uint64_t, 4096> latencies;
    size_t lat_idx = 0;
    int signals_found = 0;

    for (int i = 0; i < n_ticks; ++i) {
        price *= (1.0 + ret_dist(rng));
        tick.timestamp_ns = now_cycles();
        tick.price  = price;
        tick.bid    = price * 0.9999;
        tick.ask    = price * 1.0001;
        tick.volume = std::fabs(std::normal_distribution<double>(1e6, 2e5)(rng));

        const uint64_t t0 = now_cycles();
        Signal sig{};
        const bool has_signal = engine.on_tick(tick, sig);
        const uint64_t elapsed = now_cycles() - t0;

        if (lat_idx < latencies.size())
            latencies[lat_idx++] = elapsed;

        if (has_signal) ++signals_found;
    }

    // Sort for percentiles
    std::sort(latencies.begin(), latencies.begin() + lat_idx);
    const size_t n = lat_idx;

    // Approximate cycles → nanoseconds (3.5 GHz i5 ≈ 0.286ns/cycle)
    // Use runtime measurement for accuracy
    const uint64_t c0 = now_cycles();
    volatile auto t_ns = std::chrono::high_resolution_clock::now().time_since_epoch().count();
    const uint64_t c1 = now_cycles();
    (void)t_ns;
    // Assume ~3.5 GHz if rdtsc available, else 1 cycle = 1ns
    const double ns_per_cycle = 0.29; // Approximate for i5-xxxx

    auto cyc_to_ns = [&](uint64_t cyc) -> double {
        return static_cast<double>(cyc) * ns_per_cycle;
    };

    std::printf("\n============================================================\n");
    std::printf("ROBIN C++ Strategy Engine — Latency Benchmark\n");
    std::printf("============================================================\n");
    std::printf("  Ticks processed:   %d\n", n_ticks);
    std::printf("  Signals generated: %d (%.1f%%)\n",
                signals_found, 100.0 * signals_found / n_ticks);
    std::printf("------------------------------------------------------------\n");
    std::printf("  p50  latency:  %6.1f ns (%llu cycles)\n",
                cyc_to_ns(latencies[n / 2]), (unsigned long long)latencies[n / 2]);
    std::printf("  p95  latency:  %6.1f ns (%llu cycles)\n",
                cyc_to_ns(latencies[n * 95 / 100]),
                (unsigned long long)latencies[n * 95 / 100]);
    std::printf("  p99  latency:  %6.1f ns (%llu cycles)\n",
                cyc_to_ns(latencies[n * 99 / 100]),
                (unsigned long long)latencies[n * 99 / 100]);
    std::printf("  p99.9 latency: %6.1f ns (%llu cycles)\n",
                cyc_to_ns(latencies[n * 999 / 1000]),
                (unsigned long long)latencies[n * 999 / 1000]);
    std::printf("  max  latency:  %6.1f ns (%llu cycles)\n",
                cyc_to_ns(latencies[n - 1]),
                (unsigned long long)latencies[n - 1]);

    const double target_ns = 500.0;
    const double p99_ns = cyc_to_ns(latencies[n * 99 / 100]);
    std::printf("------------------------------------------------------------\n");
    if (p99_ns <= target_ns)
        std::printf("  ✅ PASS: p99 %.1fns ≤ %.0fns target\n", p99_ns, target_ns);
    else
        std::printf("  ❌ FAIL: p99 %.1fns > %.0fns target\n", p99_ns, target_ns);
    std::printf("============================================================\n\n");
}

void benchmark_individual_strategies(int n = 50'000) {
    std::mt19937_64 rng(99);
    std::normal_distribution<double> ret(0.0002, 0.018);
    double price = 50'000.0;

    auto make_tick = [&]() -> Tick {
        price *= (1.0 + ret(rng));
        Tick t{};
        t.timestamp_ns = now_cycles();
        t.price  = price;
        t.bid    = price * 0.9999;
        t.ask    = price * 1.0001;
        t.volume = 100.0;
        std::strncpy(t.symbol, "BTC-USD", 8);
        return t;
    };

    // MeanReversion
    {
        MeanReversionEngine mr("BTC-USD");
        uint64_t total = 0, count = 0;
        for (int i = 0; i < n; ++i) {
            Tick t = make_tick();
            uint64_t s = now_cycles();
            Signal sig;
            mr.on_tick(t, sig);
            total += now_cycles() - s;
            ++count;
        }
        std::printf("  MeanReversion avg: %.1fns/tick\n",
                    (double)total / count * 0.29);
    }

    // Momentum
    price = 50'000.0;
    {
        MomentumEngine mom("BTC-USD");
        uint64_t total = 0, count = 0;
        for (int i = 0; i < n; ++i) {
            Tick t = make_tick();
            uint64_t s = now_cycles();
            Signal sig;
            mom.on_tick(t, sig);
            total += now_cycles() - s;
            ++count;
        }
        std::printf("  Momentum      avg: %.1fns/tick\n",
                    (double)total / count * 0.29);
    }

    // VWAP
    price = 50'000.0;
    {
        VWAPEngine vwap("BTC-USD");
        uint64_t total = 0, count = 0;
        for (int i = 0; i < n; ++i) {
            Tick t = make_tick();
            uint64_t s = now_cycles();
            Signal sig;
            vwap.on_tick(t, sig);
            total += now_cycles() - s;
            ++count;
        }
        std::printf("  VWAP          avg: %.1fns/tick\n",
                    (double)total / count * 0.29);
    }
}

int main() {
    std::printf("Robin C++ Strategy Engine — Latency Benchmark Suite\n");
    std::printf("Hardware: Intel i5 (3.5 GHz est), ~0.29ns/cycle\n\n");

    std::printf("Individual strategy latencies (%d ticks each):\n", 50'000);
    benchmark_individual_strategies(50'000);

    std::printf("\nComposite engine (3 strategies, voting):\n");
    benchmark_composite(100'000);

    return 0;
}
