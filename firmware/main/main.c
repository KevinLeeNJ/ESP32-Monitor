#include "esp_log.h"
#include "esp_check.h"
#include "esp_pm.h"
#include "esp_task_wdt.h"
#include "nvs_flash.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "driver/gpio.h"

#include "display_port.h"
#include "monitor_lvgl.h"
#include "monitor_ui.h"
#include "monitor_wifi_manager.h"
#include "monitor_clock.h"
#include "monitor_sensors.h"
#include "monitor_weather.h"
#include "monitor_devices.h"
#include "monitor_display_refresh.h"
#include "monitor_pm_profile.h"

#include <string.h>
#include <sys/time.h>

#define TAG "monitor"
#define BOOT_UI_HOLD_MS 2000U
#define BOOT_ANIMATION_TIMEOUT_MS 120000U
/* Above the data producers (3-4), for every animation and mode transition. */
#define ANIMATION_TASK_PRIORITY 5U
#define MINUTE_PREPARE_MS (RLCD_HPM_PREPARE_MS + 80U)

static const rlcd_pins_t PINS = {
    .mosi = 12,
    .sclk = 11,
    .dc = 5,
    .cs = 40,
    .rst = 41,
    .te = 6,
};

static monitor_ui_demo_data_t g_demo;

static void feed_task_watchdog(void)
{
    ESP_ERROR_CHECK(esp_task_wdt_reset());
}

typedef struct {
    int battery_percent;
    int temperature_tenths_c;
    int humidity_percent;
    int aqi;
    monitor_weather_condition_t weather_condition;
    monitor_wifi_state_t wifi_state;
    monitor_devices_snapshot_t devices;
    char time_text[8];
    char date_text[16];
    bool profiling;
} display_snapshot_t;


static void refresh_monitor_data(char *time_text, size_t time_size,
                                char *date_text, size_t date_size)
{
    monitor_weather_data_t weather;
    monitor_weather_get(&weather);
    if(weather.weather_valid) g_demo.weather_condition = weather.condition;
    if(weather.aqi_valid) g_demo.aqi = weather.aqi;
    monitor_sensor_data_t sensors;
    monitor_sensors_get(&sensors);
    if(sensors.battery_valid) g_demo.board_battery_percent = sensors.battery_percent;
    if(sensors.environment_valid) {
        g_demo.temperature_tenths_c = sensors.temperature_tenths_c;
        g_demo.humidity_percent = sensors.humidity_percent;
    }
    monitor_clock_get_text(time_text, time_size, date_text, date_size);
}

static void capture_display_snapshot(display_snapshot_t *snapshot)
{
    monitor_devices_get(&snapshot->devices);
    refresh_monitor_data(snapshot->time_text, sizeof(snapshot->time_text),
                         snapshot->date_text, sizeof(snapshot->date_text));
    snapshot->battery_percent = g_demo.board_battery_percent;
    snapshot->temperature_tenths_c = g_demo.temperature_tenths_c;
    snapshot->humidity_percent = g_demo.humidity_percent;
    snapshot->aqi = g_demo.aqi;
    snapshot->weather_condition = g_demo.weather_condition;
    snapshot->wifi_state = monitor_wifi_get_state();
    snapshot->profiling = monitor_pm_profile_active();
    g_demo.wifi_state = snapshot->wifi_state;
}

static bool display_snapshot_equal(const display_snapshot_t *a,
                                  const display_snapshot_t *b)
{
    return a->devices.version == b->devices.version &&
           a->battery_percent == b->battery_percent &&
           a->temperature_tenths_c == b->temperature_tenths_c &&
           a->humidity_percent == b->humidity_percent &&
           a->aqi == b->aqi &&
           a->weather_condition == b->weather_condition &&
           a->wifi_state == b->wifi_state &&
           a->profiling == b->profiling &&
           strcmp(a->time_text, b->time_text) == 0 &&
           strcmp(a->date_text, b->date_text) == 0;
}

static lv_obj_t *monitor_screen_factory(lv_obj_t *parent)
{
    g_demo.wifi_state = monitor_wifi_get_state();
    monitor_ui_set_demo_data(&g_demo);
    return monitor_ui_create_waiting(parent, MONITOR_FRAME_NORMAL);
}

