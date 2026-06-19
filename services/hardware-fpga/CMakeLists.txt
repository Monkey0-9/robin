cmake_minimum_required(VERSION 3.25)
project(robin-fpga-sim VERSION 0.1.0 LANGUAGES CXX)
set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# NOTE: This is a SOFTWARE SIMULATION of FPGA behavior.
# Actual FPGA acceleration requires Xilinx Alveo U50 + Vitis HLS.
# The fpga_emulator uses memcpy on regular DRAM - NOT hardware.

add_executable(fpga_simulator src/fpga_emulator.cpp)
target_include_directories(fpga_simulator PRIVATE src)
