#include "monitor_clock.h"
#include "monitor_display_refresh.h"

#include <stdio.h>
#include <string.h>
#include <time.h>

#include "esp_log.h"
#include "esp_netif_sntp.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "lwip/apps/sntp.h"

#define TAG "monitor_clock"
#define SYNC_TIMEOUT_MS 15000

static TaskHandle_t s_clock_task;
static volatile bool s_sync_requested;
static bool s_sntp_initialized;
static bool s_time_valid;

static bool sync_time(void)
{
    if (!s_sntp_initialized) {
        const esp_sntp_config_t config = ESP_NETIF_SNTP_DEFAULT_CONFIG_MULTIPLE(
            3, ESP_SNTP_SERVER_LIST("time.cloudflare.com", "time.google.com", "pool.ntp.org"));
        esp_err_t err = esp_netif_sntp_init(&config);
        if (err != ESP_OK) {
            ESP_LOGE(TAG, "SNTP initialization failed: %s", esp_err_to_name(err));
            return false;
        }
        s_sntp_initialized = true;
    }

    ESP_LOGI(TAG, "Starting NTP sync (Cloudflare, Google, pool.ntp.org)");
    esp_err_t err = esp_netif_sntp_start();
    if (err != ESP_OK && err != ESP_ERR_INVALID_STATE) {
        ESP_LOGE(TAG, "SNTP start failed: %s", esp_err_to_name(err));
        return false;
    }

    err = esp_netif_sntp_sync_wait(pdMS_TO_TICKS(SYNC_TIMEOUT_MS));
    if (err != ESP_OK) {
        ESP_LOGW(TAG, "NTP sync did not complete within %d ms", SYNC_TIMEOUT_MS);
        return false;
    }

    s_time_valid = true;
    monitor_display_refresh_request();
    time_t now;
    time(&now);
    struct tm local;
    localtime_r(&now, &local);
    ESP_LOGI(TAG, "System clock synchronized: %04d-%02d-%02d %02d:%02d:%02d",
             local.tm_year + 1900, local.tm_mon + 1, local.tm_mday,
             local.tm_hour, local.tm_min, local.tm_sec);
    /* Stop polling; the task explicitly restarts SNTP at the next 02:00. */
    esp_netif_sntp_deinit();
    s_sntp_initialized = false;
    return true;
}

static uint32_t seconds_until_next_2am(void)
{
    time_t now;
    time(&now);
    struct tm next;
    localtime_r(&now, &next);
    next.tm_hour = 2;
    next.tm_min = 0;
    next.tm_sec = 0;
    if (mktime(&next) <= now) next.tm_mday++;
    time_t target = mktime(&next);
    if (target <= now) return 60;
    return (uint32_t)(target - now);
}

static void clock_task(void *arg)
{
    (void)arg;
    for (;;) {
        if (s_sync_requested) {
            s_sync_requested = false;
            sync_time();
        }
        if (!s_time_valid) {
            ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(1000));
            continue;
        }
        uint32_t seconds = seconds_until_next_2am();
        /* Wake periodically so a Wi-Fi event can request an immediate sync. */
        uint32_t wait_seconds = seconds > 60 ? 60 : seconds;
        if (ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(wait_seconds * 1000)) == 0 && seconds <= 60) {
            sync_time();
        }
    }
}

esp_err_t monitor_clock_start(void)
{
    setenv("TZ", "CST-8", 1);
    tzset();
    if (s_clock_task != NULL) return ESP_OK;
    if (xTaskCreate(clock_task, "monitor_clock", 8192, NULL, 4, &s_clock_task) != pdPASS) {
        s_clock_task = NULL;
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}

void monitor_clock_on_wifi_connected(void)
{
    if (s_clock_task == NULL) return;
    s_sync_requested = true;
    xTaskNotifyGive(s_clock_task);
}

bool monitor_clock_get_text(char *time_text, size_t time_size,
                            char *date_text, size_t date_size)
{
    if (time_text == NULL || time_size == 0 || date_text == NULL || date_size == 0) return false;
    time_t now;
    time(&now);
    struct tm local;
    localtime_r(&now, &local);
    if (!s_time_valid) {
        snprintf(time_text, time_size, "--:--");
        snprintf(date_text, date_size, "SYNCING");
        return false;
    }
    static const char *const weekdays[] = {"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"};
    static const char *const months[] = {"JAN", "FEB", "MAR", "APR", "MAY", "JUN", "JUL", "AUG", "SEP", "OCT", "NOV", "DEC"};
    snprintf(time_text, time_size, "%02d:%02d", local.tm_hour, local.tm_min);
    snprintf(date_text, date_size, "%s %02d %s", weekdays[local.tm_wday],
             local.tm_mday, months[local.tm_mon]);
    return true;
}
