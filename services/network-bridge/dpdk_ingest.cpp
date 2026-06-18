// DPDK-based kernel-bypass market data ingestion
// Build: requires DPDK 23.11+ with RTE_NEXT_ABI
// Compile: g++ -O3 -march=native $(pkg-config --cflags libdpdk) dpdk_ingest.cpp $(pkg-config --libs libdpdk)

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <atomic>

#ifndef likely
#define likely(x)   __builtin_expect(!!(x), 1)
#endif
#ifndef unlikely
#define unlikely(x) __builtin_expect(!!(x), 0)
#endif

#define CACHE_LINE_SIZE 64
#define ALIGN_PAD_64 alignas(CACHE_LINE_SIZE)
#define RTE_ETH_RX_BURST_SIZE 64

static inline uint64_t rdtscp_di() noexcept {
    uint32_t aux; uint64_t rax, rdx;
    __asm__ __volatile__("rdtscp" : "=a"(rax), "=d"(rdx) : : "rcx");
    return (rdx << 32) | rax;
}

struct alignas(CACHE_LINE_SIZE) PacketMeta {
    uint8_t* data;
    uint32_t length;
    uint64_t timestamp_ns;
};

class DPDKIngestionEngine {
public:
    DPDKIngestionEngine() noexcept {
        std::memset(&stats_, 0, sizeof(stats_));
        stats_.min_latency_ns = UINT64_MAX;
    }

    int init(int argc, char** argv) noexcept {
#if defined(RTE_EXEC_ENV)
        int ret = rte_eal_init(argc, argv);
        if (ret < 0) return ret;
        if (init_port() < 0) return -1;
        printf("[DPDK] Initialized\n");
        return 0;
#else
        (void)argc; (void)argv;
        printf("[DPDK] Not available. Install DPDK and define RTE_EXEC_ENV\n");
        return 0;
#endif
    }

#if defined(RTE_EXEC_ENV)
    int init_port() noexcept {
        struct rte_mempool* mbuf_pool = rte_pktmbuf_pool_create(
            "MBUF_POOL", 8192*16, 256, 0, RTE_MBUF_DEFAULT_BUF_SIZE, rte_socket_id());
        if (!mbuf_pool) return -1;
        struct rte_eth_conf port_conf = {};
        port_conf.rxmode.mq_mode = RTE_ETH_MQ_RX_RSS;
        int ret = rte_eth_dev_configure(0, 1, 0, &port_conf);
        if (ret < 0) return ret;
        ret = rte_eth_rx_queue_setup(0, 0, 1024, rte_eth_dev_socket_id(0), nullptr, mbuf_pool);
        if (ret < 0) return ret;
        ret = rte_eth_dev_start(0);
        if (ret < 0) return ret;
        rte_eth_promiscuous_enable(0);
        return 0;
    }

    uint16_t rx_burst(PacketMeta* metas, uint16_t max_packets) noexcept {
        struct rte_mbuf* bufs[RTE_ETH_RX_BURST_SIZE];
        uint16_t nb_rx = rte_eth_rx_burst(0, 0, bufs, max_packets);
        uint64_t now = rdtscp_di();
        for (uint16_t i = 0; i < nb_rx; ++i) {
            metas[i].data = rte_pktmbuf_mtod(bufs[i], uint8_t*);
            metas[i].length = rte_pktmbuf_pkt_len(bufs[i]);
            metas[i].timestamp_ns = now;
            stats_.packets_rx++;
            stats_.bytes_rx += metas[i].length;
            rte_pktmbuf_free(bufs[i]);
        }
        return nb_rx;
    }
#endif

    void poll_loop() noexcept {
        ALIGN_PAD_64 PacketMeta burst[RTE_ETH_RX_BURST_SIZE];
        while (running_) {
            uint16_t nb = 0;
#if defined(RTE_EXEC_ENV)
            nb = rx_burst(burst, RTE_ETH_RX_BURST_SIZE);
#endif
            if (nb == 0) { __asm__ __volatile__("pause" ::: "memory"); }
        }
    }

    void stop() noexcept { running_ = false; }

private:
    ALIGN_PAD_64 std::atomic<bool> running_{true};
    struct { uint64_t packets_rx, bytes_rx, min_latency_ns, max_latency_ns; } stats_;
};

int main(int argc, char** argv) {
    DPDKIngestionEngine engine;
    engine.init(argc, argv);
    return 0;
}
