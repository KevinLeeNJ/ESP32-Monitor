#include "monitor_clock.h"
#include "monitor_display_refresh.h"
#include "monitor_i2c.h"
#include "monitor_rtc_pcf85063.h"
#include "monitor_sensors.h"
#include "monitor_weather.h"

#include <stdio.h>
#include <stdlib.h>
#include <sys/time.h>
#include <time.h>
#include "esp_log.h"
#include "esp_netif_sntp.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#define TAG "monitor_clock"
#define SYNC_TIMEOUT_MS 15000
#define CLOCK_EVT_SYNC BIT0

static TaskHandle_t s_clock_task;
static TaskHandle_t s_sync_task;
static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static volatile bool s_time_valid;
static volatile bool s_rtc_healthy;
static bool s_rtc_present;
static volatile bool s_wifi_connected;
static unsigned s_sync_retry_index;
static int64_t s_last_sync_us;

static void publish_time_valid(bool valid, bool rtc_healthy)
{
    bool changed;
    portENTER_CRITICAL(&s_lock);
    changed = s_time_valid != valid || s_rtc_healthy != rtc_healthy;
    s_time_valid = valid;
    s_rtc_healthy = rtc_healthy;
    portEXIT_CRITICAL(&s_lock);
    if (changed) {
        monitor_display_refresh_request();
        if (s_clock_task != NULL) xTaskNotify(s_clock_task, 0, eNoAction);
    }
}

static void read_rtc_and_update_system(void)
{
    monitor_rtc_sample_t sample;
    esp_err_t err = monitor_rtc_read(&sample);
    if (err != ESP_OK) {
        s_rtc_present = false;
        if (s_time_valid) {
            time_t now; time(&now);
            esp_err_t repair = monitor_rtc_write_utc((int64_t)now);
            publish_time_valid(true, repair == ESP_OK);
        } else {
            publish_time_valid(false, false);
        }
        return;
    }
    s_rtc_present = sample.present;
    if (!sample.time_valid) {
        if (s_time_valid) {
            time_t now; time(&now);
            err = monitor_rtc_write_utc((int64_t)now);
            publish_time_valid(true, err == ESP_OK);
        } else {
            publish_time_valid(false, sample.present && sample.running && !sample.oscillator_stopped);
        }
        return;
    }
    struct timeval current;
    gettimeofday(&current, NULL);
    int64_t difference = (int64_t)current.tv_sec - sample.utc_epoch_s;
    if (!s_time_valid || difference < -2 || difference > 2) {
        struct timeval restored = {.tv_sec = (time_t)sample.utc_epoch_s, .tv_usec = 0};
        settimeofday(&restored, NULL);
    }
    publish_time_valid(true, true);
}

static void sync_worker(void *arg)
{
    (void)arg;
    bool retry_due = false;
    for (;;) {
        if (!retry_due) ulTaskNotifyTake(pdTRUE, portMAX_DELAY);
        retry_due = false;
        if (!s_wifi_connected) continue;
        const int64_t now_us = esp_timer_get_time();
        if (s_last_sync_us != 0 && now_us - s_last_sync_us < 10LL * 60 * 1000000) continue;
        bool initialized = false;
        const esp_sntp_config_t config = ESP_NETIF_SNTP_DEFAULT_CONFIG_MULTIPLE(
            3, ESP_SNTP_SERVER_LIST("time.cloudflare.com", "time.google.com", "pool.ntp.org"));
        esp_err_t err = esp_netif_sntp_init(&config);
        if (err == ESP_OK) initialized = true;
        if (err == ESP_OK) err = esp_netif_sntp_start();
        if (err == ESP_OK || err == ESP_ERR_INVALID_STATE)
            err = esp_netif_sntp_sync_wait(pdMS_TO_TICKS(SYNC_TIMEOUT_MS));
        if (initialized) esp_netif_sntp_deinit();
        if (err == ESP_OK) {
            time_t now;
            time(&now);
            esp_err_t rtc_err = monitor_rtc_write_utc((int64_t)now);
            publish_time_valid(true, rtc_err == ESP_OK);
            s_last_sync_us = esp_timer_get_time();
            s_sync_retry_index = 0;
            if (monitor_clock_is_valid()) {
                monitor_weather_on_time_valid();
                monitor_sensors_request_sample();
            }
            ESP_LOGI(TAG, "SNTP synchronized; RTC write %s", rtc_err == ESP_OK ? "verified" : "failed");
        } else {
            static const uint32_t retry_seconds[] = {60, 300, 900, 3600};
            uint32_t retry = retry_seconds[s_sync_retry_index < 4 ? s_sync_retry_index : 3];
            if (s_sync_retry_index < 3) s_sync_retry_index++;
            ESP_LOGW(TAG, "SNTP attempt failed: %s; retry in %u s", esp_err_to_name(err), (unsigned)retry);
            if (s_wifi_connected) {
                (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(retry * 1000));
                retry_due = s_wifi_connected;
            }
        }
        monitor_display_refresh_request();
    }
}

static uint32_t seconds_to_next_deadline(void)
{
    time_t now;
    time(&now);
    uint32_t next_minute = 60U - (uint32_t)(now % 60);
    struct tm local;
    localtime_r(&now, &local);
    local.tm_hour = 2; local.tm_min = 0; local.tm_sec = 0;
    if (mktime(&local) <= now) local.tm_mday++;
    time_t target = mktime(&local);
    uint32_t until_2am = target > now ? (uint32_t)(target - now) : 60;
    return next_minute < until_2am ? next_minute : until_2am;
}

