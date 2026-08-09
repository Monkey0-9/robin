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

// Ring queue that never rejects live orders: canceled / zero-qty entries are
// compacted out before the queue is reported as full, so stale cancels cannot
// clog a level and cause spurious REJECTED results.
template <size_t MaxOrders = 256>
struct alignas(64) OrderQueue {
    static_assert((MaxOrders & (MaxOrders - 1)) == 0,
                "MaxOrders must be a power of 2");
    Order  entries[MaxOrders];
    size_t head = 0;
    size_t tail = 0;

    static bool is_live(const Order& o) noexcept {
        return o.qty > 0 && o.state != OrderState::CANCELED;
    }

    bool push(const Order& o) noexcept {
        if (tail - head >= MaxOrders) compact();
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

    size_t live_count() const noexcept {
        size_t n = 0;
        for (size_t i = head; i < tail; ++i)
            if (is_live(entries[i & (MaxOrders - 1)])) n++;
        return n;
    }

    // Repack live orders contiguously from index 0; O(live) work.
    void compact() noexcept {
        size_t w = 0;
        for (size_t i = head; i < tail; ++i) {
            const Order& e = entries[i & (MaxOrders - 1)];
            if (is_live(e)) {
                if (w != (i & (MaxOrders - 1))) entries[w] = e;
                w++;
            }
        }
        head = 0;
        tail = w;
    }

    // First live order matching id, or nullptr.
    Order* find(uint64_t id) noexcept {
        for (size_t i = head; i < tail; ++i) {
            auto& e = entries[i & (MaxOrders - 1)];
            if (e.id == id && is_live(e)) return &e;
        }
        return nullptr;
    }
};

#ifdef _MSC_VER
#include <intrin.h>
inline uint64_t rdtscp_local() {
    unsigned int aux;
    return __rdtscp(&aux);
}
#elif defined(__x86_64__) || defined(_M_X64) || defined(__i386__)
inline uint64_t rdtscp_local() {
    uint32_t aux;
    uint64_t rax, rdx;
    __asm__ __volatile__("rdtscp" : "=a"(rax), "=d"(rdx), "=c"(aux));
    return (rdx << 32) | rax;
}
#else
inline uint64_t rdtscp_local() {
    return static_cast<uint64_t>(
        std::chrono::high_resolution_clock::now().time_since_epoch()).count();
}
#endif

// Full-avalanche hash (MurmurHash3 fmix64) applied to the price before masking
// to the power-of-2 table. Combined with explicit slot states (EMPTY/LIVE/
// TOMBSTONE) this removes both the weak hashing and the 0/-1 sentinel-collision
// paths that previously caused legitimate orders to be dropped or rejected.
inline uint64_t mix64(uint64_t val) noexcept {
    val ^= val >> 33;
    val *= 0xff51afd7ed558ccdULL;
    val ^= val >> 33;
    val *= 0xc4ceb9fe1a85ec53ULL;
    val ^= val >> 33;
    return val;
}

class OrderBook {
public:
    static constexpr size_t MAX_LEVELS = 131072;

    enum class SlotState : uint8_t {
        EMPTY = 0,
        LIVE = 1,
        TOMBSTONE = 2
    };

    struct Level {
        int64_t price = 0;
        OrderQueue<256>* q = nullptr;
        SlotState state = SlotState::EMPTY;
    };

    OrderBook() noexcept : instrument_id_(0), overflow_drops_(0), luld_band_bps_(500), luld_halted_(false), luld_ref_price_(0) {}
    explicit OrderBook(uint32_t instrument_id) noexcept
        : instrument_id_(instrument_id), overflow_drops_(0), luld_band_bps_(500), luld_halted_(false), luld_ref_price_(0) {}

    // Destructor is O(allocated queues), not O(MAX_LEVELS): every queue pointer
    // that was actually allocated is tracked in allocated_queues_.
    ~OrderBook() noexcept {
        for (auto* q : allocated_queues_) delete q;
    }

    // ------------------------------------------------------------------ //
    // Order lifecycle                                                     //
    // ------------------------------------------------------------------ //

