// Vitis HLS kernel for FPGA-accelerated order matching
// Target: Xilinx Alveo U50 (8GB HBM2, 8 HBM channels)
// Compile: vitis_hls -f build_order_match.tcl
// Latency target: <50ns kernel execution

#include <ap_int.h>
#include <hls_stream.h>
#include <ap_axi_sdata.h>

#define MAX_BOOK_DEPTH 256
#define MAX_MATCHES 64

typedef ap_uint<64> order_id_t;
typedef ap_uint<32> price_t;
typedef ap_uint<32> qty_t;
typedef ap_uint<16> depth_t;

struct OrderBookEntry {
    order_id_t order_id;
    price_t price;
    qty_t qty;
    ap_uint<8> side;      // 0=BID, 1=ASK
    ap_uint<8> active;
    ap_uint<48> timestamp;
};

struct MatchResultFPGA {
    order_id_t buy_order_id;
    order_id_t sell_order_id;
    price_t match_price;
    qty_t match_qty;
    ap_uint<8> matched;
};

// HLS pragma for HBM channels
// Each HBM channel is 256-bit wide, running at 450MHz
// Total bandwidth: 8 channels * 256-bit * 450MHz = 115.2 GB/s

static OrderBookEntry bid_book[MAX_BOOK_DEPTH];
static OrderBookEntry ask_book[MAX_BOOK_DEPTH];

#pragma HLS RESOURCE variable=bid_book core=RAM_2P_BRAM
#pragma HLS RESOURCE variable=ask_book core=RAM_2P_BRAM

static depth_t bid_count = 0;
static depth_t ask_count = 0;

// Top-level HLS kernel function
extern "C" {
void order_match_kernel(
    hls::stream<ap_axiu<256,0,0,0>>& input_stream,
    hls::stream<ap_axiu<256,0,0,0>>& output_stream,
    volatile ap_uint<64>* hbm_in,    // HBM channel 0 for input book state
    volatile ap_uint<64>* hbm_out    // HBM channel 1 for output matches
) {
#pragma HLS INTERFACE axis port=input_stream
#pragma HLS INTERFACE axis port=output_stream
#pragma HLS INTERFACE m_axi port=hbm_in offset=slave bundle=gmem0
#pragma HLS INTERFACE m_axi port=hbm_out offset=slave bundle=gmem1
#pragma HLS INTERFACE s_axilite port=return bundle=control

#pragma HLS PIPELINE II=1

    ap_axiu<256,0,0,0> cmd = input_stream.read();

    ap_uint<8> op = cmd.data.range(7, 0);
    price_t bid_price = cmd.data.range(63, 32);
    price_t ask_price = cmd.data.range(95, 64);
    qty_t bid_qty = cmd.data.range(127, 96);
    qty_t ask_qty = cmd.data.range(159, 128);
    order_id_t ord_id = cmd.data.range(223, 160);

    MatchResultFPGA result;
    result.matched = 0;
    result.match_price = 0;
    result.match_qty = 0;
    result.buy_order_id = 0;
    result.sell_order_id = 0;

    if (op == 1 && bid_price >= ask_price) {
        for (depth_t i = 0; i < MAX_BOOK_DEPTH; i++) {
#pragma HLS UNROLL
            if (i < ask_count && ask_book[i].active && ask_book[i].price <= bid_price) {
                qty_t match_qty = (bid_qty < ask_book[i].qty) ? bid_qty : ask_book[i].qty;
                result.matched = 1;
                result.buy_order_id = ord_id;
                result.sell_order_id = ask_book[i].order_id;
                result.match_price = ask_book[i].price;
                result.match_qty = match_qty;
                ask_book[i].qty -= match_qty;
                if (ask_book[i].qty == 0) {
                    ask_book[i].active = 0;
                }
                break;
            }
        }
    } else if (op == 2 && ask_price <= bid_price) {
        for (depth_t i = 0; i < MAX_BOOK_DEPTH; i++) {
#pragma HLS UNROLL
            if (i < bid_count && bid_book[i].active && bid_book[i].price >= ask_price) {
                qty_t match_qty = (ask_qty < bid_book[i].qty) ? ask_qty : bid_book[i].qty;
                result.matched = 1;
                result.buy_order_id = bid_book[i].order_id;
                result.sell_order_id = ord_id;
                result.match_price = bid_book[i].price;
                result.match_qty = match_qty;
                bid_book[i].qty -= match_qty;
                if (bid_book[i].qty == 0) {
                    bid_book[i].active = 0;
                }
                break;
            }
        }
    } else if (op == 3) {
        if (bid_count < MAX_BOOK_DEPTH) {
            bid_book[bid_count].order_id = ord_id;
            bid_book[bid_count].price = bid_price;
            bid_book[bid_count].qty = bid_qty;
            bid_book[bid_count].side = 0;
            bid_book[bid_count].active = 1;
            bid_count++;
        }
    } else if (op == 4) {
        if (ask_count < MAX_BOOK_DEPTH) {
            ask_book[ask_count].order_id = ord_id;
            ask_book[ask_count].price = ask_price;
            ask_book[ask_count].qty = ask_qty;
            ask_book[ask_count].side = 1;
            ask_book[ask_count].active = 1;
            ask_count++;
        }
    }

    ap_axiu<256,0,0,0> out;
    out.data.range(7, 0) = result.matched;
    out.data.range(39, 8) = result.match_price;
    out.data.range(71, 40) = result.match_qty;
    out.data.range(135, 72) = result.buy_order_id;
    out.data.range(199, 136) = result.sell_order_id;
    out.last = 1;
    output_stream.write(out);

    hbm_out[0] = result.match_price;
    hbm_out[1] = result.match_qty;
}
}

// Testbench
int main() {
    hls::stream<ap_axiu<256,0,0,0>> in;
    hls::stream<ap_axiu<256,0,0,0>> out;

    ap_axiu<256,0,0,0> cmd;
    cmd.data = 0;
    cmd.data.range(7, 0) = 3;       // op: add bid
    cmd.data.range(63, 32) = 50000;  // bid price
    cmd.data.range(127, 96) = 100;   // bid qty
    cmd.data.range(223, 160) = 1;    // order id

    in.write(cmd);
    order_match_kernel(in, out, nullptr, nullptr);
    out.read();

    cmd.data.range(7, 0) = 4;       // op: add ask
    cmd.data.range(63, 32) = 49900;  // ask price
    cmd.data.range(127, 96) = 100;   // ask qty
    cmd.data.range(223, 160) = 2;    // order id

    in.write(cmd);
    order_match_kernel(in, out, nullptr, nullptr);
    out.read();

    cmd.data.range(7, 0) = 1;       // op: match bid
    cmd.data.range(63, 32) = 50100;  // incoming bid price
    cmd.data.range(127, 96) = 50;    // incoming bid qty
    cmd.data.range(223, 160) = 3;    // order id

    in.write(cmd);
    order_match_kernel(in, out, nullptr, nullptr);
    ap_axiu<256,0,0,0> res = out.read();

    printf("Matched: %d | Price: %d | Qty: %d | BuyID: %llu | SellID: %llu\n",
           (int)res.data.range(7, 0),
           (int)res.data.range(39, 8),
           (int)res.data.range(71, 40),
           (unsigned long long)res.data.range(135, 72),
           (unsigned long long)res.data.range(199, 136));

    return 0;
}
