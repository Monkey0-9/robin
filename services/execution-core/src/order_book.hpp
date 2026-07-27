#pragma once

#include "order_state.hpp"
#include <cstdint>
#include <cstring>
#include <algorithm>
#include <cassert>
#include <chrono>

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

// Per-price-level order queue (ring buffer, no heap allocation)
template <size_t MaxOrders = 256>
struct alignas(64) OrderQueue {
    static_assert((MaxOrders & (MaxOrders - 1)) == 0,
                  "MaxOrders must be a power of 2");
    Order  entries[MaxOrders];
    size_t head = 0;
    size_t tail = 0;

    bool push(const Order& o) noexcept {
        if (tail - head >= MaxOrders) return false; // Queue full
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

// ============================================================================
// OrderBook — Price-time priority matching engine
// ============================================================================
// - 256 price levels per side (previously 64, silently dropped beyond limit)
// - No heap allocation on hot path (all storage in-line)
// - Proper price-time priority (FIFO within each price level)
// - Overflow guard: rejects new price levels when array is full
// - Market orders, IOC, FOK, and Limit orders supported
class OrderBook {
public:
    // Increased from 64 to 256 to handle realistic market depth.
    // At 64 levels the book silently dropped orders at new price points.
    static constexpr size_t MAX_PRICE_LEVELS = 256;

    OrderBook() noexcept
        : instrument_id_(0), bid_count_(0), ask_count_(0),
          overflow_drops_(0) {}

    explicit OrderBook(uint32_t instrument_id) noexcept
        : instrument_id_(instrument_id), bid_count_(0), ask_count_(0),
          overflow_drops_(0) {}

    // Returns false if the order could not be processed (should not happen
    // unless the book is catastrophically full and even max levels exhausted).
    [[nodiscard]]
    bool match_order(Order& order, FixedVector<Trade, 64>& trades) noexcept {
        if (order.type == OrderType::FOK) {
            // FOK: fill completely or cancel — do not modify book
            if (!can_fully_fill(order)) {
                order.state = OrderState::CANCELED;
                return true;
            }
            // Fall through to match (it will fill completely)
        }

        if (order.side == Side::BID) {
            match_against_side</*is_bid=*/true>(order, asks_, ask_count_, trades);
            if (order.qty > 0) {
                if (order.type == OrderType::IOC || order.type == OrderType::MARKET) {
                    order.state = OrderState::CANCELED;
                } else if (order.type != OrderType::FOK) {
                    order.state = OrderState::WORKING;
                    if (!insert_bid(order)) {
                        // Price level array full — reject the residual resting order
                        order.state = OrderState::REJECTED;
                        overflow_drops_++;
                        return false;
                    }
                } else {
                    // FOK — already verified it fills fully, so qty should be 0
                    order.state = OrderState::CANCELED; // Shouldn't reach here
                }
            } else {
                order.state = OrderState::FILLED;
            }
        } else {
            match_against_side</*is_bid=*/false>(order, bids_, bid_count_, trades);
            if (order.qty > 0) {
                if (order.type == OrderType::IOC || order.type == OrderType::MARKET) {
                    order.state = OrderState::CANCELED;
                } else if (order.type != OrderType::FOK) {
                    order.state = OrderState::WORKING;
                    if (!insert_ask(order)) {
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

    // Cancel a resting order by ID. Returns true if found and removed.
    bool cancel_order(uint64_t order_id, Side side) noexcept {
        PriceLevel* levels = (side == Side::BID) ? bids_ : asks_;
        size_t& count      = (side == Side::BID) ? bid_count_ : ask_count_;
        for (size_t i = 0; i < count; i++) {
            auto& q = levels[i].queue;
            for (size_t j = q.head; j < q.tail; j++) {
                auto& entry = q.entries[j & (256 - 1)];
                if (entry.id == order_id) {
                    entry.state = OrderState::CANCELED;
                    entry.qty   = 0; // Soft cancel — will be skipped on next match
                    return true;
                }
            }
        }
        return false;
    }

    // Best bid (highest buy price), 0 if no bids
    int64_t best_bid() const noexcept {
        return (bid_count_ > 0) ? bids_[0].price : 0;
    }

    // Best ask (lowest sell price), 0 if no asks
    int64_t best_ask() const noexcept {
        return (ask_count_ > 0) ? asks_[0].price : 0;
    }

    // Spread in price ticks, 0 if one side is empty
    int64_t spread() const noexcept {
        if (bid_count_ == 0 || ask_count_ == 0) return 0;
        return asks_[0].price - bids_[0].price;
    }

    uint32_t instrument_id()  const noexcept { return instrument_id_; }
    size_t   bid_levels()     const noexcept { return bid_count_; }
    size_t   ask_levels()     const noexcept { return ask_count_; }
    uint64_t overflow_drops() const noexcept { return overflow_drops_; }

private:
    struct PriceLevel {
        int64_t      price = 0;
        OrderQueue<256> queue;
    };

    bool can_fully_fill(const Order& order) const noexcept {
        int64_t needed = order.qty;
        bool is_bid = (order.side == Side::BID);
        const PriceLevel* levels = is_bid ? asks_ : bids_;
        size_t count = is_bid ? ask_count_ : bid_count_;

        for (size_t i = 0; i < count && needed > 0; i++) {
            bool price_ok = is_bid
                ? order.price >= levels[i].price
                : order.price <= levels[i].price;
            if (!price_ok) break;
            const auto& q = levels[i].queue;
            for (size_t j = q.head; j < q.tail && needed > 0; j++) {
                const auto& e = q.entries[j & (256 - 1)];
                if (e.qty == 0) continue; // Soft-canceled
                int64_t fill = (needed < e.qty) ? needed : e.qty;
                needed -= fill;
            }
        }
        return needed == 0;
    }

    // Find insertion position for a new bid price level (descending order)
    size_t find_insert_pos_bid(int64_t price) const noexcept {
        size_t pos = bid_count_;
        while (pos > 0 && bids_[pos - 1].price < price) pos--;
        return pos;
    }

    // Find insertion position for a new ask price level (ascending order)
    size_t find_insert_pos_ask(int64_t price) const noexcept {
        size_t pos = ask_count_;
        while (pos > 0 && asks_[pos - 1].price > price) pos--;
        return pos;
    }

    // Returns false if MAX_PRICE_LEVELS reached
    [[nodiscard]]
    bool insert_bid(const Order& order) noexcept {
        size_t pos = find_insert_pos_bid(order.price);
        if (pos < bid_count_ && bids_[pos].price == order.price) {
            return bids_[pos].queue.push(order);
        }
        if (bid_count_ >= MAX_PRICE_LEVELS) return false; // Level array full
        for (size_t i = bid_count_; i > pos; i--) bids_[i] = bids_[i - 1];
        bids_[pos].price = order.price;
        bids_[pos].queue.clear();
        bids_[pos].queue.push(order);
        bid_count_++;
        return true;
    }

    [[nodiscard]]
    bool insert_ask(const Order& order) noexcept {
        size_t pos = find_insert_pos_ask(order.price);
        if (pos < ask_count_ && asks_[pos].price == order.price) {
            return asks_[pos].queue.push(order);
        }
        if (ask_count_ >= MAX_PRICE_LEVELS) return false; // Level array full
        for (size_t i = ask_count_; i > pos; i--) asks_[i] = asks_[i - 1];
        asks_[pos].price = order.price;
        asks_[pos].queue.clear();
        asks_[pos].queue.push(order);
        ask_count_++;
        return true;
    }

    template <bool IsBid>
    void match_against_side(Order& order, PriceLevel* levels, size_t& count,
                            FixedVector<Trade, 64>& trades) noexcept {
        size_t write = 0;
        size_t i = 0;
        for (; i < count && order.qty > 0; i++) {
            if (trades.full()) break;
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
                // Skip soft-canceled orders
                if (resting->qty == 0 || resting->state == OrderState::CANCELED) {
                    q.pop_front();
                    continue;
                }
                if (trades.full()) {
                    break; // Stop matching to avoid silent trade drops
                }

                int64_t fill_qty = (order.qty < resting->qty) ? order.qty : resting->qty;
                Trade t{};
                t.trade_id     = static_cast<uint64_t>(instrument_id_) * 1000000ULL
                                 + (order.side == Side::BID ? order.id : resting->id);
                t.buy_order_id  = (order.side == Side::BID)  ? order.id   : resting->id;
                t.sell_order_id = (order.side == Side::ASK)  ? order.id   : resting->id;
                t.instrument_id = instrument_id_;
                t.price         = levels[i].price; // Resting order's price (maker price)
                t.qty           = fill_qty;
                t.timestamp     = 0; // Caller should stamp with wall-clock/TSC
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

            if (!q.empty()) {
                if (write < i) levels[write] = levels[i];
                write++;
            }
        }

        // Move unprocessed levels (beyond where loop exited) after kept levels
        for (size_t j = i; j < count; j++, write++) {
            if (write < j) levels[write] = levels[j];
        }
        count = write;
    }

    uint32_t   instrument_id_;
    PriceLevel bids_[MAX_PRICE_LEVELS];
    size_t     bid_count_;
    PriceLevel asks_[MAX_PRICE_LEVELS];
    size_t     ask_count_;
    uint64_t   overflow_drops_; // Non-zero means the book is dangerously full
};

} // namespace execution
} // namespace quantum

// ============================================================================
// OrderBook Snapshot — Binary serialization for crash recovery
// ============================================================================
// On SIGTERM, call save_snapshot(book, path) to persist all resting orders.
// On startup, call load_snapshot(book, path) to restore state.
//
// Binary format (little-endian):
//   [4B] instrument_id
//   [8B] bid_count
//   [8B] ask_count
//   For each bid level:
//     [8B] price
//     [8B] queue.head
//     [8B] queue.tail
//     For each order entry [head..tail]:
//       [sizeof(Order)] raw order bytes
//   For each ask level: same layout
// ============================================================================

#include <cstdio>
#include <cstring>

namespace quantum {
namespace execution {

inline bool save_snapshot(const OrderBook& book, const char* path) noexcept {
    FILE* f = std::fopen(path, "wb");
    if (!f) return false;

    // We need access to private members — use a POD copy via memcpy
    // The snapshot captures instrument_id, bid/ask levels, counts.
    // Since OrderBook has fixed-size in-place arrays, we just write the whole struct.
    // NOTE: This is architecture-dependent; only load on same-arch binary.
    uint32_t id  = book.instrument_id();
    size_t bids  = book.bid_levels();
    size_t asks  = book.ask_levels();

    std::fwrite(&id,   sizeof(id),   1, f);
    std::fwrite(&bids, sizeof(bids), 1, f);
    std::fwrite(&asks, sizeof(asks), 1, f);

    // Write raw struct bytes (includes all price level arrays)
    std::fwrite(&book, sizeof(OrderBook), 1, f);

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
    std::fread(&id,   sizeof(id),   1, f);
    std::fread(&bids, sizeof(bids), 1, f);
    std::fread(&asks, sizeof(asks), 1, f);

    // Read full struct
    OrderBook tmp;
    if (std::fread(&tmp, sizeof(OrderBook), 1, f) != 1) {
        std::fclose(f);
        return false;
    }
    book = tmp;
    std::fclose(f);

    std::printf("[SNAPSHOT] Loaded order book %u from %s (%zu bids, %zu asks)\n",
                id, path, bids, asks);
    return true;
}

} // namespace execution
} // namespace quantum
