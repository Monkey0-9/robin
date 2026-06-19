cmake_minimum_required(VERSION 3.25)
project(RobinTradingPlatform LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

add_subdirectory(services/execution-core)
add_subdirectory(services/network-bridge)
add_subdirectory(services/ingestion)
add_subdirectory(services/hardware-fpga)
add_subdirectory(services/pricing)
add_subdirectory(services/ai-engine)
