#include "monitor_lvgl.h"

#include <stdlib.h>
#include "esp_check.h"
#include "esp_heap_caps.h"
#include "esp_lcd_panel_interface.h"
#include "esp_lv_adapter.h"
#include "esp_timer.h"

static const char *TAG = "monitor_lvgl";

struct monitor_lvgl {
    /* First member so esp_lcd's panel handle also identifies this backend. */
    esp_lcd_panel_t panel;
    rlcd_port_t *port;
    lv_display_t *display;
    lv_obj_t *root;
    uint8_t *packed;
};

static uint32_t tick_ms(void)
{
    return (uint32_t)(esp_timer_get_time() / 1000);
}

static esp_err_t draw_bitmap(esp_lcd_panel_t *panel, int x1, int y1,
                              int x_end, int y_end, const void *pixels)
{
    monitor_lvgl_t *renderer = (monitor_lvgl_t *)panel;
    /* The adapter's HTILED path already skips the I1 palette. Partial flushes
     * use the active draw buffer's stride, including any LVGL row alignment. */
    const uint32_t stride = lv_draw_buf_width_to_stride((uint32_t)(x_end - x1),
                                                       LV_COLOR_FORMAT_I1);
    monitor_pack_i1_region(pixels, stride, MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT,
                           x1, y1, x_end - 1, y_end - 1, renderer->packed);
    /* Consume the region synchronously; commit the accumulated frame only
     * after refresh_now has finished every flush. The physical SPI ISR belongs
     * to display_port and only completes its DMA semaphore, never this flush. */
    lv_display_flush_ready(renderer->display);
    return ESP_OK;
}

esp_err_t monitor_lvgl_init(monitor_lvgl_t **out, rlcd_port_t *port,
                            monitor_screen_factory_t create_screen)
{
    ESP_RETURN_ON_FALSE(out && port && create_screen, ESP_ERR_INVALID_ARG, TAG, "invalid argument");
    *out = NULL;
    monitor_lvgl_t *renderer = calloc(1, sizeof(*renderer));
    ESP_RETURN_ON_FALSE(renderer, ESP_ERR_NO_MEM, TAG, "renderer allocation");
    renderer->port = port;
    renderer->panel.draw_bitmap = draw_bitmap;
    renderer->packed = heap_caps_calloc(1, MONITOR_FRAME_BYTES, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!renderer->packed) {
        free(renderer);
        return ESP_ERR_NO_MEM;
    }

    esp_lv_adapter_config_t config = ESP_LV_ADAPTER_DEFAULT_CONFIG();
    esp_err_t err = esp_lv_adapter_init(&config);
    if (err != ESP_OK) {
        heap_caps_free(renderer->packed);
        free(renderer);
        return err;
    }
    /* This application advances finite UI timelines explicitly and commits at
     * panel frame boundaries. Use adapter's synchronous refresh API, without
     * starting a second timer-handler task that could flush an unfinished UI
     * transaction or keep tickless idle awake every millisecond. */
    err = esp_lv_adapter_pause(-1);
    if (err != ESP_OK) goto fail;
    lv_tick_set_cb(tick_ms);
    err = esp_lv_adapter_set_default_display_idf_callback_registration_enabled(false);
    if (err != ESP_OK) goto fail;

    esp_lv_adapter_display_config_t display_config = ESP_LV_ADAPTER_DISPLAY_SPI_MONO_DEFAULT_CONFIG(
        &renderer->panel, rlcd_port_panel_io(port), MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT,
        ESP_LV_ADAPTER_ROTATE_0, ESP_LV_ADAPTER_MONO_LAYOUT_HTILED);
    display_config.profile.use_psram = true;
    renderer->display = esp_lv_adapter_register_display(&display_config);
    if (!renderer->display) { err = ESP_ERR_NO_MEM; goto fail; }
    /* MONO defaults to full redraw. HTILED at rotation 0 also accepts partial
     * I1 regions, so retain dirty-area rendering with the adapter-owned buffer. */
    lv_display_set_render_mode(renderer->display, LV_DISPLAY_RENDER_MODE_PARTIAL);
    renderer->root = create_screen(lv_display_get_screen_active(renderer->display));
    if (!renderer->root) { err = ESP_ERR_NO_MEM; goto fail; }
    ESP_LOGI(TAG, "esp_lvgl_adapter: I1 partial rendering, PSRAM buffer, synchronous TE commits");
    *out = renderer;
    return ESP_OK;

fail:
    monitor_lvgl_deinit(renderer);
    return err;
}

void monitor_lvgl_deinit(monitor_lvgl_t *renderer)
{
    if (!renderer) return;
    if (renderer->display) esp_lv_adapter_unregister_display(renderer->display);
    esp_lv_adapter_deinit();
    heap_caps_free(renderer->packed);
    free(renderer);
}

esp_err_t monitor_lvgl_render(monitor_lvgl_t *renderer)
{
    if (!renderer || !renderer->display) return ESP_ERR_INVALID_ARG;
    esp_err_t err = esp_lv_adapter_refresh_now(renderer->display);
    if (err != ESP_OK) return err;
    /* One TE wait per frame, even when pixel rounding leaves a frame unchanged.
     * Otherwise the application's TE-paced animation loop can spin at zero dt. */
    return rlcd_port_draw_diff(renderer->port, renderer->packed, MONITOR_FRAME_BYTES);
}

lv_obj_t *monitor_lvgl_root(const monitor_lvgl_t *renderer)
{
    return renderer ? renderer->root : NULL;
}

void monitor_lvgl_invalidate_all(monitor_lvgl_t *renderer)
{
    if (renderer && renderer->root) lv_obj_invalidate(renderer->root);
}
