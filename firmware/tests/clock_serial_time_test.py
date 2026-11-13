"""Apply a measured serial sample at a whole-second RTC boundary."""
from host_harness import REPO, function, run

source = (REPO / 'firmware/components/firmware/monitor_clock.c').read_text(encoding='utf-8')
code = r'''
#include <assert.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <time.h>
#define ESP_OK 0
#define eNoAction 0
#define TAG "clock"
#define ESP_LOGI(...) ((void)0)
#define pdMS_TO_TICKS(ms) ((ms) / 10)
typedef int esp_err_t;
typedef unsigned TickType_t;
#ifndef _WIN32
struct timeval { int64_t tv_sec; int64_t tv_usec; };
#endif
static bool s_rtc_trusted, s_wifi_connected, s_time_valid, healthy;
static void *s_clock_task = (void *)1;
static int64_t mono_us, write_us, written_epoch, system_us;
static int set_result, rtc_result;
static unsigned sets, writes, weather, devices, clock_wakes;
static int64_t esp_timer_get_time(void) { return mono_us; }
static void vTaskDelay(unsigned ticks) { mono_us += ticks * 10000; }
static void esp_rom_delay_us(unsigned us) { mono_us += us; }
static int test_settimeofday(const struct timeval *tv, void *tz) {
    (void)tz; ++sets; system_us = tv->tv_sec * 1000000 + tv->tv_usec;
    return set_result;
}
#define settimeofday test_settimeofday
static esp_err_t monitor_rtc_write_utc(int64_t epoch) {
    ++writes; written_epoch = epoch; write_us = mono_us;
    mono_us += 5000; // I2C work must be included when setting the system clock.
    return rtc_result;
}
static void publish_time_valid(bool valid, bool health) { s_time_valid = valid; healthy = health; }
static void monitor_weather_on_time_valid(void) { ++weather; }
static void monitor_devices_request_refresh(void) { ++devices; }
static void xTaskNotify(void *task, unsigned bits, int action) {
    assert(task == s_clock_task && !bits && action == eNoAction); ++clock_wakes;
}
'''
code += function(source, 'monitor_clock_needs_sync')
code += function(source, 'monitor_clock_apply_serial_time')
code += r'''
static void reset(void) {
    s_rtc_trusted = s_wifi_connected = s_time_valid = healthy = false;
    mono_us = 2450000; write_us = written_epoch = system_us = 0;
    set_result = rtc_result = 0;
    sets = writes = weather = devices = clock_wakes = 0;
}
int main(void) {
    const int64_t epoch = 1792972800100000LL, received = 1000000;
    reset();
    assert(monitor_clock_needs_sync());
    // A 1.45 s parsing/sample-selection delay must not shift the RTC phase.
    assert(monitor_clock_apply_serial_time(epoch, received));
    assert(write_us == 2900000 && written_epoch == 1792972802);
    assert(system_us == 1792972802005000LL && sets == 1 && writes == 1);
    assert(s_time_valid && healthy && !monitor_clock_needs_sync());
    assert(weather == 1 && devices == 1 && clock_wakes == 1);
    reset(); s_time_valid = true; s_rtc_trusted = true;
    assert(monitor_clock_apply_serial_time(epoch, received));
    assert(!weather && !devices && clock_wakes == 1);
    reset(); rtc_result = -1;
    assert(!monitor_clock_apply_serial_time(epoch, received));
    assert(s_time_valid && !healthy && monitor_clock_needs_sync());
    reset(); set_result = -1;
    assert(!monitor_clock_apply_serial_time(epoch, received));
    assert(!s_time_valid && !clock_wakes && monitor_clock_needs_sync());
    reset(); s_wifi_connected = true;
    assert(!monitor_clock_apply_serial_time(epoch, received) && !writes && !sets);
    reset();
    assert(!monitor_clock_apply_serial_time(0, received));
    assert(!monitor_clock_apply_serial_time(4102444800000000LL, received));
    assert(!monitor_clock_apply_serial_time(epoch, mono_us + 1));
    mono_us = received + 15000001;
    assert(!monitor_clock_apply_serial_time(epoch, received) && !writes && !sets);
}
'''
run(code)
