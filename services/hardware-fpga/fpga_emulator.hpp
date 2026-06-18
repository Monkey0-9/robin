#pragma once

#include <cstdint>
#include <cstring>
#include <cstdio>
#include <new>

#define CACHE_LINE_SIZE 64
#define ALIGN_PAD_64 alignas(CACHE_LINE_SIZE)

struct alignas(CACHE_LINE_SIZE) AlveoDMARegisters {
    volatile uint32_t control;
    volatile uint32_t status;
    volatile uint64_t src_addr;
    volatile uint64_t dst_addr;
    volatile uint32_t length;
    volatile uint32_t complete_events;
    volatile uint64_t latency_ns;
    char pad_[24];
};

static_assert(sizeof(AlveoDMARegisters) == 64, "AlveoDMARegisters must be 64 bytes");

struct alignas(CACHE_LINE_SIZE) OrderBookState {
    uint64_t* bid_prices;
    uint64_t* bid_volumes;
    uint64_t* ask_prices;
    uint64_t* ask_volumes;
    uint32_t bid_count;
    uint32_t ask_count;
    uint64_t last_update_ns;
    char pad_[16];
};

static_assert(sizeof(OrderBookState) == 64, "OrderBookState size check");

struct alignas(CACHE_LINE_SIZE) MatchResult {
    uint64_t buy_order_id;
    uint64_t sell_order_id;
    uint32_t price;
    uint32_t qty;
    uint8_t matched;
    char pad_[39];
};

static_assert(sizeof(MatchResult) == 64, "MatchResult size check");

static inline uint64_t rdtscp_fpga() noexcept {
    uint32_t aux;
    uint64_t rax, rdx;
    __asm__ __volatile__("rdtscp" : "=a"(rax), "=d"(rdx) : : "rcx");
    return (rdx << 32) | rax;
}

class FPGAEmulator {
public:
    static constexpr size_t FPGA_SRAM_SIZE = 65536;
    static constexpr size_t HBM_CHANNELS = 8;
    static constexpr size_t HBM_CHANNEL_SIZE = 1024 * 1024 * 1024;

    FPGAEmulator() noexcept;
    ~FPGAEmulator() noexcept;

    void initialize() noexcept {
        regs_.control = 0x0;
        regs_.status = 0x2;
        regs_.latency_ns = 125;
    }

    void write_register(uint64_t addr, uint32_t value) noexcept {
        if (addr == 0) regs_.control = value;
        else if (addr == 8) regs_.src_addr = value;
        else if (addr == 16) regs_.dst_addr = value;
    }

    uint32_t read_register(uint64_t addr) noexcept {
        if (addr == 0) return regs_.control;
        if (addr == 4) return regs_.status;
        return 0;
    }

    void trigger_dma_transfer() noexcept {
        regs_.complete_events = regs_.complete_events + 1;
    }

    void process_orders() noexcept;

private:
    ALIGN_PAD_64 AlveoDMARegisters regs_;
    char fpga_sram_[FPGA_SRAM_SIZE];
};
