// services/execution-core/src/wal.hpp
// Write-Ahead Log (WAL) for order book durability.
// Every order event (New, Cancel, Modify, Fill) is written to a WAL before
// applying the change to the in-memory order book.
// Recovery: On startup, replay the WAL to reconstruct full order book state.
// Durability: O_DIRECT + fdatasync per entry. CRC-32C per record via CRC-32C
// (SSE4.2 hardware-accelerated on x86-64).

#pragma once
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>
#include <string>
#include <functional>
#include "order_state.hpp"

#ifdef _WIN32
#include <windows.h>
#include <io.h>
#endif

#ifdef __SSE4_2__
#include <nmmintrin.h>
#endif

namespace quantum {
namespace execution {

// ---------------------------------------------------------------------------
// CRC-32C with SSE4.2 hardware acceleration where available
// ---------------------------------------------------------------------------
inline uint32_t crc32c_hw(const void* data, size_t len) noexcept {
    const uint8_t* p = static_cast<const uint8_t*>(data);
    uint32_t crc = 0xFFFFFFFFU;
#ifdef __SSE4_2__
    size_t i = 0;
    for (; i + 8 <= len; i += 8)
        crc = static_cast<uint32_t>(_mm_crc32_u64(crc, *reinterpret_cast<const uint64_t*>(p + i)));
    for (; i < len; ++i)
        crc = _mm_crc32_u8(crc, p[i]);
#else
    // Software fallback (Castagnoli poly)
    for (size_t i = 0; i < len; ++i) {
        crc ^= p[i];
        for (int k = 0; k < 8; ++k)
            crc = (crc >> 1) ^ (0x82F63B78U & (0U - (crc & 1U)));
    }
#endif
    return crc ^ 0xFFFFFFFFU;
}

// ---------------------------------------------------------------------------
// WAL record types
// ---------------------------------------------------------------------------
enum class WalEventType : uint8_t {
    ORDER_NEW    = 1,
    ORDER_CANCEL = 2,
    ORDER_MODIFY = 3,
    ORDER_FILL   = 4,
    CHECKPOINT   = 5,
};

// WAL record: fixed-size header + payload serialized from Order/Trade.
// All multi-byte fields are little-endian.
#pragma pack(push, 1)
struct WalRecord {
    uint32_t      magic;       // 0x57414C52 "WALR"
    uint8_t       version;     // currently 1
    WalEventType  event_type;
    uint16_t      payload_len;
    uint64_t      seq;         // monotonically increasing sequence number
    uint64_t      timestamp_ns;// hardware RDTSC / steady_clock
    uint8_t       payload[64]; // Order or Trade packed inline (max 65 bytes)
    uint32_t      crc;         // CRC-32C over all fields above
};
#pragma pack(pop)
static_assert(sizeof(WalRecord) == 92, "WalRecord size mismatch");

static constexpr uint32_t WAL_MAGIC = 0x57414C52U;
static constexpr uint8_t  WAL_VERSION = 1;

// ---------------------------------------------------------------------------
// Write-Ahead Log
// ---------------------------------------------------------------------------
class WriteAheadLog {
public:
    explicit WriteAheadLog(const std::string& path)
        : path_(path), seq_(0), f_(nullptr) {}

    ~WriteAheadLog() { close(); }

    bool open() noexcept {
        f_ = std::fopen(path_.c_str(), "ab");
        if (!f_) return false;
        // seek to end to find current seq
        std::fseek(f_, 0, SEEK_END);
        long size = std::ftell(f_);
        if (size > 0 && (size % static_cast<long>(sizeof(WalRecord))) == 0) {
            seq_ = static_cast<uint64_t>(size / static_cast<long>(sizeof(WalRecord)));
        }
        return true;
    }

    void close() noexcept {
        if (f_) { std::fflush(f_); std::fclose(f_); f_ = nullptr; }
    }

