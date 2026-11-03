set(FIRMWARE_ROOT "${CMAKE_CURRENT_LIST_DIR}/../..")
set(REPO_ROOT "${FIRMWARE_ROOT}/..")
idf_component_register(
    SRCS "test_main.c" "../test_contracts.c"
         "../test_companion_discovery.c"
         "../test_companion_view.c"
         "../test_lan_session.c"
         "../test_serial_session.c"
         "${REPO_ROOT}/simulator/src/sha256.c"
    INCLUDE_DIRS "${FIRMWARE_ROOT}/include"
                 "${FIRMWARE_ROOT}/components/firmware"
                 "${REPO_ROOT}/third_party" "${REPO_ROOT}/third_party/lvgl"
                 "${REPO_ROOT}/simulator/src"
    REQUIRES unity espressif__cjson monitor_core
    WHOLE_ARCHIVE)
