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

struct alignas(64) OrderQueueEntry {
    Order order;
};

struct alignas(64) OrderQueue {
    static constexpr size_t MAX_ORDERS = 128;
    OrderQueueEntry entries[MAX_ORDERS];
    size_t head = 0;
    size_t tail = 0;

    bool push(const Order& o) noexcept {
        if (tail - head >= MAX_ORDERS) return false;
        entries[tail & (MAX_ORDERS - 1)].order = o;
        tail++;
        return true;
    }

    Order* front() noexcept {
        if (head >= tail) return nullptr;
        return &entries[head & (MAX_ORDERS - 1)].order;
    }

    void pop_front() noexcept {
        if (head < tail) head++;
    }

    bool empty() const noexcept { return head >= tail; }
    size_t size() const noexcept { return tail - head; }
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
        if (order.type == OrderType::FOK) {
            if (!can_fully_fill_fok(order)) {
                order.state = OrderState::CANCELED;
                return;
            }
        }

        if (order.side == Side::BID) {
            match_against_side(order, asks_, ask_count_, trades, false);
            if (order.qty > 0) {
                if (order.type == OrderType::IOC || order.type == OrderType::MARKET || order.type == OrderType::FOK) {
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
                if (order.type == OrderType::IOC || order.type == OrderType::MARKET || order.type == OrderType::FOK) {
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
    bool can_fully_fill_fok(const Order& order) const noexcept {
        uint32_t needed = order.qty;
        if (order.side == Side::BID) {
            for (size_t i = 0; i < ask_count_ && needed > 0; i++) {
                if (order.price < asks_[i].price) break;
                auto* q = asks_[i].queue;
                if (!q) continue;
                auto* o = q->front();
                while (o && needed > 0) {
                    uint32_t fill = std::min(needed, o->qty);
                    needed -= fill;
                    q->pop_front();
                    o = q->front();
                }
            }
        } else {
            for (size_t i = 0; i < bid_count_ && needed > 0; i++) {
                if (order.price > bids_[i].price) break;
                auto* q = bids_[i].queue;
                if (!q) continue;
                auto* o = q->front();
                while (o && needed > 0) {
                    uint32_t fill = std::min(needed, o->qty);
                    needed -= fill;
                    q->pop_front();
                    o = q->front();
                }
            }
        }
        return needed == 0;
    }

    void insert_bid(const Order& order) noexcept {
        size_t pos = bid_count_;
        while (pos > 0 && bids_[pos - 1].price < order.price) {
            bids_[pos] = bids_[pos - 1];
            pos--;
        }
        bids_[pos].price = order.price;
        bids_[pos].queue = get_queue(order.price, true);
        bids_[pos].queue->push(order);
        if (pos == bid_count_) bid_count_++;
    }

    void insert_ask(const Order& order) noexcept {
        size_t pos = ask_count_;
        while (pos > 0 && asks_[pos - 1].price > order.price) {
            asks_[pos] = asks_[pos - 1];
            pos--;
        }
        asks_[pos].price = order.price;
        asks_[pos].queue = get_queue(order.price, false);
        asks_[pos].queue->push(order);
        if (pos == ask_count_) ask_count_++;
    }

    OrderQueue* get_queue(uint32_t price, bool is_bid) noexcept {
        size_t idx = price % POOL_SIZE;
        OrderQueue* q = &queue_pool_[idx];
        return q;
    }

    template <typename Comp>
    void match_against_side(Order& order, PriceLevel* levels, size_t& count,
                            FixedVector<Trade, 64>& trades, bool is_bid) noexcept {
        size_t i = 0;
        while (i < count && order.qty > 0) {
            if (order.type != OrderType::MARKET) {
                bool price_ok = is_bid
                    ? order.price >= levels[i].price
                    : order.price <= levels[i].price;
                if (!price_ok) break;
            }

            auto* q = levels[i].queue;
            if (!q) { i++; continue; }

            while (!q->empty() && order.qty > 0) {
                auto* resting = q->front();
                if (!resting) { q->pop_front(); continue; }

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
                    q->pop_front();
                } else {
                    resting->state = OrderState::PARTIAL_FILL;
                }
            }

            i++;
        }

        if (i > 0 && i < count) {
            size_t new_count = 0;
            for (size_t j = i; j < count; j++) {
                levels[new_count++] = levels[j];
            }
            count = new_count;
        } else if (i == count) {
            count = 0;
        }
    }

    struct PriceLevel {
        uint32_t price;
        OrderQueue* queue;
    };

    uint32_t instrument_id_;
    static constexpr size_t POOL_SIZE = 256;
    alignas(64) OrderQueue queue_pool_[POOL_SIZE];
    PriceLevel bids_[MAX_PRICE_LEVELS];
    size_t bid_count_ = 0;
    PriceLevel asks_[MAX_PRICE_LEVELS];
    size_t ask_count_ = 0;
};

} // namespace execution
} // namespace quantum
