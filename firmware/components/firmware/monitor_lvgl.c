#include "monitor_lvgl.h"

#include <stdlib.h>
#include <string.h>
#include "esp_check.h"
#include "esp_timer.h"

static const char *TAG = "monitor_lvgl";

#if LV_USE_LOG
static void lvgl_log(lv_log_level_t level, const char *message)
{
    if (level == LV_LOG_LEVEL_ERROR) ESP_LOGE(TAG, "%s", message);
    else ESP_LOGW(TAG, "%s", message);
}
#endif

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
    /* lv_init resets the log callback. Configure it before building the UI;
     * the shared renderer also accepts an already initialized LVGL instance. */
    lv_init();
#if LV_USE_LOG
    lv_log_register_print_cb(lvgl_log);
#endif
    lv_tick_set_cb(tick_ms);
    /* The shared renderer owns a full-height I1 partial draw buffer and the
     * accumulated panel frame. All LVGL access stays on the display task. */
    if (!monitor_renderer_init(&renderer->renderer, create_screen)) {
        free(renderer);
        return ESP_ERR_NO_MEM;
    }
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
    lv_timer_handler();
    if (!monitor_renderer_render(&renderer->renderer)) return ESP_FAIL;
    return ESP_OK;
}

uint32_t monitor_lvgl_process_timers(monitor_lvgl_t *renderer)
{
    /* Called only by the display owner. Timer callbacks may invalidate widgets
     * without changing the application snapshot (e.g. async deletion). Pack
     * these changes now; the caller decides when to commit to the panel. */
    uint32_t started = lv_tick_get();
    uint32_t delay = lv_timer_handler();
    monitor_renderer_render(&renderer->renderer);
    if (delay == LV_NO_TIMER_READY) return delay;
    uint32_t elapsed = lv_tick_elaps(started);
    return elapsed < delay ? delay - elapsed : 0;
}

bool monitor_lvgl_frame_pending(const monitor_lvgl_t *renderer)
{
    if (renderer->renderer.force_full) return true;
    for (size_t i = 0; i < sizeof(renderer->renderer.dirty.bits); ++i) {
        if (renderer->renderer.dirty.bits[i]) return true;
    }
    return false;
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
