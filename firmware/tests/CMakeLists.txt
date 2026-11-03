cmake_minimum_required(VERSION 3.20)

option(MONITOR_TEST_HOST "Run firmware regressions on the desktop" OFF)
if(MONITOR_TEST_HOST)
    project(monitor_regressions LANGUAGES C CXX)
    include(CTest)
    set(BUILD_TESTING ON CACHE BOOL "" FORCE)
    add_subdirectory("${CMAKE_CURRENT_LIST_DIR}/../../simulator" simulator)
else()
    # Independent Unity application: never starts the production app or radio.
    set(IDF_TARGET esp32s3 CACHE STRING "")
    set(COMPONENTS main)
    set(EXTRA_COMPONENT_DIRS
        "${CMAKE_CURRENT_LIST_DIR}/../components")
    # Reuse the product's PSRAM/flash/LVGL configuration; the overlay only
    # points the partition table back at the firmware project.
    set(SDKCONFIG_DEFAULTS
        "${CMAKE_CURRENT_LIST_DIR}/../sdkconfig.defaults"
        "${CMAKE_CURRENT_LIST_DIR}/sdkconfig.defaults")
    include($ENV{IDF_PATH}/tools/cmake/project.cmake)
    project(monitor_unity_tests)
endif()
