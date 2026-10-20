#include "monitor_sensors.h"
#include "monitor_clock.h"
#include "monitor_display_refresh.h"

#include <math.h>
#include <string.h>

#include "driver/gpio.h"
#include "driver/i2c_master.h"
#include "monitor_i2c.h"
#include "esp_adc/adc_cali_scheme.h"
#include "esp_adc/adc_oneshot.h"
#include "esp_check.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#define TAG "monitor_sensors"
#define BATTERY_ADC_CHANNEL ADC_CHANNEL_3 /* GPIO4 on ESP32-S3 */
#define BATTERY_DIVIDER_RATIO 3.0f       /* Waveshare board resistor divider */
#define SHTC3_ADDRESS 0x70

static adc_oneshot_unit_handle_t s_adc;
static adc_cali_handle_t s_adc_cali;
static i2c_master_dev_handle_t s_shtc3;
static TaskHandle_t s_task;
static portMUX_TYPE s_data_mux = portMUX_INITIALIZER_UNLOCKED;
static monitor_sensor_data_t s_data = {
    .battery_percent = -1, .temperature_tenths_c = 245, .humidity_percent = 58,
    .battery_voltage = 0, .battery_valid = false, .environment_valid = false,
};

static int battery_percent_from_voltage(float v)
{
    /* Typical 1S NMC/NCA discharge curve, measured under light load. Voltage
     * is intentionally piecewise-linear rather than a straight 3.0..4.2 V
     * fit, preserving the long flat plateau and steep end-of-discharge knee. */
    static const struct { float v; int pct; } curve[] = {
        {4.20f, 100}, {4.15f, 95}, {4.10f, 90}, {4.05f, 85},
        {4.00f, 80}, {3.95f, 70}, {3.90f, 60}, {3.85f, 50},
        {3.80f, 40}, {3.75f, 30}, {3.70f, 20}, {3.65f, 10},
        {3.50f, 5}, {3.30f, 0},
    };
    if (v >= curve[0].v) return 100;
    if (v <= curve[sizeof(curve) / sizeof(curve[0]) - 1].v) return 0;
    for (size_t i = 0; i + 1 < sizeof(curve) / sizeof(curve[0]); ++i) {
        if (v <= curve[i].v && v >= curve[i + 1].v) {
            const float ratio = (v - curve[i + 1].v) / (curve[i].v - curve[i + 1].v);
            return (int)lroundf(curve[i + 1].pct + ratio * (curve[i].pct - curve[i + 1].pct));
        }
    }
    return -1;
}

static bool read_battery(bool *display_changed)
{
    int raw, mv;
    if (adc_oneshot_read(s_adc, BATTERY_ADC_CHANNEL, &raw) != ESP_OK) return false;
    if (s_adc_cali == NULL || adc_cali_raw_to_voltage(s_adc_cali, raw, &mv) != ESP_OK) return false;
    const float voltage = (mv / 1000.0f) * BATTERY_DIVIDER_RATIO;
    const int battery_percent = battery_percent_from_voltage(voltage);
    portENTER_CRITICAL(&s_data_mux);
    if (display_changed != NULL) {
        *display_changed = s_data.battery_percent != battery_percent;
    }
    s_data.battery_voltage = voltage;
    s_data.battery_percent = battery_percent;
    s_data.battery_valid = true;
    portEXIT_CRITICAL(&s_data_mux);
    return true;
}

static uint8_t shtc3_crc(const uint8_t *data)
{
    uint8_t crc = 0xff;
    for (int i = 0; i < 2; ++i) {
        crc ^= data[i];
        for (int bit = 0; bit < 8; ++bit) crc = (crc & 0x80) ? (uint8_t)((crc << 1) ^ 0x31) : (uint8_t)(crc << 1);
    }
    return crc;
}

