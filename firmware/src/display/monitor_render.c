#include "monitor_render.h"

#include <stddef.h>
#include <string.h>

static void flush_cb(lv_display_t *display, const lv_area_t *area, uint8_t *px_map)
{
    monitor_renderer_t *renderer = (monitor_renderer_t *)lv_display_get_user_data(display);
    if(renderer == NULL) {
        lv_display_flush_ready(display);
        return;
    }

    const uint32_t width = MONITOR_DISPLAY_WIDTH;
    const uint32_t height = MONITOR_DISPLAY_HEIGHT;
    const uint32_t area_width = (uint32_t)(area->x2 - area->x1 + 1);
    const uint32_t area_height = (uint32_t)(area->y2 - area->y1 + 1);
    uint16_t *dst = (uint16_t *)renderer->rgb565;
    const uint16_t *src = (const uint16_t *)px_map;

    for(uint32_t y = 0; y < area_height; ++y) {
        uint32_t dst_y = (uint32_t)area->y1 + y;
        uint32_t dst_offset = dst_y * width + (uint32_t)area->x1;
        memcpy(dst + dst_offset, src + y * area_width, area_width * sizeof(uint16_t));
    }

    if(lv_display_flush_is_last(display)) {
        /* Pack exactly what the selected screen rendered. Host code can
           reconstruct a preview from these bytes to expose panel polarity and
           packing behavior instead of showing a raw LVGL surface. */
        monitor_pack_rgb565((const uint16_t *)renderer->rgb565, width, height,
                            MONITOR_FRAME_NORMAL, renderer->packed);
    }
    lv_display_flush_ready(display);
}

bool monitor_renderer_init(monitor_renderer_t *renderer,
                           monitor_screen_factory_t create_screen)
{
    if(renderer == NULL || create_screen == NULL) return false;
    memset(renderer, 0, sizeof(*renderer));

    lv_init();
    renderer->rgb565 = (uint8_t *)lv_malloc(MONITOR_RGB565_BYTES);
    renderer->packed = (uint8_t *)lv_malloc(MONITOR_FRAME_BYTES);
    if(renderer->rgb565 == NULL || renderer->packed == NULL) {
        monitor_renderer_deinit(renderer);
        return false;
    }
    memset(renderer->rgb565, 0, MONITOR_RGB565_BYTES);
    memset(renderer->packed, 0, MONITOR_FRAME_BYTES);

    renderer->display = lv_display_create(MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT);
    if(renderer->display == NULL) {
        monitor_renderer_deinit(renderer);
        return false;
    }
    lv_display_set_user_data(renderer->display, renderer);
    lv_display_set_color_format(renderer->display, LV_COLOR_FORMAT_RGB565);
    lv_display_set_buffers(renderer->display, renderer->rgb565, NULL, MONITOR_RGB565_BYTES,
                           LV_DISPLAY_RENDER_MODE_FULL);
    lv_display_set_flush_cb(renderer->display, flush_cb);

    renderer->root = create_screen(lv_screen_active());
    if(renderer->root == NULL) {
        monitor_renderer_deinit(renderer);
        return false;
    }
    return true;
}

void monitor_renderer_deinit(monitor_renderer_t *renderer)
{
    if(renderer == NULL) return;
    if(renderer->display != NULL) {
        lv_display_delete(renderer->display);
        renderer->display = NULL;
    }
    if(renderer->rgb565 != NULL) {
        lv_free(renderer->rgb565);
        renderer->rgb565 = NULL;
    }
    if(renderer->packed != NULL) {
        lv_free(renderer->packed);
        renderer->packed = NULL;
    }
    renderer->root = NULL;
}

bool monitor_renderer_render(monitor_renderer_t *renderer)
{
    if(renderer == NULL || renderer->display == NULL) return false;
    lv_obj_invalidate(renderer->root);
    lv_refr_now(renderer->display);
    return true;
}

bool monitor_renderer_rebuild(monitor_renderer_t *renderer,
                              monitor_screen_factory_t create_screen)
{
    if(renderer == NULL || renderer->display == NULL || create_screen == NULL) return false;
    if(renderer->root != NULL) lv_obj_delete(renderer->root);
    renderer->root = create_screen(lv_screen_active());
    return renderer->root != NULL;
}

lv_obj_t *monitor_renderer_root(const monitor_renderer_t *renderer)
{
    return renderer == NULL ? NULL : renderer->root;
}

const uint8_t *monitor_renderer_rgb565(const monitor_renderer_t *renderer)
{
    return renderer == NULL ? NULL : renderer->rgb565;
}

const uint8_t *monitor_renderer_packed(const monitor_renderer_t *renderer)
{
    return renderer == NULL ? NULL : renderer->packed;
}
