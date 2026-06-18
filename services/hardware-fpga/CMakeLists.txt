cmake_minimum_required(VERSION 3.25)
project(robin-fpga-simulation VERSION 1.0.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_INTERPROCEDURAL_OPTIMIZATION TRUE)
set(CMAKE_CXX_FLAGS_RELEASE "-O3 -march=native -mtune=native -flto=auto -fno-exceptions -fno-rtti -fno-stack-protector -fomit-frame-pointer -ffast-math")

add_library(fpga_emulator STATIC
    fpga_emulator.cpp
)

add_executable(vitis_hls_kernel
    vitis_hls_order_match.cpp
)
