cmake_minimum_required(VERSION 3.25)
project(robin-pricing VERSION 1.0.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
if(NOT MSVC)
    set(CMAKE_INTERPROCEDURAL_OPTIMIZATION TRUE)
    set(CMAKE_CXX_FLAGS_RELEASE "-O3 -march=native -mtune=native -flto=auto -fno-exceptions -fno-rtti -fno-stack-protector -fomit-frame-pointer -ffast-math -mavx2 -mfma")
else()
    set(CMAKE_CXX_FLAGS_RELEASE "/O2 /arch:AVX2 /fp:fast /DNDEBUG")
endif()

add_executable(monte_carlo
    src/MonteCarlo.cpp
)
