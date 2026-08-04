#include "matching_engine.hpp"
#include "order_server.hpp"
#include <cstdio>
#include <csignal>
#include <cstdlib>
#include <chrono>
#include <thread>
#include <atomic>

#if defined(__linux__)
#include <sys/mman.h>
#include <sys/stat.h>
#include <fcntl.h>
#include <unistd.h>
#endif

std::atomic<bool> g_running(true);

void signal_handler(int) {
    g_running = false;
}

#if defined(__linux__)
struct alignas(64) ShmHeader {
    alignas(64) std::atomic<uint64_t> write_idx;
    uint8_t pad1_[56];
    alignas(64) std::atomic<uint64_t> read_idx;
    uint8_t pad2_[56];
    alignas(64) uint64_t magic;
    uint32_t version;
    uint32_t size;
    uint32_t pid_writer;
    uint32_t pid_reader;
    uint8_t pad3_[40];
};

#pragma pack(push, 1)
struct ShmMessage {
    uint8_t msg_type;
    uint32_t client_id;
    uint32_t instrument_id;
    uint64_t price;
    uint64_t qty;
    uint8_t side;
    uint8_t flags;
    uint64_t order_id;
    uint64_t cl_order_id;
    uint64_t timestamp_ns;
    uint8_t _pad[13];
};
#pragma pack(pop)
static_assert(sizeof(ShmMessage) == 64, "ShmMessage size must be 64 bytes");
constexpr size_t SHM_CAPACITY = 65536;
constexpr size_t SHM_SIZE = sizeof(ShmHeader) + SHM_CAPACITY * sizeof(ShmMessage);

void shm_poll_thread(quantum::execution::MatchingEngine* engine) {
    int fd = shm_open("/robin_risk_match", O_RDWR, 0666);
    if (fd < 0) {
        std::printf("[ENGINE] SHM /robin_risk_match not found. Waiting...\n");
        return;
    }
    void* mapped = mmap(nullptr, SHM_SIZE, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
    close(fd);
    if (mapped == MAP_FAILED) return;

    ShmHeader* header = static_cast<ShmHeader*>(mapped);
    ShmMessage* ring = reinterpret_cast<ShmMessage*>(static_cast<uint8_t*>(mapped) + sizeof(ShmHeader));

    std::printf("[ENGINE] Connected to /robin_risk_match SHM\n");

    uint64_t local_read_idx = header->read_idx.load(std::memory_order_relaxed);

    while (g_running) {
        uint64_t write_idx = header->write_idx.load(std::memory_order_acquire);
        if (local_read_idx < write_idx) {
            size_t slot = local_read_idx & (SHM_CAPACITY - 1);
            ShmMessage& msg = ring[slot];
            
            quantum::execution::Order order{};
            order.id = msg.order_id;
            order.price = msg.price;
            order.qty = msg.qty;
            order.instrument_id = msg.instrument_id;
            order.client_id = msg.client_id;
            order.side = msg.side == 0 ? quantum::execution::Side::BID : quantum::execution::Side::ASK;
            order.state = quantum::execution::OrderState::NEW;
            order.type = quantum::execution::OrderType::LIMIT;
            
            engine->submit_order(order);
            
            local_read_idx++;
            header->read_idx.store(local_read_idx, std::memory_order_release);
        } else {
            std::this_thread::yield();
        }
    }
    munmap(mapped, SHM_SIZE);
}
#endif

int main(int argc, char* argv[]) {
    using namespace quantum::execution;

    uint16_t port = 9091;
    if (argc > 1) {
        int p = std::atoi(argv[1]);
        if (p > 0 && p < 65536) port = (uint16_t)p;
    }

    std::signal(SIGINT, signal_handler);
    std::signal(SIGTERM, signal_handler);

    auto engine = std::make_unique<MatchingEngine>();
    engine->init(0, 2);
    engine->start();

    OrderServer server(engine.get(), port);
    if (!server.start()) {
        std::fprintf(stderr, "[ENGINE] Failed to start TCP server on port %u\n", port);
        return 1;
    }

    std::printf("[ENGINE] Robin Matching Engine v1.0\n");
    std::printf("[ENGINE] TCP server listening on port %u\n", port);
    std::printf("[ENGINE] Send JSON orders or 'health' to check status\n");
    std::printf("[ENGINE] Ctrl+C to stop\n\n");

#if defined(__linux__)
    std::thread shm_thread(shm_poll_thread, engine.get());
#endif

    auto last_stats = std::chrono::steady_clock::now();
    while (g_running) {
        std::this_thread::sleep_for(std::chrono::milliseconds(100));

        auto now = std::chrono::steady_clock::now();
        if (now - last_stats > std::chrono::seconds(5)) {
            last_stats = now;
            const auto& s = engine->stats();
            std::printf("[ENGINE] Orders=%llu Trades=%llu Rejected=%llu AvgLat=%llu ns\n",
                (unsigned long long)s.orders_submitted,
                (unsigned long long)s.trades_executed,
                (unsigned long long)s.orders_rejected,
                s.cycle_count ? (unsigned long long)(s.total_latency_ns / s.cycle_count) : 0);
        }
    }

    std::printf("\n[ENGINE] Shutting down...\n");
#if defined(__linux__)
    if (shm_thread.joinable()) shm_thread.join();
#endif
    server.stop();
    engine->stop();

    const auto& s = engine->stats();
    std::printf("[ENGINE] Final: Orders=%llu Trades=%llu Rejected=%llu\n",
        (unsigned long long)s.orders_submitted,
        (unsigned long long)s.trades_executed,
        (unsigned long long)s.orders_rejected);
    return 0;
}
