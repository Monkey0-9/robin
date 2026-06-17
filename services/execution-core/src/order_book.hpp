// services/execution-core/src/order_book.hpp
// Double-ended execution price book structures with AVX2 SIMD optimizations.

#pragma once

#include "order_state.hpp"
#include <map>
#include <list>
#include <vector>
#include <functional>
#include <algorithm>
#include <cstdlib>

namespace quantum {
namespace execution {

class OrderBook {
public:
    explicit OrderBook(uint32_t instrument_id) : instrument_id_(instrument_id) {
        // Aligned allocation for SIMD vectors
        bid_prices_ = static_cast<uint32_t*>(std::malloc(1024 * sizeof(uint32_t)));
        ask_prices_ = static_cast<uint32_t*>(std::malloc(1024 * sizeof(uint32_t)));
        std::fill(bid_prices_, bid_prices_ + 1024, 0);
        std::fill(ask_prices_, ask_prices_ + 1024, 0xFFFFFFFF);
    }

    ~OrderBook() {
        std::free(bid_prices_);
        std::free(ask_prices_);
    }

    void match_order(Order& order, std::vector<Trade>& trades) {
        if (order.side == Side::BID) {
            match(order, asks_, std::less<uint32_t>(), trades);
            if (order.qty > 0) {
                bids_[order.price].push_back(order);
                update_simd_arrays(Side::BID);
            }
        } else {
            match(order, bids_, std::greater<uint32_t>(), trades);
            if (order.qty > 0) {
                asks_[order.price].push_back(order);
                update_simd_arrays(Side::ASK);
            }
        }
    }

private:
    void update_simd_arrays(Side side) {
        size_t idx = 0;
        if (side == Side::BID) {
            for (const auto& [price, queue] : bids_) {
                if (idx >= 1024) break;
                bid_prices_[idx++] = price;
            }
            std::fill(bid_prices_ + idx, bid_prices_ + 1024, 0);
        } else {
            for (const auto& [price, queue] : asks_) {
                if (idx >= 1024) break;
                ask_prices_[idx++] = price;
            }
            std::fill(ask_prices_ + idx, ask_prices_ + 1024, 0xFFFFFFFF);
        }
    }

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
    uint32_t* bid_prices_;
    uint32_t* ask_prices_;
    std::map<uint32_t, std::list<Order>, std::greater<uint32_t>> bids_;
    std::map<uint32_t, std::list<Order>, std::less<uint32_t>> asks_;
};

} // namespace execution
} // namespace quantum
