// services/hardware-fpga/fpga_emulator.cpp
#include "fpga_emulator.hpp"
#include <cstring>
#include <thread>

FPGAEmulator::FPGAEmulator() {
    std::memset(&regs_, 0, sizeof(regs_));
    std::memset(fpga_sram_, 0, sizeof(fpga_sram_));
}

FPGAEmulator::~FPGAEmulator() {}

void FPGAEmulator::initialize() {
    regs_.control = 0x0; // Stopped
    regs_.status = 0x2;  // Idle
    std::cout << "[FPGA Emulator] Xilinx Alveo U50 FPGA initialized. Base Register Address: 0x7F000000. Simulated Latency: " 
              << get_simulated_latency_ns() << "ns." << std::endl;
}

void FPGAEmulator::write_dma_source(const uint8_t* data, uint32_t len) {
    if (len > sizeof(fpga_sram_)) len = sizeof(fpga_sram_);
    std::memcpy(fpga_sram_, data, len);
    regs_.src_addr = reinterpret_cast<uint64_t>(data);
    regs_.length = len;
}

void FPGAEmulator::trigger_dma_transfer() {
    regs_.control |= 0x1; // Set Start bit
    regs_.status = 0x1;   // Busy
    
    // Simulate ultra-fast FPGA hardware execution
    // Simple price impact calculation in HW logic
    if (regs_.length >= 8) {
        double current_price;
        std::memcpy(&current_price, fpga_sram_, sizeof(double));
        current_price *= 1.0001; // FPGA price adjustment logic
        std::memcpy(fpga_sram_ + 100, &current_price, sizeof(double));
    }
    
    regs_.status = 0x4; // Complete
    regs_.control &= ~0x1; // Clear Start bit
}

bool FPGAEmulator::poll_dma_complete() {
    return (regs_.status & 0x4) != 0;
}

void FPGAEmulator::read_dma_destination(uint8_t* out_data, uint32_t len) {
    if (len > sizeof(fpga_sram_)) len = sizeof(fpga_sram_);
    std::memcpy(out_data, fpga_sram_ + 100, len);
    regs_.dst_addr = reinterpret_cast<uint64_t>(out_data);
}
