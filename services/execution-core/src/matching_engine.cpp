#include "order_state.hpp"
#include "lockfree_queue.hpp"
#include "memory_pool.hpp"
#include "order_book.hpp"
#include <atomic>
#include <thread>
#include <cstring>
#include <cstdio>
#include <cstdlib>

#if defined(_WIN32)
#include <malloc.h>
#elif defined(__linux__)
#include <pthread.h>
#include <sched.h>
#include <numa.h>
#include <numaif.h>
#endif

#ifndef likely
#define likely(x)   __builtin_expect(!!(x), 1)
#endif
#ifndef unlikely
#define unlikely(x) __builtin_expect(!!(x), 0)
#endif
#define CACHE_LINE_SIZE 64
#define ALIGN_PAD_64 alignas(CACHE_LINE_SIZE)

static inline uint64_t rdtscp_e() noexcept {
    uint32_t aux; uint64_t rax, rdx;
    __asm__ __volatile__("rdtscp" : "=a"(rax), "=d"(rdx) : : "rcx");
    return (rdx << 32) | rax;
}

namespace quantum { namespace execution {

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

    MatchingEngine() noexcept : running_(false), numa_node_(0) {
        std::memset(&stats_, 0, sizeof(stats_));
        stats_.min_latency_ns = UINT64_MAX;
        std::memset(books_, 0, sizeof(books_));
    }

    ~MatchingEngine() noexcept {
        stop();
        for (auto& book : books_) {
            if (book) {
                book->~OrderBook();
                std::free(book);
                book = nullptr;
            }
        }
    }

    bool init(uint32_t numa_node = 0) noexcept {
        numa_node_ = numa_node;
        pin_to_numa_node(numa_node);
        return true;
    }

    bool submit_order(const Order& order) noexcept {
        const uint64_t start = rdtscp_e();
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
            void* mem = nullptr;
#if defined(_WIN32)
            mem = _aligned_malloc(sizeof(OrderBook), CACHE_LINE_SIZE);
#elif defined(__linux__)
            mem = std::aligned_alloc(CACHE_LINE_SIZE, sizeof(OrderBook));
#else
            if (posix_memalign(&mem, CACHE_LINE_SIZE, sizeof(OrderBook)) != 0) mem = nullptr;
#endif
            if (unlikely(!mem)) return nullptr;
            books_[idx] = new (mem) OrderBook(instrument_id);
        }
        return books_[idx];
    }

private:
    void pin_to_numa_node(int node) noexcept {
#if defined(__linux__)
        cpu_set_t cpuset;
        CPU_ZERO(&cpuset);
        CPU_SET(2, &cpuset);
        pthread_setaffinity_np(pthread_self(), sizeof(cpu_set_t), &cpuset);
        struct bitmask* numa_mask = numa_allocate_cpumask();
        if (numa_node_to_cpus(node, numa_mask) == 0)
            numa_sched_setaffinity(0, numa_mask);
        numa_free_cpumask(numa_mask);
#endif
    }

    void run_matching_loop() noexcept {
        Order incoming;
        while (running_) {
            if (inbound_queue_.pop(incoming)) {
                const uint64_t start_ns = rdtscp_e();
                OrderBook* book = get_or_create_book(incoming.instrument_id);
                if (unlikely(!book)) { stats_.orders_rejected++; continue; }
                std::vector<Trade> matches;
                book->match_order(incoming, matches);
                for (const auto& trade : matches)
                    outbound_queue_.push(trade);
                const uint64_t end_ns = rdtscp_e();
                const uint64_t latency = end_ns - start_ns;
                stats_.orders_submitted++;
                stats_.trades_executed += matches.size();
                stats_.total_latency_ns += latency;
                stats_.cycle_count++;
                if (latency > stats_.max_latency_ns) stats_.max_latency_ns = latency;
                if (latency < stats_.min_latency_ns) stats_.min_latency_ns = latency;
            } else {
                __asm__ __volatile__("pause" ::: "memory");
            }
        }
    }

    ALIGN_PAD_64 std::atomic<bool> running_;
    int numa_node_;
    ALIGN_PAD_64 std::thread matching_thread_;
    ALIGN_PAD_64 LockFreeSPSCQueue<Order, INBOUND_CAPACITY> inbound_queue_;
    ALIGN_PAD_64 LockFreeSPSCQueue<Trade, OUTBOUND_CAPACITY> outbound_queue_;
    ALIGN_PAD_64 OrderBook* books_[BOOK_COUNT];
    ALIGN_PAD_64 EngineStats stats_;
};

}} // namespace quantum::execution

int main() {
    using namespace quantum::execution;
    MatchingEngine engine;
    engine.init(0);
    engine.start();
    Order order;
    std::memset(&order, 0, sizeof(order));
    order.id = 1; order.price = 500000; order.qty = 100;
    order.instrument_id = 1; order.side = Side::BID;
    order.state = OrderState::NEW;
    engine.submit_order(order);
    for (int i = 0; i < 1000000 && engine.stats().orders_submitted == 0 && engine.stats().orders_rejected == 0; ++i) {
        std::this_thread::yield();
    }
    engine.stop();
    const auto& s = engine.stats();
    std::printf("[ENGINE] Orders=%llu Trades=%llu Rejected=%llu AvgLat=%llu ns\n",
           (unsigned long long)s.orders_submitted, (unsigned long long)s.trades_executed,
           (unsigned long long)s.orders_rejected,
           s.cycle_count ? (unsigned long long)(s.total_latency_ns / s.cycle_count) : 0);
    return 0;
}
