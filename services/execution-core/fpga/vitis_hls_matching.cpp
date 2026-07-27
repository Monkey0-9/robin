// Phase 2: FPGA Hardware Acceleration (Vitis HLS)
// Target: AMD Xilinx Alveo U50/U55C

#include <cstdint>

// HLS pragmas are ignored by standard C++ compilers but used by Vitis HLS
// to synthesize Register-Transfer Level (RTL) Verilog/VHDL logic.

struct FpgaOrder {
    uint64_t order_id;
    uint64_t price;
    uint32_t qty;
    uint8_t side; // 0=Bid, 1=Ask
};

// Represents a deeply pipelined order book matcher in hardware
void fpga_matching_engine(FpgaOrder incoming_stream[1024], FpgaOrder matched_stream[1024]) {
    // Pipeline directive forces the synthesizer to accept a new input every clock cycle (Initiation Interval = 1)
    #pragma HLS pipeline II=1
    #pragma HLS INTERFACE ap_fifo port=incoming_stream
    #pragma HLS INTERFACE ap_fifo port=matched_stream

    for (int i = 0; i < 1024; i++) {
        #pragma HLS unroll factor=4
        FpgaOrder order = incoming_stream[i];
        
        // Simplified matching logic for synthesis estimation
        if (order.qty > 0) {
            matched_stream[i] = order;
            matched_stream[i].qty = 0; // Filled
        }
    }
}
