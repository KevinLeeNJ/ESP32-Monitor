#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "lvgl.h"
#include "monitor_ui.h"

#ifdef __cplusplus
extern "C" {
#endif

typedef lv_obj_t *(*monitor_screen_factory_t)(lv_obj_t *parent);

/* LVGL I1 includes an 8-byte palette; pixels are row-major, MSB first. */
#define MONITOR_I1_PALETTE_BYTES 8U
#define MONITOR_I1_DRAW_BUFFER_BYTES ((MONITOR_DISPLAY_WIDTH / 8U) * 40U + MONITOR_I1_PALETTE_BYTES)

typedef struct {
    lv_display_t *display;
    lv_obj_t *root;
    uint8_t *draw_buffer;
    uint8_t *packed;
} monitor_renderer_t;

/* The factory must build its UI under parent. This keeps the renderer reusable
   for the current monitor screen and for future application screens. */
bool monitor_renderer_init(monitor_renderer_t *renderer,
                           monitor_screen_factory_t create_screen);
void monitor_renderer_deinit(monitor_renderer_t *renderer);
bool monitor_renderer_render(monitor_renderer_t *renderer);
void monitor_renderer_invalidate_all(monitor_renderer_t *renderer);
bool monitor_renderer_rebuild(monitor_renderer_t *renderer,
                              monitor_screen_factory_t create_screen);
lv_obj_t *monitor_renderer_root(const monitor_renderer_t *renderer);
const uint8_t *monitor_renderer_packed(const monitor_renderer_t *renderer);

#ifdef __cplusplus
}
#endif
