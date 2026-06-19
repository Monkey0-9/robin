#pragma once

#include <cstdint>
#include <cstddef>

// SOFTWARE SIMULATION of FPGA-based order matching.
// This is NOT hardware-accelerated. It uses regular DRAM (memcpy) on CPU.
// Actual FPGA acceleration requires Xilinx Alveo U50 + Vitis HLS synthesis.

static constexpr size_t FPGA_SRAM_SIZE = 1048576; // 1MB simulated SRAM

struct alignas(8) FpgaRegs {
    uint32_t status;
    uint64_t latency_ns;
    uint32_t complete_events;
    uint64_t total_cycles;
    uint32_t error_count;
    char pad_[28];
};

struct alignas(8) MatchResult {
    uint64_t buy_order_id;
    uint64_t sell_order_id;
    uint32_t price;
    uint32_t qty;
    uint8_t matched;
    char pad_[23];
};

static inline uint64_t rdtscp_fpga() noexcept {
    uint32_t aux;
    uint64_t rax, rdx;
    __asm__ __volatile__("rdtscp" : "=a"(rax), "=d"(rdx) : : "rcx");
    return (rdx << 32) | rax;
}

class FPGAEmulator {
public:
    FPGAEmulator() noexcept;
    ~FPGAEmulator() noexcept;
    void process_orders() noexcept;
    const FpgaRegs& regs() const noexcept { return regs_; }

private:
    alignas(64) FpgaRegs regs_;
    alignas(64) uint8_t fpga_sram_[FPGA_SRAM_SIZE];
};
