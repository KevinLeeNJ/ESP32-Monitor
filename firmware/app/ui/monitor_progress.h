#pragma once

#include <stdint.h>
#include "lvgl.h"

static void monitor_progress_draw(lv_event_t *event)
{
    lv_obj_t *obj = lv_event_get_target(event);
    int filled = (int)(uintptr_t)lv_obj_get_user_data(obj);
    lv_area_t coords;
    lv_obj_get_coords(obj, &coords);
    int width = coords.x2 - coords.x1 - 1;
    lv_draw_rect_dsc_t dsc;
    lv_draw_rect_dsc_init(&dsc);
    dsc.bg_opa = LV_OPA_COVER;
    lv_color_t ink = lv_obj_get_style_border_color(obj, 0);
    lv_color_t paper = lv_obj_get_style_bg_color(obj, 0);
    for(int i = 0; i < 10; ++i) {
        int left = i * width / 10;
        int right = (i + 1) * width / 10;
        if(i < filled) {
            dsc.bg_color = ink;
            lv_area_t area = {coords.x1 + 1 + left, coords.y1 + 1,
                              coords.x1 + right, coords.y2 - 1};
            lv_draw_rect(lv_event_get_layer(event), &dsc, &area);
        }
        if(i < 9) {
            dsc.bg_color = i < filled ? paper : ink;
            lv_area_t separator = {coords.x1 + right, coords.y1 + 1,
                                   coords.x1 + right, coords.y2 - 1};
            lv_draw_rect(lv_event_get_layer(event), &dsc, &separator);
        }
    }
}

static void monitor_progress_set(lv_obj_t *obj, int percent)
{
    if(percent > 100) percent = 100;
    int filled = percent > 0 ? (percent + 9) / 10 : 0;
    if((int)(uintptr_t)lv_obj_get_user_data(obj) == filled) return;
    lv_obj_set_user_data(obj, (void *)(uintptr_t)filled);
    lv_obj_invalidate(obj);
}
