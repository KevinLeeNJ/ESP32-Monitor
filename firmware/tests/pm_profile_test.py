"""Run production PM capture/report logic with deterministic time, USB and NVS."""
from pathlib import Path
import re
from host_harness import run

root = Path(__file__).resolve().parents[2]
directory = root / 'firmware/components/firmware'
report = (directory / 'monitor_pm_profile_report.c').read_text()
profile = (directory / 'monitor_pm_profile.c').read_text()
profile = re.sub(r'^#include[^\n]*\n', '', profile, flags=re.M)

prefix = r'''
#include <assert.h>
#include <stdarg.h>
#include <setjmp.h>
#include <stdlib.h>
#include <string.h>
#define CONFIG_MONITOR_PM_PROFILE 1
#include "monitor_pm_profile.h"
#define CONFIG_PM_PROFILING 1
#define CONFIG_ESP_PHY_MAC_BB_PD 1
#define portMUX_INITIALIZER_UNLOCKED 0
#define portENTER_CRITICAL(p) ((void)(p))
#define portEXIT_CRITICAL(p) ((void)(p))
#define pdTRUE 1
#define pdPASS 1
#define portMAX_DELAY UINT32_MAX
#define pdMS_TO_TICKS(ms) (ms)
#define ESP_LOGE(...) (++error_logs)
#define ESP_LOGW(...) (++warning_logs)
#define NVS_READONLY 0
#define NVS_READWRITE 1
#define MALLOC_CAP_SPIRAM 1
#define MALLOC_CAP_8BIT 2
#define heap_caps_calloc(n, size, caps) calloc(n, size)
typedef int portMUX_TYPE;
typedef uint32_t TickType_t;
typedef void *TaskHandle_t;
typedef int nvs_handle_t;
static int error_logs, warning_logs, captures, refreshes, saves, commits, prints, notifications;
static int64_t now = 10000000;
static bool usb, malformed, fail_save, inject_hold, attach_during_capture;
static char stored[16000];
static void (*worker)(void *);
static unsigned worker_waits;
static jmp_buf done;
#define esp_err_to_name(err) "fake error"
static bool usb_serial_jtag_is_connected(void) { return usb; }
static int64_t esp_timer_get_time(void) { return now; }
static void monitor_display_refresh_request(void) { ++refreshes; }
static uint32_t ulTaskNotifyTake(int clear, uint32_t ticks) {
    (void)clear;
    if (ticks == portMAX_DELAY) {
        if (++worker_waits > 1) longjmp(done, 1);
        return 1;
    }
    if (!ticks) return 0;
    assert(monitor_pm_profile_active());
    if (inject_hold) {
        inject_hold = false;
        now += 1000000;
        monitor_pm_profile_key_hold();
        return 1;
    }
    if (attach_during_capture) usb = true;
    now += (int64_t)ticks * 1000;
    return 0;
}
static void xTaskNotifyGive(TaskHandle_t task) { assert(task); ++notifications; }
static int xTaskCreate(void (*fn)(void *), const char *name, unsigned stack,
                       void *arg, unsigned priority, TaskHandle_t *out) {
    (void)name; (void)stack; (void)arg; (void)priority;
    worker = fn; *out = (void *)1; return pdPASS;
}
static int nvs_open(const char *ns, int mode, nvs_handle_t *out) {
    (void)mode; assert(!strcmp(ns, "pm_profile")); *out = 1; return ESP_OK;
}
static int nvs_get_blob(nvs_handle_t handle, const char *key, void *out, size_t *len) {
    (void)handle; assert(!strcmp(key, "report"));
    if (!stored[0]) return ESP_FAIL;
    size_t needed = strlen(stored) + 1;
    if (out) { assert(*len >= needed); memcpy(out, stored, needed); }
    *len = needed; return ESP_OK;
}
static int nvs_set_blob(nvs_handle_t handle, const char *key, const void *blob, size_t len) {
    const char *value = blob;
    assert(len == strlen(value) + 1);
    (void)handle; assert(!strcmp(key, "report"));
    assert(!monitor_pm_profile_active() && captures % 2 == 0);
    ++saves;
    if (fail_save) return ESP_FAIL;
    assert(strlen(value) < sizeof(stored)); strcpy(stored, value); return ESP_OK;
}
static int nvs_commit(nvs_handle_t handle) { (void)handle; ++commits; return ESP_OK; }
static void nvs_close(nvs_handle_t handle) { (void)handle; }
static int fake_puts(const char *text) {
    assert(!monitor_pm_profile_active() && strstr(text, "END PM PROFILE"));
    ++prints; return 0;
}
static int fake_printf(const char *format, ...) { (void)format; return 0; }
static int esp_pm_dump_locks(FILE *out) {
    assert(out != stdout && out != stderr); // Never print live counters to UART.
    ++captures;
    if (malformed) { fputs("incomplete SDK dump", out); return ESP_OK; }
    fprintf(out, "Time since boot up: %lld us\nLock stats:\n"
                 "Name            Type            Arg Active Total_count Time(us) Time(%%)\n"
                 "render          CPU_FREQ_MAX    0   0      %u %lld 1%%\n"
                 "Mode stats:\nMode CPU_freq Time(us) Time(%%)\n"
                 "SLEEP    40 M       %lld 80%%\n"
                 "APB_MIN  40 M       %lld 10%%\n"
                 "APB_MAX  80 M       %lld 5%%\n"
                 "CPU_MAX  240M       %lld 5%%\n"
                 "Sleep stats:\nlight_sleep_counts:%u light_sleep_reject_counts:%u\n",
            (long long)now, (unsigned)(now / 1000000), (long long)(now / 100),
            (long long)(now * 8 / 10), (long long)(now / 10),
            (long long)(now / 20), (long long)(now / 20),
            (unsigned)(now / 100000), (unsigned)(now / 10000000));
    return ESP_OK;
}
#define puts fake_puts
#define printf fake_printf
'''

