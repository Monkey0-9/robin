// Vitis HLS kernel for FPGA-accelerated order matching
// Target: Xilinx Alveo U50 (8GB HBM2, 8 HBM channels)
// Compile: vitis_hls -f build_order_match.tcl
// Latency target: <50ns kernel execution, <200ns end-to-end
//
// Architecture:
//   - PIPO (pipelined) design: 1 order processed per clock cycle
//   - HBM channel 0-3: bid book state (256 levels, 4-way banked)
//   - HBM channel 4-7: ask book state (256 levels, 4-way banked)
//   - AXI4-Stream input: incoming order commands
//   - AXI4-Stream output: match results
//   - Dual-port BRAM for book storage (one read, one write per cycle)

#include <ap_int.h>
#include <hls_stream.h>
#include <hls_math.h>
#include <ap_axi_sdata.h>

#define MAX_BOOK_DEPTH 256
#define MAX_MATCHES 64
#define PRICE_SCALE 10000ULL
#define HBM_CHANNEL_WIDTH 256

typedef ap_uint<64> order_id_t;
typedef ap_uint<48> price_t;
typedef ap_uint<32> qty_t;
typedef ap_uint<16> depth_t;
typedef ap_uint<8> op_t;

// Operation codes
const op_t OP_RESET    = 0;
const op_t OP_MATCH_BID = 1;
const op_t OP_MATCH_ASK = 2;
const op_t OP_ADD_BID  = 3;
const op_t OP_ADD_ASK  = 4;
const op_t OP_CANCEL   = 5;
const op_t OP_REPLACE  = 6;

struct OrderBookEntry {
    order_id_t order_id;
    price_t price;
    qty_t qty;
    ap_uint<8> side;
    ap_uint<8> active;
    ap_uint<48> timestamp;
    ap_uint<8> reserved[2];
};

struct MatchResultFPGA {
    order_id_t buy_order_id;
    order_id_t sell_order_id;
    price_t match_price;
    qty_t match_qty;
    ap_uint<8> matched;
    ap_uint<8> partial;
    ap_uint<48> latency_cycles;
};

// HBM-interfaced book storage
// Using dataflow + array partition for 4 concurrent HBM reads/writes
OrderBookEntry bid_book[MAX_BOOK_DEPTH];
OrderBookEntry ask_book[MAX_BOOK_DEPTH];

#pragma HLS RESOURCE variable=bid_book core=RAM_T2P_BRAM
#pragma HLS RESOURCE variable=ask_book core=RAM_T2P_BRAM
#pragma HLS ARRAY_PARTITION variable=bid_book complete dim=1
#pragma HLS ARRAY_PARTITION variable=ask_book complete dim=1

depth_t bid_count = 0;
depth_t ask_count = 0;

// Priority encoder: find best match in O(1) using parallel comparison tree
static void find_best_match_bid(
    const OrderBookEntry book[MAX_BOOK_DEPTH],
    depth_t count,
    price_t bid_price,
    depth_t& best_idx,
    price_t& best_price) {

    depth_t idx = MAX_BOOK_DEPTH;
    price_t px = 0;

    for (depth_t i = 0; i < MAX_BOOK_DEPTH; i++) {
#pragma HLS UNROLL factor=32
        if (i < count && book[i].active && book[i].price <= bid_price) {
            if (i < idx) {
                idx = i;
                px = book[i].price;
            }
        }
    }

    best_idx = idx;
    best_price = px;
}

static void find_best_match_ask(
    const OrderBookEntry book[MAX_BOOK_DEPTH],
    depth_t count,
    price_t ask_price,
    depth_t& best_idx,
    price_t& best_price) {

    depth_t idx = MAX_BOOK_DEPTH;
    price_t px = 0;

    for (depth_t i = 0; i < MAX_BOOK_DEPTH; i++) {
#pragma HLS UNROLL factor=32
        if (i < count && book[i].active && book[i].price >= ask_price) {
            if (i < idx) {
                idx = i;
                px = book[i].price;
            }
        }
    }

    best_idx = idx;
    best_price = px;
}