static esp_err_t commit_monitor_frame(monitor_lvgl_t *renderer)
{
    /* FreeRTOS owns the CPU_MAX lock while runnable. Blocking TE/SPI waits
     * must be free to lower the CPU frequency through the normal PM hooks. */
    ESP_RETURN_ON_ERROR(monitor_lvgl_render(renderer), TAG, "render frame");
    return monitor_lvgl_present(renderer);
}

static esp_err_t restore_panel_lpm(rlcd_port_t *port)
{
    esp_err_t result = rlcd_port_set_animation_mode(port, false);
    if (result != ESP_OK) result = rlcd_port_set_animation_mode(port, false);
    return result;
}

static void wait_animation_frame(TickType_t *epoch, TickType_t max_wait)
{
    TickType_t now = xTaskGetTickCount();
    const TickType_t elapsed = now - *epoch;
    const TickType_t target = *epoch + monitor_animation_next_frame(elapsed, configTICK_RATE_HZ);
    const TickType_t wait = target - now;
    if (wait > max_wait) vTaskDelay(max_wait);
    else vTaskDelayUntil(&now, wait);
}

static UBaseType_t raise_animation_priority(void)
{
    UBaseType_t original = uxTaskPriorityGet(NULL);
    if (original < ANIMATION_TASK_PRIORITY) vTaskPrioritySet(NULL, ANIMATION_TASK_PRIORITY);
    return original;
}

static int64_t wall_time_us(void)
{
    struct timeval now;
    gettimeofday(&now, NULL);
    return (int64_t)now.tv_sec * 1000000 + now.tv_usec;
}

static uint32_t milliseconds_to_minute(void)
{
    const int64_t minute_us = 60000000LL;
    return (uint32_t)((minute_us - wall_time_us() % minute_us + 999) / 1000);
}

static bool minute_needs_hpm(void)
{
    return monitor_clock_is_valid() && milliseconds_to_minute() <= MINUTE_PREPARE_MS;
}

/* The sole display owner prepares HPM before :00. Notifications received while
 * preparing are coalesced, but cannot move the absolute minute deadline. */
static uint32_t wait_display_refresh(rlcd_port_t *port, monitor_lvgl_t *renderer)
{
    uint32_t reasons = 0;
    for (;;) {
        uint32_t timer_delay = monitor_lvgl_process_timers(renderer);
        if (monitor_lvgl_frame_pending(renderer)) return MONITOR_REFRESH_LVGL;
        if (minute_needs_hpm()) {
            const int64_t now = wall_time_us();
            const int64_t boundary = (now / 60000000LL + 1) * 60000000LL;
            UBaseType_t original = raise_animation_priority();
            esp_err_t err = rlcd_port_set_animation_mode(port, true);
            if (err != ESP_OK) ESP_LOGE(TAG, "minute HPM preparation failed: %s", esp_err_to_name(err));
            int64_t left;
            while ((left = boundary - wall_time_us()) > 0 && left <= 1000000) {
                uint32_t more = 0;
                TickType_t ticks = pdMS_TO_TICKS((uint32_t)((left + 999) / 1000));
                xTaskNotifyWait(0, UINT32_MAX, &more, ticks ? ticks : 1);
                feed_task_watchdog();
                reasons |= more;
            }
            vTaskPrioritySet(NULL, original);
            return reasons | MONITOR_REFRESH_CLOCK;
        }
        uint32_t until = UINT32_MAX;
        if (monitor_clock_is_valid()) {
            uint32_t remaining = milliseconds_to_minute();
            /* Time may advance into the preparation window since the check. */
            if (remaining <= MINUTE_PREPARE_MS) continue;
            until = remaining - MINUTE_PREPARE_MS;
        }
        if (timer_delay < until) until = timer_delay;
        TickType_t ticks = until == UINT32_MAX ? portMAX_DELAY : pdMS_TO_TICKS(until);
        /* Only active display work needs a deadline. Idle tasks remain watched
         * on both cores; a quiet screen can block until the next real event. */
        ESP_ERROR_CHECK(esp_task_wdt_delete(NULL));
        BaseType_t notified = xTaskNotifyWait(0, UINT32_MAX, &reasons, ticks ? ticks : 1);
        ESP_ERROR_CHECK(esp_task_wdt_add(NULL));
        if (notified) return reasons;
    }
}

