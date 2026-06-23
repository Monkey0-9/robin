#pragma once
#include <cstdint>
#include <vector>

namespace quantum {
namespace execution {

class SmartOrderRouter {
public:
    enum class Venue {
        NYSE,
        NASDAQ,
        CBOE,
        DARK_POOL_A,
        DARK_POOL_B
    };

    struct RouteResult {
        Venue venue;
        uint32_t qty;
    };

    std::vector<RouteResult> route_order(uint32_t instrument_id, uint32_t total_qty, double price, bool is_buy);
};

} // namespace execution
} // namespace quantum
