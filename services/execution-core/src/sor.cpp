#include "sor.hpp"
#include <cstdint>
#include <vector>

namespace quantum {
namespace execution {

std::vector<SmartOrderRouter::RouteResult> SmartOrderRouter::route_order(
    uint32_t instrument_id, uint32_t total_qty, double price, bool is_buy)
{
    (void)instrument_id;
    (void)price;
    (void)is_buy;
    std::vector<RouteResult> routes;

    uint32_t dark_qty = total_qty * 0.20;
    if (dark_qty > 0) {
        routes.push_back({Venue::DARK_POOL_A, dark_qty / 2});
        routes.push_back({Venue::DARK_POOL_B, dark_qty - (dark_qty / 2)});
    }

    uint32_t lit_qty = total_qty - dark_qty;
    if (lit_qty > 0) {
        uint32_t third = lit_qty / 3;
        routes.push_back({Venue::NYSE, third});
        routes.push_back({Venue::NASDAQ, third});
        routes.push_back({Venue::CBOE, lit_qty - 2 * third});
    }

    return routes;
}

} // namespace execution
} // namespace quantum