static void apply_display_snapshot(monitor_lvgl_t *renderer, const display_snapshot_t *snapshot,
                                   bool devices_changed);

/* An already running device transition must not defer a minute roll. HPM is
 * already stable, so merge the clock into that timeline at the next frame. */
static void update_animation_clock(monitor_lvgl_t *renderer, display_snapshot_t *displayed)
{
    if (!displayed) return;
    char previous_time[sizeof(displayed->time_text)], previous_date[sizeof(displayed->date_text)];
    memcpy(previous_time, displayed->time_text, sizeof(previous_time));
    memcpy(previous_date, displayed->date_text, sizeof(previous_date));
    monitor_clock_get_text(displayed->time_text, sizeof(displayed->time_text),
                           displayed->date_text, sizeof(displayed->date_text));
    if (strcmp(previous_time, displayed->time_text) || strcmp(previous_date, displayed->date_text)) {
        apply_display_snapshot(renderer, displayed, false);
    }
}

/* The UI owns finite timelines, the display task owns panel mode and cadence.
 * No animation task/timer remains running after the last frame. Data producer
 * notifications stay queued while a short animation is being presented. */
static esp_err_t present_monitor(monitor_lvgl_t *renderer, rlcd_port_t *port,
                                 display_snapshot_t *displayed)
{
    lv_obj_t *root = monitor_lvgl_root(renderer);
    bool animated = monitor_ui_animation_active(root);
    const UBaseType_t original_priority = raise_animation_priority();
    esp_err_t result = animated ? rlcd_port_set_animation_mode(port, true) : ESP_OK;
    if (result != ESP_OK) monitor_ui_set_animations_enabled(root, false);
    TickType_t frame_tick = xTaskGetTickCount();
    TickType_t previous = frame_tick;
    do {
        feed_task_watchdog();
        if (animated && result == ESP_OK) update_animation_clock(renderer, displayed);
        esp_err_t draw = commit_monitor_frame(renderer);
        if (draw != ESP_OK) { result = draw; break; }
        if (!monitor_ui_animation_active(root)) break;
        if (!rlcd_port_frame_sync_active(port)) wait_animation_frame(&frame_tick, portMAX_DELAY);
        TickType_t now = xTaskGetTickCount();
        monitor_ui_advance(root, (uint32_t)((now - previous) * portTICK_PERIOD_MS));
        previous = now;
    } while (true);
    if (result != ESP_OK) {
        /* Errors also leave a settled scene and attempt to restore LPM. */
        monitor_ui_set_animations_enabled(root, false);
        monitor_ui_set_animations_enabled(root, true);
        esp_err_t retry = commit_monitor_frame(renderer);
        if (retry != ESP_OK) ESP_LOGE(TAG, "settled frame retry failed: %s", esp_err_to_name(retry));
    }
    if (animated) vTaskDelay(pdMS_TO_TICKS(MONITOR_UI_ANIMATION_FRAME_MS)); /* Let the final HPM scan finish. */
    /* Include the 100ms LPM transition itself in the look-ahead, otherwise
     * returning to LPM can consume the remaining HPM preparation window. */
    const bool keep_hpm = result == ESP_OK && monitor_clock_is_valid() &&
                          milliseconds_to_minute() <= MINUTE_PREPARE_MS + 100U;
    esp_err_t low_power = keep_hpm ? ESP_OK : restore_panel_lpm(port);
    vTaskPrioritySet(NULL, original_priority);
    return low_power != ESP_OK ? low_power : result;
}

static void apply_display_snapshot(monitor_lvgl_t *renderer, const display_snapshot_t *snapshot,
                                     bool devices_changed)
{
    monitor_ui_snapshot_t ui = {
        .time_text = snapshot->time_text, .date_text = snapshot->date_text,
        .wifi_state = snapshot->wifi_state, .battery_percent = snapshot->battery_percent,
        .weather_condition = snapshot->weather_condition,
        .temperature_tenths_c = snapshot->temperature_tenths_c,
        .humidity_percent = snapshot->humidity_percent, .aqi = snapshot->aqi,
        .profiling = snapshot->profiling,
    };
    monitor_ui_apply_snapshot(monitor_lvgl_root(renderer), &ui);
    if (devices_changed) monitor_ui_apply_devices_snapshot(monitor_lvgl_root(renderer), &snapshot->devices);
}

