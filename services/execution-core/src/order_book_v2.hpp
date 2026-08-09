#pragma once
// FastOrderBook — sparse order book (v2).
//
// Replaces the previous dense `std::array<PriceLevel, 100_000_000>` which cost
// ~1.6 GB per instrument. This version uses a fixed-size open-addressing hash
// table (SPARSE_SLOTS slots) so memory is a few MB regardless of the price
// grid, while keeping the same public API plus side-aware cancel.
//
// Bids and asks are tracked per price slot with separate quantities so the best
// bid / best ask remain correct even when the same price holds both sides.
//
// Trade-offs (documented):
//  - add_order / cancel_order are O(1) amortized with an atomic best bid/ask
//    cache. When the cached best level is removed and becomes stale, best_bid()
//    / best_ask() lazily rescan the hash table once (O(SPARSE_SLOTS)), which is
//    a rare event for a live book.
//  - get_snapshot probes at most `max_levels` prices around the best levels.

#include <atomic>
#include <cstdint>
#include <memory>
#include <thread>
#include <vector>
#include <utility>

#if defined(_MSC_VER)
#include <intrin.h>
#elif defined(__x86_64__) || defined(_M_X64) || defined(__i386__)
#include <immintrin.h>
#endif

constexpr int64_t PRICE_SCALE = 100; // cents
constexpr int64_t MIN_PRICE = 0;
constexpr int64_t MAX_PRICE = 100000000; // $1M in cents
constexpr int64_t BAD_PRICE = -1;

class SpinLock {
public:
    void lock() noexcept {
        while (flag_.test_and_set(std::memory_order_acquire)) {
#if defined(_MSC_VER) || defined(__x86_64__) || defined(_M_X64) || defined(__i386__)
            _mm_pause();
#else
            std::this_thread::yield();
#endif
        }
    }
    void unlock() noexcept { flag_.clear(std::memory_order_release); }
private:
    std::atomic_flag flag_ = ATOMIC_FLAG_INIT;
};

inline uint64_t fastbook_mix64(uint64_t val) noexcept {
    val ^= val >> 33;
    val *= 0xff51afd7ed558ccdULL;
    val ^= val >> 33;
    val *= 0xc4ceb9fe1a85ec53ULL;
    val ^= val >> 33;
    return val;
}

class FastOrderBook {
public:
    static constexpr size_t SPARSE_SLOTS = 1u << 18; // 262144 slots ≈ 4 MB
    static constexpr size_t NPOS = (size_t)-1;

    enum SlotState : uint8_t { SLOT_EMPTY = 0, SLOT_LIVE = 1, SLOT_TOMBSTONE = 2 };

    struct Slot {
        int64_t price = BAD_PRICE;
        uint8_t state = SLOT_EMPTY;
        std::atomic<uint64_t> bid_qty{0};
        std::atomic<uint64_t> ask_qty{0};
    };

    struct Snapshot {
        int64_t best_bid = BAD_PRICE;
        int64_t best_ask = BAD_PRICE;
        std::vector<std::pair<uint64_t, uint64_t>> bids;
        std::vector<std::pair<uint64_t, uint64_t>> asks;
    };

    FastOrderBook() noexcept
        : slots_(new Slot[SPARSE_SLOTS]()) {
        best_bid_.store(BAD_PRICE, std::memory_order_relaxed);
        best_ask_.store(BAD_PRICE, std::memory_order_relaxed);
    }
    FastOrderBook(const FastOrderBook&) = delete;
    FastOrderBook& operator=(const FastOrderBook&) = delete;
    ~FastOrderBook() noexcept = default;

    // Estimated resident bytes of the slot table.
    size_t memory_bytes() const noexcept {
        return SPARSE_SLOTS * sizeof(Slot);
    }

