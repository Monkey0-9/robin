#include "fpga_emulator.hpp"
#include <cstring>

FPGAEmulator::FPGAEmulator() noexcept {
    std::memset(&regs_, 0, sizeof(regs_));
    std::memset(fpga_sram_, 0, FPGA_SRAM_SIZE);
    regs_.status = 0x2;
}

FPGAEmulator::~FPGAEmulator() noexcept = default;

void FPGAEmulator::process_orders() noexcept {
    regs_.status = 0x1;
    regs_.latency_ns = rdtscp_fpga();
}
