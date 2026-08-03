#pragma once
#include <array>
#include <atomic>
#include <cstdint>
#include <vector>

constexpr int64_t PRICE_SCALE = 100; // cents
constexpr int64_t MIN_PRICE = 0;
constexpr int64_t MAX_PRICE = 100000000; // $1M in cents
constexpr size_t PRICE_LEVELS = MAX_PRICE - MIN_PRICE + 1;

struct PriceLevel {
    std::atomic<uint64_t> total_qty{0};
    std::atomic<uint32_t> order_count{0};
};

class FastOrderBook {
    std::array<PriceLevel, PRICE_LEVELS> levels_;
    std::atomic<int64_t> best_bid_{-1};
    std::atomic<int64_t> best_ask_{-1};
    
public:
    bool add_order(uint64_t price_cents, uint64_t qty, bool is_bid) noexcept {
        if (price_cents >= PRICE_LEVELS) return false;
        
        levels_[price_cents].total_qty.fetch_add(qty, std::memory_order_relaxed);
        levels_[price_cents].order_count.fetch_add(1, std::memory_order_relaxed);
        
        if (is_bid) {
            int64_t current = best_bid_.load(std::memory_order_relaxed);
            while ((int64_t)price_cents > current) {
                if (best_bid_.compare_exchange_weak(
                    current, (int64_t)price_cents,
                    std::memory_order_relaxed)) break;
            }
        } else {
            int64_t current = best_ask_.load(std::memory_order_relaxed);
            while ((int64_t)price_cents < current || current == -1) {
                if (best_ask_.compare_exchange_weak(
                    current, (int64_t)price_cents,
                    std::memory_order_relaxed)) break;
            }
        }
        return true;
    }
    
    bool cancel_order(uint64_t price_cents, uint64_t qty) noexcept {
        if (price_cents >= PRICE_LEVELS) return false;
        levels_[price_cents].total_qty.fetch_sub(qty, std::memory_order_relaxed);
        levels_[price_cents].order_count.fetch_sub(1, std::memory_order_relaxed);
        return true;
    }
    
    struct Snapshot {
        int64_t best_bid;
        int64_t best_ask;
        std::vector<std::pair<uint64_t, uint64_t>> bids;
        std::vector<std::pair<uint64_t, uint64_t>> asks;
    };
    
    Snapshot get_snapshot(size_t max_levels = 10) const noexcept {
        Snapshot s;
        s.best_bid = best_bid_.load(std::memory_order_relaxed);
        s.best_ask = best_ask_.load(std::memory_order_relaxed);
        
        // Scan bids from best downward
        for (int64_t p = s.best_bid; p >= 0 && s.bids.size() < max_levels; --p) {
            uint64_t qty = levels_[p].total_qty.load(std::memory_order_relaxed);
            if (qty > 0) s.bids.emplace_back(p, qty);
        }
        
        // Scan asks from best upward
        for (int64_t p = s.best_ask; p >= 0 && p < (int64_t)PRICE_LEVELS && s.asks.size() < max_levels; ++p) {
            uint64_t qty = levels_[p].total_qty.load(std::memory_order_relaxed);
            if (qty > 0) s.asks.emplace_back(p, qty);
        }
        return s;
    }
};
