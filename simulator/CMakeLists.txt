cmake_minimum_required(VERSION 3.16)
project(esp32_monitor_simulator LANGUAGES C CXX)

set(CMAKE_C_STANDARD 11)
set(CMAKE_C_STANDARD_REQUIRED ON)
set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# Emit build/compile_commands.json so clangd and IDE tooling can index the
# simulator without a second build system. tools/gen_compile_commands.py merges
# it with the ESP-IDF database into the repository-root compile_commands.json.
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

set(REPO_ROOT "${CMAKE_CURRENT_LIST_DIR}/..")
set(FIRMWARE_ROOT "${REPO_ROOT}/firmware")
set(LVGL_ROOT "${REPO_ROOT}/third_party/lvgl")
set(LV_BUILD_CONF_PATH "${REPO_ROOT}/third_party/lv_conf.h" CACHE PATH
    "Shared LVGL configuration")
set(CONFIG_LV_BUILD_DEMOS OFF CACHE BOOL "" FORCE)
set(CONFIG_LV_BUILD_EXAMPLES OFF CACHE BOOL "" FORCE)

add_subdirectory("${LVGL_ROOT}" "${CMAKE_BINARY_DIR}/lvgl")

# Reuse the firmware's pinned cJSON when available. A desktop-only checkout
# fetches that same upstream version without requiring the ESP-IDF toolchain.
set(CJSON_SOURCE_DIR "${FIRMWARE_ROOT}/managed_components/espressif__cjson/cJSON")
if(NOT EXISTS "${CJSON_SOURCE_DIR}/cJSON.c")
    include(FetchContent)
    FetchContent_Declare(monitor_cjson
        GIT_REPOSITORY https://github.com/DaveGamble/cJSON.git
        GIT_TAG v1.7.19
        GIT_SHALLOW TRUE
        SOURCE_SUBDIR unused
    )
    FetchContent_MakeAvailable(monitor_cjson)
    set(CJSON_SOURCE_DIR "${monitor_cjson_SOURCE_DIR}")
endif()

add_library(monitor_sim_core STATIC
    "${FIRMWARE_ROOT}/src/display/monitor_pack.c"
    "${FIRMWARE_ROOT}/src/display/monitor_devices_data.c"
    "${CJSON_SOURCE_DIR}/cJSON.c"
    "${FIRMWARE_ROOT}/src/display/monitor_render.c"
    "${FIRMWARE_ROOT}/app/ui/monitor_ui.c"
    "${FIRMWARE_ROOT}/app/ui/weather_assets.c"
    "${FIRMWARE_ROOT}/app/ui/fonts/monitor_misans_8.c"
    "${FIRMWARE_ROOT}/app/ui/fonts/monitor_misans_12.c"
    "${FIRMWARE_ROOT}/app/ui/fonts/monitor_misans_14.c"
    "${FIRMWARE_ROOT}/app/ui/fonts/monitor_misans_20.c"
    "${FIRMWARE_ROOT}/app/ui/fonts/monitor_misans_30.c"
    "${FIRMWARE_ROOT}/app/ui/fonts/monitor_ndot57_8.c"
    "${FIRMWARE_ROOT}/app/ui/fonts/monitor_ndot57_12.c"
    "${FIRMWARE_ROOT}/app/ui/fonts/monitor_ndot57_14.c"
    "${FIRMWARE_ROOT}/app/ui/fonts/monitor_ndot57_20.c"
    "${FIRMWARE_ROOT}/app/ui/fonts/monitor_ndot57_30.c"
    src/png_writer.c
    src/scene_registry.c
    src/sha256.c
)
target_include_directories(monitor_sim_core PUBLIC
    "${CJSON_SOURCE_DIR}"
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
    target_include_directories(monitor_unit_tests PRIVATE "${FIRMWARE_ROOT}/components/firmware")
    target_link_libraries(monitor_unit_tests PRIVATE monitor_sim_core)
    add_test(NAME monitor_unit_tests COMMAND monitor_unit_tests)
    add_executable(monitor_animation_tests tests/animation_tests.c)
    target_link_libraries(monitor_animation_tests PRIVATE monitor_sim_core)
    add_test(NAME monitor_animation_tests COMMAND monitor_animation_tests)
    find_package(Python3 COMPONENTS Interpreter REQUIRED)
    add_test(NAME monitor_firmware_animation_tests
        COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/tests/firmware_animation_test.py"
                "${CMAKE_C_COMPILER}")
    add_test(NAME monitor_lvgl_adapter_tests
        COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/tests/lvgl_adapter_test.py"
                "${CMAKE_C_COMPILER}")
    add_test(NAME monitor_devices_transport_tests
        COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/tests/devices_transport_test.py"
                "${CMAKE_C_COMPILER}")
    add_test(NAME monitor_firmware_watchdog_tests
        COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/tests/firmware_watchdog_test.py")
    find_program(NODE_EXECUTABLE node)
    if(NODE_EXECUTABLE)
        add_test(NAME monitor_config_page_tests
            COMMAND "${NODE_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/tests/config_page_test.cjs")
    endif()

    add_test(
        NAME monitor_simulator_e2e
        COMMAND "${CMAKE_COMMAND}"
            "-DSIMULATOR_EXE=$<TARGET_FILE:monitor_simulator>"
            "-DOUTPUT_DIR=${CMAKE_BINARY_DIR}/e2e-output"
            -P "${CMAKE_CURRENT_LIST_DIR}/tests/e2e_check.cmake"
    )
endif()
