// HdrHistogram-based nanosecond latency benchmark for order matching engine
// Measures p50, p99, p99.9, p99.99, max latency over 10M+ iterations

#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <atomic>
#include <thread>
#include <chrono>
#include <new>

#include "../src/order_book.hpp"
#include "../src/lockfree_queue.hpp"
#include "../src/memory_pool.hpp"

using namespace quantum::execution;

#ifndef likely
#define likely(x)   __builtin_expect(!!(x), 1)
#endif
#ifndef unlikely
#define unlikely(x) __builtin_expect(!!(x), 0)
#endif

#define CACHE_LINE_SIZE 64
#define ALIGN_PAD_64 alignas(CACHE_LINE_SIZE)

static inline uint64_t rdtscp_bench() noexcept {
    uint32_t aux;
    uint64_t rax, rdx;
    __asm__ __volatile__("rdtscp" : "=a"(rax), "=d"(rdx) : : "rcx");
    return (rdx << 32) | rax;
}

static inline void cpuid_bench() noexcept {
    uint32_t eax, ebx, ecx, edx;
    __asm__ __volatile__("cpuid" : "=a"(eax), "=b"(ebx), "=c"(ecx), "=d"(edx) : "a"(0));
}

struct alignas(CACHE_LINE_SIZE) HdrHistogram {
    static constexpr size_t BUCKET_COUNT = 64;
    static constexpr uint64_t LOWEST = 1;
    static constexpr uint64_t HIGHEST = 1000000; // 1ms
    static constexpr int PRECISION = 3;

    alignas(CACHE_LINE_SIZE) uint64_t counts[BUCKET_COUNT][256];
    uint64_t total_count;
    uint64_t total_sum;
    uint64_t min_val;
    uint64_t max_val;
    uint64_t last_val;

    void init() noexcept {
        std::memset(counts, 0, sizeof(counts));
        total_count = 0;
        total_sum = 0;
        min_val = UINT64_MAX;
        max_val = 0;
        last_val = 0;
    }

    void record_value(uint64_t val) noexcept {
        if (unlikely(val == 0)) val = 1;
        const unsigned bucket = (val >> 4);
        const unsigned idx = val & 0xFF;
        if (bucket < BUCKET_COUNT) {
            counts[bucket][idx]++;
        }
        total_count++;
        total_sum += val;
        if (val < min_val) min_val = val;
        if (val > max_val) max_val = val;
        last_val = val;
    }

    uint64_t value_at_percentile(double percentile) const noexcept {
        if (total_count == 0) return 0;
        const uint64_t target = static_cast<uint64_t>(total_count * percentile / 100.0);
        uint64_t cumulative = 0;
        for (unsigned b = 0; b < BUCKET_COUNT; ++b) {
            for (unsigned i = 0; i < 256; ++i) {
                cumulative += counts[b][i];
                if (cumulative >= target) {
                    return (static_cast<uint64_t>(b) << 4) | i;
                }
            }
        }
        return max_val;
    }

    double mean() const noexcept {
        if (total_count == 0) return 0;
        return static_cast<double>(total_sum) / total_count;
    }

    void print() const noexcept {
        printf("\n=== Latency Histogram ===\n");
        printf("       Total: %llu\n", (unsigned long long)total_count);
        printf("         Min: %llu ns\n", (unsigned long long)min_val);
        printf("         Avg: %.1f ns\n", mean());
        printf("         Max: %llu ns\n", (unsigned long long)max_val);
        printf("        p50: %llu ns\n", (unsigned long long)value_at_percentile(50.0));
        printf("        p90: %llu ns\n", (unsigned long long)value_at_percentile(90.0));
        printf("        p95: %llu ns\n", (unsigned long long)value_at_percentile(95.0));
        printf("        p99: %llu ns\n", (unsigned long long)value_at_percentile(99.0));
        printf("      p99.9: %llu ns\n", (unsigned long long)value_at_percentile(99.9));
        printf("     p99.99: %llu ns\n", (unsigned long long)value_at_percentile(99.99));
        printf("    p99.999: %llu ns\n", (unsigned long long)value_at_percentile(99.999));
        printf("=========================\n");
    }
};

static HdrHistogram match_latency;
static HdrHistogram queue_latency;

