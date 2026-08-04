#pragma once

#include "order_state.hpp"
#include "lockfree_queue.hpp"
#include "memory_pool.hpp"
#include "order_book.hpp"
#include <atomic>
#include <thread>
#include <cstring>
#include <cstdio>
#include <cstdlib>
#include <memory>
#include <array>
#include <vector>
#include <cstddef>
#if defined(__x86_64__) || defined(_M_X64) || defined(__i386__)
#include <immintrin.h>
#endif
#ifdef _MSC_VER
#include <intrin.h>
#endif

#ifdef _WIN32
#include <malloc.h>
#else
#include <pthread.h>
#include <sched.h>
#include <numa.h>
#include <numaif.h>
#endif

#ifndef likely
#ifdef _MSC_VER
#define likely(x) (x)
#else
#define likely(x)   __builtin_expect(!!(x), 1)
#endif
#endif
#ifndef unlikely
#ifdef _MSC_VER
#define unlikely(x) (x)
#else
#define unlikely(x) __builtin_expect(!!(x), 0)
#endif
#endif

#define CACHE_LINE_SIZE 64
#define ALIGN_PAD_64 alignas(CACHE_LINE_SIZE)

namespace quantum {
namespace execution {

struct alignas(64) EngineStats {
    uint64_t orders_submitted;
    uint64_t trades_executed;
    uint64_t orders_rejected;
    uint64_t orders_cancelled;
    uint64_t total_latency_ns;
    uint64_t max_latency_ns;
    uint64_t min_latency_ns;
    uint64_t cycle_count;
    char pad_[8];
};

class MatchingEngine {
public:
    static constexpr size_t INBOUND_CAPACITY = 65536;
    static constexpr size_t OUTBOUND_CAPACITY = 65536;
    static constexpr size_t BOOK_COUNT = 1024;

    MatchingEngine() noexcept : running_(false), numa_node_(0), cpu_core_(2) {
        std::memset(&stats_, 0, sizeof(stats_));
        stats_.min_latency_ns = UINT64_MAX;
        // allocate book storage on the heap to avoid exceeding static array size limits on MSVC
        book_storage_.reset(new std::array<std::byte, sizeof(OrderBook)>[BOOK_COUNT]);
        for (auto& book : books_) book = nullptr;
    }

    ~MatchingEngine() noexcept { stop(); }

    bool init(uint32_t numa_node = 0, int cpu_core = 2) noexcept {
        numa_node_ = numa_node;
        cpu_core_ = cpu_core;
        pin_to_cpu(cpu_core);
        bind_to_numa_node(numa_node);
        return true;
    }

    bool submit_order(const Order& order) noexcept {
        const uint64_t start = rdtscp();
        bool result = inbound_queue_.push(order);
        if (unlikely(!result)) stats_.orders_rejected++;
        return result;
    }

    bool poll_trade(Trade& trade) noexcept { return outbound_queue_.pop(trade); }

    void start() noexcept {
        running_ = true;
        matching_thread_ = std::thread(&MatchingEngine::run_matching_loop, this);
    }

    void stop() noexcept {
        running_ = false;
        if (matching_thread_.joinable()) matching_thread_.join();
    }

    const EngineStats& stats() const noexcept { return stats_; }

    OrderBook* get_or_create_book(uint32_t instrument_id) noexcept {
        const size_t idx = instrument_id % BOOK_COUNT;
        if (unlikely(!books_[idx])) {
            void* mem = static_cast<void*>(book_storage_[idx].data());
            books_[idx] = new (mem) OrderBook(instrument_id);
        }
        return books_[idx];
    }

private:
    void pin_to_cpu(int cpu) noexcept {
#ifdef __linux__
        cpu_set_t cpuset;
        CPU_ZERO(&cpuset);
        CPU_SET(cpu, &cpuset);
        pthread_setaffinity_np(pthread_self(), sizeof(cpu_set_t), &cpuset);
#endif
    }

    void bind_to_numa_node(uint32_t node) noexcept {
#ifdef __linux__
        struct bitmask *nodemask = numa_allocate_nodemask();
        if (nodemask) {
            numa_bitmask_setbit(nodemask, node);
            numa_bind(nodemask);
            numa_free_nodemask(nodemask);
        }
#endif
    }

    void run_matching_loop() noexcept {
        Order incoming;
        while (running_) {
            if (inbound_queue_.pop(incoming)) {
                const uint64_t start_ns = rdtscp();
                OrderBook* book = get_or_create_book(incoming.instrument_id);
                if (unlikely(!book)) { stats_.orders_rejected++; continue; }

                if (unlikely(incoming.type == OrderType::CANCEL)) {
                    if (book->cancel_order(incoming.id, incoming.price, incoming.side)) {
                        stats_.orders_cancelled++;
                    }
                    continue;
                }

                FixedVector<Trade, 64> matches;
                if (unlikely(!book->match_order(incoming, matches))) {
                    stats_.orders_rejected++;
                    continue;
                }
                for (size_t i = 0; i < matches.size(); ++i)
                    outbound_queue_.push(matches[i]);
                const uint64_t end_ns = rdtscp();
                const uint64_t latency = end_ns - start_ns;
                stats_.orders_submitted++;
                stats_.trades_executed += matches.size();
                stats_.total_latency_ns += latency;
                stats_.cycle_count++;
                if (latency > stats_.max_latency_ns) stats_.max_latency_ns = latency;
                if (latency < stats_.min_latency_ns) stats_.min_latency_ns = latency;
            } else {
                _mm_pause();
            }
        }
    }

    static inline uint64_t rdtscp() noexcept {
#ifdef _MSC_VER
        unsigned int aux;
        return __rdtscp(&aux);
#elif defined(__x86_64__) || defined(_M_X64) || defined(__i386__)
        uint32_t aux;
        uint64_t rax, rdx;
        __asm__ __volatile__("rdtscp" : "=a"(rax), "=d"(rdx), "=c"(aux));
        return (rdx << 32) | rax;
#else
        return static_cast<uint64_t>(
            std::chrono::high_resolution_clock::now().time_since_epoch().count());
#endif
    }

    ALIGN_PAD_64 std::atomic<bool> running_;
    int numa_node_;
    int cpu_core_;
    ALIGN_PAD_64 std::thread matching_thread_;
    ALIGN_PAD_64 LockFreeSPSCQueue<Order, INBOUND_CAPACITY> inbound_queue_;
    ALIGN_PAD_64 LockFreeSPSCQueue<Trade, OUTBOUND_CAPACITY> outbound_queue_;
    /* heap-allocated byte storage to avoid 5.3GB embedded array;
       placement-new constructs OrderBook instances on demand. */
    std::unique_ptr<std::array<std::byte, sizeof(OrderBook)>[]> book_storage_;
    OrderBook* books_[BOOK_COUNT];
    ALIGN_PAD_64 EngineStats stats_;
};

}} // namespace
