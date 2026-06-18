#include <cstdint>
#include <cstring>
#include <cstdio>
#include <atomic>
#include <thread>
#include <new>

#ifndef likely
#define likely(x)   __builtin_expect(!!(x), 1)
#endif
#ifndef unlikely
#define unlikely(x) __builtin_expect(!!(x), 0)
#endif

#define CACHE_LINE_SIZE 64
#define ALIGN_PAD_64 alignas(CACHE_LINE_SIZE)

static inline uint64_t rdtscp_p() noexcept {
    uint32_t aux;
    uint64_t rax, rdx;
    __asm__ __volatile__("rdtscp" : "=a"(rax), "=d"(rdx) : : "rcx");
    return (rdx << 32) | rax;
}

#pragma pack(push, 1)
struct ITCHMessageHeader {
    uint16_t msg_length;
    uint8_t msg_type;
    uint64_t timestamp_ns;
};

struct ITCHAddOrder {
    ITCHMessageHeader hdr;
    uint64_t order_ref;
    uint8_t buy_sell;
    uint32_t shares;
    uint32_t stock;
    uint32_t price;
};

struct ITCHOrderExecuted {
    ITCHMessageHeader hdr;
    uint64_t order_ref;
    uint32_t executed_shares;
    uint64_t match_number;
};

struct ITCHOrderCancel {
    ITCHMessageHeader hdr;
    uint64_t order_ref;
    uint32_t cancelled_shares;
};
#pragma pack(pop)

struct alignas(CACHE_LINE_SIZE) ParsedOrder {
    uint64_t order_ref;
    uint32_t price;
    uint32_t qty;
    uint32_t instrument_id;
    uint8_t side;
    uint64_t timestamp_ns;
    uint8_t msg_type;
    char pad_[2];
};

static_assert(sizeof(ParsedOrder) == 64, "ParsedOrder must be 64 bytes");

struct alignas(CACHE_LINE_SIZE) IngestionStats {
    uint64_t packets_parsed;
    uint64_t bytes_processed;
    uint64_t orders_parsed;
    uint64_t trades_parsed;
    uint64_t cancels_parsed;
    uint64_t parse_errors;
    uint64_t total_latency_ns;
    uint64_t max_latency_ns;
    uint64_t min_latency_ns;
    char pad_[40];
};

static_assert(sizeof(IngestionStats) == 128, "IngestionStats must be 128 bytes");

class CPPIngestionPipeline {
public:
    CPPIngestionPipeline() noexcept : running_(false) {
        std::memset(&stats_, 0, sizeof(stats_));
        stats_.min_latency_ns = UINT64_MAX;
    }

    void start_ingestion() noexcept {
        running_ = true;
        ingestion_thread_ = std::thread(&CPPIngestionPipeline::run_network_loop, this);
    }

    void stop_ingestion() noexcept {
        running_ = false;
        if (ingestion_thread_.joinable()) {
            ingestion_thread_.join();
        }
    }

    const IngestionStats& stats() const noexcept { return stats_; }

private:
    void run_network_loop() noexcept {
        std::printf("[INGESTION] ITCH/OUCH parser started\n");

        while (running_) {
            parse_packet_batch();
        }
    }

    void parse_packet_batch() noexcept {
        uint8_t raw_packet[2048];
        const uint64_t start = rdtscp_p();

        alignas(64) static uint8_t mock_data[] = {
            0x00, 0x22, 'A', 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x86, 0xA0,
            'B', 0x00, 0x00, 0x00, 0x64, 0x42, 0x54, 0x43, 0x55, 0x1D, 0xCD, 0x65, 0x00
        };
        std::memcpy(raw_packet, mock_data, sizeof(mock_data));

        if (likely(parse_itch_message(raw_packet, sizeof(mock_data)))) {
            stats_.packets_parsed++;
            stats_.bytes_processed += sizeof(mock_data);
        }
    }

    bool parse_itch_message(const uint8_t* data, size_t len) noexcept {
        if (unlikely(len < sizeof(ITCHMessageHeader))) {
            stats_.parse_errors++;
            return false;
        }

        const ITCHMessageHeader* hdr = reinterpret_cast<const ITCHMessageHeader*>(data);
        const uint64_t now = rdtscp_p();
        const uint64_t latency = now - hdr->timestamp_ns;

        switch (hdr->msg_type) {
        case 'A':
        case 'F': {
            if (unlikely(len < sizeof(ITCHAddOrder))) {
                stats_.parse_errors++;
                return false;
            }
            const ITCHAddOrder* add = reinterpret_cast<const ITCHAddOrder*>(data);
            stats_.orders_parsed++;

            ParsedOrder order;
            order.order_ref = add->order_ref;
            order.price = add->price;
            order.qty = add->shares;
            order.instrument_id = add->stock;
            order.side = add->buy_sell;
            order.timestamp_ns = add->hdr.timestamp_ns;
            order.msg_type = add->hdr.msg_type;

            stats_.total_latency_ns += latency;
            if (latency > stats_.max_latency_ns) stats_.max_latency_ns = latency;
            if (latency < stats_.min_latency_ns) stats_.min_latency_ns = latency;
            break;
        }
        case 'E':
        case 'C': {
            if (unlikely(len < sizeof(ITCHOrderExecuted))) {
                stats_.parse_errors++;
                return false;
            }
            const ITCHOrderExecuted* exec = reinterpret_cast<const ITCHOrderExecuted*>(data);
            stats_.trades_parsed++;
            break;
        }
        case 'X':
        case 'D': {
            if (unlikely(len < sizeof(ITCHOrderCancel))) {
                stats_.parse_errors++;
                return false;
            }
            const ITCHOrderCancel* cancel = reinterpret_cast<const ITCHOrderCancel*>(data);
            stats_.cancels_parsed++;
            break;
        }
        default:
            stats_.parse_errors++;
            return false;
        }

        return true;
    }

    ALIGN_PAD_64 std::atomic<bool> running_;
    ALIGN_PAD_64 std::thread ingestion_thread_;
    ALIGN_PAD_64 IngestionStats stats_;
};

int main() {
    CPPIngestionPipeline pipeline;
    pipeline.start_ingestion();

    std::this_thread::sleep_for(std::chrono::seconds(1));
    pipeline.stop_ingestion();

    const auto& s = pipeline.stats();
    std::printf("[INGESTION] Packets: %llu | Orders: %llu | Trades: %llu | Cancels: %llu | Errors: %llu\n",
           (unsigned long long)s.packets_parsed,
           (unsigned long long)s.orders_parsed,
           (unsigned long long)s.trades_parsed,
           (unsigned long long)s.cancels_parsed,
           (unsigned long long)s.parse_errors);

    return 0;
}
