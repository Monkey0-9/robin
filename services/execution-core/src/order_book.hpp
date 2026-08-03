#pragma once

#include "order_state.hpp"
#include <cstdint>
#include <cstring>
#include <algorithm>
#include <cassert>
#include <chrono>
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

#ifdef _MSC_VER
#include <intrin.h>
inline uint64_t rdtscp_local() {
    unsigned int aux;
    return __rdtscp(&aux);
}
#else
#include <x86intrin.h>
inline uint64_t rdtscp_local() {
    unsigned int aux;
    return __rdtscp(&aux);
}
#endif

class OrderBook {
public:
    static constexpr size_t MAX_LEVELS = 131072;
    struct Level {
        int64_t price = 0;
        OrderQueue<256>* q = nullptr;
    };

    OrderBook() noexcept : instrument_id_(0), overflow_drops_(0) {}
    explicit OrderBook(uint32_t instrument_id) noexcept : instrument_id_(instrument_id), overflow_drops_(0) {}
    ~OrderBook() {
        for(size_t i=0; i<MAX_LEVELS; ++i) {
            if (bids_levels_[i].q) delete bids_levels_[i].q;
            if (asks_levels_[i].q) delete asks_levels_[i].q;
        }
    }

    [[nodiscard]]
    bool match_order(Order& order, FixedVector<Trade, 64>& trades) noexcept {
        if (order.type == OrderType::FOK) {
            if (!can_fully_fill(order)) { order.state = OrderState::CANCELED; return true; }
        }

        if (order.side == Side::BID) {
            match_against_side<true>(order, trades);
            if (order.qty > 0) {
                if (order.type == OrderType::IOC || order.type == OrderType::MARKET) {
                    order.state = OrderState::CANCELED;
                } else if (order.type != OrderType::FOK) {
                    order.state = OrderState::WORKING;
                    if (!push_bid(order)) {
                        order.state = OrderState::REJECTED;
                        overflow_drops_++;
                        return false;
                    }
                } else { order.state = OrderState::CANCELED; }
            } else { order.state = OrderState::FILLED; }
        } else {
            match_against_side<false>(order, trades);
            if (order.qty > 0) {
                if (order.type == OrderType::IOC || order.type == OrderType::MARKET) {
                    order.state = OrderState::CANCELED;
                } else if (order.type != OrderType::FOK) {
                    order.state = OrderState::WORKING;
                    if (!push_ask(order)) {
                        order.state = OrderState::REJECTED;
                        overflow_drops_++;
                        return false;
                    }
                } else { order.state = OrderState::CANCELED; }
            } else { order.state = OrderState::FILLED; }
        }
        return true;
    }

    bool cancel_order(uint64_t order_id, int64_t price, Side side) noexcept {
        size_t idx = price & (MAX_LEVELS - 1);
        auto* q = (side == Side::BID) ? bids_levels_[idx].q : asks_levels_[idx].q;
        if (!q) return false;
        for (size_t i = q->head; i < q->tail; ++i) {
            auto& e = q->entries[i & (256 - 1)];
            if (e.id == order_id && e.state != OrderState::CANCELED) {
                e.state = OrderState::CANCELED;
                e.qty = 0;
                return true;
            }
        }
        return false;
    }

    int64_t best_bid() const noexcept { return active_bids_.empty() ? 0 : active_bids_[0]; }
    int64_t best_ask() const noexcept { return active_asks_.empty() ? 0 : active_asks_[0]; }

    int64_t spread() const noexcept {
        int64_t bb = best_bid(); int64_t ba = best_ask();
        if (bb == 0 || ba == 0) return 0;
        return ba - bb;
    }

    uint32_t instrument_id()  const noexcept { return instrument_id_; }
    size_t   bid_levels()     const noexcept { return active_bids_.size(); }
    size_t   ask_levels()     const noexcept { return active_asks_.size(); }
    uint64_t overflow_drops() const noexcept { return overflow_drops_; }
    