static bool boot_data_complete(const display_snapshot_t *snapshot)
{
    /* A successful empty Devices response is complete too. Failed/unconfigured
     * sources retain -- until the fixed deadline; battery is not a roller. */
    if (snapshot->wifi_state == MONITOR_WIFI_OFFLINE)
        return snapshot->temperature_tenths_c != MONITOR_TEMPERATURE_UNAVAILABLE &&
               snapshot->humidity_percent >= 0;
    return snapshot->time_text[0] != '-' && snapshot->time_text[0] != '\0' &&
           snapshot->temperature_tenths_c != MONITOR_TEMPERATURE_UNAVAILABLE &&
           snapshot->humidity_percent >= 0 &&
           snapshot->weather_condition != MONITOR_WEATHER_UNKNOWN && snapshot->aqi >= 0 &&
           (snapshot->devices.status == MONITOR_DEVICES_READY ||
            snapshot->devices.status == MONITOR_DEVICES_STALE);
}

static esp_err_t present_boot_monitor(monitor_lvgl_t *renderer, rlcd_port_t *port,
                                       display_snapshot_t *displayed)
{
    lv_obj_t *root = monitor_lvgl_root(renderer);
    const TickType_t timeout = pdMS_TO_TICKS(BOOT_ANIMATION_TIMEOUT_MS);
    const TickType_t started = xTaskGetTickCount();
    TickType_t previous = started;
    TickType_t frame_tick = started;
    const UBaseType_t original_priority = uxTaskPriorityGet(NULL);
    const bool raised_priority = original_priority < ANIMATION_TASK_PRIORITY;
    if (raised_priority) vTaskPrioritySet(NULL, ANIMATION_TASK_PRIORITY);
    bool have_snapshot = false, complete = false;
    bool hpm = true;
    memset(displayed, 0, sizeof(*displayed));
    esp_err_t result = rlcd_port_set_animation_mode(port, true);
    ESP_LOGI(TAG, "boot animation started (timeout=%u ms, task priority=%u)",
             (unsigned)BOOT_ANIMATION_TIMEOUT_MS, (unsigned)uxTaskPriorityGet(NULL));
    while (result == ESP_OK) {
        feed_task_watchdog();
        uint32_t timer_delay = monitor_lvgl_process_timers(renderer);
        TickType_t now = xTaskGetTickCount();
        if (now - started >= timeout) break;
        const bool was_animating = monitor_ui_animation_active(root);
        /* Advance existing timelines before applying newly arrived data, so a
         * long wait for a source cannot instantly finish its fresh animation. */
        bool changed = monitor_ui_advance(root, (uint32_t)((now - previous) * portTICK_PERIOD_MS));
        changed |= monitor_lvgl_frame_pending(renderer);
        previous = now;
        display_snapshot_t current;
        capture_display_snapshot(&current);
        if (!have_snapshot || !display_snapshot_equal(displayed, &current)) {
            apply_display_snapshot(renderer, &current,
                                   !have_snapshot || displayed->devices.version != current.devices.version);
            *displayed = current;
            have_snapshot = true;
            changed = true;
        }
        /* A fresh animation after waiting for a source starts a new cadence;
         * its first frame must not inherit an old, long-expired deadline. */
        if (!was_animating && monitor_ui_animation_active(root)) frame_tick = now;
        if (changed) {
            if (!hpm && monitor_ui_animation_active(root)) {
                result = rlcd_port_set_animation_mode(port, true);
                if (result != ESP_OK) break;
                hpm = true;
                /* Settling is not animation time: show every fresh timeline
                 * from its first frame after the panel becomes ready. */
                frame_tick = previous = xTaskGetTickCount();
            }
            result = commit_monitor_frame(renderer);
            if (result != ESP_OK) break;
        }
        if (boot_data_complete(displayed) && !monitor_ui_animation_active(root)) {
            complete = true;
            break;
        }
        TickType_t elapsed = xTaskGetTickCount() - started;
        if (elapsed >= timeout) break;
        TickType_t remaining = timeout - elapsed;
        if (monitor_ui_animation_active(root)) {
            if (!rlcd_port_frame_sync_active(port)) wait_animation_frame(&frame_tick, remaining);
        } else {
            /* No animation is running. Let the final scan finish, then wait
             * in LPM for producer data or the absolute startup deadline. */
            if (hpm) {
                vTaskDelay(pdMS_TO_TICKS(MONITOR_UI_ANIMATION_FRAME_MS));
                result = restore_panel_lpm(port);
                if (result != ESP_OK) break;
                hpm = false;
            }
            elapsed = xTaskGetTickCount() - started;
            if (elapsed >= timeout) break;
            remaining = timeout - elapsed;
            if (timer_delay != LV_NO_TIMER_READY) {
                TickType_t timer_ticks = pdMS_TO_TICKS(timer_delay);
                if (!timer_ticks) timer_ticks = 1;
                if (timer_ticks < remaining) remaining = timer_ticks;
            }
            uint32_t reasons = 0;
            ESP_ERROR_CHECK(esp_task_wdt_delete(NULL));
            xTaskNotifyWait(0, UINT32_MAX, &reasons, remaining);
            ESP_ERROR_CHECK(esp_task_wdt_add(NULL));
        }
    }
    if (!complete) {
        /* Timeout or error ends the entire startup transaction. Land any
         * partial roll/pop, retain unavailable placeholders, then restore LPM. */
        monitor_ui_set_animations_enabled(root, false);
        monitor_ui_set_animations_enabled(root, true);
        esp_err_t commit = commit_monitor_frame(renderer);
        if (result == ESP_OK) result = commit;
    }
    if (hpm) vTaskDelay(pdMS_TO_TICKS(MONITOR_UI_ANIMATION_FRAME_MS)); /* Finish the final HPM scan. */
    esp_err_t low_power = restore_panel_lpm(port);
    if (raised_priority) vTaskPrioritySet(NULL, original_priority);
    ESP_LOGI(TAG, "boot animation %s after %u ms", result != ESP_OK || low_power != ESP_OK ? "failed" :
             (complete ? "complete" : "timed out"),
             (unsigned)((xTaskGetTickCount() - started) * portTICK_PERIOD_MS));
    return low_power != ESP_OK ? low_power : result;
}