    [[nodiscard]]
    bool match_order(Order& order, FixedVector<Trade, 64>& trades) noexcept {
        if (order.type == OrderType::FOK) {
            if (!can_fully_fill(order)) { order.state = OrderState::CANCELED; return true; }
        }
        if (order.type == OrderType::IOC && (order.flags & FLAG_IOC_RESTRICT) && order.min_qty > 0) {
            if (!can_fill_min_qty(order, order.min_qty)) { order.state = OrderState::CANCELED; return true; }
        }
        if ((order.flags & FLAG_POST_ONLY) && would_cross(order)) {
            order.state = OrderState::REJECTED;
            return true;
        }
        if (order.min_qty > 0 && order.qty < order.min_qty) {
            // Odd-lot / below-minimum resting order: reject rather than book it.
            order.state = OrderState::REJECTED;
            return true;
        }
        if (luld_halted_) {
            order.state = OrderState::REJECTED;
            return true;
        }
        if (luld_ref_price_ > 0 && !price_within_luld(order.price)) {
            order.state = OrderState::REJECTED;
            return true;
        }

        int64_t filled_before = 0;
        if (order.side == Side::BID) {
            if (!match_against_side<true>(order, trades)) { order.state = OrderState::REJECTED; return true; }
            if (order.qty > 0) {
                bool reduce_only = (order.flags & FLAG_REDUCE_ONLY) != 0;
                if (order.type == OrderType::IOC || order.type == OrderType::MARKET || reduce_only) {
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
            if (!match_against_side<false>(order, trades)) { order.state = OrderState::REJECTED; return true; }
            if (order.qty > 0) {
                bool reduce_only = (order.flags & FLAG_REDUCE_ONLY) != 0;
                if (order.type == OrderType::IOC || order.type == OrderType::MARKET || reduce_only) {
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
        (void)filled_before;
        return true;
    }

    // Cancel-replace: remove the resting order identified by (id, price, side)
    // and re-enter it at new_price/new_qty, matching if it now crosses.
    [[nodiscard]]
    bool replace_order(const Order& request, FixedVector<Trade, 64>& trades) noexcept {
        Order* resting = find_order(request.id, request.price, request.side);
        if (!resting) return false;

        Order repl = request;
        repl.price = request.new_price;
        repl.qty = request.new_qty;
        repl.state = OrderState::REPLACED;
        repl.type = (request.type == OrderType::REPLACE)
            ? OrderType::LIMIT
            : request.type;

        resting->state = OrderState::CANCELED;
        resting->qty = 0;
        purge_level_if_empty(request.side);

        if (repl.qty <= 0) return true; // reduce-only replace: pure cancel
        if (repl.min_qty > 0 && repl.qty < repl.min_qty) return false;
        return match_order(repl, trades);
    }

    bool cancel_order(uint64_t order_id, int64_t price, Side side) noexcept {
        if (price != 0) {
            size_t idx = (side == Side::BID) ? find_bid_idx(price) : find_ask_idx(price);
            if (idx != (size_t)-1) {
                auto* q = (side == Side::BID) ? bids_levels_[idx].q : asks_levels_[idx].q;
                if (q) {
                    Order* e = q->find(order_id);
                    if (e) {
                        e->state = OrderState::CANCELED;
                        e->qty = 0;
                        purge_level_if_empty(side);
                        return true;
                    }
                }
            }
        }
        // Price unknown (or price-based lookup failed): scan active levels for id.
        auto& active = (side == Side::BID) ? active_bids_ : active_asks_;
        for (size_t i = 0; i < active.size(); ++i) {
            int64_t level_price = active[i];
            size_t idx = (side == Side::BID) ? find_bid_idx(level_price) : find_ask_idx(level_price);
            if (idx == (size_t)-1) continue;
            auto* q = (side == Side::BID) ? bids_levels_[idx].q : asks_levels_[idx].q;
            if (!q) continue;
            Order* e = q->find(order_id);
            if (e) {
                e->state = OrderState::CANCELED;
                e->qty = 0;
                purge_level_if_empty(side);
                return true;
            }
        }
        return false;
    }

    // ------------------------------------------------------------------ //
    // Introspection                                                       //
    // ------------------------------------------------------------------ //

    OrderQueue<256>* get_bid_queue(int64_t price) const noexcept {
        size_t idx = find_bid_idx(price);
        return (idx != (size_t)-1) ? bids_levels_[idx].q : nullptr;
    }

    OrderQueue<256>* get_ask_queue(int64_t price) const noexcept {
        size_t idx = find_ask_idx(price);
        return (idx != (size_t)-1) ? asks_levels_[idx].q : nullptr;
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

    // Tear down all internal state (levels, queues, halts) so the book can be
    // rebuilt from a snapshot without copying a 6MB stack object.
    void reset() noexcept {
        for (auto* q : allocated_queues_) delete q;
        allocated_queues_.clear();
        for (auto& l : bids_levels_) { l = Level(); }
        for (auto& l : asks_levels_) { l = Level(); }
        active_bids_.clear();
        active_asks_.clear();
        overflow_drops_ = 0;
        luld_halted_ = false;
        luld_ref_price_ = 0;
        luld_band_bps_ = 500;
    }

    void set_luld_band_bps(uint32_t band_bps) noexcept { luld_band_bps_ = band_bps; }
    bool luld_halted() const noexcept { return luld_halted_; }
    void halt_luld() noexcept { luld_halted_ = true; }
    void resume_luld() noexcept { luld_halted_ = false; }

    void add_bid_level(int64_t price, const OrderQueue<256>& q) {
        size_t idx = find_or_create_bid_idx(price);
        if (idx == (size_t)-1) return;
        bids_levels_[idx].price = price;
        if (!bids_levels_[idx].q) {
            bids_levels_[idx].q = new OrderQueue<256>();
            allocated_queues_.push_back(bids_levels_[idx].q);
        }
        *bids_levels_[idx].q = q;
        bids_levels_[idx].state = SlotState::LIVE;
        insert_active_bid(price);
    }
    void add_ask_level(int64_t price, const OrderQueue<256>& q) {
        size_t idx = find_or_create_ask_idx(price);
        if (idx == (size_t)-1) return;
        asks_levels_[idx].price = price;
        if (!asks_levels_[idx].q) {
            asks_levels_[idx].q = new OrderQueue<256>();
            allocated_queues_.push_back(asks_levels_[idx].q);
        }
        *asks_levels_[idx].q = q;
        asks_levels_[idx].state = SlotState::LIVE;
        insert_active_ask(price);
    }

private:
    // ------------------------------------------------------------------ //
    // Hash table (open addressing, linear probing, tombstones)            //
    // ------------------------------------------------------------------ //

    static inline size_t hash_price(int64_t price) noexcept {
        return static_cast<size_t>(mix64(static_cast<uint64_t>(price))) & (MAX_LEVELS - 1);
    }

    size_t find_or_create_bid_idx(int64_t price) noexcept {
        size_t idx = hash_price(price);
        size_t start = idx;
        size_t first_tombstone = (size_t)-1;
        for (;;) {
            SlotState st = bids_levels_[idx].state;
            if (st == SlotState::EMPTY) {
                return (first_tombstone != (size_t)-1) ? first_tombstone : idx;
            }
            if (st == SlotState::TOMBSTONE) {
                if (first_tombstone == (size_t)-1) first_tombstone = idx;
            } else if (bids_levels_[idx].price == price) {
                return idx;
            }
            idx = (idx + 1) & (MAX_LEVELS - 1);
            if (idx == start) break;
        }
        return first_tombstone; // table full: reuse tombstone or fail
    }

    size_t find_or_create_ask_idx(int64_t price) noexcept {
        size_t idx = hash_price(price);
        size_t start = idx;
        size_t first_tombstone = (size_t)-1;
        for (;;) {
            SlotState st = asks_levels_[idx].state;
            if (st == SlotState::EMPTY) {
                return (first_tombstone != (size_t)-1) ? first_tombstone : idx;
            }
            if (st == SlotState::TOMBSTONE) {
                if (first_tombstone == (size_t)-1) first_tombstone = idx;
            } else if (asks_levels_[idx].price == price) {
                return idx;
            }
            idx = (idx + 1) & (MAX_LEVELS - 1);
            if (idx == start) break;
        }
        return first_tombstone;
    }

    size_t find_bid_idx(int64_t price) const noexcept {
        size_t idx = hash_price(price);
        size_t start = idx;
        while (bids_levels_[idx].state != SlotState::EMPTY) {
            if (bids_levels_[idx].state == SlotState::LIVE && bids_levels_[idx].price == price) return idx;
            idx = (idx + 1) & (MAX_LEVELS - 1);
            if (idx == start) break;
        }
        return (size_t)-1;
    }

    size_t find_ask_idx(int64_t price) const noexcept {
        size_t idx = hash_price(price);
        size_t start = idx;
        while (asks_levels_[idx].state != SlotState::EMPTY) {
            if (asks_levels_[idx].state == SlotState::LIVE && asks_levels_[idx].price == price) return idx;
            idx = (idx + 1) & (MAX_LEVELS - 1);
            if (idx == start) break;
        }
        return (size_t)-1;
    }

    // ------------------------------------------------------------------ //
    // Level management                                                    //
    // ------------------------------------------------------------------ //

    OrderQueue<256>* allocate_queue() {
        auto* q = new OrderQueue<256>();
        allocated_queues_.push_back(q);
        return q;
    }

    // Remove a queue from the tracking vector and release it. Keeps the
    // destructor (and purge) from double-freeing.
    void release_queue(OrderQueue<256>* q) noexcept {
        for (size_t k = 0; k < allocated_queues_.size(); ++k) {
            if (allocated_queues_[k] == q) {
                allocated_queues_.erase(allocated_queues_.begin() + k);
                break;
            }
        }
        delete q;
    }

    bool push_bid(const Order& order) noexcept {
        size_t idx = find_or_create_bid_idx(order.price);
        if (idx == (size_t)-1) return false;
        bids_levels_[idx].price = order.price;
        if (!bids_levels_[idx].q) bids_levels_[idx].q = allocate_queue();
        bids_levels_[idx].state = SlotState::LIVE;
        if (!bids_levels_[idx].q->push(order)) return false;
        insert_active_bid(order.price);
        return true;
    }
    bool push_ask(const Order& order) noexcept {
        size_t idx = find_or_create_ask_idx(order.price);
        if (idx == (size_t)-1) return false;
        asks_levels_[idx].price = order.price;
        if (!asks_levels_[idx].q) asks_levels_[idx].q = allocate_queue();
        asks_levels_[idx].state = SlotState::LIVE;
        if (!asks_levels_[idx].q->push(order)) return false;
        insert_active_ask(order.price);
        return true;
    }

    // Remove the level (queue deleted, slot tombstoned, active list updated)
    // if it no longer contains any live order.
    void purge_level_if_empty(Side side) noexcept {
        auto& active = (side == Side::BID) ? active_bids_ : active_asks_;
        for (size_t i = 0; i < active.size(); ++i) {
            int64_t level_price = active[i];
            size_t idx = (side == Side::BID) ? find_bid_idx(level_price) : find_ask_idx(level_price);
            if (idx == (size_t)-1) continue;
            auto* q = (side == Side::BID) ? bids_levels_[idx].q : asks_levels_[idx].q;
            if (!q) continue;
            if (q->live_count() > 0) continue;
            release_queue(q);
            if (side == Side::BID) {
                bids_levels_[idx].q = nullptr;
                bids_levels_[idx].state = SlotState::TOMBSTONE;
                remove_active_bid(level_price);
            } else {
                asks_levels_[idx].q = nullptr;
                asks_levels_[idx].state = SlotState::TOMBSTONE;
                remove_active_ask(level_price);
            }
            return;
        }
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
                return;
            }
        }
    }
    void remove_active_ask(int64_t price) noexcept {
        for (size_t i = 0; i < active_asks_.size(); ++i) {
            if (active_asks_[i] == price) {
                std::memmove(&active_asks_[i], &active_asks_[i + 1], (active_asks_.size() - i - 1) * sizeof(int64_t));
                active_asks_.sz--;
                return;
            }
        }
    }

    Order* find_order(uint64_t order_id, int64_t price, Side side) noexcept {
        if (price != 0) {
            size_t idx = (side == Side::BID) ? find_bid_idx(price) : find_ask_idx(price);
            if (idx != (size_t)-1) {
                auto* q = (side == Side::BID) ? bids_levels_[idx].q : asks_levels_[idx].q;
                if (q) {
                    if (Order* e = q->find(order_id)) return e;
                }
            }
        }
        auto& active = (side == Side::BID) ? active_bids_ : active_asks_;
        for (size_t i = 0; i < active.size(); ++i) {
            size_t idx = (side == Side::BID) ? find_bid_idx(active[i]) : find_ask_idx(active[i]);
            if (idx == (size_t)-1) continue;
            auto* q = (side == Side::BID) ? bids_levels_[idx].q : asks_levels_[idx].q;
            if (!q) continue;
            if (Order* e = q->find(order_id)) return e;
        }
        return nullptr;
    }

    // ------------------------------------------------------------------ //
    // Risk semantics                                                      //
    // ------------------------------------------------------------------ //

    bool same_account(const Order& a, const Order& b) const noexcept {
        return a.account_id != 0 && a.account_id == b.account_id;
    }

    bool would_cross(const Order& order) const noexcept {
        int64_t opposite_best = (order.side == Side::BID) ? best_ask() : best_bid();
        if (opposite_best == 0) return false;
        return (order.side == Side::BID) ? (opposite_best <= order.price) : (opposite_best >= order.price);
    }

    bool price_within_luld(int64_t price) const noexcept {
        if (luld_ref_price_ <= 0) return true;
        int64_t band = (luld_ref_price_ * static_cast<int64_t>(luld_band_bps_)) / 10000;
        int64_t lo = luld_ref_price_ - band;
        int64_t hi = luld_ref_price_ + band;
        return price >= lo && price <= hi;
    }

    bool can_fully_fill(const Order& order) const noexcept {
        return can_fill_min_qty(order, order.qty);
    }

    bool can_fill_min_qty(const Order& order, int64_t min_qty) const noexcept {
        int64_t available = 0;
        if (order.side == Side::BID) {
            for (size_t i = 0; i < active_asks_.size() && available < min_qty; ++i) {
                int64_t price = active_asks_[i];
                if (order.price < price) break;
                available += level_liquidity<false>(price, order);
            }
        } else {
            for (size_t i = 0; i < active_bids_.size() && available < min_qty; ++i) {
                int64_t price = active_bids_[i];
                if (order.price > price) break;
                available += level_liquidity<true>(price, order);
            }
        }
        return available >= min_qty;
    }

    template <bool RestingIsBid>
    int64_t level_liquidity(int64_t price, const Order& aggressor) const noexcept {
        int64_t sum = 0;
        size_t idx = RestingIsBid ? find_bid_idx(price) : find_ask_idx(price);
        auto* q = (idx != (size_t)-1) ? (RestingIsBid ? bids_levels_[idx].q : asks_levels_[idx].q) : nullptr;
        if (!q) return 0;
        for (size_t j = q->head; j < q->tail; ++j) {
            const auto& e = q->entries[j & (256 - 1)];
            if (!OrderQueue<256>::is_live(e)) continue;
            if (same_account(e, aggressor) && aggressor.stp_mode != STP_ALLOW) continue;
            sum += e.qty;
        }
        return sum;
    }

    template <bool IsBid>
    bool match_against_side(Order& order, FixedVector<Trade, 64>& trades) noexcept {
        auto& active_levels = IsBid ? active_asks_ : active_bids_;
        auto& levels = IsBid ? asks_levels_ : bids_levels_;

        for (size_t i = 0; i < active_levels.size() && order.qty > 0;) {
            if (trades.full()) break;
            int64_t price = active_levels[i];

            if (order.type != OrderType::MARKET) {
                bool price_ok = IsBid ? (order.price >= price) : (order.price <= price);
                if (!price_ok) break;
            }

            if (luld_ref_price_ > 0 && !price_within_luld(price)) {
                // Fill would exceed the LULD band: halt this instrument.
                luld_halted_ = true;
                order.state = OrderState::REJECTED;
                return false;
            }

            size_t idx = IsBid ? find_ask_idx(price) : find_bid_idx(price);
            auto* q = (idx != (size_t)-1) ? levels[idx].q : nullptr;
            if (!q) {
                if (IsBid) remove_active_ask(price); else remove_active_bid(price);
                continue;
            }

            bool level_dirty = false;
            while (!q->empty() && order.qty > 0) {
                auto* resting = q->front();
                if (resting->qty == 0 || resting->state == OrderState::CANCELED) {
                    q->pop_front();
                    level_dirty = true;
                    continue;
                }
                if (trades.full()) break;

                if (same_account(*resting, order)) {
                    if (order.stp_mode == STP_ALLOW) {
                        // fall through and trade (testing / explicit override)
                    } else if (order.stp_mode == STP_CANCEL_OLDEST) {
                        resting->qty = 0;
                        resting->state = OrderState::CANCELED;
                        q->pop_front();
                        level_dirty = true;
                        continue;
                    } else {
                        // STP_BLOCK: reject the aggressor rather than self-trade.
                        order.state = OrderState::REJECTED;
                        return false;
                    }
                }

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
                luld_ref_price_ = price;
                if (resting->qty == 0) {
                    resting->state = OrderState::FILLED;
                    q->pop_front();
                    level_dirty = true;
                } else {
                    resting->state = OrderState::PARTIAL_FILL;
                }
            }
            if (q->empty()) {
                if (IsBid) remove_active_ask(price); else remove_active_bid(price);
                // level removed in place; do not advance i
            } else {
                ++i;
            }
        }
        return true;
    }

    uint32_t   instrument_id_;
    Level      bids_levels_[MAX_LEVELS];
    Level      asks_levels_[MAX_LEVELS];
    std::vector<OrderQueue<256>*> allocated_queues_;
    FixedVector<int64_t, 1024> active_bids_;
    FixedVector<int64_t, 1024> active_asks_;
    uint64_t   overflow_drops_;
    uint32_t   luld_band_bps_;
    bool       luld_halted_;
    int64_t    luld_ref_price_;
};

} // namespace execution
} // namespace quantum

// ----------------------------------------------------------------------- //
// Snapshot serialization (v2): CRC-32C verified, explicit little-endian,   //
// versioned, with hard bounds on counts to survive corrupt input.          //
// ----------------------------------------------------------------------- //
#include <cstdio>

namespace quantum {
namespace execution {

constexpr uint32_t SNAPSHOT_MAGIC = 0x524F424E; // "ROBN"
constexpr uint32_t SNAPSHOT_VERSION = 2;

// CRC-32C (Castagnoli, reflected poly 0x82F63B78), bitwise — portable.
inline uint32_t crc32c(const void* data, size_t size, uint32_t crc = 0xFFFFFFFFU) noexcept {
    const uint8_t* p = static_cast<const uint8_t*>(data);
    crc ^= 0xFFFFFFFFU;
    for (size_t i = 0; i < size; ++i) {
        crc ^= p[i];
        for (int k = 0; k < 8; ++k)
            crc = (crc >> 1) ^ (0x82F63B78U & (0U - (crc & 1U)));
    }
    return crc ^ 0xFFFFFFFFU;
}

namespace snap {
inline void put_u8(std::vector<uint8_t>& b, uint8_t v) noexcept { b.push_back(v); }
inline void put_u16(std::vector<uint8_t>& b, uint16_t v) noexcept {
    b.push_back((uint8_t)(v)); b.push_back((uint8_t)(v >> 8));
}
inline void put_u32(std::vector<uint8_t>& b, uint32_t v) noexcept {
    b.push_back((uint8_t)(v)); b.push_back((uint8_t)(v >> 8));
    b.push_back((uint8_t)(v >> 16)); b.push_back((uint8_t)(v >> 24));
}
inline void put_u64(std::vector<uint8_t>& b, uint64_t v) noexcept {
    for (int i = 0; i < 8; ++i) b.push_back((uint8_t)(v >> (8 * i)));
}
inline void put_i64(std::vector<uint8_t>& b, int64_t v) noexcept { put_u64(b, static_cast<uint64_t>(v)); }

inline uint8_t  get_u8(const uint8_t* p, size_t& o) noexcept { return p[o++]; }
inline uint16_t get_u16(const uint8_t* p, size_t& o) noexcept {
    uint16_t v = p[o] | (uint16_t)(p[o + 1] << 8); o += 2; return v;
}
inline uint32_t get_u32(const uint8_t* p, size_t& o) noexcept {
    uint32_t v = p[o] | (uint32_t)(p[o + 1] << 8) | (uint32_t)(p[o + 2] << 16) | (uint32_t)(p[o + 3] << 24);
    o += 4; return v;
}
inline uint64_t get_u64(const uint8_t* p, size_t& o) noexcept {
    uint64_t v = 0;
    for (int i = 0; i < 8; ++i) v |= (uint64_t)p[o + i] << (8 * i);
    o += 8; return v;
}
inline int64_t get_i64(const uint8_t* p, size_t& o) noexcept { return static_cast<int64_t>(get_u64(p, o)); }

// Explicit little-endian encoding of an Order (host-endian independent).
inline void put_order(std::vector<uint8_t>& b, const Order& o) noexcept {
    put_u64(b, o.id);
    put_i64(b, o.price);
    put_i64(b, o.qty);
    put_i64(b, o.min_qty);
    put_i64(b, o.new_price);
    put_i64(b, o.new_qty);
    put_u32(b, o.instrument_id);
    put_u32(b, o.client_id);
    put_u32(b, o.account_id);
    put_u8(b, o.flags);
    put_u8(b, o.stp_mode);
    put_u8(b, static_cast<uint8_t>(o.side));
    put_u8(b, static_cast<uint8_t>(o.state));
    put_u8(b, static_cast<uint8_t>(o.type));
}

inline bool get_order(const uint8_t* p, size_t& o, size_t end, Order& out) noexcept {
    if (o + 65 > end) return false;
    out.id = get_u64(p, o);
    out.price = get_i64(p, o);
    out.qty = get_i64(p, o);
    out.min_qty = get_i64(p, o);
    out.new_price = get_i64(p, o);
    out.new_qty = get_i64(p, o);
    out.instrument_id = get_u32(p, o);
    out.client_id = get_u32(p, o);
    out.account_id = get_u32(p, o);
    out.flags = get_u8(p, o);
    out.stp_mode = get_u8(p, o);
    out.side = static_cast<Side>(get_u8(p, o));
    out.state = static_cast<OrderState>(get_u8(p, o));
    out.type = static_cast<OrderType>(get_u8(p, o));
    return true;
}
} // namespace snap

inline bool save_snapshot(const OrderBook& book, const char* path) noexcept {
    std::vector<uint8_t> out;
    out.reserve(4096);
    snap::put_u32(out, SNAPSHOT_MAGIC);
    snap::put_u32(out, SNAPSHOT_VERSION);
    snap::put_u32(out, book.instrument_id());
    snap::put_u32(out, static_cast<uint32_t>(book.bid_levels()));
    snap::put_u32(out, static_cast<uint32_t>(book.ask_levels()));
    snap::put_u32(out, book.luld_halted() ? 1u : 0u);

    for (size_t i = 0; i < book.bid_levels(); ++i) {
        int64_t price = book.get_active_bids()[i];
        auto* q = book.get_bid_queue(price);
        snap::put_i64(out, price);
        snap::put_u16(out, 0); // reserved
        size_t count_pos = out.size();
        snap::put_u32(out, 0); // live count (patched below)
        uint32_t live = 0;
        for (size_t j = q ? q->head : 0; q && j < q->tail; ++j) {
            const auto& e = q->entries[j & 255];
            if (OrderQueue<256>::is_live(e)) {
                snap::put_order(out, e);
                live++;
            }
        }
        out[count_pos]     = (uint8_t)(live);
        out[count_pos + 1] = (uint8_t)(live >> 8);
        out[count_pos + 2] = (uint8_t)(live >> 16);
        out[count_pos + 3] = (uint8_t)(live >> 24);
    }
    for (size_t i = 0; i < book.ask_levels(); ++i) {
        int64_t price = book.get_active_asks()[i];
        auto* q = book.get_ask_queue(price);
        snap::put_i64(out, price);
        snap::put_u16(out, 0);
        size_t count_pos = out.size();
        snap::put_u32(out, 0);
        uint32_t live = 0;
        for (size_t j = q ? q->head : 0; q && j < q->tail; ++j) {
            const auto& e = q->entries[j & 255];
            if (OrderQueue<256>::is_live(e)) {
                snap::put_order(out, e);
                live++;
            }
        }
        out[count_pos]     = (uint8_t)(live);
        out[count_pos + 1] = (uint8_t)(live >> 8);
        out[count_pos + 2] = (uint8_t)(live >> 16);
        out[count_pos + 3] = (uint8_t)(live >> 24);
    }

    uint32_t payload_crc = crc32c(out.data(), out.size());
    snap::put_u32(out, payload_crc);

    FILE* f = std::fopen(path, "wb");
    if (!f) return false;
    size_t written = std::fwrite(out.data(), 1, out.size(), f);
    std::fclose(f);
    if (written != out.size()) return false;
    std::printf("[SNAPSHOT] Saved order book %u to %s (%zu bytes, crc=0x%08X)\n",
                book.instrument_id(), path, out.size(), payload_crc);
    return true;
}

inline bool load_snapshot(OrderBook& book, const char* path) noexcept {
    FILE* f = std::fopen(path, "rb");
    if (!f) return false;
    std::fseek(f, 0, SEEK_END);
    long fsize = std::ftell(f);
    std::fseek(f, 0, SEEK_SET);
    if (fsize < 40) { std::fclose(f); return false; }
    std::vector<uint8_t> buf(static_cast<size_t>(fsize));
    if (std::fread(buf.data(), 1, buf.size(), f) != buf.size()) { std::fclose(f); return false; }
    std::fclose(f);

    size_t o = 0;
    uint32_t magic = snap::get_u32(buf.data(), o);
    uint32_t version = snap::get_u32(buf.data(), o);
    if (magic != SNAPSHOT_MAGIC || version != SNAPSHOT_VERSION) {
        std::printf("[SNAPSHOT] Invalid header magic=0x%08X ver=%u (expected ver=%u)\n",
                    magic, version, SNAPSHOT_VERSION);
        return false;
    }
    uint32_t id = snap::get_u32(buf.data(), o);
    uint32_t n_bids = snap::get_u32(buf.data(), o);
    uint32_t n_asks = snap::get_u32(buf.data(), o);
    uint32_t halted = snap::get_u32(buf.data(), o);

    // Bound-check: header (20) + per-level min 14 bytes + 4 crc trailer.
    if (n_bids > OrderBook::MAX_LEVELS || n_asks > OrderBook::MAX_LEVELS) return false;
    const size_t end = buf.size() - 4; // trailing crc
    if (o > end) return false;

    book.reset();
    book.set_instrument_id(id);
    book.set_luld_band_bps(500);
    if (halted) book.halt_luld();

    for (uint32_t i = 0; i < n_bids && o <= end; ++i) {
        int64_t price = snap::get_i64(buf.data(), o);
        o += 2; // reserved u16
        uint32_t live = snap::get_u32(buf.data(), o);
        if (live > 256 || o + (size_t)live * 65 > end) return false;
        OrderQueue<256> q;
        for (uint32_t j = 0; j < live; ++j) {
            Order ord;
            if (!snap::get_order(buf.data(), o, end, ord)) return false;
            ord.state = OrderState::WORKING;
            q.push(ord);
        }
        book.add_bid_level(price, q);
    }
    for (uint32_t i = 0; i < n_asks && o <= end; ++i) {
        int64_t price = snap::get_i64(buf.data(), o);
        o += 2;
        uint32_t live = snap::get_u32(buf.data(), o);
        if (live > 256 || o + (size_t)live * 65 > end) return false;
        OrderQueue<256> q;
        for (uint32_t j = 0; j < live; ++j) {
            Order ord;
            if (!snap::get_order(buf.data(), o, end, ord)) return false;
            ord.state = OrderState::WORKING;
            q.push(ord);
        }
        book.add_ask_level(price, q);
    }
    if (o != end) return false;

    uint32_t stored_crc = snap::get_u32(buf.data(), o);
    uint32_t calc_crc = crc32c(buf.data(), end);
    if (stored_crc != calc_crc) {
        std::printf("[SNAPSHOT] CRC mismatch: file=0x%08X calc=0x%08X\n", stored_crc, calc_crc);
        return false;
    }

    std::printf("[SNAPSHOT] Loaded order book %u from %s (%u bids, %u asks, crc verified)\n",
                id, path, n_bids, n_asks);
    return true;
}

} // namespace execution
} // namespace quantum
