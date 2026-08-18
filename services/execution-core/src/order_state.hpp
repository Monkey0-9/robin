// services/execution-core/src/order_state.hpp
// Enum and state tracker structures for matching engine orders.

#pragma once

#include <atomic>
#include <chrono>
#include <cstdint>
#include <thread>

namespace quantum {
namespace execution {

// OrderIDGenerator — restart-safe, collision-free order id allocation.
//
// Id layout: 48-bit wall-clock bucket (system_clock ns >> 16, ~65us granularity)
//            + 16-bit sequence within the bucket.
//
//   id = (bucket << 16) | seq
//
// Guarantees:
//  - Unique within a process: the (bucket, seq) pair is a single packed atomic
//    state updated by compare-and-swap, so each successful CAS returns a
//    distinct id. The sequence saturates below 2^16 so it never overflows into
//    the bucket bits.
//  - Unique across restarts: the bucket comes from the wall clock, so a
//    restarted process continues with larger buckets than any id it generated
//    before the restart (and than any persisted id).
//  - Monotonic within a run: a monotonic guard prevents the bucket from
//    rewinding if the wall clock steps backwards (NTP).
//
// OrderServer and TWAPEngine share this generator so no two id streams (client
// default ids, algorithm child order ids) can collide.
class OrderIDGenerator {
public:
    static uint64_t next() noexcept {
        uint64_t cur = state_.load(std::memory_order_relaxed);
        for (;;) {
            uint64_t bucket = cur >> kSeqBits;
            uint64_t now = wall_bucket();
            if (now < bucket) now = bucket; // monotonic guard against clock rewinds
            uint64_t nxt;
            if (now != bucket) {
                nxt = now << kSeqBits;          // advance bucket, restart seq at 0
            } else if ((cur & kSeqMask) + 1 < kSeqLimit) {
                nxt = cur + 1;                  // next sequence in this bucket
            } else {
                std::this_thread::sleep_for(std::chrono::microseconds(50)); // wait for next bucket
                continue;
            }
            if (state_.compare_exchange_weak(cur, nxt, std::memory_order_acq_rel)) return nxt;
        }
    }

private:
    static constexpr int kSeqBits = 16;
    static constexpr uint64_t kSeqLimit = 1u << kSeqBits;
    static constexpr uint64_t kSeqMask = kSeqLimit - 1;

    static uint64_t wall_bucket() noexcept {
        static const auto epoch_anchor = std::chrono::system_clock::now().time_since_epoch();
        static const auto steady_start = std::chrono::steady_clock::now();

        auto steady_now = std::chrono::steady_clock::now();
        auto elapsed = steady_now - steady_start;
        auto total_ns = std::chrono::duration_cast<std::chrono::nanoseconds>(epoch_anchor + elapsed).count();
        return static_cast<uint64_t>(total_ns) >> 16;
    }

    static std::atomic<uint64_t> state_;
};

inline std::atomic<uint64_t> OrderIDGenerator::state_{0};

enum class OrderState : uint8_t {
    NEW = 0,
    PENDING_NEW = 1,
    WORKING = 2,
    PARTIAL_FILL = 3,
    FILLED = 4,
    PENDING_CANCEL = 5,
    CANCELED = 6,
    PENDING_REPLACE = 7,
    REPLACED = 8,
    REJECTED = 9,
    SUSPENDED = 10,
    CONFIRMED = 11
};

enum class Side : uint8_t {
    BID = 0,
    ASK = 1
};

enum class MessageType : uint8_t {
    NEW = 0,
    CANCEL = 1,
    REPLACE = 2,
    EXECUTION = 3
};

enum class OrderType : uint8_t {
    LIMIT = 0,
    MARKET = 1,
    IOC = 2,
    FOK = 3,
    CANCEL = 4, // Deprecated: use MessageType::CANCEL
    REPLACE = 5
};

// Order flags (bitmask)
enum OrderFlags : uint8_t {
    FLAG_POST_ONLY     = 1u << 0, // never take liquidity; reject if it would cross
    FLAG_IOC_RESTRICT  = 1u << 1, // IOC that must fill at least min_qty or cancel entirely
    FLAG_REDUCE_ONLY   = 1u << 2  // never increases a position
};

// Self-trade prevention modes
enum StpMode : uint8_t {
    STP_BLOCK         = 0, // cancel the new (aggressor) order if it would trade with itself
    STP_CANCEL_OLDEST = 1, // cancel the resting order, keep the aggressor
    STP_ALLOW         = 2  // explicitly allow self-trade (testing)
};

struct Order {
    uint64_t id;
    int64_t price;       // Scaled (e.g., *1e8)
    int64_t qty;
    int64_t min_qty;     // 0 = any quantity; >0 = min fill / odd-lot policy
    int64_t new_price;   // REPLACE only: replacement limit price
    int64_t new_qty;     // REPLACE only: replacement quantity
    uint32_t instrument_id;
    uint32_t client_id;
    uint32_t account_id; // STP owner id; 0 = unknown (STP not enforced)
    uint8_t  flags;      // OrderFlags bitmask
    uint8_t  stp_mode;   // StpMode
    Side side;
    OrderState state;
    OrderType type;
};


struct Trade {
    uint64_t trade_id;
    uint64_t buy_order_id;
    uint64_t sell_order_id;
    uint32_t instrument_id;
    int64_t price;
    int64_t qty;
    uint64_t timestamp;
    Side aggressor_side;
};

} // namespace execution
} // namespace quantum
