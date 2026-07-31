#pragma once

#include "order_state.hpp"
#include <cstdint>
#include <cstring>
#include <algorithm>
#include <cassert>
#include <chrono>
#include <map>
#include <vector>

namespace quantum {
namespace execution {

template <typename T, size_t N>
class FixedVector {
public:
    T      data[N];
    size_t sz = 0;

    void push_back(const T& val) noexcept {
        if (sz < N) data[sz++] = val;
    }
    size_t       size()  const noexcept { return sz; }
    bool         full()  const noexcept { return sz >= N; }
    bool         empty() const noexcept { return sz == 0; }
    void         clear() noexcept       { sz = 0; }
    T*           begin() noexcept       { return data; }
    T*           end()   noexcept       { return data + sz; }
    const T*     begin() const noexcept { return data; }
    const T*     end()   const noexcept { return data + sz; }
    T&       operator[](size_t i) noexcept       { return data[i]; }
    const T& operator[](size_t i) const noexcept { return data[i]; }
};

template <size_t MaxOrders = 256>
struct alignas(64) OrderQueue {
    static_assert((MaxOrders & (MaxOrders - 1)) == 0,
                "MaxOrders must be a power of 2");
    Order  entries[MaxOrders];
    size_t head = 0;
    size_t tail = 0;

    bool push(const Order& o) noexcept {
        if (tail - head >= MaxOrders) return false;
        entries[tail & (MaxOrders - 1)] = o;
        tail++;
        return true;
    }
    Order* front() noexcept {
        if (head >= tail) return nullptr;
        return &entries[head & (MaxOrders - 1)];
    }
    void   pop_front() noexcept { if (head < tail) head++; }
    bool   empty()     const noexcept { return head >= tail; }
    size_t size()      const noexcept { return tail - head; }
    void   clear()     noexcept       { head = 0; tail = 0; }
};

class OrderBook {
public:
    OrderBook() noexcept
        : instrument_id_(0), overflow_drops_(0) {}

    explicit OrderBook(uint32_t instrument_id) noexcept
        : instrument_id_(instrument_id), overflow_drops_(0) {}

    [[nodiscard]]
    bool match_order(Order& order, FixedVector<Trade, 64>& trades) noexcept {
        if (order.type == OrderType::FOK) {
            if (!can_fully_fill(order)) {
                order.state = OrderState::CANCELED;
                return true;
            }
        }

        if (order.side == Side::BID) {
            match_against_side<true>(order, asks_, trades);
            if (order.qty > 0) {
                if (order.type == OrderType::IOC || order.type == OrderType::MARKET) {
                    order.state = OrderState::CANCELED;
                } else if (order.type != OrderType::FOK) {
                    order.state = OrderState::WORKING;
                    if (!bids_[order.price].push(order)) {
                        order.state = OrderState::REJECTED;
                        overflow_drops_++;
                        return false;
                    }
                } else {
                    order.state = OrderState::CANCELED;
                }
            } else {
                order.state = OrderState::FILLED;
            }
        } else {
            match_against_side<false>(order, bids_, trades);
            if (order.qty > 0) {
                if (order.type == OrderType::IOC || order.type == OrderType::MARKET) {
                    order.state = OrderState::CANCELED;
                } else if (order.type != OrderType::FOK) {
                    order.state = OrderState::WORKING;
                    if (!asks_[order.price].push(order)) {
                        order.state = OrderState::REJECTED;
                        overflow_drops_++;
                        return false;
                    }
                } else {
                    order.state = OrderState::CANCELED;
                }
            } else {
                order.state = OrderState::FILLED;
            }
        }
        return true;
    }

    bool cancel_order(uint64_t order_id, Side side) noexcept {
        if (side == Side::BID) {
            for (auto& [price, q] : bids_) {
                for (size_t j = q.head; j < q.tail; j++) {
                    auto& entry = q.entries[j & (256 - 1)];
                    if (entry.id == order_id) {
                        entry.state = OrderState::CANCELED;
                        entry.qty   = 0;
                        return true;
                    }
                }
            }
        } else {
            for (auto& [price, q] : asks_) {
                for (size_t j = q.head; j < q.tail; j++) {
                    auto& entry = q.entries[j & (256 - 1)];
                    if (entry.id == order_id) {
                        entry.state = OrderState::CANCELED;
                        entry.qty   = 0;
                        return true;
                    }
                }
            }
        }
        return false;
    }

    int64_t best_bid() const noexcept {
        for (auto it = bids_.begin(); it != bids_.end(); ++it) {
            if (!it->second.empty()) return it->first;
        }
        return 0;
    }

    int64_t best_ask() const noexcept {
        for (auto it = asks_.begin(); it != asks_.end(); ++it) {
            if (!it->second.empty()) return it->first;
        }
        return 0;
    }

    int64_t spread() const noexcept {
        int64_t bb = best_bid();
        int64_t ba = best_ask();
        if (bb == 0 || ba == 0) return 0;
        return ba - bb;
    }