    bool add_order(uint64_t price_cents, uint64_t qty, bool is_bid) noexcept {
        if (qty == 0 || price_cents > (uint64_t)MAX_PRICE) return false;
        lock_.lock();
        size_t idx = find_or_create((int64_t)price_cents);
        bool ok = idx != NPOS;
        if (ok) {
            slots_[idx].price = (int64_t)price_cents;
            slots_[idx].state = SLOT_LIVE;
            if (is_bid) slots_[idx].bid_qty.fetch_add(qty, std::memory_order_relaxed);
            else        slots_[idx].ask_qty.fetch_add(qty, std::memory_order_relaxed);
        }
        lock_.unlock();
        if (!ok) return false;

        int64_t target = (int64_t)price_cents;
        if (is_bid) {
            int64_t cur = best_bid_.load(std::memory_order_relaxed);
            while (target > cur) {
                if (best_bid_.compare_exchange_weak(cur, target, std::memory_order_relaxed)) break;
            }
        } else {
            int64_t cur = best_ask_.load(std::memory_order_relaxed);
            while (cur == BAD_PRICE || target < cur) {
                if (best_ask_.compare_exchange_weak(cur, target, std::memory_order_relaxed)) break;
            }
        }
        return true;
    }

    // Cancel `qty` from the given side at a price. Deprecated 2-arg form
    // (without side) is provided below for source compatibility.
    bool cancel_order(uint64_t price_cents, uint64_t qty, bool is_bid) noexcept {
        lock_.lock();
        size_t idx = find_live((int64_t)price_cents);
        if (idx == NPOS) { lock_.unlock(); return false; }
        Slot& s = slots_[idx];
        if (is_bid) {
            uint64_t before = s.bid_qty.load(std::memory_order_relaxed);
            s.bid_qty.store((qty >= before) ? 0 : before - qty, std::memory_order_relaxed);
        } else {
            uint64_t before = s.ask_qty.load(std::memory_order_relaxed);
            s.ask_qty.store((qty >= before) ? 0 : before - qty, std::memory_order_relaxed);
        }
        if (s.bid_qty.load(std::memory_order_relaxed) == 0 && s.ask_qty.load(std::memory_order_relaxed) == 0)
            s.state = SLOT_TOMBSTONE;
        lock_.unlock();
        return true;
    }

    bool cancel_order(uint64_t price_cents, uint64_t qty) noexcept {
        // Side unknown: cancel from whichever side holds quantity (bid first).
        lock_.lock();
        size_t idx = find_live((int64_t)price_cents);
        if (idx == NPOS) { lock_.unlock(); return false; }
        Slot& s = slots_[idx];
        if (s.bid_qty.load(std::memory_order_relaxed) > 0) {
            uint64_t before = s.bid_qty.load(std::memory_order_relaxed);
            s.bid_qty.store((qty >= before) ? 0 : before - qty, std::memory_order_relaxed);
        } else {
            uint64_t before = s.ask_qty.load(std::memory_order_relaxed);
            s.ask_qty.store((qty >= before) ? 0 : before - qty, std::memory_order_relaxed);
        }
        if (s.bid_qty.load(std::memory_order_relaxed) == 0 && s.ask_qty.load(std::memory_order_relaxed) == 0)
            s.state = SLOT_TOMBSTONE;
        lock_.unlock();
        return true;
    }

    // Lazily-corrected best bid: returns cached value unless it is stale
    // (zero bid qty), in which case the hash table is rescanned once.
    int64_t best_bid() const noexcept {
        int64_t p = best_bid_.load(std::memory_order_relaxed);
        if (p == BAD_PRICE || bid_qty_at(p) > 0) return p;
        return recompute_best_bid();
    }

    int64_t best_ask() const noexcept {
        int64_t p = best_ask_.load(std::memory_order_relaxed);
        if (p == BAD_PRICE || ask_qty_at(p) > 0) return p;
        return recompute_best_ask();
    }

    int64_t spread() const noexcept {
        int64_t bb = best_bid(), ba = best_ask();
        if (bb == BAD_PRICE || ba == BAD_PRICE) return 0;
        return ba - bb;
    }

    Snapshot get_snapshot(size_t max_levels = 10) const noexcept {
        Snapshot s;
        s.best_bid = best_bid();
        s.best_ask = best_ask();
        if (s.best_bid != BAD_PRICE) {
            size_t guard = 0;
            for (int64_t p = s.best_bid; p >= MIN_PRICE && s.bids.size() < max_levels && guard < 1000000; --p, ++guard) {
                uint64_t qty = bid_qty_at(p);
                if (qty > 0) s.bids.emplace_back((uint64_t)p, qty);
            }
        }
        if (s.best_ask != BAD_PRICE) {
            size_t guard = 0;
            for (int64_t p = s.best_ask; p <= MAX_PRICE && s.asks.size() < max_levels && guard < 1000000; ++p, ++guard) {
                uint64_t qty = ask_qty_at(p);
                if (qty > 0) s.asks.emplace_back((uint64_t)p, qty);
            }
        }
        return s;
    }

