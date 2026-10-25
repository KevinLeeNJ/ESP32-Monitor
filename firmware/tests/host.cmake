add_executable(monitor_unit_tests "${CMAKE_CURRENT_LIST_DIR}/unit_tests.c")
target_include_directories(monitor_unit_tests PRIVATE "${FIRMWARE_ROOT}/components/firmware")
target_link_libraries(monitor_unit_tests PRIVATE monitor_sim_core)
add_test(NAME monitor_unit_tests COMMAND monitor_unit_tests)
add_executable(monitor_animation_tests "${CMAKE_CURRENT_LIST_DIR}/animation_tests.c")
target_link_libraries(monitor_animation_tests PRIVATE monitor_sim_core)
add_test(NAME monitor_animation_tests COMMAND monitor_animation_tests)
find_package(Python3 COMPONENTS Interpreter REQUIRED)
add_test(NAME monitor_pm_profile_tests
    COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/pm_profile_test.py")
add_test(NAME monitor_clock_sync_tests
    COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/clock_sync_test.py")
add_test(NAME monitor_station_scan_tests
    COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/station_scan_test.py")
add_test(NAME monitor_wifi_buttons_tests
    COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/wifi_buttons_test.py")
add_test(NAME monitor_wifi_session_tests
    COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/wifi_session_test.py")
add_test(NAME monitor_firmware_animation_tests
    COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/firmware_animation_test.py"
            "${CMAKE_C_COMPILER}")
add_test(NAME monitor_boot_power_tests
    COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/boot_power_test.py"
            "${CMAKE_C_COMPILER}")
add_test(NAME monitor_lvgl_adapter_tests
    COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/lvgl_adapter_test.py"
            "${CMAKE_C_COMPILER}")
add_test(NAME monitor_devices_transport_tests
    COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/devices_transport_test.py"
            "${CMAKE_C_COMPILER}")
add_test(NAME monitor_firmware_watchdog_tests
    COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/firmware_watchdog_test.py")
find_program(NODE_EXECUTABLE node)
if(NOT NODE_EXECUTABLE)
    message(FATAL_ERROR "Node.js is required for portal behavior regressions")
endif()
add_test(NAME monitor_config_page_tests
    COMMAND "${NODE_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/config_page_test.cjs")

add_test(
    NAME monitor_simulator_e2e
    COMMAND "${CMAKE_COMMAND}"
        "-DSIMULATOR_EXE=$<TARGET_FILE:monitor_simulator>"
        "-DOUTPUT_DIR=${CMAKE_BINARY_DIR}/e2e-output"
        -P "${CMAKE_CURRENT_LIST_DIR}/e2e_check.cmake"
)

# Use the real Unity framework already pinned with LVGL. Only Unity's own
# translation units receive LV_BUILD_TEST; the production LVGL config is intact.
add_library(monitor_test_unity STATIC "${LVGL_ROOT}/tests/unity/unity.c")
target_include_directories(monitor_test_unity PUBLIC "${LVGL_ROOT}/tests/unity")
target_compile_definitions(monitor_test_unity PUBLIC LV_BUILD_TEST=1)
add_executable(monitor_contract_tests "${CMAKE_CURRENT_LIST_DIR}/test_contracts.c")
target_include_directories(monitor_contract_tests PRIVATE "${FIRMWARE_ROOT}/components/firmware")
target_link_libraries(monitor_contract_tests PRIVATE monitor_sim_core monitor_test_unity)
add_test(NAME monitor_contract_tests COMMAND monitor_contract_tests)
add_test(NAME monitor_api_server_tests
    COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/test_api_server.py")

# Bound hung fakes, deadlocks and subprocesses in both entry points.
get_property(monitor_tests DIRECTORY PROPERTY TESTS)
set_tests_properties(${monitor_tests} PROPERTIES TIMEOUT 120)
set_tests_properties(monitor_contract_tests PROPERTIES LABELS "unit;contracts")

add_executable(monitor_rtc_tests "${CMAKE_CURRENT_LIST_DIR}/test_rtc.c")
target_include_directories(monitor_rtc_tests PRIVATE "${CMAKE_CURRENT_LIST_DIR}/fakes")
target_link_libraries(monitor_rtc_tests PRIVATE monitor_test_unity)
add_test(NAME monitor_rtc_tests COMMAND monitor_rtc_tests)
set_tests_properties(monitor_rtc_tests PROPERTIES TIMEOUT 30 LABELS "unit;rtc")

add_test(NAME monitor_sensors_tests
    COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/sensors_test.py" "${CMAKE_C_COMPILER}")
set_tests_properties(monitor_sensors_tests PROPERTIES TIMEOUT 30 LABELS "behavior;sensors")

add_test(NAME monitor_display_diff_tests
    COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/display_diff_test.py" "${CMAKE_C_COMPILER}")
set_tests_properties(monitor_display_diff_tests PROPERTIES TIMEOUT 30 LABELS "behavior;display")

foreach(target IN ITEMS monitor_contract_tests monitor_rtc_tests)
    target_compile_options(${target} PRIVATE -Wall -Wextra -Werror)
endforeach()

add_test(NAME monitor_wifi_retry_tests
    COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/wifi_retry_test.py" "${CMAKE_CXX_COMPILER}")
add_test(NAME monitor_devices_lifecycle_tests
    COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/devices_lifecycle_test.py" "${CMAKE_C_COMPILER}" "${CJSON_SOURCE_DIR}")
add_test(NAME monitor_weather_behavior_tests
    COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/weather_behavior_test.py" "${CMAKE_C_COMPILER}" "${CJSON_SOURCE_DIR}")
# This exercises the HTTP wrapper and uses host zlib for the ROM codec boundary.
find_package(ZLIB REQUIRED)
add_test(NAME monitor_weather_transport_tests
    COMMAND "${Python3_EXECUTABLE}" "${CMAKE_CURRENT_LIST_DIR}/weather_transport_test.py" "${CMAKE_C_COMPILER}")
set_tests_properties(monitor_wifi_retry_tests monitor_devices_lifecycle_tests
    monitor_weather_behavior_tests monitor_weather_transport_tests
    PROPERTIES TIMEOUT 90 LABELS "behavior;network")

add_executable(monitor_damage_tests "${CMAKE_CURRENT_LIST_DIR}/damage_tests.c")
target_link_libraries(monitor_damage_tests PRIVATE monitor_sim_core)
target_compile_options(monitor_damage_tests PRIVATE -Wall -Wextra -Werror -UNDEBUG)
add_test(NAME monitor_damage_tests COMMAND monitor_damage_tests)
set_tests_properties(monitor_damage_tests PROPERTIES TIMEOUT 30 LABELS "behavior;display")
