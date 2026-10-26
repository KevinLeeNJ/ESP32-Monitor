#pragma once

#include "display_port.h"
#include "monitor_render.h"

typedef struct monitor_lvgl monitor_lvgl_t;

/* Firmware backend: all UI calls and frame commits belong to the main display
 * task. The desktop renderer continues to use the same UI and packing code. */
esp_err_t monitor_lvgl_init(monitor_lvgl_t **out, rlcd_port_t *port,
                            monitor_screen_factory_t create_screen);
void monitor_lvgl_deinit(monitor_lvgl_t *renderer);
/* Both calls must remain on the sole display owner task. */
esp_err_t monitor_lvgl_render(monitor_lvgl_t *renderer);
esp_err_t monitor_lvgl_present(monitor_lvgl_t *renderer);
lv_obj_t *monitor_lvgl_root(const monitor_lvgl_t *renderer);
void monitor_lvgl_invalidate_all(monitor_lvgl_t *renderer);
/* Process ordinary LVGL timers and pack invalidations without panel I/O.
 * Returns milliseconds until the next timer, or LV_NO_TIMER_READY. */
uint32_t monitor_lvgl_process_timers(monitor_lvgl_t *renderer);
bool monitor_lvgl_frame_pending(const monitor_lvgl_t *renderer);
