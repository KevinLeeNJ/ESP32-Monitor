# LVGL v9.4 is kept as a pinned submodule. This wrapper lets ESP-IDF's
# component system build it with the same lv_conf.h used by the host runner.
set(LVGL_ROOT_DIR "${CMAKE_CURRENT_LIST_DIR}/../../../third_party/lvgl")
include("${LVGL_ROOT_DIR}/env_support/cmake/esp.cmake")

# IDF's Xtensa port leaves portFORCE_INLINE undefined; atomic.h then emits
# unused static functions. Supply the inline fallback only for this adapter.
set_property(SOURCE "${LVGL_ROOT_DIR}/src/osal/lv_freertos.c" APPEND PROPERTY
    COMPILE_OPTIONS "-include" "${CMAKE_CURRENT_LIST_DIR}/freertos_atomic_compat.h")