    uint64_t qty_at_price(uint64_t price_cents) const noexcept {
        size_t idx = find_live((int64_t)price_cents);
        if (idx == NPOS) return 0;
        const Slot& s = slots_[idx];
        return s.bid_qty.load(std::memory_order_relaxed) + s.ask_qty.load(std::memory_order_relaxed);
    }

    uint64_t bid_qty_at(uint64_t price_cents) const noexcept { return bid_qty_at((int64_t)price_cents); }
    uint64_t ask_qty_at(uint64_t price_cents) const noexcept { return ask_qty_at((int64_t)price_cents); }

    size_t live_levels() const noexcept {
        size_t n = 0;
        for (size_t i = 0; i < SPARSE_SLOTS; ++i) {
            const Slot& s = slots_[i];
            if (s.state == SLOT_LIVE &&
                (s.bid_qty.load(std::memory_order_relaxed) > 0 || s.ask_qty.load(std::memory_order_relaxed) > 0)) n++;
        }
        return n;
    }

private:
    static inline size_t hash_price(int64_t price) noexcept {
        return static_cast<size_t>(fastbook_mix64(static_cast<uint64_t>(price))) & (SPARSE_SLOTS - 1);
    }

    size_t find_or_create(int64_t price) noexcept {
        size_t idx = hash_price(price);
        size_t start = idx;
        size_t tomb = NPOS;
        for (;;) {
            uint8_t st = slots_[idx].state;
            if (st == SLOT_EMPTY) return (tomb != NPOS) ? tomb : idx;
            if (st == SLOT_TOMBSTONE) {
                if (tomb == NPOS) tomb = idx;
            } else if (slots_[idx].price == price) {
                return idx;
            }
            idx = (idx + 1) & (SPARSE_SLOTS - 1);
            if (idx == start) break;
        }
        return tomb;
    }

    size_t find_live(int64_t price) const noexcept {
        size_t idx = hash_price(price);
        size_t start = idx;
        while (slots_[idx].state != SLOT_EMPTY) {
            if (slots_[idx].state == SLOT_LIVE && slots_[idx].price == price) return idx;
            idx = (idx + 1) & (SPARSE_SLOTS - 1);
            if (idx == start) break;
        }
        return NPOS;
    }

    uint64_t bid_qty_at(int64_t price) const noexcept {
        size_t idx = find_live(price);
        return (idx != NPOS) ? slots_[idx].bid_qty.load(std::memory_order_relaxed) : 0;
    }

    uint64_t ask_qty_at(int64_t price) const noexcept {
        size_t idx = find_live(price);
        return (idx != NPOS) ? slots_[idx].ask_qty.load(std::memory_order_relaxed) : 0;
    }

    int64_t recompute_best_bid() const noexcept {
        int64_t best = BAD_PRICE;
        for (size_t i = 0; i < SPARSE_SLOTS; ++i) {
            const Slot& s = slots_[i];
            if (s.state == SLOT_LIVE && s.bid_qty.load(std::memory_order_relaxed) > 0 && s.price > best)
                best = s.price;
        }
        const_cast<FastOrderBook*>(this)->best_bid_.store(best, std::memory_order_relaxed);
        return best;
    }

    int64_t recompute_best_ask() const noexcept {
        int64_t best = BAD_PRICE;
        for (size_t i = 0; i < SPARSE_SLOTS; ++i) {
            const Slot& s = slots_[i];
            if (s.state == SLOT_LIVE && s.ask_qty.load(std::memory_order_relaxed) > 0 &&
                (best == BAD_PRICE || s.price < best))
                best = s.price;
        }
        const_cast<FastOrderBook*>(this)->best_ask_.store(best, std::memory_order_relaxed);
        return best;
    }

    std::unique_ptr<Slot[]> slots_;
    std::atomic<int64_t> best_bid_;
    std::atomic<int64_t> best_ask_;
    mutable SpinLock lock_;
};
