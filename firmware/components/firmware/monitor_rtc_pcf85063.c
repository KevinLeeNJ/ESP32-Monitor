#include "monitor_rtc_pcf85063.h"

#include <string.h>
#include <time.h>
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

#define RTC_ADDRESS 0x51
#define RTC_REG_CONTROL_1 0x00
#define RTC_REG_SECONDS 0x04
#define RTC_OS_BIT 0x80
#define RTC_STOP_BIT 0x20
#define RTC_12H_BIT 0x02

static i2c_master_dev_handle_t s_device;
static SemaphoreHandle_t s_lock;

static uint8_t from_bcd(uint8_t value) { return (uint8_t)(((value >> 4) * 10) + (value & 0x0f)); }
static bool valid_bcd(uint8_t value, uint8_t max)
{
    return (value & 0x0f) <= 9 && ((value >> 4) <= 9) && from_bcd(value) <= max;
}
static uint8_t to_bcd(unsigned value) { return (uint8_t)(((value / 10) << 4) | (value % 10)); }

static bool decode(const uint8_t r[7], int64_t *epoch, bool *os, bool *running)
{
    uint8_t control;
    if (i2c_master_transmit_receive(s_device, (uint8_t[]){RTC_REG_CONTROL_1}, 1,
                                    &control, 1, 100) != ESP_OK) return false;
    const bool stopped = (control & RTC_STOP_BIT) != 0;
    const bool oscillator = (r[0] & RTC_OS_BIT) != 0;
    *os = oscillator;
    *running = !stopped;
    if (oscillator || stopped || (control & RTC_12H_BIT) != 0) return false;
    if (!valid_bcd(r[0] & 0x7f, 59) || !valid_bcd(r[1] & 0x7f, 59) ||
        !valid_bcd(r[2] & 0x3f, 23) || !valid_bcd(r[3] & 0x3f, 31) ||
        !valid_bcd(r[4] & 0x07, 6) || !valid_bcd(r[5] & 0x1f, 12) ||
        !valid_bcd(r[6], 99)) return false;
    const int year = 2000 + from_bcd(r[6]);
    const int month = from_bcd(r[5] & 0x1f);
    const int day = from_bcd(r[3] & 0x3f);
    const int hour = from_bcd(r[2] & 0x3f);
    const int minute = from_bcd(r[1] & 0x7f);
    const int second = from_bcd(r[0] & 0x7f);
    if (year < 2024 || month < 1 || month > 12 || hour > 23 || minute > 59 || second > 59) return false;
    struct tm tmv = {0};
    tmv.tm_year = year - 1900; tmv.tm_mon = month - 1; tmv.tm_mday = day;
    tmv.tm_hour = hour; tmv.tm_min = minute; tmv.tm_sec = second;
    static const int month_days[] = {31,28,31,30,31,30,31,31,30,31,30,31};
    int mdays = month_days[month - 1];
    if (month == 2 && ((year % 4 == 0 && year % 100 != 0) || year % 400 == 0)) mdays++;
    if (day < 1 || day > mdays) return false;
    time_t value = timegm(&tmv);
    if (value < 0) return false;
    *epoch = (int64_t)value;
    return true;
}

esp_err_t monitor_rtc_init(i2c_master_bus_handle_t bus)
{
    if (bus == NULL) return ESP_ERR_INVALID_ARG;
    if (s_device != NULL) return ESP_OK;
    if (s_lock == NULL) {
        s_lock = xSemaphoreCreateMutex();
        if (s_lock == NULL) return ESP_ERR_NO_MEM;
    }
    const i2c_device_config_t config = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7,
        .device_address = RTC_ADDRESS,
        .scl_speed_hz = 100000,
    };
    return i2c_master_bus_add_device(bus, &config, &s_device);
}

esp_err_t monitor_rtc_read(monitor_rtc_sample_t *out)
{
    if (out == NULL || s_device == NULL || s_lock == NULL) return ESP_ERR_INVALID_STATE;
    memset(out, 0, sizeof(*out));
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(200)) != pdTRUE) return ESP_ERR_TIMEOUT;
    uint8_t regs[7] = {0};
    esp_err_t err = i2c_master_transmit_receive(s_device, (uint8_t[]){RTC_REG_SECONDS}, 1,
                                                regs, sizeof(regs), 100);
    out->present = err == ESP_OK;
    if (err == ESP_OK) {
        out->time_valid = decode(regs, &out->utc_epoch_s,
                                 &out->oscillator_stopped, &out->running);
    }
    xSemaphoreGive(s_lock);
    return err;
}

esp_err_t monitor_rtc_write_utc(int64_t utc_epoch_s)
{
    if (s_device == NULL || s_lock == NULL || utc_epoch_s < 1704067200LL || utc_epoch_s >= 4102444800LL)
        return ESP_ERR_INVALID_ARG;
    if (xSemaphoreTake(s_lock, pdMS_TO_TICKS(200)) != pdTRUE) return ESP_ERR_TIMEOUT;
    struct tm tmv;
    time_t epoch = (time_t)utc_epoch_s;
    gmtime_r(&epoch, &tmv);
    uint8_t control = 0;
    esp_err_t err = i2c_master_transmit_receive(s_device, (uint8_t[]){RTC_REG_CONTROL_1}, 1,
                                                &control, 1, 100);
    bool stopped = false;
    if (err == ESP_OK) {
        stopped = (control & RTC_STOP_BIT) != 0;
        uint8_t stop = (uint8_t)(control | RTC_STOP_BIT);
        err = i2c_master_transmit(s_device, (uint8_t[]){RTC_REG_CONTROL_1, stop}, 2, 100);
    }
    uint8_t data[8] = {RTC_REG_SECONDS,
        to_bcd((unsigned)tmv.tm_sec), to_bcd((unsigned)tmv.tm_min), to_bcd((unsigned)tmv.tm_hour),
        to_bcd((unsigned)tmv.tm_mday), to_bcd((unsigned)tmv.tm_wday),
        to_bcd((unsigned)(tmv.tm_mon + 1)), to_bcd((unsigned)(tmv.tm_year - 100))};
    if (err == ESP_OK) err = i2c_master_transmit(s_device, data, sizeof(data), 100);
    esp_err_t restart = i2c_master_transmit(s_device,
        (uint8_t[]){RTC_REG_CONTROL_1, (uint8_t)(control & (uint8_t)~RTC_STOP_BIT)}, 2, 100);
    if (err == ESP_OK) err = restart;
    (void)stopped;
    xSemaphoreGive(s_lock);
    if (err != ESP_OK) return err;
    monitor_rtc_sample_t check;
    err = monitor_rtc_read(&check);
    if (err != ESP_OK) return err;
    int64_t delta = check.utc_epoch_s - utc_epoch_s;
    if (!check.time_valid || delta < -2 || delta > 2) return ESP_ERR_INVALID_RESPONSE;
    return ESP_OK;
}
