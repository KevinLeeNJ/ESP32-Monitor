#include "esp_log.h"
#include "esp_pm.h"
#include "nvs_flash.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#include "display_port.h"
#include "monitor_render.h"
#include "monitor_ui.h"
#include "monitor_wifi_manager.h"
#include "monitor_clock.h"
#include "monitor_sensors.h"
#include "monitor_weather.h"
#include "monitor_devices.h"
#include "monitor_display_refresh.h"

#include <string.h>

#define TAG "monitor"

static const rlcd_pins_t PINS = {
    .mosi = 12,
    .sclk = 11,
    .dc = 5,
    .cs = 40,
    .rst = 41,
    .te = 6,
};

static monitor_ui_demo_data_t g_demo;

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
} display_snapshot_t;

static display_snapshot_t g_rendered_snapshot;

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
           strcmp(a->time_text, b->time_text) == 0 &&
           strcmp(a->date_text, b->date_text) == 0;
}

static lv_obj_t *monitor_screen_factory(lv_obj_t *parent)
{
    capture_display_snapshot(&g_rendered_snapshot);
    monitor_ui_set_demo_data(&g_demo);
    monitor_ui_set_clock_text(g_rendered_snapshot.time_text,
                              g_rendered_snapshot.date_text);
    lv_obj_t *root = monitor_ui_create_with_devices_battery_wifi_variant(
        parent, NULL, 0, g_demo.board_battery_percent,
        g_rendered_snapshot.wifi_state, MONITOR_FRAME_NORMAL);
    monitor_ui_apply_devices_snapshot(root, &g_rendered_snapshot.devices);
    return root;
}

static void reset_demo(void)
{
    g_demo.wifi_state = MONITOR_WIFI_CONNECTED;
    g_demo.board_battery_percent = -1;
    g_demo.weather_condition = MONITOR_WEATHER_FEW_CLOUDS;
    g_demo.temperature_tenths_c = 245;
    g_demo.humidity_percent = 58;
    g_demo.aqi = -1;
}


void app_main(void)
{
    esp_err_t nvs_err = nvs_flash_init();
    if(nvs_err == ESP_ERR_NVS_NO_FREE_PAGES || nvs_err == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        ESP_ERROR_CHECK(nvs_flash_erase());
        nvs_err = nvs_flash_init();
    }
    ESP_ERROR_CHECK(nvs_err);

    const esp_pm_config_t pm_config = {
        .max_freq_mhz = 160,
        .min_freq_mhz = 40,
        .light_sleep_enable = true,
    };
    ESP_ERROR_CHECK(esp_pm_configure(&pm_config));
    monitor_display_refresh_register();

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
    monitor_renderer_t renderer;
    if(!monitor_renderer_init(&renderer, monitor_screen_factory)) {
        ESP_LOGE(TAG, "LVGL renderer initialization failed");
        return;
    }

    monitor_renderer_invalidate_all(&renderer);
    if(!monitor_renderer_render(&renderer)) {
        ESP_LOGE(TAG, "initial render failed");
        return;
    }
    ESP_ERROR_CHECK(rlcd_port_draw_diff(port, monitor_renderer_packed(&renderer), MONITOR_FRAME_BYTES));
    display_snapshot_t displayed = g_rendered_snapshot;
    for(;;) {
        uint32_t reasons = 0;
        xTaskNotifyWait(0, UINT32_MAX, &reasons, portMAX_DELAY);
        /* Merge a burst of producers without periodic polling or busy waiting. */
        uint32_t more = 0;
        xTaskNotifyWait(0, UINT32_MAX, &more, pdMS_TO_TICKS(50));
        reasons |= more;
        (void)reasons;
        display_snapshot_t current;
        capture_display_snapshot(&current);
        if (!display_snapshot_equal(&displayed, &current)) {
            monitor_ui_snapshot_t ui = {
                .time_text = current.time_text,
                .date_text = current.date_text,
                .wifi_state = current.wifi_state,
                .battery_percent = current.battery_percent,
                .weather_condition = current.weather_condition,
                .temperature_tenths_c = current.temperature_tenths_c,
                .humidity_percent = current.humidity_percent,
                .aqi = current.aqi,
            };
            monitor_ui_apply_snapshot(monitor_renderer_root(&renderer), &ui);
            if (displayed.devices.version != current.devices.version)
                monitor_ui_apply_devices_snapshot(monitor_renderer_root(&renderer), &current.devices);
            if (monitor_renderer_render(&renderer)) {
                esp_err_t draw_err = rlcd_port_draw_diff(port, monitor_renderer_packed(&renderer), MONITOR_FRAME_BYTES);
                if (draw_err != ESP_OK) ESP_LOGE(TAG, "display commit failed: %s", esp_err_to_name(draw_err));
                else displayed = current;
            }
        }
    }
}
