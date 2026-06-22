cmake_minimum_required(VERSION 3.25)
project(robin-ai-engine VERSION 0.1.0 LANGUAGES CXX)
set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

add_executable(onnx_inference onnx_inference.cpp)
target_compile_options(onnx_inference PRIVATE -O3 -march=native -ffast-math)
