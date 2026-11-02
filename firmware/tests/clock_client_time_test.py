"""Exercise the Client-time fallback priority against RTC and Wi-Fi state."""
from host_harness import REPO, function, run

source = (REPO / 'firmware/components/firmware/monitor_clock.c').read_text()
code = r'''
#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#define ESP_OK 0
#define eNoAction 0
#define TAG "clock"
#define ESP_LOGI(...) ((void)0)
typedef int esp_err_t;
struct timeval { int64_t tv_sec; int64_t tv_usec; };
static int test_settimeofday(const struct timeval *tv, void *tz);
static int test_gettimeofday(struct timeval *tv, void *tz);
#define settimeofday test_settimeofday
#define gettimeofday test_gettimeofday
static bool s_rtc_trusted, s_wifi_connected, s_time_valid, healthy;
static void *s_clock_task = (void *)1;
static int64_t wall_sec, last_set;
static int set_result, rtc_result;
static unsigned sets, writes, weather, devices, display, clock_wakes;
static int test_gettimeofday(struct timeval *tv, void *tz) {
    (void)tz; tv->tv_sec = wall_sec; tv->tv_usec = 0; return 0;
}
static int test_settimeofday(const struct timeval *tv, void *tz) {
    (void)tz; ++sets; last_set = tv->tv_sec;
    if (!set_result) wall_sec = tv->tv_sec;
    return set_result;
}
static esp_err_t monitor_rtc_write_utc(int64_t epoch) {
    (void)epoch; ++writes; return rtc_result;
}
static void publish_time_valid(bool valid, bool health) { s_time_valid = valid; healthy = health; }
static void monitor_weather_on_time_valid(void) { ++weather; }
static void monitor_devices_request_refresh(void) { ++devices; }
static void monitor_display_refresh_request(void) { ++display; }
static void xTaskNotify(void *task, unsigned bits, int action) {
    assert(task == s_clock_task && !bits && action == eNoAction); ++clock_wakes;
}
'''
code += function(source, 'monitor_clock_offer_client_time')
code += r'''
static void reset(bool trusted, bool wifi, bool valid, int set, int rtc) {
    s_rtc_trusted = trusted; s_wifi_connected = wifi; s_time_valid = valid; healthy = false;
    wall_sec = 1792972800; last_set = 0;
    set_result = set; rtc_result = rtc;
    sets = writes = weather = devices = display = clock_wakes = 0;
}
int main(void) {
    reset(true, false, false, 0, ESP_OK);
    monitor_clock_offer_client_time(wall_sec + 600);
    assert(!sets && !writes && !s_time_valid && !weather && !devices && !display && !clock_wakes);
    reset(false, true, false, 0, ESP_OK);
    monitor_clock_offer_client_time(wall_sec + 600);
    assert(!sets && !writes && !s_time_valid && !s_rtc_trusted);
    reset(false, false, false, 0, ESP_OK);
    monitor_clock_offer_client_time(wall_sec + 600);
    assert(sets == 1 && last_set == 1792973400 && writes == 1 && s_time_valid && healthy &&
           s_rtc_trusted && weather == 1 && devices == 1 && display == 1 && clock_wakes == 1);
    reset(false, false, true, 0, ESP_OK);
    monitor_clock_offer_client_time(wall_sec);
    assert(!sets && writes == 1 && s_time_valid && s_rtc_trusted && !weather && !devices &&
           !display && !clock_wakes);
    reset(false, false, false, 0, -1);
    monitor_clock_offer_client_time(wall_sec + 60);
    assert(sets == 1 && writes == 1 && s_time_valid && !healthy && !s_rtc_trusted &&
           weather == 1 && devices == 1 && display == 1 && clock_wakes == 1);
    reset(false, false, true, 0, ESP_OK);
    monitor_clock_offer_client_time(wall_sec + 60);
    assert(sets == 1 && display == 1 && clock_wakes == 1 && !weather && !devices && s_rtc_trusted);
    reset(false, false, false, -1, ESP_OK);
    monitor_clock_offer_client_time(wall_sec + 60);
    assert(sets == 1 && !writes && !s_time_valid && !s_rtc_trusted && !display && !clock_wakes);
    reset(false, false, false, 0, ESP_OK);
    monitor_clock_offer_client_time(0);
    assert(!sets && !writes && !s_time_valid && !s_rtc_trusted);
}
'''
run(code)
