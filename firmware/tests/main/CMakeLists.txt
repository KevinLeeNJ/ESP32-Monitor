set(FIRMWARE_ROOT "${CMAKE_CURRENT_LIST_DIR}/../..")
set(REPO_ROOT "${FIRMWARE_ROOT}/..")
idf_component_register(
    SRCS "test_main.c" "../test_contracts.c"
         "${FIRMWARE_ROOT}/src/display/monitor_pack.c"
         "${FIRMWARE_ROOT}/src/display/monitor_devices_data.c"
    INCLUDE_DIRS "${FIRMWARE_ROOT}/include"
                 "${FIRMWARE_ROOT}/components/firmware"
                 "${REPO_ROOT}/third_party" "${REPO_ROOT}/third_party/lvgl"
    REQUIRES unity espressif__cjson
    WHOLE_ARCHIVE)
