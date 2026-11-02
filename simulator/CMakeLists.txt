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
    "${FIRMWARE_ROOT}/third_party/nanopb/pb_common.c"
    "${FIRMWARE_ROOT}/third_party/nanopb/pb_decode.c"
    "${FIRMWARE_ROOT}/third_party/nanopb/pb_encode.c"
    "${FIRMWARE_ROOT}/src/companion/companion.pb.c"
    "${FIRMWARE_ROOT}/src/companion/monitor_discovery.c"
    "${FIRMWARE_ROOT}/src/companion/monitor_discovery_auth.c"
    "${FIRMWARE_ROOT}/src/companion/monitor_pairing_wire.c"
    "${FIRMWARE_ROOT}/src/companion/monitor_pairing_code.c"
    "${FIRMWARE_ROOT}/src/companion/monitor_pairing_session.c"
    "${FIRMWARE_ROOT}/src/companion/monitor_lan_session.c"
    "${FIRMWARE_ROOT}/src/companion/monitor_serial_proto.c"
    "${FIRMWARE_ROOT}/src/companion/monitor_mqtt_wire.c"
    "${FIRMWARE_ROOT}/src/companion/monitor_proto_strict.c"
    "${FIRMWARE_ROOT}/src/companion/monitor_companion_view.c"
    "${FIRMWARE_ROOT}/src/display/monitor_pack.c"
    "${FIRMWARE_ROOT}/src/display/monitor_damage.c"
    "${FIRMWARE_ROOT}/src/display/monitor_devices_data.c"
    "${CJSON_SOURCE_DIR}/cJSON.c"
    "${FIRMWARE_ROOT}/src/display/monitor_render.c"
    "${FIRMWARE_ROOT}/app/ui/monitor_ui.c"
    "${FIRMWARE_ROOT}/app/ui/monitor_activity.c"
    "${FIRMWARE_ROOT}/app/ui/monitor_music_font.c"
    "${FIRMWARE_ROOT}/app/ui/monitor_pairing_ui.c"
    "${FIRMWARE_ROOT}/app/ui/monitor_pages.c"
    "${FIRMWARE_ROOT}/app/ui/activity_assets.c"
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
    "${FIRMWARE_ROOT}/third_party/nanopb"
    "${FIRMWARE_ROOT}/src/companion"
    "${CJSON_SOURCE_DIR}"
    "${FIRMWARE_ROOT}/include"
    "${FIRMWARE_ROOT}/app/ui"
    "${REPO_ROOT}/third_party"
    "${CMAKE_CURRENT_LIST_DIR}/src"
)
target_link_libraries(monitor_sim_core PUBLIC lvgl)
target_compile_definitions(monitor_sim_core PRIVATE MONITOR_MUSIC_FONT_PATH="${FIRMWARE_ROOT}/assets/fonts/music-cjk.bin")

add_executable(monitor_simulator src/main.c)
target_link_libraries(monitor_simulator PRIVATE monitor_sim_core)

include(CTest)
if(BUILD_TESTING)
    include("${FIRMWARE_ROOT}/tests/host.cmake")
endif()
