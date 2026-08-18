#pragma once
// ============================================================================
// Zero-Copy OUCH 4.2 / 5.0 Protocol Codec
// services/execution-core/src/ouch_codec.hpp
// ============================================================================
// Ultra-low latency binary protocol parser for Nasdaq OUCH 4.2 & 5.0 formats.
// Directly parses into quantum::execution::Order and Trade structures without
// intermediate copies or heap allocations.
//
// Message formats:
//   Inbound:
//     - Enter Order ('O')
//     - Cancel Order ('X')
//     - Replace Order ('U')
//   Outbound:
//     - Order Accepted ('A')
//     - Order Canceled ('C')
//     - Order Executed ('E')
//     - Order Rejected ('J')
// ============================================================================

#include "order_state.hpp"
#include <cstdint>
#include <cstring>
#include <span>
#include <string_view>

#if defined(_MSC_VER)
#  define QUANTUM_BYTESWAP16(x) _byteswap_ushort(x)
#  define QUANTUM_BYTESWAP32(x) _byteswap_ulong(x)
#  define QUANTUM_BYTESWAP64(x) _byteswap_uint64(x)
#else
#  define QUANTUM_BYTESWAP16(x) __builtin_bswap16(x)
#  define QUANTUM_BYTESWAP32(x) __builtin_bswap32(x)
#  define QUANTUM_BYTESWAP64(x) __builtin_bswap64(x)
#endif

namespace quantum {
namespace ouch {

#pragma pack(push, 1)

// ── Inbound OUCH Messages (Client -> Exchange) ──────────────────────────────

struct EnterOrderMessage {
    char     msg_type;          // 'O'
    char     cl_ord_id[14];     // User-assigned order reference
    char     side;              // 'B' = Buy, 'S' = Sell
    uint32_t shares;            // Big-endian binary quantity
    char     stock[8];          // Padded stock symbol
    uint32_t price;             // Big-endian fixed point (4 implied decimals)
    uint32_t time_in_force;     // 0 = Day, 99998 = IOC
    char     firm[4];           // Firm identifier
    char     display;           // 'Y' = Display, 'N' = Non-display
    char     capacity;          // 'A' = Agency, 'P' = Principal
    char     intermarket_sweep; // 'Y' / 'N'
    uint32_t min_qty;           // Minimum executable quantity
    char     customer_type;     // Customer type identifier
};

struct CancelOrderMessage {
    char     msg_type;          // 'X'
    char     cl_ord_id[14];     // Existing order token to cancel
    uint32_t shares;            // 0 = cancel entire order, >0 = cancel specific qty
};

struct ReplaceOrderMessage {
    char     msg_type;          // 'U'
    char     orig_cl_ord_id[14];// Token of order being modified
    char     new_cl_ord_id[14]; // New token assigned to modified order
    uint32_t shares;            // New total share quantity
    uint32_t price;             // New limit price (4 implied decimals)
    uint32_t time_in_force;     // New time in force
    char     display;           // Display preference
    char     intermarket_sweep; // Sweep flag
    uint32_t min_qty;           // New minimum quantity
};

// ── Outbound OUCH Messages (Exchange -> Client) ─────────────────────────────

struct OrderAcceptedMessage {
    char     msg_type;          // 'A'
    uint64_t timestamp_ns;      // Nanoseconds from midnight
    char     cl_ord_id[14];
    char     side;
    uint32_t shares;
    char     stock[8];
    uint32_t price;
    uint32_t time_in_force;
    char     firm[4];
    char     display;
    uint64_t order_ref_num;     // Exchange unique order reference
    char     capacity;
    char     intermarket_sweep;
    uint32_t min_qty;
    char     state;             // 'L' = Live
};

struct OrderCanceledMessage {
    char     msg_type;          // 'C'
    uint64_t timestamp_ns;
    char     cl_ord_id[14];
    uint32_t decrement_shares;
    char     reason;            // 'U' = User requested, 'T' = Timeout, 'S' = Supervisory
};

struct OrderExecutedMessage {
    char     msg_type;          // 'E'
    uint64_t timestamp_ns;
    char     cl_ord_id[14];
    uint32_t executed_shares;
    uint32_t execution_price;   // 4 decimal fixed point
    char     liquidity_flag;    // 'A' = Added, 'R' = Removed
    uint64_t match_number;      // Unique trade identifier
};

struct OrderRejectedMessage {
    char     msg_type;          // 'J'
    uint64_t timestamp_ns;
    char     cl_ord_id[14];
    char     reason;            // 'C' = Quote collar, 'H' = Halted, 'X' = Risk
};

#pragma pack(pop)

// ── Zero-Copy Parser & Serializer ───────────────────────────────────────────

class OuchCodec {
public:
    /// Parse an inbound binary OUCH buffer directly into an execution Order struct
    [[nodiscard]]
    static bool parse_enter_order(const uint8_t* data, size_t len, execution::Order& out) noexcept {
        if (len < sizeof(EnterOrderMessage) || data[0] != 'O') return false;

        const auto* msg = reinterpret_cast<const EnterOrderMessage*>(data);
        
        out.id = parse_order_token(msg->cl_ord_id);
        out.side = (msg->side == 'B' || msg->side == 'b') ? execution::Side::BID : execution::Side::ASK;
        out.qty = static_cast<int64_t>(QUANTUM_BYTESWAP32(msg->shares));
        
        // Convert 4 decimal fixed point price (e.g. 1502500 for $150.25) to 6-decimal engine price
        uint32_t raw_price = QUANTUM_BYTESWAP32(msg->price);
        out.price = static_cast<int64_t>(raw_price) * 100;
        
        uint32_t tif = QUANTUM_BYTESWAP32(msg->time_in_force);
        out.type = (tif == 99998) ? execution::OrderType::IOC : execution::OrderType::LIMIT;
        
        out.min_qty = static_cast<int64_t>(QUANTUM_BYTESWAP32(msg->min_qty));
        out.state = execution::OrderState::NEW;
        out.stp_mode = execution::STP_CANCEL_OLDEST;
        out.flags = 0;

        return true;
    }