static void reset_demo(void)
{
    g_demo.wifi_state = MONITOR_WIFI_CONNECTED;
    g_demo.board_battery_percent = -1;
    g_demo.weather_condition = MONITOR_WEATHER_UNKNOWN;
    g_demo.temperature_tenths_c = MONITOR_TEMPERATURE_UNAVAILABLE;
    g_demo.humidity_percent = -1;
    g_demo.aqi = -1;
}


void app_main(void)
{
    ESP_ERROR_CHECK(esp_task_wdt_add(NULL));
    feed_task_watchdog();
    esp_err_t nvs_err = nvs_flash_init();
    if(nvs_err == ESP_ERR_NVS_NO_FREE_PAGES || nvs_err == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        ESP_ERROR_CHECK(nvs_flash_erase());
        nvs_err = nvs_flash_init();
    }
    ESP_ERROR_CHECK(nvs_err);

    const esp_pm_config_t pm_config = {
        .max_freq_mhz = CONFIG_ESP_DEFAULT_CPU_FREQ_MHZ,
        .min_freq_mhz = 40,
        .light_sleep_enable = true,
    };
    /* USB Serial/JTAG normally hides startup sleep bugs by keeping the chip
     * awake. Protect reset/initialization explicitly for standalone power. */
    esp_pm_lock_handle_t startup_lock;
    ESP_ERROR_CHECK(esp_pm_lock_create(ESP_PM_NO_LIGHT_SLEEP, 0, "lcd_startup", &startup_lock));
    ESP_ERROR_CHECK(esp_pm_lock_acquire(startup_lock));
    ESP_ERROR_CHECK(esp_pm_configure(&pm_config));
    /* One shared, IRAM-safe GPIO ISR service for panel TE and both buttons. */
    ESP_ERROR_CHECK(gpio_install_isr_service(ESP_INTR_FLAG_IRAM));
    monitor_display_refresh_register();
    ESP_ERROR_CHECK(monitor_pm_profile_init());

    rlcd_port_t *port = NULL;
    ESP_ERROR_CHECK(rlcd_port_init(&port, &PINS));

    ESP_ERROR_CHECK(monitor_clock_start());
    ESP_ERROR_CHECK(monitor_weather_start());
    ESP_ERROR_CHECK(monitor_devices_start());
    esp_err_t sensor_err = monitor_sensors_start();
    if(sensor_err != ESP_OK) ESP_LOGW(TAG, "sensor initialization failed: %s", esp_err_to_name(sensor_err));
    esp_err_t wifi_err = monitor_wifi_start();
    if(wifi_err != ESP_OK) {
        ESP_LOGE(TAG, "Wi-Fi manager failed: %s", esp_err_to_name(wifi_err));
    }
    reset_demo();
    monitor_lvgl_t *renderer = NULL;
    if(monitor_lvgl_init(&renderer, port, monitor_screen_factory) != ESP_OK) {
        ESP_LOGE(TAG, "LVGL renderer initialization failed");
        ESP_ERROR_CHECK(esp_pm_lock_release(startup_lock));
        ESP_ERROR_CHECK(esp_pm_lock_delete(startup_lock));
        ESP_ERROR_CHECK(esp_task_wdt_delete(NULL));
        return;
    }

    monitor_lvgl_invalidate_all(renderer);
    ESP_ERROR_CHECK(present_monitor(renderer, port, NULL));
    ESP_ERROR_CHECK(esp_pm_lock_release(startup_lock));
    ESP_ERROR_CHECK(esp_pm_lock_delete(startup_lock));
    /* Start the hold after the complete placeholder frame has been transferred.
     * Data tasks may fill their caches, but only this task applies UI snapshots.
     * vTaskDelay is not interrupted by their notifications: even a valid RTC
     * and ready sensors cannot replace -- before the full two seconds elapse. */
    ESP_ERROR_CHECK(esp_task_wdt_delete(NULL));
    vTaskDelay(pdMS_TO_TICKS(BOOT_UI_HOLD_MS));
    ESP_ERROR_CHECK(esp_task_wdt_add(NULL));
    feed_task_watchdog();
    /* Sample the latest caches after the hold, then reuse normal animations. */
    display_snapshot_t displayed;
    ESP_ERROR_CHECK(present_boot_monitor(renderer, port, &displayed));
    for(;;) {
        feed_task_watchdog();
        uint32_t reasons = wait_display_refresh(port, renderer);
        if (!monitor_display_refresh_should_render(reasons, monitor_clock_is_valid())) continue;
        if (reasons == MONITOR_REFRESH_LVGL) {
            /* A timer wake must not publish cached sensor/network values ahead
             * of their normal clock boundary. Producer notifications stay queued. */
            esp_err_t draw_err = present_monitor(renderer, port, &displayed);
            if (draw_err != ESP_OK) {
                ESP_LOGE(TAG, "timer frame commit failed: %s", esp_err_to_name(draw_err));
                vTaskDelay(pdMS_TO_TICKS(1000));
            }
            continue;
        }
        /* Commit clock boundaries immediately. Fresh sensor/network results
         * arriving later remain cached for the next scheduled update. */
        display_snapshot_t current;
        capture_display_snapshot(&current);
        if (!display_snapshot_equal(&displayed, &current)) {
            apply_display_snapshot(renderer, &current, displayed.devices.version != current.devices.version);
            esp_err_t draw_err = present_monitor(renderer, port, &current);
            if (draw_err != ESP_OK) {
                ESP_LOGE(TAG, "display commit failed: %s", esp_err_to_name(draw_err));
                /* Pending damage retries independently of producer events.
                 * Back off in restored LPM if the hardware keeps failing. */
                vTaskDelay(pdMS_TO_TICKS(1000));
            } else displayed = current;
        } else {
            /* Minute preparation may have entered HPM even if a time
             * correction leaves the visible snapshot unchanged. */
            ESP_ERROR_CHECK(restore_panel_lpm(port));
        }
    }
}
