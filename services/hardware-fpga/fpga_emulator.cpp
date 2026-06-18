#include "fpga_emulator.hpp"
#include <cstring>
#include <algorithm>

FPGAEmulator::FPGAEmulator() noexcept {
    std::memset(&regs_, 0, sizeof(regs_));
    std::memset(fpga_sram_, 0, FPGA_SRAM_SIZE);
    regs_.status = 0x2;
}

FPGAEmulator::~FPGAEmulator() noexcept = default;

void FPGAEmulator::process_orders() noexcept {
    regs_.status = 0x1;
    regs_.latency_ns = rdtscp_fpga();

    uint32_t head = 0;
    uint32_t tail = 0;
    std::memcpy(&head, fpga_sram_, sizeof(head));
    std::memcpy(&tail, fpga_sram_ + 4, sizeof(tail));

    if (head == tail) {
        regs_.status = 0x2;
        return;
    }

    uint32_t processed = 0;
    constexpr uint32_t MAX_BATCH = 64;

    while (head != tail && processed < MAX_BATCH) {
        size_t offset = 8 + (head % 1024) * 32;
        if (offset + 32 > FPGA_SRAM_SIZE) break;

        uint64_t order_id;
        uint32_t price;
        uint32_t qty;
        uint8_t side;
        std::memcpy(&order_id, fpga_sram_ + offset, 8);
        std::memcpy(&price, fpga_sram_ + offset + 8, 4);
        std::memcpy(&qty, fpga_sram_ + offset + 12, 4);
        std::memcpy(&side, fpga_sram_ + offset + 16, 1);

        if (qty == 0) break;

        size_t result_offset = 8 + (head % 1024) * 64 + FPGA_SRAM_SIZE / 2;
        if (result_offset + 64 > FPGA_SRAM_SIZE) break;

        MatchResult result;
        result.buy_order_id = (side == 0) ? order_id : 0;
        result.sell_order_id = (side == 1) ? order_id : 0;
        result.price = price;
        result.qty = qty;
        result.matched = 1;
        std::memcpy(fpga_sram_ + result_offset, &result, sizeof(result));

        regs_.complete_events++;
        processed++;
        head++;
    }

    std::memcpy(fpga_sram_, &head, sizeof(head));
    regs_.latency_ns = rdtscp_fpga() - regs_.latency_ns;
    regs_.status = 0x2;
}
