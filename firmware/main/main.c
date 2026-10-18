#include "esp_log.h"
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

#define TAG "monitor_test"

static const rlcd_pins_t PINS = {
    .mosi = 12,
    .sclk = 11,
    .dc = 5,
    .cs = 40,
    .rst = 41,
    .te = 6,
};

static monitor_ui_demo_data_t g_demo;
static monitor_device_t g_devices[4];
static uint8_t g_device_count;

static lv_obj_t *test_screen_factory(lv_obj_t *parent)
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
    monitor_ui_set_demo_data(&g_demo);
    char time_text[8];
    char date_text[16];
    monitor_clock_get_text(time_text, sizeof(time_text), date_text, sizeof(date_text));
    monitor_ui_set_clock_text(time_text, date_text);
    return monitor_ui_create_with_devices_battery_wifi_variant(
        parent, g_devices, 4, g_demo.board_battery_percent,
        monitor_wifi_get_state(), MONITOR_FRAME_NORMAL);
}

static void set_devices(uint8_t count, int level)
{
    static const char *names[] = {"NAS", "PI", "PC", "ROUTER"};
    g_device_count = count;
    for(uint8_t i = 0; i < 4; ++i) {
        g_devices[i].name = names[i];
        g_devices[i].cpu_percent = level;
        g_devices[i].ram_percent = level;
        g_devices[i].battery_percent = level;
        g_devices[i].online = i < count;
    }
}

static void reset_demo(void)
{
    g_demo.wifi_state = MONITOR_WIFI_CONNECTED;
    g_demo.board_battery_percent = -1;
    g_demo.weather_condition = MONITOR_WEATHER_FEW_CLOUDS;
    g_demo.temperature_tenths_c = 245;
    g_demo.humidity_percent = 58;
    g_demo.aqi = -1;
    set_devices(4, 50);
}

static void show_frame(monitor_renderer_t *renderer, rlcd_port_t *port)
{
    if(!monitor_renderer_rebuild(renderer, test_screen_factory) ||
       !monitor_renderer_render(renderer)) {
        ESP_LOGE(TAG, "render failed");
        return;
    }
    ESP_ERROR_CHECK(rlcd_port_draw(port, monitor_renderer_packed(renderer),
                                   MONITOR_FRAME_BYTES));
}

static void wait_frame(monitor_renderer_t *renderer, rlcd_port_t *port,
                       TickType_t ticks)
{
    show_frame(renderer, port);
    vTaskDelay(ticks);
}

static void run_test_cycle(monitor_renderer_t *renderer, rlcd_port_t *port)
{
    /* Wi-Fi is driven by the real provisioning/connection manager. Do not
     * synthesize Wi-Fi states in the demo cycle. */

    /* Battery, temperature and humidity are read from the hardware sensor task. */
    wait_frame(renderer, port, pdMS_TO_TICKS(1000));

    /* Weather and AQI are fetched from QWeather. */
    wait_frame(renderer, port, pdMS_TO_TICKS(1000));

    /* Temperature and humidity are hardware-backed and refreshed every 5s. */
    wait_frame(renderer, port, pdMS_TO_TICKS(1000));

    /* Devices: 0 through 4 online devices, each with all metrics swept from
     * 0% through 100%. Offline slots are intentionally not rendered. */
    for(uint8_t count = 0; count <= 4; ++count) {
        for(int level = 0; level <= 100; level += 25) {
            set_devices(count, level);
            wait_frame(renderer, port, pdMS_TO_TICKS(350));
        }
    }
}

void app_main(void)
{
    rlcd_port_t *port = NULL;
    ESP_ERROR_CHECK(rlcd_port_init(&port, &PINS));

    esp_err_t nvs_err = nvs_flash_init();
    if(nvs_err == ESP_ERR_NVS_NO_FREE_PAGES || nvs_err == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        ESP_ERROR_CHECK(nvs_flash_erase());
        nvs_err = nvs_flash_init();
    }
    ESP_ERROR_CHECK(nvs_err);

    ESP_ERROR_CHECK(monitor_clock_start());
    ESP_ERROR_CHECK(monitor_weather_start());
    esp_err_t sensor_err = monitor_sensors_start();
    if(sensor_err != ESP_OK) ESP_LOGW(TAG, "sensor initialization failed: %s", esp_err_to_name(sensor_err));
    esp_err_t wifi_err = monitor_wifi_start();
    if(wifi_err != ESP_OK) {
        ESP_LOGE(TAG, "Wi-Fi manager failed: %s", esp_err_to_name(wifi_err));
    }
    reset_demo();
    monitor_renderer_t renderer;
    if(!monitor_renderer_init(&renderer, test_screen_factory)) {
        ESP_LOGE(TAG, "LVGL renderer initialization failed");
        return;
    }

    for(;;) {
        run_test_cycle(&renderer, port);
        ESP_LOGI(TAG, "test cycle complete");
    }
}
