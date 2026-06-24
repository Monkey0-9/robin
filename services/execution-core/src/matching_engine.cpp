#include "matching_engine.hpp"
#include "order_server.hpp"
#include <cstdio>
#include <csignal>
#include <cstdlib>
#include <chrono>
#include <thread>

std::atomic<bool> g_running(true);

void signal_handler(int) {
    g_running = false;
}

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
    server.stop();
    engine->stop();

    const auto& s = engine->stats();
    std::printf("[ENGINE] Final: Orders=%llu Trades=%llu Rejected=%llu\n",
        (unsigned long long)s.orders_submitted,
        (unsigned long long)s.trades_executed,
        (unsigned long long)s.orders_rejected);
    return 0;
}
