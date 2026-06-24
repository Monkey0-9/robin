#pragma once

#include "order_state.hpp"
#include <cstdint>
#include <cstring>
#include <ctime>
#include <unordered_map>
#include <vector>
#include <algorithm>

namespace quantum {
namespace execution {

struct PositionRecord {
    int64_t net_position;
    uint64_t last_update_ns;
};

class RiskEngine {
public:
    static constexpr uint32_t MAX_QTY = 1000000;
    static constexpr uint64_t PRICE_COLLAR_BPS = 500;
    static constexpr uint64_t VELOCITY_WINDOW_NS = 1000000000ULL;
    static constexpr size_t MAX_VELOCITY = 100;
    static constexpr size_t VELOCITY_RING_SIZE = 512;
    static constexpr size_t POSITIONS_SIZE = 4096;
    static constexpr int64_t POSITION_LIMIT = 100000;
    static constexpr uint64_t DUPLICATE_WINDOW_NS = 1000000ULL;

    RiskEngine() : velocity_head_(0), kill_switch_active_(false), circuit_breaker_tripped_(false) {
        recent_orders_.resize(4096, {0, 0});
        positions_.resize(4096, {0, 0});
        velocity_ring_.resize(512, 0);
        std::memset(&restricted_symbols_, 0, sizeof(restricted_symbols_));
        std::memset(&last_trade_prices_, 0, sizeof(last_trade_prices_));
    }

    void set_kill_switch(bool active) { kill_switch_active_ = active; }
    bool is_kill_switch_active() const { return kill_switch_active_; }

    void set_circuit_breaker(bool tripped) { circuit_breaker_tripped_ = tripped; }
    bool is_circuit_breaker_tripped() const { return circuit_breaker_tripped_; }

    void add_restricted_symbol(uint32_t instrument_id) {
        for (auto& s : restricted_symbols_) {
            if (s == 0) { s = instrument_id; break; }
        }
    }

    void update_reference_price(uint32_t instrument_id, uint32_t price) {
        last_trade_prices_[instrument_id & 4095] = price;
    }

    bool check_order(const Order& order, uint64_t timestamp_ns) {
        if (kill_switch_active_) return false;
        if (circuit_breaker_tripped_) return false;
        if (order.qty > MAX_QTY) return false;
        for (auto s : restricted_symbols_) {
            if (s == order.instrument_id) return false;
        }
        {
            size_t slot = order.id & 4095;
            auto [old_id, old_ts] = recent_orders_[slot];
            if (old_id == order.id && (timestamp_ns - old_ts) < DUPLICATE_WINDOW_NS)
                return false;
        }
        {
            size_t slot = order.instrument_id & 4095;
            uint64_t last = last_trade_prices_[slot];
            if (last > 0) {
                uint64_t p = order.price;
                uint64_t min_p = (last * 95) / 100;
                uint64_t max_p = (last * 105) / 100;
                if (p < min_p || p > max_p) return false;
            }
        }
        {
            size_t slot = order.instrument_id & 4095;
            int64_t current = positions_[slot].net_position;
            int64_t next = (order.side == Side::BID)
                ? current + static_cast<int64_t>(order.qty)
                : current - static_cast<int64_t>(order.qty);
            if (std::abs(next) > POSITION_LIMIT) return false;
            positions_[slot].net_position = next;
            positions_[slot].last_update_ns = timestamp_ns;
        }
        {
            size_t lookback = (velocity_head_ + VELOCITY_RING_SIZE - MAX_VELOCITY) % VELOCITY_RING_SIZE;
            uint64_t oldest_ts = velocity_ring_[lookback];
            if (oldest_ts > 0 && (timestamp_ns - oldest_ts) < VELOCITY_WINDOW_NS)
                return false;
        }
        velocity_ring_[velocity_head_] = timestamp_ns;
        velocity_head_ = (velocity_head_ + 1) % VELOCITY_RING_SIZE;
        recent_orders_[order.id & 4095] = {order.id, timestamp_ns};
        return true;
    }

    void rollback_position(const Order& order) {
        size_t slot = order.instrument_id & 4095;
        if (order.side == Side::BID)
            positions_[slot].net_position -= static_cast<int64_t>(order.qty);
        else
            positions_[slot].net_position += static_cast<int64_t>(order.qty);
    }

    int64_t get_position(uint32_t instrument_id) const {
        return positions_[instrument_id & 4095].net_position;
    }

private:
    std::vector<std::pair<uint64_t, uint64_t>> recent_orders_;
    std::vector<PositionRecord> positions_;
    std::vector<uint64_t> velocity_ring_;
    size_t velocity_head_;
    uint32_t restricted_symbols_[128];
    uint64_t last_trade_prices_[4096];
    bool kill_switch_active_;
    bool circuit_breaker_tripped_;
};

} // namespace execution
} // namespace quantum
