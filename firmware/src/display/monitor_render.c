#include "monitor_render.h"

#include <stddef.h>
#include <string.h>

#ifdef ESP_PLATFORM
#include "esp_heap_caps.h"

static void *renderer_buffer_alloc(size_t bytes)
{
    /* These CPU-only I1 buffers never go to SPI DMA. Leave internal RAM for
     * the DMA staging buffer, network stacks and concurrent TLS requests. */
    void *buffer = heap_caps_malloc(bytes, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if(buffer == NULL) buffer = heap_caps_malloc(bytes, MALLOC_CAP_8BIT);
    return buffer;
}
#define renderer_buffer_free heap_caps_free
#else
#define renderer_buffer_alloc lv_malloc
#define renderer_buffer_free lv_free
#endif

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
    monitor_damage_mark(&renderer->dirty, area->x1, area->y1, area->x2, area->y2);
    lv_display_flush_ready(display);
}

bool monitor_renderer_init(monitor_renderer_t *renderer,
                           monitor_screen_factory_t create_screen)
{
    if(renderer == NULL || create_screen == NULL) return false;
    memset(renderer, 0, sizeof(*renderer));

    renderer->force_full = true;
    if (!lv_is_initialized()) lv_init();
    renderer->draw_buffer = (uint8_t *)renderer_buffer_alloc(MONITOR_I1_DRAW_BUFFER_BYTES);
    renderer->packed = (uint8_t *)renderer_buffer_alloc(MONITOR_FRAME_BYTES);
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
    /* The owner schedules rendering after UI/timer updates, independently of
     * LVGL timers. This avoids a periodic refresh wakeup on a static screen. */
    lv_display_delete_refr_timer(renderer->display);

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
        renderer_buffer_free(renderer->draw_buffer);
        renderer->draw_buffer = NULL;
    }
    if(renderer->packed != NULL) {
        renderer_buffer_free(renderer->packed);
        renderer->packed = NULL;
    }
    renderer->root = NULL;
}

bool monitor_renderer_render(monitor_renderer_t *renderer)
{
    if(renderer == NULL || renderer->display == NULL) return false;
    lv_display_set_default(renderer->display);
    lv_display_refr_timer(NULL);
    return true;
}

void monitor_renderer_invalidate_all(monitor_renderer_t *renderer)
{
    if (renderer != NULL && renderer->root != NULL) {
        renderer->force_full = true;
        lv_obj_invalidate(renderer->root);
    }
}

bool monitor_renderer_rebuild(monitor_renderer_t *renderer,
                              monitor_screen_factory_t create_screen)
{
    if(renderer == NULL || renderer->display == NULL || create_screen == NULL) return false;
    if(renderer->root != NULL) lv_obj_delete(renderer->root);
    renderer->root = create_screen(lv_screen_active());
    renderer->force_full = true;
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
