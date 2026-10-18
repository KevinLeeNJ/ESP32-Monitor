cmake_minimum_required(VERSION 3.16)
project(esp32_monitor_simulator LANGUAGES C CXX)

set(CMAKE_C_STANDARD 11)
set(CMAKE_C_STANDARD_REQUIRED ON)
set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

set(REPO_ROOT "${CMAKE_CURRENT_LIST_DIR}/..")
set(FIRMWARE_ROOT "${REPO_ROOT}/firmware")
set(LVGL_ROOT "${REPO_ROOT}/third_party/lvgl")
set(LV_BUILD_CONF_PATH "${REPO_ROOT}/third_party/lv_conf.h" CACHE PATH
    "Shared LVGL configuration")
set(CONFIG_LV_BUILD_DEMOS OFF CACHE BOOL "" FORCE)
set(CONFIG_LV_BUILD_EXAMPLES OFF CACHE BOOL "" FORCE)

add_subdirectory("${LVGL_ROOT}" "${CMAKE_BINARY_DIR}/lvgl")

add_library(monitor_sim_core STATIC
    "${FIRMWARE_ROOT}/src/display/monitor_pack.c"
    "${FIRMWARE_ROOT}/src/display/monitor_render.c"
    "${FIRMWARE_ROOT}/app/ui/monitor_ui.c"
    "${FIRMWARE_ROOT}/app/ui/weather_assets.c"
    "${FIRMWARE_ROOT}/app/ui/weather_zh_font.c"
    src/png_writer.c
    src/scene_registry.c
    src/sha256.c
)
target_include_directories(monitor_sim_core PUBLIC
    "${FIRMWARE_ROOT}/include"
    "${FIRMWARE_ROOT}/app/ui"
    "${REPO_ROOT}/third_party"
    "${CMAKE_CURRENT_LIST_DIR}/src"
)
target_link_libraries(monitor_sim_core PUBLIC lvgl)

add_executable(monitor_simulator src/main.c)
target_link_libraries(monitor_simulator PRIVATE monitor_sim_core)

include(CTest)
if(BUILD_TESTING)
    add_executable(monitor_unit_tests tests/unit_tests.c)
    target_link_libraries(monitor_unit_tests PRIVATE monitor_sim_core)
    add_test(NAME monitor_unit_tests COMMAND monitor_unit_tests)

    add_test(
        NAME monitor_simulator_e2e
        COMMAND "${CMAKE_COMMAND}"
            "-DSIMULATOR_EXE=$<TARGET_FILE:monitor_simulator>"
            "-DOUTPUT_DIR=${CMAKE_BINARY_DIR}/e2e-output"
            -P "${CMAKE_CURRENT_LIST_DIR}/tests/e2e_check.cmake"
    )
endif()
