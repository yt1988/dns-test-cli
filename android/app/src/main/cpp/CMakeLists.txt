cmake_minimum_required(VERSION 3.22.1)
project(dnstest_jni)

add_library(dnstest_jni SHARED native-lib.cpp)
find_library(log-lib log)

target_link_libraries(dnstest_jni PRIVATE ${log-lib} dl)