static bool read_environment(bool *display_changed)
{
    const uint8_t command[] = {0x78, 0x66};
    uint8_t data[6];
    if (i2c_master_transmit(s_shtc3, command, sizeof(command), 100) != ESP_OK) return false;
    vTaskDelay(pdMS_TO_TICKS(20));
    if (i2c_master_receive(s_shtc3, data, sizeof(data), 100) != ESP_OK) return false;
    if (shtc3_crc(data) != data[2] || shtc3_crc(&data[3]) != data[5]) return false;
    const uint16_t raw_t = ((uint16_t)data[0] << 8) | data[1];
    const uint16_t raw_h = ((uint16_t)data[3] << 8) | data[4];
    const float temperature = -45.0f + 175.0f * raw_t / 65536.0f - 4.0f;
    const float humidity = 100.0f * raw_h / 65536.0f;
    const int temperature_tenths_c = (int)lroundf(temperature * 10.0f);
    const int humidity_percent = (int)lroundf(humidity);
    portENTER_CRITICAL(&s_data_mux);
    if (display_changed != NULL) {
        *display_changed = s_data.temperature_tenths_c != temperature_tenths_c ||
                           s_data.humidity_percent != humidity_percent;
    }
    s_data.temperature_tenths_c = temperature_tenths_c;
    s_data.humidity_percent = humidity_percent;
    s_data.environment_valid = true;
    portEXIT_CRITICAL(&s_data_mux);
    return true;
}

static void sensor_task(void *arg)
{
    (void)arg;
    for (;;) {
        bool display_changed = false;
        bool value_changed = false;
        if (!read_battery(&value_changed)) { portENTER_CRITICAL(&s_data_mux); s_data.battery_valid = false; portEXIT_CRITICAL(&s_data_mux); }
        display_changed |= value_changed;
        value_changed = false;
        if (!read_environment(&value_changed)) { portENTER_CRITICAL(&s_data_mux); s_data.environment_valid = false; portEXIT_CRITICAL(&s_data_mux); }
        display_changed |= value_changed;
        if (display_changed) monitor_display_refresh_request();
        const TickType_t wait = monitor_clock_is_valid() ? portMAX_DELAY : pdMS_TO_TICKS(60000);
        (void)ulTaskNotifyTake(pdTRUE, wait);
    }
}

esp_err_t monitor_sensors_start(void)
{
    adc_oneshot_unit_init_cfg_t adc_config = {.unit_id = ADC_UNIT_1, .ulp_mode = ADC_ULP_MODE_DISABLE};
    ESP_RETURN_ON_ERROR(adc_oneshot_new_unit(&adc_config, &s_adc), TAG, "ADC init failed");
    adc_oneshot_chan_cfg_t channel_config = {.atten = ADC_ATTEN_DB_12, .bitwidth = ADC_BITWIDTH_12};
    ESP_RETURN_ON_ERROR(adc_oneshot_config_channel(s_adc, BATTERY_ADC_CHANNEL, &channel_config), TAG, "ADC channel failed");
    adc_cali_curve_fitting_config_t cali_config = {
        .unit_id = ADC_UNIT_1, .chan = BATTERY_ADC_CHANNEL,
        .atten = ADC_ATTEN_DB_12, .bitwidth = ADC_BITWIDTH_12,
    };
    if (adc_cali_create_scheme_curve_fitting(&cali_config, &s_adc_cali) != ESP_OK) {
        ESP_LOGW(TAG, "ADC calibration unavailable");
    }

    i2c_master_bus_handle_t bus = monitor_i2c_get_bus();
    if (bus == NULL) return ESP_ERR_INVALID_STATE;
    i2c_device_config_t dev_config = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7, .device_address = SHTC3_ADDRESS, .scl_speed_hz = 400000,
    };
    ESP_RETURN_ON_ERROR(i2c_master_bus_add_device(bus, &dev_config, &s_shtc3), TAG, "SHTC3 init failed");
    if (xTaskCreate(sensor_task, "monitor_sensors", 4096, NULL, 4, &s_task) != pdPASS) return ESP_ERR_NO_MEM;
    return ESP_OK;
}

void monitor_sensors_get(monitor_sensor_data_t *out)
{
    if (out == NULL) return;
    portENTER_CRITICAL(&s_data_mux);
    *out = s_data;
    portEXIT_CRITICAL(&s_data_mux);
}

void monitor_sensors_request_sample(void)
{
    if (s_task != NULL) xTaskNotifyGive(s_task);
}