    /// Encode an OrderAccepted outbound message into caller-supplied buffer
    static size_t encode_accepted(
        std::span<uint8_t> buf,
        uint64_t timestamp_ns,
        uint64_t cl_ord_id,
        execution::Side side,
        uint32_t shares,
        std::string_view symbol,
        uint32_t price_4dp,
        uint64_t order_ref_num) noexcept
    {
        if (buf.size() < sizeof(OrderAcceptedMessage)) return 0;

        auto* msg = reinterpret_cast<OrderAcceptedMessage*>(buf.data());
        msg->msg_type = 'A';
        msg->timestamp_ns = QUANTUM_BYTESWAP64(timestamp_ns);
        format_order_token(cl_ord_id, msg->cl_ord_id);
        msg->side = (side == execution::Side::BID) ? 'B' : 'S';
        msg->shares = QUANTUM_BYTESWAP32(shares);
        
        std::memset(msg->stock, ' ', sizeof(msg->stock));
        std::memcpy(msg->stock, symbol.data(), std::min(symbol.size(), sizeof(msg->stock)));
        
        msg->price = QUANTUM_BYTESWAP32(price_4dp);
        msg->time_in_force = 0;
        std::memcpy(msg->firm, "ROBN", 4);
        msg->display = 'Y';
        msg->order_ref_num = QUANTUM_BYTESWAP64(order_ref_num);
        msg->capacity = 'A';
        msg->intermarket_sweep = 'N';
        msg->min_qty = 0;
        msg->state = 'L';

        return sizeof(OrderAcceptedMessage);
    }

    /// Encode an OrderExecuted outbound message
    static size_t encode_executed(
        std::span<uint8_t> buf,
        uint64_t timestamp_ns,
        uint64_t cl_ord_id,
        uint32_t executed_shares,
        uint32_t exec_price_4dp,
        char liquidity_flag,
        uint64_t match_number) noexcept
    {
        if (buf.size() < sizeof(OrderExecutedMessage)) return 0;

        auto* msg = reinterpret_cast<OrderExecutedMessage*>(buf.data());
        msg->msg_type = 'E';
        msg->timestamp_ns = QUANTUM_BYTESWAP64(timestamp_ns);
        format_order_token(cl_ord_id, msg->cl_ord_id);
        msg->executed_shares = QUANTUM_BYTESWAP32(executed_shares);
        msg->execution_price = QUANTUM_BYTESWAP32(exec_price_4dp);
        msg->liquidity_flag = liquidity_flag;
        msg->match_number = QUANTUM_BYTESWAP64(match_number);

        return sizeof(OrderExecutedMessage);
    }

private:
    static uint64_t parse_order_token(const char token[14]) noexcept {
        uint64_t val = 0;
        for (int i = 0; i < 14; ++i) {
            char c = token[i];
            if (c >= '0' && c <= '9') {
                val = val * 10 + (c - '0');
            }
        }
        return val;
    }

    static void format_order_token(uint64_t id, char token[14]) noexcept {
        for (int i = 13; i >= 0; --i) {
            token[i] = '0' + static_cast<char>(id % 10);
            id /= 10;
        }
    }
};

} // namespace ouch
} // namespace quantum