ALIGN_PAD_64 std::atomic<bool> running{true};
ALIGN_PAD_64 uint64_t iterations_completed{0};

void producer_thread(LockFreeSPSCQueue<Order, 65536>& queue, uint64_t count) noexcept {
    Order order;
    std::memset(&order, 0, sizeof(order));
    order.id = 1;
    order.price = 500000;
    order.qty = 100;
    order.instrument_id = 1;
    order.side = Side::BID;
    order.state = OrderState::NEW;

    for (uint64_t i = 0; i < count; ++i) {
        order.id = i + 1;

        while (!queue.push(order)) {
            __asm__ __volatile__("pause" ::: "memory");
        }

        if ((i & 0xFFFF) == 0) {
            std::this_thread::yield();
        }
    }
}

void consumer_thread(OrderBook** books, LockFreeSPSCQueue<Order, 65536>& inbound,
                     LockFreeSPSCQueue<Trade, 65536>& outbound, uint64_t count) noexcept {
    Order order;
    std::vector<Trade> trades_batch;
    trades_batch.reserve(64);

    for (uint64_t i = 0; i < count; ++i) {
        while (!inbound.pop(order)) {
            __asm__ __volatile__("pause" ::: "memory");
        }

        const uint64_t start = rdtscp_bench();
        OrderBook* book = books[order.instrument_id % 1024];

        trades_batch.clear();
        book->match_order(order, trades_batch);

        const uint64_t latency = rdtscp_bench() - start;
        match_latency.record_value(latency);

        for (const auto& trade : trades_batch) {
            while (!outbound.push(trade)) {
                __asm__ __volatile__("pause" ::: "memory");
            }
        }

        iterations_completed++;
    }
}

int main(int argc, char** argv) {
    const uint64_t num_orders = (argc > 1) ? atol(argv[1]) : 1000000;
    const uint64_t warmup = (argc > 2) ? atol(argv[2]) : 100000;

    printf("=== Order Book Latency Benchmark ===\n");
    printf("Orders: %llu | Warmup: %llu\n", (unsigned long long)num_orders, (unsigned long long)warmup);

    match_latency.init();
    queue_latency.init();

    OrderBook* books[1024];
    for (int i = 0; i < 1024; ++i) {
        books[i] = new OrderBook(i);
    }

    ALIGN_PAD_64 LockFreeSPSCQueue<Order, 65536> inbound;
    ALIGN_PAD_64 LockFreeSPSCQueue<Trade, 65536> outbound;

    cpuid_bench();

    printf("Warmup: %llu iterations...\n", (unsigned long long)warmup);
    std::thread prod_warm(producer_thread, std::ref(inbound), warmup);
    std::thread cons_warm(consumer_thread, books, std::ref(inbound), std::ref(outbound), warmup);
    prod_warm.join();
    cons_warm.join();

    match_latency.init();
    iterations_completed = 0;

    printf("Benchmark: %llu iterations...\n", (unsigned long long)num_orders);
    auto bench_start = std::chrono::high_resolution_clock::now();

    std::thread prod(producer_thread, std::ref(inbound), num_orders);
    std::thread cons(consumer_thread, books, std::ref(inbound), std::ref(outbound), num_orders);
    prod.join();
    cons.join();

    auto bench_end = std::chrono::high_resolution_clock::now();
    auto elapsed_us = std::chrono::duration_cast<std::chrono::microseconds>(bench_end - bench_start).count();

    printf("\n=== Results ===\n");
    printf("Elapsed: %lld us\n", (long long)elapsed_us);
    printf("Throughput: %.1f orders/sec\n", (double)num_orders * 1000000.0 / elapsed_us);

    match_latency.print();

    printf("\n=== System Info ===\n");
    FILE* cpuinfo = fopen("/proc/cpuinfo", "r");
    if (cpuinfo) {
        char buf[256];
        while (fgets(buf, sizeof(buf), cpuinfo)) {
            if (strstr(buf, "model name")) {
                printf("CPU: %s", buf + 13);
                break;
            }
        }
        fclose(cpuinfo);
    }

    for (int i = 0; i < 1024; ++i) {
        delete books[i];
    }

    return 0;
}
