// services/execution-core/src/matching_engine.cpp
// Lock-free execution matching engine pinned to NUMA core for sub-microsecond latency.

#include "order_state.hpp"
#include "lockfree_queue.hpp"
#include "memory_pool.hpp"
#include "order_book.hpp"
#include <unordered_map>
#include <iostream>
#include <thread>
#include <chrono>

#if defined(__linux__)
#include <pthread.h>
#endif

namespace quantum {
namespace execution {

class MatchingEngine {
public:
    MatchingEngine() : running_(false) {
        pin_to_numa_node();
    }

    ~MatchingEngine() {
        stop();
    }

    void pin_to_numa_node() {
#if defined(__linux__)
        cpu_set_t cpuset;
        CPU_ZERO(&cpuset);
        CPU_SET(2, &cpuset); // Pin matching engine to core 2 to isolate cache
        pthread_setaffinity_np(pthread_self(), sizeof(cpu_set_t), &cpuset);
        std::cout << "[Matching Engine] Pinned to isolated CPU Core 2" << std::endl;
#endif
    }

    void start() {
        running_ = true;
        matching_thread_ = std::thread(&MatchingEngine::run_matching_loop, this);
    }

    void stop() {
        running_ = false;
        if (matching_thread_.joinable()) {
            matching_thread_.join();
        }
    }

    bool submit_order(const Order& order) {
        return inbound_queue_.push(order);
    }

    bool poll_trade(Trade& trade) {
        return outbound_queue_.pop(trade);
    }

private:
    void run_matching_loop() {
        Order incoming_order;
        while (running_) {
            if (inbound_queue_.pop(incoming_order)) {
                // Pre-allocate matching structures using MemoryPool
                Order* pooled_order = order_pool_.allocate();
                if (!pooled_order) {
                    std::cerr << "[Matching Engine] Critical: Memory Pool exhausted!" << std::endl;
                    continue;
                }

                *pooled_order = incoming_order;
                process_limit_order(pooled_order);
                order_pool_.deallocate(pooled_order);
            } else {
                std::this_thread::yield(); // Backpressure spin yield
            }
        }
    }

    void process_limit_order(Order* order) {
        // Core price matching logic
        auto book_it = books_.find(order->instrument_id);
        if (book_it == books_.end()) {
            books_.emplace(order->instrument_id, OrderBook(order->instrument_id));
            book_it = books_.find(order->instrument_id);
        }

        OrderBook& book = book_it->second;
        std::vector<Trade> matches;
        
        // Execute price-time priority crossing
        book.match_order(*order, matches);

        for (const auto& trade : matches) {
            outbound_queue_.push(trade);
            dispatch_audit_trail(trade);
        }
    }

    void dispatch_audit_trail(const Trade& trade) {
        // Drop-copy mock function for immutable logs
    }

    std::atomic<bool> running_;
    std::thread matching_thread_;

    LockFreeSPSCQueue<Order, 1024> inbound_queue_;
    LockFreeSPSCQueue<Trade, 1024> outbound_queue_;
    MemoryPool<Order, 2048> order_pool_;
    std::unordered_map<uint32_t, OrderBook> books_;
};

} // namespace execution
} // namespace quantum

int main() {
    using namespace quantum::execution;
    std::cout << "[Matching Engine] Starting core execution thread..." << std::endl;
    MatchingEngine engine;
    engine.start();
    
    // Submit sample order
    Order order = {1, 500000, 10, 1, 100, Side::BID, OrderState::NEW};
    engine.submit_order(order);
    
    std::this_thread::sleep_for(std::chrono::milliseconds(50));
    engine.stop();
    std::cout << "[Matching Engine] Stopped execution engine cleanly." << std::endl;
    return 0;
}