// Insert into price-time sorted book
static void insert_bid(OrderBookEntry book[MAX_BOOK_DEPTH], depth_t& count,
                        const OrderBookEntry& entry) {
    if (count >= MAX_BOOK_DEPTH) return;

    depth_t pos = count;
    for (depth_t i = 0; i < count; i++) {
#pragma HLS UNROLL factor=32
        if (book[i].active && entry.price > book[i].price) {
            pos = i;
            break;
        }
    }

    for (depth_t i = count; i > pos; i--) {
#pragma HLS UNROLL factor=32
        book[i] = book[i - 1];
    }

    book[pos] = entry;
    count++;
}

static void insert_ask(OrderBookEntry book[MAX_BOOK_DEPTH], depth_t& count,
                        const OrderBookEntry& entry) {
    if (count >= MAX_BOOK_DEPTH) return;

    depth_t pos = count;
    for (depth_t i = 0; i < count; i++) {
#pragma HLS UNROLL factor=32
        if (book[i].active && entry.price < book[i].price) {
            pos = i;
            break;
        }
    }

    for (depth_t i = count; i > pos; i--) {
#pragma HLS UNROLL factor=32
        book[i] = book[i - 1];
    }

    book[pos] = entry;
    count++;
}

extern "C" {
void order_match_kernel(
    hls::stream<ap_axiu<256,0,0,0>>& input_stream,
    hls::stream<ap_axiu<256,0,0,0>>& output_stream,
    volatile ap_uint<64>* hbm_in,
    volatile ap_uint<64>* hbm_out
) {
#pragma HLS INTERFACE axis port=input_stream
#pragma HLS INTERFACE axis port=output_stream
#pragma HLS INTERFACE m_axi port=hbm_in offset=slave bundle=gmem0
#pragma HLS INTERFACE m_axi port=hbm_out offset=slave bundle=gmem1
#pragma HLS INTERFACE s_axilite port=return bundle=control
#pragma HLS PIPELINE II=1

    ap_axiu<256,0,0,0> cmd = input_stream.read();
    ap_uint<256> data = cmd.data;

    op_t op = data.range(7, 0);
    price_t bid_price = data.range(55, 8);
    price_t ask_price = data.range(103, 56);
    qty_t bid_qty = data.range(135, 104);
    qty_t ask_qty = data.range(167, 136);
    order_id_t ord_id = data.range(231, 168);
    order_id_t replace_id = data.range(255, 232);

    MatchResultFPGA result;
    result.matched = 0;
    result.partial = 0;
    result.match_price = 0;
    result.match_qty = 0;
    result.buy_order_id = 0;
    result.sell_order_id = 0;
    result.latency_cycles = 0;

    switch (op) {
        case OP_RESET:
            for (depth_t i = 0; i < MAX_BOOK_DEPTH; i++) {
#pragma HLS UNROLL factor=32
                bid_book[i].active = 0;
                ask_book[i].active = 0;
            }
            bid_count = 0;
            ask_count = 0;
            break;

        case OP_MATCH_BID: {
            depth_t best_idx;
            price_t best_price;
            find_best_match_ask(ask_book, ask_count, bid_price, best_idx, best_price);

            if (best_idx < MAX_BOOK_DEPTH && ask_book[best_idx].active) {
                qty_t match_qty = (bid_qty < ask_book[best_idx].qty) ? bid_qty : ask_book[best_idx].qty;

                result.matched = 1;
                result.buy_order_id = ord_id;
                result.sell_order_id = ask_book[best_idx].order_id;
                result.match_price = ask_book[best_idx].price;
                result.match_qty = match_qty;

                ask_book[best_idx].qty -= match_qty;
                if (ask_book[best_idx].qty == 0) {
                    ask_book[best_idx].active = 0;
                    result.partial = 0;
                } else {
                    result.partial = 1;
                }
            }
            break;
        }

        case OP_MATCH_ASK: {
            depth_t best_idx;
            price_t best_price;
            find_best_match_bid(bid_book, bid_count, ask_price, best_idx, best_price);

            if (best_idx < MAX_BOOK_DEPTH && bid_book[best_idx].active) {
                qty_t match_qty = (ask_qty < bid_book[best_idx].qty) ? ask_qty : bid_book[best_idx].qty;

                result.matched = 1;
                result.buy_order_id = bid_book[best_idx].order_id;
                result.sell_order_id = ord_id;
                result.match_price = bid_book[best_idx].price;
                result.match_qty = match_qty;

                bid_book[best_idx].qty -= match_qty;
                if (bid_book[best_idx].qty == 0) {
                    bid_book[best_idx].active = 0;
                    result.partial = 0;
                } else {
                    result.partial = 1;
                }
            }
            break;
        }

        case OP_ADD_BID: {
            OrderBookEntry entry;
            entry.order_id = ord_id;
            entry.price = bid_price;
            entry.qty = bid_qty;
            entry.side = 0;
            entry.active = 1;
            entry.timestamp = 0;
            insert_bid(bid_book, bid_count, entry);
            break;
        }

        case OP_ADD_ASK: {
            OrderBookEntry entry;
            entry.order_id = ord_id;
            entry.price = ask_price;
            entry.qty = ask_qty;
            entry.side = 1;
            entry.active = 1;
            entry.timestamp = 0;
            insert_ask(ask_book, ask_count, entry);
            break;
        }

        case OP_CANCEL: {
            for (depth_t i = 0; i < MAX_BOOK_DEPTH; i++) {
#pragma HLS UNROLL factor=32
                if (bid_book[i].active && bid_book[i].order_id == ord_id) {
                    bid_book[i].active = 0;
                    break;
                }
            }
            for (depth_t i = 0; i < MAX_BOOK_DEPTH; i++) {
#pragma HLS UNROLL factor=32
                if (ask_book[i].active && ask_book[i].order_id == ord_id) {
                    ask_book[i].active = 0;
                    break;
                }
            }
            break;
        }

        case OP_REPLACE: {
            for (depth_t i = 0; i < MAX_BOOK_DEPTH; i++) {
#pragma HLS UNROLL factor=32
                if (bid_book[i].active && bid_book[i].order_id == replace_id) {
                    bid_book[i].order_id = ord_id;
                    bid_book[i].price = bid_price;
                    bid_book[i].qty = bid_qty;
                    break;
                }
            }
            for (depth_t i = 0; i < MAX_BOOK_DEPTH; i++) {
#pragma HLS UNROLL factor=32
                if (ask_book[i].active && ask_book[i].order_id == replace_id) {
                    ask_book[i].order_id = ord_id;
                    ask_book[i].price = ask_price;
                    ask_book[i].qty = ask_qty;
                    break;
                }
            }
            break;
        }
    }

    // Write result
    ap_axiu<256,0,0,0> out;
    out.data = 0;
    out.data.range(7, 0) = result.matched;
    out.data.range(8, 8) = result.partial;
    out.data.range(56, 9) = result.match_price;
    out.data.range(88, 57) = result.match_qty;
    out.data.range(152, 89) = result.buy_order_id;
    out.data.range(216, 153) = result.sell_order_id;
    out.data.range(248, 217) = result.latency_cycles;
    out.last = 1;
    out.keep = 0xFFFFFFFF;
    out.strb = 0xFFFFFFFF;
    output_stream.write(out);

    // Write to HBM for host read
    hbm_out[0] = result.match_price;
    hbm_out[1] = result.match_qty;
    hbm_out[2] = result.buy_order_id;
    hbm_out[3] = result.sell_order_id;
}
}
