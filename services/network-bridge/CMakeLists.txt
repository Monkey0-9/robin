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

target_compile_options(kernel_bypass_ingest PRIVATE -mavx2 -msse4.2 -mpopcnt)
target_include_directories(kernel_bypass_ingest PRIVATE src)

add_executable(dpdk_ingest
    dpdk_ingest.cpp
)

target_compile_options(dpdk_ingest PRIVATE -mavx2 -msse4.2)
target_include_directories(dpdk_ingest PRIVATE src)

# AVX-512 ITCH parser header (available via src/ include path)
target_sources(kernel_bypass_ingest PRIVATE src/itch_parser.hpp)
target_sources(dpdk_ingest PRIVATE src/itch_parser.hpp)
