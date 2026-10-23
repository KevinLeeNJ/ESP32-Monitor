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

    /* I1 reserves two ARGB8888 palette entries before its MSB-first rows.
       Partial buffers use the stride of this flush area, not the panel width. */
    const uint32_t stride = lv_draw_buf_width_to_stride(
        (uint32_t)lv_area_get_width(area), LV_COLOR_FORMAT_I1);
    monitor_pack_i1_region(px_map + MONITOR_I1_PALETTE_BYTES, stride,
                           MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT,
                           area->x1, area->y1, area->x2, area->y2, renderer->packed);
    lv_display_flush_ready(display);
}

bool monitor_renderer_init(monitor_renderer_t *renderer,
                           monitor_screen_factory_t create_screen)
{
    if(renderer == NULL || create_screen == NULL) return false;
    memset(renderer, 0, sizeof(*renderer));

    lv_init();
    renderer->draw_buffer = (uint8_t *)lv_malloc(MONITOR_I1_DRAW_BUFFER_BYTES);
    renderer->packed = (uint8_t *)lv_malloc(MONITOR_FRAME_BYTES);
    if(renderer->draw_buffer == NULL || renderer->packed == NULL) {
        monitor_renderer_deinit(renderer);
        return false;
    }
    memset(renderer->packed, 0, MONITOR_FRAME_BYTES);

    renderer->display = lv_display_create(MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT);
    if(renderer->display == NULL) {
        monitor_renderer_deinit(renderer);
        return false;
    }
    lv_display_set_user_data(renderer->display, renderer);
    lv_display_set_color_format(renderer->display, LV_COLOR_FORMAT_I1);
    lv_display_set_buffers(renderer->display, renderer->draw_buffer, NULL,
                           MONITOR_I1_DRAW_BUFFER_BYTES, LV_DISPLAY_RENDER_MODE_PARTIAL);
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
    if(renderer->draw_buffer != NULL) {
        lv_free(renderer->draw_buffer);
        renderer->draw_buffer = NULL;
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
    lv_refr_now(renderer->display);
    return true;
}

void monitor_renderer_invalidate_all(monitor_renderer_t *renderer)
{
    if (renderer != NULL && renderer->root != NULL) lv_obj_invalidate(renderer->root);
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

const uint8_t *monitor_renderer_packed(const monitor_renderer_t *renderer)
{
    return renderer == NULL ? NULL : renderer->packed;
}
