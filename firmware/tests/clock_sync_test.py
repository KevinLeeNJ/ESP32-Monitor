"""Exercise the real SNTP worker's initial-time and RTC-restored notifications."""
from host_harness import REPO, function, run

source = (REPO / 'firmware/components/firmware/monitor_clock.c').read_text()
code = r'''
#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#include <setjmp.h>
#include <time.h>
#define ESP_OK 0
#define ESP_FAIL -1
#define ESP_ERR_INVALID_STATE 2
#define pdTRUE 1
#define portMAX_DELAY UINT32_MAX
#define pdMS_TO_TICKS(ms) (ms)
#define SYNC_TIMEOUT_MS 15000
#define eNoAction 0
#define TAG "clock"
#define ESP_LOGI(...) ((void)0)
#define ESP_LOGW(...) ((void)0)
#define ESP_SNTP_SERVER_LIST(...) 0
#define ESP_NETIF_SNTP_DEFAULT_CONFIG_MULTIPLE(...) 0
typedef int esp_err_t, esp_sntp_config_t;
static bool s_wifi_connected, valid, healthy;
static int64_t s_last_sync_us;
static unsigned s_sync_retry_index, waits, weather, devices, display, clock_wakes, writes;
static int sync_result, rtc_result;
static void *s_clock_task = (void *)1;
static jmp_buf done;
static unsigned ulTaskNotifyTake(int clear, unsigned ticks) {
    (void)clear; (void)ticks;
    if (++waits > 1) longjmp(done, 1);
    return 1;
}
static int64_t esp_timer_get_time(void) { return 1000000; }
static int esp_netif_sntp_init(const esp_sntp_config_t *config) { (void)config; return ESP_OK; }
static int esp_netif_sntp_start(void) { return ESP_OK; }
static int esp_netif_sntp_sync_wait(unsigned ticks) { assert(ticks == 15000); return sync_result; }
static void esp_netif_sntp_deinit(void) {}
static int monitor_rtc_write_utc(int64_t epoch) { assert(epoch > 0); ++writes; return rtc_result; }
static bool monitor_clock_is_valid(void) { return valid; }
static void publish_time_valid(bool v, bool h) { valid = v; healthy = h; }
static void monitor_weather_on_time_valid(void) { ++weather; }
static void monitor_devices_request_refresh(void) { ++devices; }
static void monitor_display_refresh_request(void) { ++display; }
static void xTaskNotify(void *task, unsigned bits, int action) {
    assert(task == s_clock_task && !bits && action == eNoAction); ++clock_wakes;
}
'''
code += function(source, 'sync_worker')
code += r'''
static void scenario(bool rtc_valid, int sync, int rtc) {
    s_wifi_connected = true; valid = rtc_valid; healthy = false;
    waits = weather = devices = display = clock_wakes = writes = 0;
    s_last_sync_us = s_sync_retry_index = 0; sync_result = sync; rtc_result = rtc;
    if (!setjmp(done)) sync_worker(NULL);
}
int main(void) {
    scenario(true, ESP_OK, ESP_OK);
    assert(valid && healthy && writes == 1 && !weather && !devices && clock_wakes == 1);
    scenario(false, ESP_OK, ESP_OK);
    assert(valid && healthy && weather == 1 && devices == 1 && clock_wakes == 1);
    scenario(false, ESP_OK, ESP_FAIL);
    assert(valid && !healthy && weather == 1 && devices == 1);
    scenario(false, ESP_FAIL, ESP_OK);
    assert(!valid && !weather && !devices && !writes);
}
'''
run(code)
