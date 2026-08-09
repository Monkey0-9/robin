#pragma once
#include <cstdint>
#include <vector>
#include "order_state.hpp"
#include "order_book.hpp"

namespace quantum {
namespace execution {

struct TWAPOrder {
    uint64_t parent_id;
    uint32_t instrument_id;
    uint8_t side; // 1 = buy, 2 = sell
    uint64_t total_qty;
    uint64_t executed_qty;
    uint64_t start_time_ns;
    uint64_t end_time_ns;
    uint64_t interval_ns;
    uint64_t next_execution_ns;
    uint64_t slice_qty;
};

class TWAPEngine {
public:
    TWAPEngine();
    
    // Register a new TWAP parent order
    void add_twap_order(const TWAPOrder& order);
    
    // Evaluate if any TWAP slices should be emitted at the current time
    // Returns a list of child orders to be sent to the matcher
    std::vector<Order> tick(uint64_t current_time_ns);

private:
    std::vector<TWAPOrder> active_twaps_;
};

} // namespace execution
} // namespace quantum
