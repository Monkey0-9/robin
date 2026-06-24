cmake_minimum_required(VERSION 3.25)
project(robin-execution-core VERSION 1.0.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

if(MSVC)
    set(CMAKE_CXX_FLAGS_RELEASE "/O2 /arch:AVX2 /DNDEBUG")
    set(CMAKE_EXE_LINKER_FLAGS_RELEASE "")
elseif(MINGW)
    set(CMAKE_CXX_FLAGS_RELEASE "-O3 -march=native -fno-exceptions -fno-rtti -fomit-frame-pointer")
    set(CMAKE_EXE_LINKER_FLAGS_RELEASE "")
else()
    set(CMAKE_CXX_FLAGS_RELEASE "-O3 -march=native -mtune=native -flto=auto -fno-exceptions -fno-rtti -fomit-frame-pointer -frename-registers -falign-functions=64")
    set(CMAKE_EXE_LINKER_FLAGS_RELEASE "-flto=auto -Wl,--gc-sections")
endif()

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
)
target_include_directories(matching_engine PRIVATE src)

if(WIN32)
    target_link_libraries(matching_engine PRIVATE ws2_32)
endif()
if(NOT MSVC)
    target_compile_options(matching_engine PRIVATE -mavx2 -mrdrnd -msse4.2 -mpopcnt)
    if(CMAKE_SYSTEM_NAME STREQUAL "Linux")
        target_link_libraries(matching_engine PRIVATE numa)
    endif()
endif()

add_executable(order_book_benchmark bench/order_book_benchmark.cpp)
target_include_directories(order_book_benchmark PRIVATE src)

if(NOT MSVC)
    target_compile_options(order_book_benchmark PRIVATE -mavx2)
endif()
target_sources(order_book_benchmark PRIVATE src/latency_benchmark.hpp)
