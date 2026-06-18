# Vitis HLS build script for FPGA order matching kernel
# Target: Xilinx Alveo U50 (xcvu50-fsvh2104-2-e)
# Usage: vitis_hls -f build_order_match.tcl

open_project -reset order_match_project

set_top order_match_kernel

add_files vitis_hls_order_match.cpp

open_solution -reset "solution1"

# Target device
set_part {xcu50-fsvh2104-2-e}

# Clock: 300MHz -> 3.33ns period for 50ns target kernel latency
create_clock -period 3.33 -name default

# HLS configuration
config_compile -pipeline_loops 1
config_compile -name_max_length 256
config_schedule -effort high
config_interface -m_axi_addr64

# Interface configuration
config_interface -m_axi_alignment_byte_size 64
config_interface -m_axi_max_widen_bitwidth 512
config_interface -m_axi_latency 0

# Pipeline loops aggressively
config_compile -pipeline_loops 1

# Map order book to URAM
config_bind -mem_bank_type URAM

# Use HBM via AXI3 interfaces
config_interface -m_axi_bus_width 512

csim_design
csynth_design
cosim_design

export_design -format xclbin -output order_match_kernel.xclbin

puts "========================================"
puts " FPGA Order Matching Kernel Build Done"
puts " Output: order_match_kernel.xclbin"
puts "========================================"

exit
