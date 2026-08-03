#include <cstdint>
#include <cstddef>
#include <string>
#include <vector>
#include "order_book.hpp"

using namespace quantum::execution;

extern "C" int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
    if (size < sizeof(Order)) {
        return 0; // Not enough data to create an Order
    }

    OrderBook book(1); // Initialize order book for instrument 1

    // Treat the random fuzzing data as an array of Orders
    size_t num_orders = size / sizeof(Order);
    const Order* orders = reinterpret_cast<const Order*>(data);

    for (size_t i = 0; i < num_orders; ++i) {
        Order order = orders[i];
        
        // Ensure some basic valid enum states so we don't immediately fail assertions
        // (though in a real fuzzer you might want to test those too, but here we
        // want to test the matching logic deep inside the book)
        order.instrument_id = 1;
        
        if (order.side != Side::BID && order.side != Side::ASK) {
            order.side = Side::BID;
        }
        
        if (order.state != OrderState::NEW && order.state != OrderState::PARTIAL_FILL) {
            order.state = OrderState::NEW;
        }

        if (order.type != OrderType::LIMIT && order.type != OrderType::MARKET) {
            order.type = OrderType::LIMIT;
        }

        // Avoid zero qty unless testing cancels (which require existing orders)
        if (order.qty == 0) {
            order.qty = 100;
        }
        
        if (order.price == 0) {
            order.price = 1000;
        }

        FixedVector<Trade, 64> trades;
        book.match_order(order, trades);
    }

    return 0;
}

#ifndef __clang__
int main() {
    uint8_t dummy_data[sizeof(Order)] = {0};
    LLVMFuzzerTestOneInput(dummy_data, sizeof(dummy_data));
    return 0;
}
#endif
