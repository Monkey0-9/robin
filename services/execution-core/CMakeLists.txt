cmake_minimum_required(VERSION 3.25)
project(robin-execution-core VERSION 1.0.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# ─── Compiler flags ───────────────────────────────────────────────────────────
if(MSVC)
    set(CMAKE_CXX_FLAGS_RELEASE "/O2 /arch:AVX2 /DNDEBUG")
elseif(MINGW)
    set(CMAKE_CXX_FLAGS_RELEASE "-O3 -march=native -fno-exceptions -fno-rtti -fomit-frame-pointer")
else()
    set(CMAKE_CXX_FLAGS_RELEASE "-O2 -mavx2 -fno-exceptions -fno-rtti -fomit-frame-pointer")
    set(CMAKE_EXE_LINKER_FLAGS_RELEASE "-Wl,--gc-sections")
endif()

# ─── Matching Engine ──────────────────────────────────────────────────────────
add_executable(matching_engine
    src/matching_engine.cpp
    src/matching_engine.hpp
    src/order_book.hpp
    src/order_state.hpp
    src/lockfree_queue.hpp
    src/memory_pool.hpp
    src/risk_checks.hpp
    src/order_server.hpp
    src/latency_benchmark.hpp
    src/indicators.cpp
    src/indicators.hpp
    src/twap_engine.cpp
    src/twap_engine.hpp
    src/sor.cpp
    src/sor.hpp
)
target_include_directories(matching_engine PRIVATE src)

# ─── C++ Live Feed (WebSocket → Strategy Engine → Signals) ────────────────────
add_executable(live_feed
    src/live_feed.cpp
    src/strategy_engine.hpp
)
target_include_directories(live_feed PRIVATE src ../../shared ../../research/ai-engine)

# Optional ONNX Runtime integration for ML signal engine
option(USE_ONNX_RUNTIME "Build with ONNX Runtime support" OFF)
if(USE_ONNX_RUNTIME)
    target_compile_definitions(live_feed PRIVATE USE_ONNX_RUNTIME)
    target_link_libraries(live_feed PRIVATE onnxruntime)
    message(STATUS "ONNX Runtime enabled for live_feed ML signal engine")
endif()

# ─── C++ Strategy Python Bindings (DLL/SO) ────────────────────────────────────
add_library(strategy_bindings SHARED src/strategy_bindings.cpp)
set_property(TARGET strategy_bindings PROPERTY POSITION_INDEPENDENT_CODE ON)
target_include_directories(strategy_bindings PRIVATE src)

# ─── Benchmarks ───────────────────────────────────────────────────────────────
add_executable(order_book_benchmark bench/order_book_benchmark.cpp)
target_include_directories(order_book_benchmark PRIVATE src)
target_sources(order_book_benchmark PRIVATE src/latency_benchmark.hpp)

add_executable(strategy_benchmark bench/strategy_benchmark.cpp)
target_include_directories(strategy_benchmark PRIVATE src)

# ─── Fuzzing (Clang only) ─────────────────────────────────────────────────────
if (CMAKE_CXX_COMPILER_ID MATCHES "Clang")
    add_executable(order_book_fuzzer src/order_book_fuzzer.cpp)
    target_include_directories(order_book_fuzzer PRIVATE src)
    target_compile_options(order_book_fuzzer PRIVATE -fsanitize=fuzzer,address)
    target_link_options(order_book_fuzzer PRIVATE -fsanitize=fuzzer,address)
endif()

# ─── Platform-specific flags for targets ─────────────────────────────────────
foreach(tgt matching_engine live_feed order_book_benchmark strategy_benchmark strategy_bindings)
    if(WIN32)
        target_link_libraries(${tgt} PRIVATE ws2_32)
    endif()
    if(NOT MSVC)
        target_compile_options(${tgt} PRIVATE -mavx2 -msse4.2 -mpopcnt)
        if(CMAKE_SYSTEM_NAME STREQUAL "Linux")
            target_link_libraries(${tgt} PRIVATE pthread rt)
        endif()
    endif()
endforeach()

if(CMAKE_SYSTEM_NAME STREQUAL "Linux" AND NOT MSVC)
    find_library(NUMA_LIB numa)
    if(NUMA_LIB)
        target_link_libraries(matching_engine PRIVATE ${NUMA_LIB})
    else()
        target_compile_definitions(matching_engine PRIVATE NO_NUMA)
    endif()
endif()
