#include <cstdint>
#include <cstring>
#include <cstdio>
#include <atomic>
#include <thread>
#include <new>

#if defined(_WIN32)
#include <winsock2.h>
#include <ws2tcpip.h>
#pragma comment(lib, "ws2_32.lib")
#else
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <fcntl.h>
#include <unistd.h>
#include <sys/mman.h>
#include <sys/stat.h>
#endif

#ifndef likely
#define likely(x)   __builtin_expect(!!(x), 1)
#endif
#ifndef unlikely
#define unlikely(x) __builtin_expect(!!(x), 0)
#endif

#define CACHE_LINE_SIZE 64
#define ALIGN_PAD_64 alignas(CACHE_LINE_SIZE)

static inline uint64_t rdtscp_p() noexcept {
#if defined(_MSC_VER)
    return __rdtscp(&aux);
#elif defined(__x86_64__)
    uint32_t aux;
    uint64_t rax, rdx;
    __asm__ __volatile__("rdtscp" : "=a"(rax), "=d"(rdx) : : "rcx");
    return (rdx << 32) | rax;
#else
    return 0; // Fallback
#endif
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

struct ShmHeader {
    std::atomic<uint64_t> write_idx;
    std::atomic<uint64_t> read_idx;
    uint64_t magic;
    uint32_t version;
    uint32_t size;
    uint32_t pid_writer;
    uint32_t pid_reader;
    uint8_t pad_[24];
};

struct ShmMessage {
    uint8_t msg_type;
    uint32_t client_id;
    uint32_t instrument_id;
    uint32_t price;
    uint32_t qty;
    uint8_t side;
    uint8_t flags;
    uint64_t order_id;
    uint64_t cl_order_id;
    uint64_t timestamp_ns;
    uint8_t _pad[21];
};
#pragma pack(pop)

static_assert(sizeof(ShmMessage) == 64, "ShmMessage size must be 64 bytes");

constexpr size_t SHM_CAPACITY = 65536;
constexpr size_t SHM_SIZE = sizeof(ShmHeader) + SHM_CAPACITY * sizeof(ShmMessage);

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
    CPPIngestionPipeline() noexcept
        : running_(false),
          sock_(-1),
          shm_header_(nullptr),
          shm_ring_(nullptr),
          shm_mapped_(nullptr)
    {
        std::memset(&stats_, 0, sizeof(stats_));
        stats_.min_latency_ns = UINT64_MAX;
    }

    ~CPPIngestionPipeline() noexcept {
        stop_ingestion();
        cleanup_shm();
        cleanup_socket();
    }

    bool init_shm(const char* path) noexcept {
#ifdef __linux__
        int fd = shm_open(path, O_RDWR, 0666);
        if (fd < 0) {
            std::printf("[WARN] shm_open failed, attempting to create...\n");
            fd = shm_open(path, O_CREAT | O_RDWR, 0666);
            if (fd >= 0) {
                if (ftruncate(fd, SHM_SIZE) < 0) {
                    close(fd);
                    return false;
                }
            }
        }
        if (fd >= 0) {
            shm_mapped_ = mmap(nullptr, SHM_SIZE, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
            close(fd);
            if (shm_mapped_ == MAP_FAILED) {
                shm_mapped_ = nullptr;
                return false;
            }
            shm_header_ = static_cast<ShmHeader*>(shm_mapped_);
            shm_ring_ = reinterpret_cast<ShmMessage*>(static_cast<uint8_t*>(shm_mapped_) + sizeof(ShmHeader));
            std::printf("[INGESTION] Mapped POSIX shared memory at %s\n", path);
            return true;
        }
#endif
        // Fallback or Windows mock
        shm_mapped_ = std::malloc(SHM_SIZE);
        if (shm_mapped_) {
            std::memset(shm_mapped_, 0, SHM_SIZE);
            shm_header_ = static_cast<ShmHeader*>(shm_mapped_);
            shm_header_->magic = 0x524f42494e484d5f;
            shm_header_->version = 1;
            shm_header_->size = SHM_CAPACITY;
            shm_ring_ = reinterpret_cast<ShmMessage*>(static_cast<uint8_t*>(shm_mapped_) + sizeof(ShmHeader));
            std::printf("[INGESTION] Mocked shared memory fallback initialized\n");
            return true;
        }
        return false;
    }

    bool init_socket(const char* group, int port) noexcept {
#if defined(_WIN32)
        WSADATA wsaData;
        if (WSAStartup(MAKEWORD(2, 2), &wsaData) != 0) {
            return false;
        }
        sock_ = socket(AF_INET, SOCK_DGRAM, IPPROTO_UDP);
#else
        sock_ = socket(AF_INET, SOCK_DGRAM, 0);
#endif
        if (sock_ < 0) return false;

        int reuse = 1;
        setsockopt(sock_, SOL_SOCKET, SO_REUSEADDR, (const char*)&reuse, sizeof(reuse));

        sockaddr_in local_addr{};
        local_addr.sin_family = AF_INET;
        local_addr.sin_addr.s_addr = INADDR_ANY;
        local_addr.sin_port = htons(port);

        if (bind(sock_, (struct sockaddr*)&local_addr, sizeof(local_addr)) < 0) {
            cleanup_socket();
            return false;
        }

        ip_mreq mreq{};
        mreq.imr_multiaddr.s_addr = inet_addr(group);
        mreq.imr_interface.s_addr = INADDR_ANY;
        setsockopt(sock_, IPPROTO_IP, IP_ADD_MEMBERSHIP, (const char*)&mreq, sizeof(mreq));

#if defined(_WIN32)
        u_long mode = 1;
        ioctlsocket(sock_, FIONBIO, &mode);
#else
        int flags = fcntl(sock_, F_GETFL, 0);
        fcntl(sock_, F_SETFL, flags | O_NONBLOCK);
#endif

        std::printf("[INGESTION] Subscribed to multicast %s:%d\n", group, port);
        return true;
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
    void cleanup_shm() noexcept {
        if (shm_mapped_) {
#ifdef __linux__
            munmap(shm_mapped_, SHM_SIZE);
#else
            std::free(shm_mapped_);
#endif
            shm_mapped_ = nullptr;
            shm_header_ = nullptr;
            shm_ring_ = nullptr;
        }
    }

    void cleanup_socket() noexcept {
        if (sock_ >= 0) {
#if defined(_WIN32)
            closesocket(sock_);
            WSACleanup();
#else
            close(sock_);
#endif
            sock_ = -1;
        }
    }

    void run_network_loop() noexcept {
        std::printf("[INGESTION] ITCH/OUCH network parser loop started\n");
        while (running_) {
            parse_packet_batch();
        }
    }

    void parse_packet_batch() noexcept {
        uint8_t buffer[2048];
        int len = recv(sock_, reinterpret_cast<char*>(buffer), sizeof(buffer), 0);

        if (len > 0) {
            if (likely(parse_itch_message(buffer, len))) {
                stats_.packets_parsed++;
                stats_.bytes_processed += len;
            }
        } else {
            // Hot loop polling yield
            std::this_thread::yield();
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

            ShmMessage msg{};
            msg.msg_type = add->hdr.msg_type;
            msg.client_id = 42;
            msg.instrument_id = add->stock;
            msg.price = add->price;
            msg.qty = add->shares;
            msg.side = add->buy_sell;
            msg.order_id = add->order_ref;
            msg.cl_order_id = add->order_ref;
            msg.timestamp_ns = add->hdr.timestamp_ns;

            push_to_shm(&msg);

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
            stats_.trades_parsed++;
            break;
        }
        case 'X':
        case 'D': {
            if (unlikely(len < sizeof(ITCHOrderCancel))) {
                stats_.parse_errors++;
                return false;
            }
            stats_.cancels_parsed++;
            break;
        }
        default:
            stats_.parse_errors++;
            return false;
        }

        return true;
    }

    bool push_to_shm(const ShmMessage* msg) noexcept {
        if (unlikely(!shm_header_ || !shm_ring_)) return false;

        uint64_t write_idx = shm_header_->write_idx.load(std::memory_order_relaxed);
        uint64_t read_idx = shm_header_->read_idx.load(std::memory_order_acquire);

        if (write_idx - read_idx >= SHM_CAPACITY) {
            return false; // Queue full
        }

        size_t slot = static_cast<size_t>(write_idx & (SHM_CAPACITY - 1));
        std::memcpy(&shm_ring_[slot], msg, sizeof(ShmMessage));
        shm_header_->write_idx.store(write_idx + 1, std::memory_order_release);
        return true;
    }

    ALIGN_PAD_64 std::atomic<bool> running_;
    std::thread ingestion_thread_;
#if defined(_WIN32)
    SOCKET sock_;
#else
    int sock_;
#endif
    ShmHeader* shm_header_;
    ShmMessage* shm_ring_;
    void* shm_mapped_;
    ALIGN_PAD_64 IngestionStats stats_;
};

int main() {
    CPPIngestionPipeline pipeline;
    if (!pipeline.init_shm("/robin_ingest_risk")) {
        std::printf("[FATAL] SHM init failed\n");
        return 1;
    }

    if (!pipeline.init_socket("233.0.0.1", 5000)) {
        std::printf("[WARN] Multicast bind failed (normal if no net config). Running in mock passive mode.\n");
    }

    pipeline.start_ingestion();
    std::printf("[INGESTION] Pipeline running. Press Ctrl+C to stop.\n");

    // Run indefinitely until interrupted
    while (true) {
        std::this_thread::sleep_for(std::chrono::seconds(1));
    }

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
