cmake_minimum_required(VERSION 3.25)
project(robin-network-bridge VERSION 1.0.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_INTERPROCEDURAL_OPTIMIZATION TRUE)
set(CMAKE_CXX_FLAGS_RELEASE "-O3 -march=native -mtune=native -flto=auto -fno-exceptions -fno-rtti -fno-stack-protector -fomit-frame-pointer -mno-red-zone -ffast-math -fno-math-errno -frename-registers -falign-functions=64 -fvisibility=hidden")

find_package(PkgConfig QUIET)

add_executable(kernel_bypass_ingest
    kernel_bypass_ingest.cpp
)

target_compile_options(kernel_bypass_ingest PRIVATE -mavx512f -mavx512bw -msse4.2 -mpopcnt)

add_executable(dpdk_ingest
    dpdk_ingest.cpp
)

target_compile_options(dpdk_ingest PRIVATE -mavx512f -mavx512bw -msse4.2)
