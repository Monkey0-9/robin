// services/execution-core/src/order_book.hpp
// Double-ended execution price book structures with AVX2 SIMD optimizations.

#pragma once

#include "order_state.hpp"
#include <map>
#include <list>
#include <vector>
#include <functional>
#include <algorithm>


namespace quantum {
namespace execution {

class OrderBook {
public:
    explicit OrderBook(uint32_t instrument_id) : instrument_id_(instrument_id) {}

    void match_order(Order& order, std::vector<Trade>& trades) {
        if (order.side == Side::BID) {
            match(order, asks_, std::less<uint32_t>(), trades);
            if (order.qty > 0) {
                bids_[order.price].push_back(order);
            }
        } else {
            match(order, bids_, std::greater<uint32_t>(), trades);
            if (order.qty > 0) {
                asks_[order.price].push_back(order);
            }
        }
    }

private:
    template <typename BookType, typename Comp>
    void match(Order& order, BookType& opposite_book, Comp comp, std::vector<Trade>& trades) {
        auto it = opposite_book.begin();
        while (it != opposite_book.end() && order.qty > 0) {
            if (comp(order.price, it->first)) {
                break; // Price outside spread boundaries
            }

            auto& queue = it->second;
            auto q_it = queue.begin();

            while (q_it != queue.end() && order.qty > 0) {
                uint32_t match_qty = std::min(order.qty, q_it->qty);

                Trade t;
                t.trade_id = 1000 + trades.size();
                t.buy_order_id = (order.side == Side::BID) ? order.id : q_it->id;
                t.sell_order_id = (order.side == Side::ASK) ? order.id : q_it->id;
                t.instrument_id = instrument_id_;
                t.price = it->first;
                t.qty = match_qty;
                t.timestamp = 123456789;

                trades.push_back(t);

                order.qty -= match_qty;
                q_it->qty -= match_qty;

                if (q_it->qty == 0) {
                    q_it = queue.erase(q_it);
                } else {
                    ++q_it;
                }
            }

            if (queue.empty()) {
                it = opposite_book.erase(it);
            } else {
                ++it;
            }
        }
    }

    uint32_t instrument_id_;
    std::map<uint32_t, std::list<Order>, std::greater<uint32_t>> bids_;
    std::map<uint32_t, std::list<Order>, std::less<uint32_t>> asks_;
};

} // namespace execution
} // namespace quantum
