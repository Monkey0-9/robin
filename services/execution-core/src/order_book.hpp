#pragma once

#include "order_state.hpp"
#include <cstdint>
#include <cstring>
#include <algorithm>

namespace quantum {
namespace execution {

template <typename T, size_t N>
class FixedVector {
public:
    T data[N];
    size_t sz = 0;

    void push_back(const T& val) noexcept {
        if (sz < N) data[sz++] = val;
    }

    size_t size() const noexcept { return sz; }
    T* begin() noexcept { return data; }
    T* end() noexcept { return data + sz; }
    const T* begin() const noexcept { return data; }
    const T* end() const noexcept { return data + sz; }
    T& operator[](size_t i) noexcept { return data[i]; }
    const T& operator[](size_t i) const noexcept { return data[i]; }
    void clear() noexcept { sz = 0; }
};

template <size_t MaxOrders = 128>
struct alignas(64) OrderQueue {
    static_assert((MaxOrders & (MaxOrders - 1)) == 0, "MaxOrders must be a power of 2");
    Order entries[MaxOrders];
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

    void pop_front() noexcept {
        if (head < tail) head++;
    }

    bool empty() const noexcept { return head >= tail; }
    size_t size() const noexcept { return tail - head; }
    void clear() noexcept { head = 0; tail = 0; }
};

static inline uint32_t price_from_order(const Order& o) noexcept {
    return o.price;
}

static inline bool can_match_bid(uint32_t bid_price, uint32_t ask_price) noexcept {
    return bid_price >= ask_price;
}

static inline bool can_match_ask(uint32_t ask_price, uint32_t bid_price) noexcept {
    return ask_price <= bid_price;
}

class OrderBook {
public:
    static constexpr size_t MAX_PRICE_LEVELS = 64;

    explicit OrderBook(uint32_t instrument_id) noexcept : instrument_id_(instrument_id) {}

    void match_order(Order& order, FixedVector<Trade, 64>& trades) noexcept {
        if (order.type == OrderType::FOK || order.type == OrderType::IOC) {
            if (!can_fully_fill(order)) {
                order.state = OrderState::CANCELED;
                return;
            }
            if (order.type == OrderType::FOK) {
                order.state = OrderState::CANCELED;
                return;
            }
        }

        if (order.side == Side::BID) {
            match_against_side(order, asks_, ask_count_, trades, false);
            if (order.qty > 0) {
                if (order.type == OrderType::IOC || order.type == OrderType::MARKET) {
                    order.state = OrderState::CANCELED;
                } else {
                    order.state = OrderState::WORKING;
                    insert_bid(order);
                }
            } else {
                order.state = OrderState::FILLED;
            }
        } else {
            match_against_side(order, bids_, bid_count_, trades, true);
            if (order.qty > 0) {
                if (order.type == OrderType::IOC || order.type == OrderType::MARKET) {
                    order.state = OrderState::CANCELED;
                } else {
                    order.state = OrderState::WORKING;
                    insert_ask(order);
                }
            } else {
                order.state = OrderState::FILLED;
            }
        }
    }

private:
    bool can_fully_fill(const Order& order) const noexcept {
        uint32_t needed = order.qty;
        bool is_bid = order.side == Side::BID;
        const auto& levels = is_bid ? asks_ : bids_;
        size_t count = is_bid ? ask_count_ : bid_count_;

        for (size_t i = 0; i < count && needed > 0; i++) {
            bool price_ok = is_bid
                ? order.price >= levels[i].price
                : order.price <= levels[i].price;
            if (!price_ok) break;
            const auto& q = levels[i].queue;
            size_t idx = q.head;
            while (idx < q.tail && needed > 0) {
                const auto& entry = q.entries[idx & 127];
                uint32_t fill = std::min(needed, entry.qty);
                needed -= fill;
                if (entry.qty == fill) idx++;
                else break;
            }
        }
        return needed == 0;
    }

    size_t find_insert_pos_bid(uint32_t price) const noexcept {
        size_t pos = bid_count_;
        while (pos > 0 && bids_[pos - 1].price < price) pos--;
        return pos;
    }

    size_t find_insert_pos_ask(uint32_t price) const noexcept {
        size_t pos = ask_count_;
        while (pos > 0 && asks_[pos - 1].price > price) pos--;
        return pos;
    }

    void insert_bid(const Order& order) noexcept {
        size_t pos = find_insert_pos_bid(order.price);
        if (pos < bid_count_ && bids_[pos].price == order.price) {
            bids_[pos].queue.push(order);
            return;
        }
        for (size_t i = bid_count_; i > pos; i--) bids_[i] = bids_[i - 1];
        bids_[pos].price = order.price;
        bids_[pos].queue.clear();
        bids_[pos].queue.push(order);
        bid_count_++;
    }

    void insert_ask(const Order& order) noexcept {
        size_t pos = find_insert_pos_ask(order.price);
        if (pos < ask_count_ && asks_[pos].price == order.price) {
            asks_[pos].queue.push(order);
            return;
        }
        for (size_t i = ask_count_; i > pos; i--) asks_[i] = asks_[i - 1];
        asks_[pos].price = order.price;
        asks_[pos].queue.clear();
        asks_[pos].queue.push(order);
        ask_count_++;
    }

    template <bool IsBid>
    void match_against_side(Order& order, PriceLevel* levels, size_t& count,
                            FixedVector<Trade, 64>& trades) noexcept {
        size_t write = 0;
        for (size_t i = 0; i < count && order.qty > 0; i++) {
            if (order.type != OrderType::MARKET) {
                bool price_ok = IsBid
                    ? order.price >= levels[i].price
                    : order.price <= levels[i].price;
                if (!price_ok) {
                    if (write < i) levels[write] = levels[i];
                    write++;
                    continue;
                }
            }

            auto& q = levels[i].queue;
            while (!q.empty() && order.qty > 0) {
                auto* resting = q.front();
                uint32_t fill_qty = std::min(order.qty, resting->qty);
                Trade t;
                t.trade_id = 1000 + trades.size();
                t.buy_order_id = order.side == Side::BID ? order.id : resting->id;
                t.sell_order_id = order.side == Side::ASK ? order.id : resting->id;
                t.instrument_id = instrument_id_;
                t.price = levels[i].price;
                t.qty = fill_qty;
                t.timestamp = 123456789;
                trades.push_back(t);

                order.qty -= fill_qty;
                resting->qty -= fill_qty;

                if (resting->qty == 0) {
                    resting->state = OrderState::FILLED;
                    q.pop_front();
                } else {
                    resting->state = OrderState::PARTIAL_FILL;
                }
            }

            if (!q.empty() || order.qty == 0) {
                if (write < i) levels[write] = levels[i];
                write++;
            }
        }

        size_t remaining = 0;
        for (size_t i = write; i < count; i++) {
            levels[remaining++] = levels[i];
        }
        count = remaining;
    }

    void match_against_side(Order& order, PriceLevel* levels, size_t& count,
                            FixedVector<Trade, 64>& trades, bool is_bid) noexcept {
        if (is_bid)
            match_against_side<true>(order, levels, count, trades);
        else
            match_against_side<false>(order, levels, count, trades);
    }

    struct PriceLevel {
        uint32_t price;
        OrderQueue<128> queue;
    };

    uint32_t instrument_id_;
    PriceLevel bids_[MAX_PRICE_LEVELS];
    size_t bid_count_ = 0;
    PriceLevel asks_[MAX_PRICE_LEVELS];
    size_t ask_count_ = 0;
};

} // namespace execution
} // namespace quantum