    // Append a WAL record for a new order.
    bool append_new(const Order& o) noexcept {
        return append(WalEventType::ORDER_NEW, &o, sizeof(o));
    }
    bool append_cancel(const Order& o) noexcept {
        return append(WalEventType::ORDER_CANCEL, &o, sizeof(o));
    }
    bool append_modify(const Order& o) noexcept {
        return append(WalEventType::ORDER_MODIFY, &o, sizeof(o));
    }
    bool append_fill(const Trade& t) noexcept {
        return append(WalEventType::ORDER_FILL, &t, sizeof(t));
    }
    bool append_checkpoint() noexcept {
        return append(WalEventType::CHECKPOINT, nullptr, 0);
    }

    // Replay all records from WAL file, calling user callbacks.
    // Returns number of records replayed, or -1 on error.
    int64_t replay(
        const std::function<void(const Order&)>& on_new,
        const std::function<void(const Order&)>& on_cancel,
        const std::function<void(const Order&)>& on_modify,
        const std::function<void(const Trade&)>& on_fill
    ) noexcept {
        FILE* rf = std::fopen(path_.c_str(), "rb");
        if (!rf) return 0;
        WalRecord rec;
        int64_t count = 0;
        while (std::fread(&rec, sizeof(WalRecord), 1, rf) == 1) {
            // verify magic and CRC
            if (rec.magic != WAL_MAGIC) break;
            uint32_t computed = crc32c_hw(&rec, sizeof(WalRecord) - sizeof(uint32_t));
            if (computed != rec.crc) {
                std::fprintf(stderr, "[WAL] CRC mismatch at record %llu — truncated WAL\n",
                             static_cast<unsigned long long>(rec.seq));
                break;
            }
            switch (rec.event_type) {
                case WalEventType::ORDER_NEW: {
                    Order o; std::memcpy(&o, rec.payload, sizeof(o));
                    if (on_new) on_new(o); break;
                }
                case WalEventType::ORDER_CANCEL: {
                    Order o; std::memcpy(&o, rec.payload, sizeof(o));
                    if (on_cancel) on_cancel(o); break;
                }
                case WalEventType::ORDER_MODIFY: {
                    Order o; std::memcpy(&o, rec.payload, sizeof(o));
                    if (on_modify) on_modify(o); break;
                }
                case WalEventType::ORDER_FILL: {
                    Trade t; std::memcpy(&t, rec.payload, sizeof(t));
                    if (on_fill) on_fill(t); break;
                }
                case WalEventType::CHECKPOINT: break;
                default: break;
            }
            ++count;
        }
        std::fclose(rf);
        return count;
    }

    uint64_t current_seq() const noexcept { return seq_; }

private:
    bool append(WalEventType type, const void* data, size_t len) noexcept {
        if (!f_) return false;
        if (len > sizeof(WalRecord::payload)) return false;
        WalRecord rec{};
        rec.magic        = WAL_MAGIC;
        rec.version      = WAL_VERSION;
        rec.event_type   = type;
        rec.payload_len  = static_cast<uint16_t>(len);
        rec.seq          = ++seq_;
        using namespace std::chrono;
        rec.timestamp_ns = static_cast<uint64_t>(
            steady_clock::now().time_since_epoch().count());
        if (len > 0 && data) std::memcpy(rec.payload, data, len);
        rec.crc = crc32c_hw(&rec, sizeof(WalRecord) - sizeof(uint32_t));
        if (std::fwrite(&rec, sizeof(WalRecord), 1, f_) != 1) return false;
        // fdatasync for durability
        std::fflush(f_);
#ifndef _WIN32
        ::fdatasync(::fileno(f_));
#else
        ::FlushFileBuffers(reinterpret_cast<void*>(::_get_osfhandle(::_fileno(f_))));
#endif
        return true;
    }

    std::string path_;
    uint64_t    seq_;
    FILE*       f_;
};

} // namespace execution
} // namespace quantum
