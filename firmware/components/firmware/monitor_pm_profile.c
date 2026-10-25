#include "monitor_pm_profile.h"
#include "monitor_pm_profile_report.h"
#include "monitor_display_refresh.h"

#include <stdlib.h>
#include <string.h>
#include "driver/usb_serial_jtag.h"
#include "esp_log.h"
#include "esp_heap_caps.h"
#include "esp_pm.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "nvs.h"
#include "sdkconfig.h"

#if !CONFIG_PM_PROFILING
#error "KEY profiling requires CONFIG_PM_PROFILING"
#endif

#define PROFILE_DURATION_US (300LL * 1000000)
#define PROFILE_REPORT_MAX 12288
#define TAG "pm_profile"
#if CONFIG_ESP_PHY_MAC_BB_PD
#define MAC_BB_STATE "enabled"
#else
#define MAC_BB_STATE "disabled"
#endif

static TaskHandle_t s_task;
static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static bool s_active;
static char *s_report;
static bool s_persisted;

bool monitor_pm_profile_active(void)
{
    portENTER_CRITICAL(&s_lock);
    bool active = s_active;
    portEXIT_CRITICAL(&s_lock);
    return active;
}

static void set_active(bool active)
{
    portENTER_CRITICAL(&s_lock);
    s_active = active;
    portEXIT_CRITICAL(&s_lock);
    monitor_display_refresh_request();
}

static esp_err_t capture_snapshot(monitor_pm_snapshot_t *snapshot)
{
    char *text = NULL;
    size_t length = 0;
    FILE *stream = open_memstream(&text, &length);
    if (!stream) return ESP_ERR_NO_MEM;
    esp_err_t err = esp_pm_dump_locks(stream);
    if (fclose(stream) != 0 && err == ESP_OK) err = ESP_FAIL;
    if (err == ESP_OK && (!text || !monitor_pm_snapshot_parse(text, snapshot))) err = ESP_ERR_INVALID_RESPONSE;
    free(text);
    return err;
}

static void load_report(void)
{
    nvs_handle_t handle;
    if (nvs_open("pm_profile", NVS_READONLY, &handle) != ESP_OK) return;
    size_t length = 0;
    if (nvs_get_blob(handle, "report", NULL, &length) == ESP_OK && length > 1 && length <= PROFILE_REPORT_MAX) {
        char *text = malloc(length);
        if (text && nvs_get_blob(handle, "report", text, &length) == ESP_OK && text[length - 1] == '\0') {
            s_report = text;
            s_persisted = true;
        } else free(text);
    }
    nvs_close(handle);
}

static void save_report(void)
{
    s_persisted = false;
    nvs_handle_t handle;
    esp_err_t err = nvs_open("pm_profile", NVS_READWRITE, &handle);
    if (err == ESP_OK) {
        err = nvs_set_blob(handle, "report", s_report, strlen(s_report) + 1);
        if (err == ESP_OK) err = nvs_commit(handle);
        nvs_close(handle);
    }
    s_persisted = err == ESP_OK;
    if (!s_persisted) ESP_LOGW(TAG, "report remains in RAM only: %s", esp_err_to_name(err));
}

static void run_profile(void)
{
    /* Allocate once at the boundary; no periodic task, timer or logging. */
    monitor_pm_snapshot_t *snapshots = heap_caps_calloc(2, sizeof(*snapshots), MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (!snapshots) { ESP_LOGE(TAG, "snapshot allocation failed"); return; }
    const bool usb_at_start = usb_serial_jtag_is_connected();
    esp_err_t err = capture_snapshot(&snapshots[0]);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "baseline capture failed: %s", esp_err_to_name(err));
        free(snapshots);
        return;
    }
    const int64_t deadline = esp_timer_get_time() + PROFILE_DURATION_US;
    set_active(true);
    /* KEY notifications cannot restart, shorten, or print the live window. */
    for (;;) {
        int64_t left = deadline - esp_timer_get_time();
        if (left <= 0) break;
        TickType_t ticks = pdMS_TO_TICKS((uint32_t)((left + 999) / 1000));
        ulTaskNotifyTake(pdTRUE, ticks ? ticks : 1);
    }
    err = capture_snapshot(&snapshots[1]);
    const bool usb_at_end = usb_serial_jtag_is_connected();
    /* End counters are frozen BEFORE restoring date, formatting or NVS writes. */
    set_active(false);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "end capture failed: %s", esp_err_to_name(err));
        free(snapshots);
        return;
    }
    char *report = NULL;
    size_t length = 0;
    FILE *stream = open_memstream(&report, &length);
    bool valid = false;
    if (stream) {
        fprintf(stream, "USB host at start: %s; at end: %s. MAC/BB power-down: %s\n",
                usb_at_start ? "yes" : "no", usb_at_end ? "yes" : "no",
                MAC_BB_STATE);
        if (usb_at_start || usb_at_end)
            fprintf(stream, "WARNING: USB host connection can block automatic Light Sleep.\n");
        valid = monitor_pm_report_write(stream, &snapshots[0], &snapshots[1]);
        if (fclose(stream) != 0) valid = false;
    }
    free(snapshots);
    if (!valid || !report || length >= PROFILE_REPORT_MAX) {
        free(report);
        ESP_LOGE(TAG, "report generation failed; previous result retained");
        return;
    }
    free(s_report);
    s_report = report;
    save_report();
}

static void profile_task(void *arg)
{
    (void)arg;
    for (;;) {
        ulTaskNotifyTake(pdTRUE, portMAX_DELAY);
        if (s_report && usb_serial_jtag_is_connected()) {
            puts(s_report);
            printf("Report storage: %s\n", s_persisted ? "NVS (survives reset)" : "RAM only");
            fflush(stdout);
        } else run_profile();
        /* Drop holds made during capture/printing; require a fresh gesture. */
        ulTaskNotifyTake(pdTRUE, 0);
    }
}

void monitor_pm_profile_key_hold(void)
{
    if (s_task && !monitor_pm_profile_active()) xTaskNotifyGive(s_task);
}

esp_err_t monitor_pm_profile_init(void)
{
    if (s_task) return ESP_OK;
    load_report();
    if (xTaskCreate(profile_task, "pm_profile", 6144, NULL, 4, &s_task) != pdPASS) {
        free(s_report);
        s_report = NULL;
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}
