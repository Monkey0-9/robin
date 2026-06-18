cmake_minimum_required(VERSION 3.25)
project(robin-ingestion VERSION 1.0.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_INTERPROCEDURAL_OPTIMIZATION TRUE)
set(CMAKE_CXX_FLAGS_RELEASE "-O3 -march=native -mtune=native -flto=auto -fno-exceptions -fno-rtti -fno-stack-protector -fomit-frame-pointer -mno-red-zone -ffast-math -frename-registers -falign-functions=64")

add_executable(cpp_ingestion
    cpp_ingestion.cpp
)