static void clock_task(void *arg)
{
    (void)arg;
    uint32_t rtc_retry_s = 5;
    int64_t next_rtc_retry = 0;
    int64_t last_sensor_minute = -1;
    if (s_time_valid) {
        time_t initial_time;
        time(&initial_time);
        last_sensor_minute = (int64_t)initial_time / 60;
    }
    for (;;) {
        uint32_t wait_s = s_time_valid ? seconds_to_next_deadline() : rtc_retry_s;
        if (!s_time_valid && s_rtc_present) wait_s = UINT32_MAX;
        const int64_t before_wait_us = esp_timer_get_time();
        const bool should_retry_rtc = !s_rtc_present || (s_time_valid && !s_rtc_healthy);
        if (should_retry_rtc && next_rtc_retry > before_wait_us) {
            uint32_t retry_left = (uint32_t)((next_rtc_retry - before_wait_us + 999999LL) / 1000000LL);
            if (retry_left < wait_s) wait_s = retry_left;
        }
        if (wait_s == 0) wait_s = 1;
        uint32_t notified = 0;
        TickType_t wait_ticks = wait_s == UINT32_MAX ? portMAX_DELAY : pdMS_TO_TICKS(wait_s * 1000);
        xTaskNotifyWait(0, UINT32_MAX, &notified, wait_ticks);
        if ((notified & CLOCK_EVT_SYNC) && s_sync_task) xTaskNotifyGive(s_sync_task);
        int64_t now_us = esp_timer_get_time();
        if (now_us >= next_rtc_retry && !(s_rtc_present && !s_time_valid)) {
            read_rtc_and_update_system();
            if (s_rtc_healthy) {
                rtc_retry_s = 5;
                next_rtc_retry = now_us + 60000000LL;
            } else {
                next_rtc_retry = now_us + (int64_t)rtc_retry_s * 1000000LL;
                if (rtc_retry_s < 60) rtc_retry_s *= 2;
                if (rtc_retry_s > 60) rtc_retry_s = 60;
            }
        }
        time_t now; time(&now);
        if (s_time_valid) {
            const int64_t current_minute = (int64_t)now / 60;
            if (last_sensor_minute >= 0 && current_minute != last_sensor_minute)
                monitor_sensors_request_sample();
            last_sensor_minute = current_minute;
        }
        struct tm local; localtime_r(&now, &local);
        if (s_time_valid && local.tm_hour == 2 && local.tm_min == 0 && s_sync_task)
            xTaskNotifyGive(s_sync_task);
        monitor_display_refresh_request();
    }
}

esp_err_t monitor_clock_start(void)
{
    setenv("TZ", "CST-8", 1);
    tzset();
    if (s_clock_task != NULL) return ESP_OK;
    esp_err_t err = monitor_i2c_init();
    if (err == ESP_OK) err = monitor_rtc_init(monitor_i2c_get_bus());
    if (err == ESP_OK) read_rtc_and_update_system();
    else ESP_LOGW(TAG, "RTC initialization failed: %s", esp_err_to_name(err));
    if (xTaskCreate(sync_worker, "monitor_sntp", 4096, NULL, 3, &s_sync_task) != pdPASS)
        return ESP_ERR_NO_MEM;
    if (xTaskCreate(clock_task, "monitor_clock", 4096, NULL, 4, &s_clock_task) != pdPASS) {
        s_clock_task = NULL;
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}

void monitor_clock_on_wifi_connected(void)
{
    s_wifi_connected = true;
    if (s_clock_task != NULL) xTaskNotify(s_clock_task, CLOCK_EVT_SYNC, eSetBits);
}

void monitor_clock_on_wifi_disconnected(void)
{
    s_wifi_connected = false;
    if (s_sync_task != NULL) xTaskNotifyGive(s_sync_task);
}

bool monitor_clock_is_valid(void)
{
    bool valid;
    portENTER_CRITICAL(&s_lock); valid = s_time_valid; portEXIT_CRITICAL(&s_lock);
    return valid;
}

bool monitor_clock_get_text(char *time_text, size_t time_size, char *date_text, size_t date_size)
{
    if (time_text == NULL || time_size == 0 || date_text == NULL || date_size == 0) return false;
    bool valid;
    portENTER_CRITICAL(&s_lock); valid = s_time_valid; portEXIT_CRITICAL(&s_lock);
    if (!valid) {
        snprintf(time_text, time_size, "--:--");
        snprintf(date_text, date_size, "SYNCING");
        return false;
    }
    time_t now; time(&now);
    struct tm local; localtime_r(&now, &local);
    static const char *const weekdays[] = {"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"};
    static const char *const months[] = {"JAN", "FEB", "MAR", "APR", "MAY", "JUN", "JUL", "AUG", "SEP", "OCT", "NOV", "DEC"};
    snprintf(time_text, time_size, "%02d:%02d", local.tm_hour, local.tm_min);
    snprintf(date_text, date_size, "%s %02d %s", weekdays[local.tm_wday], local.tm_mday, months[local.tm_mon]);
    return true;
}
