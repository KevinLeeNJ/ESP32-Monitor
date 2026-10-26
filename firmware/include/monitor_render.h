#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "lvgl.h"
#include "monitor_ui.h"
#include "monitor_damage.h"

#ifdef __cplusplus
extern "C" {
#endif

typedef lv_obj_t *(*monitor_screen_factory_t)(lv_obj_t *parent);

/* LVGL I1 includes an 8-byte palette; pixels are row-major, MSB first. */
#define MONITOR_I1_PALETTE_BYTES 8U
/* A full-height partial buffer avoids splitting large animation viewports.
 * I1 needs only 15 KB; LVGL still draws and flushes only invalidated areas. */
#define MONITOR_I1_DRAW_BUFFER_BYTES (MONITOR_FRAME_BYTES + MONITOR_I1_PALETTE_BYTES)

typedef struct {
    lv_display_t *display;
    lv_obj_t *root;
    uint8_t *draw_buffer;
    uint8_t *packed;
    monitor_damage_t dirty;
    bool force_full;
} monitor_renderer_t;

/* The factory must build its UI under parent. This keeps the renderer reusable
   for the current monitor screen and for future application screens. */
bool monitor_renderer_init(monitor_renderer_t *renderer,
                           monitor_screen_factory_t create_screen);
void monitor_renderer_deinit(monitor_renderer_t *renderer);
/* Refresh timer is decoupled. The owner must service lv_timer_handler() on
 * its returned deadlines, then render/commit changes on the same task. */
bool monitor_renderer_render(monitor_renderer_t *renderer);
void monitor_renderer_invalidate_all(monitor_renderer_t *renderer);
bool monitor_renderer_rebuild(monitor_renderer_t *renderer,
                              monitor_screen_factory_t create_screen);
lv_obj_t *monitor_renderer_root(const monitor_renderer_t *renderer);
const uint8_t *monitor_renderer_packed(const monitor_renderer_t *renderer);

#ifdef __cplusplus
}
#endif
