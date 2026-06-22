#include "fpga_emulator.hpp"
#include <cstring>
#include <algorithm>
#include <cstdio>

// ============================================================================
// SoftwareOrderMatchSimulator implementation
// CPU-based simulation of FPGA order-matching interface
// ============================================================================

SoftwareOrderMatchSimulator::SoftwareOrderMatchSimulator() noexcept {
    std::memset(&regs_, 0, sizeof(regs_));
    std::memset(sim_sram_, 0, SIM_SRAM_SIZE);
    regs_.status = 0x2; // Idle
}

SoftwareOrderMatchSimulator::~SoftwareOrderMatchSimulator() noexcept = default;

void SoftwareOrderMatchSimulator::process_orders() noexcept {
    regs_.status = 0x1; // Processing
    const uint64_t t0 = rdtscp_sim();

    uint32_t head = 0;
    uint32_t tail = 0;
    std::memcpy(&head, sim_sram_,     sizeof(head));
    std::memcpy(&tail, sim_sram_ + 4, sizeof(tail));

    if (head == tail) {
        regs_.status = 0x2; // Idle — nothing to do
        return;
    }

    uint32_t processed = 0;
    constexpr uint32_t MAX_BATCH = 64;
    constexpr uint32_t RING_SIZE = 1024;

    while (head != tail && processed < MAX_BATCH) {
        size_t offset = 8 + (head % RING_SIZE) * 32;
        if (offset + 32 > SIM_SRAM_SIZE) {
            regs_.error_count++;
            break;
        }

        uint64_t order_id = 0;
        uint32_t price    = 0;
        uint32_t qty      = 0;
        uint8_t  side     = 0;
        std::memcpy(&order_id, sim_sram_ + offset,      8);
        std::memcpy(&price,    sim_sram_ + offset + 8,  4);
        std::memcpy(&qty,      sim_sram_ + offset + 12, 4);
        std::memcpy(&side,     sim_sram_ + offset + 16, 1);

        if (qty == 0) break;

        // Write match result to second half of sim_sram_
        size_t result_offset = (SIM_SRAM_SIZE / 2) + (head % RING_SIZE) * sizeof(MatchResult);
        if (result_offset + sizeof(MatchResult) > SIM_SRAM_SIZE) {
            regs_.error_count++;
            break;
        }

        MatchResult result{};
        result.buy_order_id  = (side == 0) ? order_id : 0;
        result.sell_order_id = (side == 1) ? order_id : 0;
        result.price         = price;
        result.qty           = qty;
        result.matched       = 1;
        std::memcpy(sim_sram_ + result_offset, &result, sizeof(result));

        regs_.complete_events++;
        regs_.total_cycles++;
        processed++;
        head++;
    }

    std::memcpy(sim_sram_, &head, sizeof(head));
    regs_.latency_cycles = rdtscp_sim() - t0;
    regs_.status = 0x2; // Idle
}

// ============================================================================
// Demo / self-test
// ============================================================================
#ifdef FPGA_EMULATOR_MAIN
int main() {
    SoftwareOrderMatchSimulator sim;
    std::printf("[SIM] Backend: %s\n", SoftwareOrderMatchSimulator::backend_description());
    std::printf("[SIM] is_hardware_fpga: %s\n",
                SoftwareOrderMatchSimulator::is_hardware_fpga() ? "true" : "false");

    // Inject a synthetic order into sim_sram_
    // (In integration, the matching engine writes here via IPC)
    sim.process_orders();

    const auto& r = sim.regs();
    std::printf("[SIM] Status=%u Events=%u Errors=%u LatCycles=%llu\n",
                r.status, r.complete_events, r.error_count,
                (unsigned long long)r.latency_cycles);
    return 0;
}
#endif
