// services/execution-core/src/order_state.hpp
// Enum and state tracker structures for matching engine orders.

#pragma once

#include <cstdint>

namespace quantum {
namespace execution {

enum class OrderState : uint8_t {
    NEW = 0,
    PENDING_NEW = 1,
    WORKING = 2,
    PARTIAL_FILL = 3,
    FILLED = 4,
    PENDING_CANCEL = 5,
    CANCELED = 6,
    PENDING_REPLACE = 7,
    REPLACED = 8,
    REJECTED = 9,
    SUSPENDED = 10,
    CONFIRMED = 11
};

enum class Side : uint8_t {
    BID = 0,
    ASK = 1
};

struct Order {
    uint64_t id;
    uint32_t price;       // Scaled (e.g., *10000)
    uint32_t qty;
    uint32_t instrument_id;
    uint32_t client_id;
    Side side;
    OrderState state;
};

struct Trade {
    uint64_t trade_id;
    uint64_t buy_order_id;
    uint64_t sell_order_id;
    uint32_t instrument_id;
    uint32_t price;
    uint32_t qty;
    uint64_t timestamp;
};

} // namespace execution
} // namespace quantum