    const FixedVector<int64_t, 1024>& get_active_bids() const { return active_bids_; }
    const FixedVector<int64_t, 1024>& get_active_asks() const { return active_asks_; }
    const Level* get_bids_levels() const { return bids_levels_; }
    const Level* get_asks_levels() const { return asks_levels_; }

    void set_instrument_id(uint32_t id) { instrument_id_ = id; }
    void add_bid_level(int64_t price, const OrderQueue<256>& q) {
        size_t idx = price & (MAX_LEVELS - 1);
        bids_levels_[idx].price = price;
        if (!bids_levels_[idx].q) bids_levels_[idx].q = new OrderQueue<256>();
        *bids_levels_[idx].q = q;
        insert_active_bid(price);
    }
    void add_ask_level(int64_t price, const OrderQueue<256>& q) {
        size_t idx = price & (MAX_LEVELS - 1);
        asks_levels_[idx].price = price;
        if (!asks_levels_[idx].q) asks_levels_[idx].q = new OrderQueue<256>();
        *asks_levels_[idx].q = q;
        insert_active_ask(price);
    }

private:
    bool push_bid(const Order& order) noexcept {
        size_t idx = order.price & (MAX_LEVELS - 1);
        if (bids_levels_[idx].price != 0 && bids_levels_[idx].price != order.price) return false; // Hash collision (very rare)
        bids_levels_[idx].price = order.price;
        if (!bids_levels_[idx].q) bids_levels_[idx].q = new OrderQueue<256>();
        if (!bids_levels_[idx].q->push(order)) return false;
        insert_active_bid(order.price);
        return true;
    }
    bool push_ask(const Order& order) noexcept {
        size_t idx = order.price & (MAX_LEVELS - 1);
        if (asks_levels_[idx].price != 0 && asks_levels_[idx].price != order.price) return false;
        asks_levels_[idx].price = order.price;
        if (!asks_levels_[idx].q) asks_levels_[idx].q = new OrderQueue<256>();
        if (!asks_levels_[idx].q->push(order)) return false;
        insert_active_ask(order.price);
        return true;
    }

    void insert_active_bid(int64_t price) noexcept {
        for (size_t i = 0; i < active_bids_.size(); ++i) {
            if (active_bids_[i] == price) return;
            if (active_bids_[i] < price) {
                if (active_bids_.full()) return;
                std::memmove(&active_bids_[i + 1], &active_bids_[i], (active_bids_.size() - i) * sizeof(int64_t));
                active_bids_[i] = price;
                active_bids_.sz++;
                return;
            }
        }
        active_bids_.push_back(price);
    }
    void insert_active_ask(int64_t price) noexcept {
        for (size_t i = 0; i < active_asks_.size(); ++i) {
            if (active_asks_[i] == price) return;
            if (active_asks_[i] > price) {
                if (active_asks_.full()) return;
                std::memmove(&active_asks_[i + 1], &active_asks_[i], (active_asks_.size() - i) * sizeof(int64_t));
                active_asks_[i] = price;
                active_asks_.sz++;
                return;
            }
        }
        active_asks_.push_back(price);
    }
    void remove_active_bid(int64_t price) noexcept {
        for (size_t i = 0; i < active_bids_.size(); ++i) {
            if (active_bids_[i] == price) {
                std::memmove(&active_bids_[i], &active_bids_[i + 1], (active_bids_.size() - i - 1) * sizeof(int64_t));
                active_bids_.sz--;
                bids_levels_[price & (MAX_LEVELS - 1)].price = 0;
                return;
            }
        }
    }
    void remove_active_ask(int64_t price) noexcept {
        for (size_t i = 0; i < active_asks_.size(); ++i) {
            if (active_asks_[i] == price) {
                std::memmove(&active_asks_[i], &active_asks_[i + 1], (active_asks_.size() - i - 1) * sizeof(int64_t));
                active_asks_.sz--;
                asks_levels_[price & (MAX_LEVELS - 1)].price = 0;
                return;
            }
        }
    }

