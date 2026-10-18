#pragma once

#include <stddef.h>

#include "lvgl.h"
#include "monitor_ui.h"

#ifdef __cplusplus
extern "C" {
#endif

/*
 * A simulator scene is a named LVGL screen factory. The simulator does not
 * assume the monitor UI: add future screens here and select them with
 * --scene NAME.
 */
typedef lv_obj_t *(*simulator_scene_create_t)(lv_obj_t *parent,
                                               monitor_frame_variant_t variant);

typedef struct {
    const char *name;
    const char *description;
    simulator_scene_create_t create;
} simulator_scene_t;

const simulator_scene_t *simulator_scene_default(void);
const simulator_scene_t *simulator_scene_find(const char *name);
size_t simulator_scene_count(void);
const simulator_scene_t *simulator_scene_at(size_t index);

#ifdef __cplusplus
}
#endif