    uint32_t instrument_id()  const noexcept { return instrument_id_; }
    size_t   bid_levels()     const noexcept { return bids_.size(); }
    size_t   ask_levels()     const noexcept { return asks_.size(); }
    uint64_t overflow_drops() const noexcept { return overflow_drops_; }
    // For snapshot
    const std::map<int64_t, OrderQueue<256>, std::greater<int64_t>>& get_bids() const { return bids_; }
    const std::map<int64_t, OrderQueue<256>, std::less<int64_t>>& get_asks() const { return asks_; }
    void set_instrument_id(uint32_t id) { instrument_id_ = id; }
    void add_bid_level(int64_t price, const OrderQueue<256>& q) { bids_[price] = q; }
    void add_ask_level(int64_t price, const OrderQueue<256>& q) { asks_[price] = q; }

private:
    bool can_fully_fill(const Order& order) const noexcept {
        int64_t needed = order.qty;
        bool is_bid = (order.side == Side::BID);

        if (is_bid) {
            for (auto& [price, q] : asks_) {
                if (order.price < price) break;
                for (size_t j = q.head; j < q.tail && needed > 0; j++) {
                    const auto& e = q.entries[j & (256 - 1)];
                    if (e.qty == 0) continue;
                    int64_t fill = (needed < e.qty) ? needed : e.qty;
                    needed -= fill;
                }
                if (needed == 0) break;
            }
        } else {
            for (auto& [price, q] : bids_) {
                if (order.price > price) break;
                for (size_t j = q.head; j < q.tail && needed > 0; j++) {
                    const auto& e = q.entries[j & (256 - 1)];
                    if (e.qty == 0) continue;
                    int64_t fill = (needed < e.qty) ? needed : e.qty;
                    needed -= fill;
                }
                if (needed == 0) break;
            }
        }
        return needed == 0;
    }

    template <bool IsBid, typename MapType>
    void match_against_side(Order& order, MapType& levels,
                            FixedVector<Trade, 64>& trades) noexcept {
        for (auto it = levels.begin(); it != levels.end() && order.qty > 0;) {
            if (trades.full()) break;
            if (order.type != OrderType::MARKET) {
                bool price_ok = IsBid
                    ? order.price >= it->first
                    : order.price <= it->first;
                if (!price_ok) break;
            }

            auto& q = it->second;
            while (!q.empty() && order.qty > 0) {
                auto* resting = q.front();
                if (resting->qty == 0 || resting->state == OrderState::CANCELED) {
                    q.pop_front();
                    continue;
                }
                if (trades.full()) break;

                int64_t fill_qty = (order.qty < resting->qty) ? order.qty : resting->qty;
                Trade t{};
                t.trade_id     = static_cast<uint64_t>(instrument_id_) * 1000000ULL
                            + (order.side == Side::BID ? order.id : resting->id);
                t.buy_order_id  = (order.side == Side::BID)  ? order.id   : resting->id;
                t.sell_order_id = (order.side == Side::ASK)  ? order.id   : resting->id;
                t.instrument_id = instrument_id_;
                t.price         = it->first;
                t.qty           = fill_qty;
                t.timestamp     = 0;
                trades.push_back(t);

                order.qty   -= fill_qty;
                resting->qty -= fill_qty;
                if (resting->qty == 0) {
                    resting->state = OrderState::FILLED;
                    q.pop_front();
                } else {
                    resting->state = OrderState::PARTIAL_FILL;
                }
            }
            if (q.empty()) {
                it = levels.erase(it);
            } else {
                ++it;
            }
        }
    }

    uint32_t   instrument_id_;
    std::map<int64_t, OrderQueue<256>, std::greater<int64_t>> bids_;
    std::map<int64_t, OrderQueue<256>, std::less<int64_t>> asks_;
    uint64_t   overflow_drops_;
};

} // namespace execution
} // namespace quantum

#include <cstdio>
#include <cstring>

namespace quantum {
namespace execution {

inline bool save_snapshot(const OrderBook& book, const char* path) noexcept {
    FILE* f = std::fopen(path, "wb");
    if (!f) return false;

    uint32_t id  = book.instrument_id();
    size_t bids  = book.bid_levels();
    size_t asks  = book.ask_levels();

    std::fwrite(&id,   sizeof(id),   1, f);
    std::fwrite(&bids, sizeof(bids), 1, f);
    std::fwrite(&asks, sizeof(asks), 1, f);

    for (const auto& pair : book.get_bids()) {
        std::fwrite(&pair.first, sizeof(pair.first), 1, f);
        std::fwrite(&pair.second, sizeof(pair.second), 1, f);
    }
    for (const auto& pair : book.get_asks()) {
        std::fwrite(&pair.first, sizeof(pair.first), 1, f);
        std::fwrite(&pair.second, sizeof(pair.second), 1, f);
    }

    std::fclose(f);
    std::printf("[SNAPSHOT] Saved order book %u to %s (%zu bids, %zu asks)\n",
                id, path, bids, asks);
    return true;
}

inline bool load_snapshot(OrderBook& book, const char* path) noexcept {
    FILE* f = std::fopen(path, "rb");
    if (!f) return false;

    uint32_t id;
    size_t bids, asks;
    if (std::fread(&id,   sizeof(id),   1, f) != 1) { std::fclose(f); return false; }
    if (std::fread(&bids, sizeof(bids), 1, f) != 1) { std::fclose(f); return false; }
    if (std::fread(&asks, sizeof(asks), 1, f) != 1) { std::fclose(f); return false; }

    book.set_instrument_id(id);

    for (size_t i = 0; i < bids; ++i) {
        int64_t price;
        OrderQueue<256> q;
        if (std::fread(&price, sizeof(price), 1, f) != 1) { std::fclose(f); return false; }
        if (std::fread(&q, sizeof(q), 1, f) != 1) { std::fclose(f); return false; }
        book.add_bid_level(price, q);
    }
    for (size_t i = 0; i < asks; ++i) {
        int64_t price;
        OrderQueue<256> q;
        if (std::fread(&price, sizeof(price), 1, f) != 1) { std::fclose(f); return false; }
        if (std::fread(&q, sizeof(q), 1, f) != 1) { std::fclose(f); return false; }
        book.add_ask_level(price, q);
    }

    std::fclose(f);
    std::printf("[SNAPSHOT] Loaded order book %u from %s (%zu bids, %zu asks)\n",
                id, path, bids, asks);
    return true;
}

} // namespace execution
} // namespace quantum
