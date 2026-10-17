cmake_minimum_required(VERSION 3.20)
project(demo LANGUAGES C CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

option(BUILD_TESTS "Build the tests" OFF)

add_executable(demo main.cpp)
target_link_libraries(demo PRIVATE fmt::fmt)

if(BUILD_TESTS)
    enable_testing()
    add_test(NAME demo_test COMMAND demo)
endif()
