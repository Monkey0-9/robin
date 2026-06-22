#pragma once

#include <cstdint>
#include <cstddef>

// ============================================================================
// SoftwareOrderMatchSimulator — CPU-based order matching simulation
// ============================================================================
// This is a SOFTWARE SIMULATION running entirely on CPU using DRAM (memcpy).
// It models the interface that a real FPGA accelerator would expose.
//
// What this IS:
//   - A functional prototype of FPGA-style order processing logic
//   - Useful for testing the FPGA interface contract on a dev machine
//   - Latency numbers here are CPU latencies, NOT FPGA latencies
//
// What this IS NOT:
//   - Hardware accelerated
//   - Running on an FPGA (no synthesis, no bitstream, no Xilinx Alveo)
//   - Representative of real FPGA throughput (~10ns) or determinism
//
// To synthesize real FPGA logic: see vitis_hls_order_match.cpp + build_order_match.tcl
// Hardware requirement: Xilinx Alveo U50 + Vitis HLS 2023.2

static constexpr size_t SIM_SRAM_SIZE = 1048576; // 1MB simulated SRAM (software buffer)

struct alignas(8) SimRegs {
    uint32_t status;           // 0x1 = processing, 0x2 = idle
    uint64_t latency_cycles;   // CPU TSC cycles (NOT FPGA clock cycles)
    uint32_t complete_events;  // Orders processed
    uint64_t total_cycles;
    uint32_t error_count;
    char pad_[28];
};

struct alignas(8) MatchResult {
    uint64_t buy_order_id;
    uint64_t sell_order_id;
    uint32_t price;
    uint32_t qty;
    uint8_t  matched;
    char     pad_[23];
};

static inline uint64_t rdtscp_sim() noexcept {
#if defined(__x86_64__) || defined(__i386__)
    uint32_t aux;
    uint64_t rax, rdx;
    __asm__ __volatile__("rdtscp" : "=a"(rax), "=d"(rdx) : : "rcx");
    return (rdx << 32) | rax;
#else
    return 0;
#endif
}

// Renamed from FPGAEmulator to SoftwareOrderMatchSimulator to clearly
// communicate that this is a CPU simulation, not actual FPGA hardware.
class SoftwareOrderMatchSimulator {
public:
    SoftwareOrderMatchSimulator() noexcept;
    ~SoftwareOrderMatchSimulator() noexcept;
    void process_orders() noexcept;
    const SimRegs& regs() const noexcept { return regs_; }

    // Returns false always — this is a software sim, not real hardware
    static constexpr bool is_hardware_fpga() noexcept { return false; }
    static constexpr const char* backend_description() noexcept {
        return "CPU Software Simulation (memcpy, DRAM) — NOT FPGA hardware";
    }

private:
    alignas(64) SimRegs  regs_;
    alignas(64) uint8_t  sim_sram_[SIM_SRAM_SIZE];
};

// Backward-compat alias so any existing code using old name gets a clear error
// with a helpful message rather than a silent link failure.
// Remove this alias once all references are updated.
using FPGAEmulator = SoftwareOrderMatchSimulator;
