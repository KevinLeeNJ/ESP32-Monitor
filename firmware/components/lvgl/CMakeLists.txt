# LVGL v9.4 is kept as a pinned submodule. This wrapper lets ESP-IDF's
# component system build it with the same lv_conf.h used by the host runner.
set(LVGL_ROOT_DIR "${CMAKE_CURRENT_LIST_DIR}/../../../third_party/lvgl")
include("${LVGL_ROOT_DIR}/env_support/cmake/esp.cmake")
if(NOT CMAKE_BUILD_EARLY_EXPANSION)
    target_compile_options(${COMPONENT_LIB} PRIVATE -O2)
endif()
