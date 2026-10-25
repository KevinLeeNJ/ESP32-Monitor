#include "monitor_lvgl.h"

#include <stdlib.h>
#include <string.h>
#include "esp_check.h"
#include "esp_timer.h"

static const char *TAG = "monitor_lvgl";

struct monitor_lvgl {
    rlcd_port_t *port;
    monitor_renderer_t renderer;
};

static uint32_t tick_ms(void)
{
    return (uint32_t)(esp_timer_get_time() / 1000);
}

esp_err_t monitor_lvgl_init(monitor_lvgl_t **out, rlcd_port_t *port,
                            monitor_screen_factory_t create_screen)
{
    ESP_RETURN_ON_FALSE(out && port && create_screen, ESP_ERR_INVALID_ARG, TAG, "invalid argument");
    *out = NULL;
    monitor_lvgl_t *renderer = calloc(1, sizeof(*renderer));
    ESP_RETURN_ON_FALSE(renderer, ESP_ERR_NO_MEM, TAG, "renderer allocation");
    renderer->port = port;
    /* The shared renderer owns a full-height I1 partial draw buffer and the
     * accumulated panel frame. All LVGL access stays on the display task. */
    if (!monitor_renderer_init(&renderer->renderer, create_screen)) {
        free(renderer);
        return ESP_ERR_NO_MEM;
    }
    lv_tick_set_cb(tick_ms);
    ESP_LOGI(TAG, "native LVGL: I1 partial rendering, PSRAM buffer, TE-paced diff commits");
    *out = renderer;
    return ESP_OK;
}

void monitor_lvgl_deinit(monitor_lvgl_t *renderer)
{
    if (!renderer) return;
    monitor_renderer_deinit(&renderer->renderer);
    free(renderer);
}

esp_err_t monitor_lvgl_render(monitor_lvgl_t *renderer)
{
    if (!renderer || !renderer->renderer.display) return ESP_ERR_INVALID_ARG;
    if (!monitor_renderer_render(&renderer->renderer)) return ESP_FAIL;
    return ESP_OK;
}

esp_err_t monitor_lvgl_present(monitor_lvgl_t *renderer)
{
    if (!renderer || !renderer->renderer.display) return ESP_ERR_INVALID_ARG;
    /* Pack every dirty LVGL region before the single physical frame commit.
     * Dirty tiles survive render calls until a successful commit. display_port
     * retains TE pacing even when the packed frame is unchanged. */
    esp_err_t err = rlcd_port_draw_dirty(renderer->port,
        monitor_renderer_packed(&renderer->renderer), MONITOR_FRAME_BYTES,
        &renderer->renderer.dirty, renderer->renderer.force_full);
    if (err == ESP_OK) {
        memset(&renderer->renderer.dirty, 0, sizeof(renderer->renderer.dirty));
        renderer->renderer.force_full = false;
    }
    return err;
}

lv_obj_t *monitor_lvgl_root(const monitor_lvgl_t *renderer)
{
    return renderer ? monitor_renderer_root(&renderer->renderer) : NULL;
}

void monitor_lvgl_invalidate_all(monitor_lvgl_t *renderer)
{
    if (renderer) monitor_renderer_invalidate_all(&renderer->renderer);
}
