cmake_minimum_required(VERSION 3.20)
project(hardware-fpga)

add_executable(uio_driver uio_driver.cpp)
target_compile_features(uio_driver PUBLIC cxx_std_20)