    bool can_fully_fill(const Order& order) const noexcept {
        int64_t needed = order.qty;
        if (order.side == Side::BID) {
            for (size_t i = 0; i < active_asks_.size(); ++i) {
                int64_t price = active_asks_[i];
                if (order.price < price) break;
                auto* q = asks_levels_[price & (MAX_LEVELS - 1)].q;
                if (!q) continue;
                for (size_t j = q->head; j < q->tail && needed > 0; j++) {
                    const auto& e = q->entries[j & (256 - 1)];
                    if (e.qty > 0 && e.state != OrderState::CANCELED) {
                        int64_t fill = (needed < e.qty) ? needed : e.qty;
                        needed -= fill;
                    }
                }
                if (needed == 0) break;
            }
        } else {
            for (size_t i = 0; i < active_bids_.size(); ++i) {
                int64_t price = active_bids_[i];
                if (order.price > price) break;
                auto* q = bids_levels_[price & (MAX_LEVELS - 1)].q;
                if (!q) continue;
                for (size_t j = q->head; j < q->tail && needed > 0; j++) {
                    const auto& e = q->entries[j & (256 - 1)];
                    if (e.qty > 0 && e.state != OrderState::CANCELED) {
                        int64_t fill = (needed < e.qty) ? needed : e.qty;
                        needed -= fill;
                    }
                }
                if (needed == 0) break;
            }
        }
        return needed == 0;
    }

    template <bool IsBid>
    void match_against_side(Order& order, FixedVector<Trade, 64>& trades) noexcept {
        auto& active_levels = IsBid ? active_asks_ : active_bids_;
        auto& levels = IsBid ? asks_levels_ : bids_levels_;
        
        for (size_t i = 0; i < active_levels.size() && order.qty > 0;) {
            if (trades.full()) break;
            int64_t price = active_levels[i];
            
            if (order.type != OrderType::MARKET) {
                bool price_ok = IsBid ? (order.price >= price) : (order.price <= price);
                if (!price_ok) break;
            }

            auto* q = levels[price & (MAX_LEVELS - 1)].q;
            if (!q) {
                if (IsBid) remove_active_ask(price);
                else remove_active_bid(price);
                continue;
            }
            while (!q->empty() && order.qty > 0) {
                auto* resting = q->front();
                if (resting->qty == 0 || resting->state == OrderState::CANCELED) {
                    q->pop_front();
                    continue;
                }
                if (trades.full()) break;

                int64_t fill_qty = (order.qty < resting->qty) ? order.qty : resting->qty;
                Trade t{};
                t.trade_id = static_cast<uint64_t>(instrument_id_) * 1000000ULL + (IsBid ? order.id : resting->id);
                t.buy_order_id = IsBid ? order.id : resting->id;
                t.sell_order_id = !IsBid ? order.id : resting->id;
                t.instrument_id = instrument_id_;
                t.price = price;
                t.qty = fill_qty;
                t.timestamp = rdtscp_local(); // Hardware timestamp!
                t.aggressor_side = IsBid ? Side::BID : Side::ASK;
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
            if (q->empty()) {
                if (IsBid) remove_active_ask(price);
                else remove_active_bid(price);
                // remove_active modifies array in place, so do not increment i
            } else {
                ++i;
            }
        }
    }

    uint32_t   instrument_id_;
    Level      bids_levels_[MAX_LEVELS];
    Level      asks_levels_[MAX_LEVELS];
    FixedVector<int64_t, 1024> active_bids_;
    FixedVector<int64_t, 1024> active_asks_;
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

    for (size_t i = 0; i < bids; ++i) {
        int64_t price = book.get_active_bids()[i];
        std::fwrite(&price, sizeof(price), 1, f);
        auto* q = book.get_bids_levels()[price & (OrderBook::MAX_LEVELS - 1)].q;
        if (q) std::fwrite(q, sizeof(OrderQueue<256>), 1, f);
    }
    for (size_t i = 0; i < asks; ++i) {
        int64_t price = book.get_active_asks()[i];
        std::fwrite(&price, sizeof(price), 1, f);
        auto* q = book.get_asks_levels()[price & (OrderBook::MAX_LEVELS - 1)].q;
        if (q) std::fwrite(q, sizeof(OrderQueue<256>), 1, f);
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
