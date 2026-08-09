#include "twap_engine.hpp"
#include <algorithm>

namespace quantum {
namespace execution {

TWAPEngine::TWAPEngine() = default;

void TWAPEngine::add_twap_order(const TWAPOrder& order) {
    active_twaps_.push_back(order);
}

std::vector<Order> TWAPEngine::tick(uint64_t current_time_ns) {
    std::vector<Order> generated_orders;
    
    for (auto it = active_twaps_.begin(); it != active_twaps_.end(); ) {
        if (current_time_ns >= it->end_time_ns || it->executed_qty >= it->total_qty) {
            it = active_twaps_.erase(it);
            continue;
        }
        
        if (current_time_ns >= it->next_execution_ns) {
            uint64_t remaining_qty = it->total_qty - it->executed_qty;
            uint64_t slice = std::min(it->slice_qty, remaining_qty);
            
            Order child_order;
            child_order.id = OrderIDGenerator::next();
            child_order.instrument_id = it->instrument_id;
            child_order.price = 0; // Market order for TWAP slice
            child_order.qty = slice;
            child_order.side = (it->side == 1) ? Side::BID : Side::ASK;
            child_order.type = OrderType::MARKET;
            child_order.state = OrderState::NEW;
            child_order.client_id = 9999;
            
            generated_orders.push_back(child_order);
            
            it->executed_qty += slice;
            it->next_execution_ns += it->interval_ns;
        }
        
        ++it;
    }
    
    return generated_orders;
}

} // namespace execution
} // namespace quantum