tests = r'''
#undef puts
#undef printf
static void gesture(void) {
    worker_waits = 0;
    if (!setjmp(done)) worker(NULL);
}
static void test_parser_and_deltas(void) {
    monitor_pm_snapshot_t a, b;
    assert(capture_snapshot(&a) == ESP_OK);
    now += 300000000;
    assert(capture_snapshot(&b) == ESP_OK);
    char *text = NULL; size_t size = 0;
    FILE *out = open_memstream(&text, &size);
    assert(monitor_pm_report_write(out, &a, &b)); fclose(out);
    assert(strstr(text, "Window(us): 300000000"));
    assert(strstr(text, "light_sleep_counts: 3000"));
    assert(strstr(text, "240000000") && strstr(text, "80.00"));
    assert(strstr(text, "3000000") && strstr(text, "matched"));
    free(text);
    // Identical/truncated lock names must never produce invented deltas.
    a.locks[1] = a.locks[0]; a.lock_count = 2;
    b.locks[1] = b.locks[0]; b.lock_count = 2;
    out = open_memstream(&text, &size);
    assert(monitor_pm_report_write(out, &a, &b)); fclose(out);
    assert(strstr(text, "ambiguous-or-recreated")); free(text);
    b.locks[1] = b.locks[0]; strcpy(b.locks[1].name, "new_lock");
    out = open_memstream(&text, &size);
    assert(monitor_pm_report_write(out, &a, &b)); fclose(out);
    assert(strstr(text, "new\n")); free(text);
    a.locks[1] = a.locks[0]; strcpy(a.locks[1].name, "removed");
    out = open_memstream(&text, &size);
    assert(monitor_pm_report_write(out, &a, &b)); fclose(out);
    assert(strstr(text, "removed during window")); free(text);
    assert(!monitor_pm_snapshot_parse("Time since boot up: 1 us\n", &a));
    out = tmpfile(); assert(out);
    assert(!monitor_pm_report_write(out, &b, &b)); fclose(out);
}
int main(void) {
    test_parser_and_deltas();
    captures = 0; now = 10000000;
    assert(monitor_pm_profile_init() == ESP_OK && worker);
    assert(!monitor_pm_profile_active());
    monitor_pm_profile_key_hold(); assert(notifications == 1);
    inject_hold = true;
    gesture();
    assert(now == 310000000 && captures == 2 && !monitor_pm_profile_active());
    assert(notifications == 1); // Live gesture ignored; no restart or early output.
    assert(saves == 1 && commits == 1 && refreshes == 2 && prints == 0);
    assert(s_report && s_persisted && !strcmp(stored, s_report));
    assert(strstr(s_report, "Window(us): 300000000"));
    char *frozen = strdup(s_report); assert(frozen);
    // USB attachment and later uptime must not alter the captured report.
    usb = true; now += 700000000;
    gesture(); gesture();
    assert(prints == 2 && captures == 2 && saves == 1 && !strcmp(s_report, frozen));
    // Simulate monitor resetting the MCU: reload the completed NVS result.
    free(s_report); s_report = NULL; s_task = NULL; s_persisted = false;
    assert(monitor_pm_profile_init() == ESP_OK && !strcmp(s_report, frozen));
    gesture(); assert(prints == 3 && captures == 2);
    // A disconnected gesture starts a new run and replaces the report only at end.
    usb = false; attach_during_capture = true;
    gesture();
    assert(captures == 4 && saves == 2 && strstr(s_report, "WARNING: USB host"));
    attach_during_capture = false; usb = false; fail_save = true;
    gesture();
    assert(s_report && !s_persisted && warning_logs == 1);
    // Parse failure keeps the previous good result and never leaves UI active.
    malformed = true;
    free(frozen); frozen = strdup(s_report);
    gesture();
    assert(!monitor_pm_profile_active() && !strcmp(frozen, s_report) && error_logs == 1);
    free(frozen); free(s_report);
    puts("PM profile: 5-minute window, frozen USB output, NVS reset recovery, live holds, malformed dumps and storage failure passed");
}
'''

run(report + prefix + profile + tests)

# Production builds must link without any capture/report implementation.
run(r'''
#include <assert.h>
#include "monitor_pm_profile.h"
int main(void) {
    assert(monitor_pm_profile_init() == ESP_OK);
    monitor_pm_profile_key_hold();
    assert(!monitor_pm_profile_active());
}
''')
