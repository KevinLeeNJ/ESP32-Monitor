"""Run the firmware's page-one handover snapshot with fake producers.

The Client continues the device's wall-clock refresh slots, so its proxy
sources stay pending for a while after a session starts. This checks the
production capture code keeps the cached Standalone weather/AQI/city/devices
visible until the Client supplies each source, then hands ownership over. When
the session ends, the Client's last values seed the direct caches so page one
does not fall back to the pre-session reading while polling waits for its slot.
"""
import re
import sys
from host_harness import REPO, function, run

main = (REPO / 'firmware/main/main.c').read_text(encoding='utf-8')
snapshot_type = re.search(r'typedef struct \{[^}]*\} display_snapshot_t;', main, re.S)
assert snapshot_type, 'display_snapshot_t missing'
cache_type = re.search(r'typedef struct \{[^}]*\} companion_page_cache_t;', main, re.S)
assert cache_type, 'companion_page_cache_t missing'

code = r'''
#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include "monitor_ui.h"
#include "monitor_weather.h"
#include "monitor_sensors.h"

typedef struct {
    bool weather_valid;
    monitor_weather_condition_t weather_condition;
    bool weather_pending;
    char city[32];
    bool aqi_valid;
    bool aqi_pending;
    int aqi;
    bool time_valid;
    int64_t time_utc_ms;
    char time_text[8];
    char date_text[16];
    bool proxy_owned;
    bool devices_valid;
    bool devices_pending;
    monitor_devices_snapshot_t devices;
} monitor_companion_view_t;

static monitor_ui_demo_data_t g_demo;
static monitor_companion_view_t s_companion_view;
static bool s_companion_overlay, s_companion_weather_seen, s_companion_aqi_seen;
static bool s_companion_devices_supplied;
static uint32_t s_companion_devices_version = 1;
static monitor_weather_data_t local_weather;
static monitor_devices_snapshot_t local_devices;
static monitor_sensor_data_t local_sensors;
static monitor_wifi_state_t wifi_state;
static bool serial_active;

bool monitor_serial_active(void) { return serial_active; }
monitor_wifi_state_t monitor_wifi_get_state(void) { return wifi_state; }
bool monitor_pm_profile_active(void) { return false; }
void monitor_devices_get(monitor_devices_snapshot_t *out) { *out = local_devices; }
void monitor_weather_get(monitor_weather_data_t *out) { *out = local_weather; }
void monitor_weather_adopt_cached(monitor_weather_condition_t condition, int aqi, const char *city) {
    if (condition != MONITOR_WEATHER_UNKNOWN) {
        local_weather.condition = condition;
        local_weather.weather_valid = true;
    }
    if (aqi >= 0) { local_weather.aqi = aqi; local_weather.aqi_valid = true; }
    if (city && city[0])
        snprintf(local_weather.city, sizeof local_weather.city, "%s", city);
}
void monitor_devices_adopt_snapshot(const monitor_devices_snapshot_t *snapshot) {
    local_devices = *snapshot;
    local_devices.version = 42;
}
void monitor_sensors_get(monitor_sensor_data_t *out) { *out = local_sensors; }
bool monitor_clock_get_text(char *time_text, size_t time_size, char *date_text, size_t date_size) {
    snprintf(time_text, time_size, "%s", "20:15");
    snprintf(date_text, date_size, "%s", "SAT 1 NOV");
    return true;
}

''' + snapshot_type[0] + '\n' + cache_type[0] + '\n'

code += function(main, 'refresh_monitor_data') + '\n'
code += function(main, 'companion_owns') + '\n'
code += function(main, 'capture_companion_page_cache') + '\n'
code += function(main, 'adopt_companion_page_cache') + '\n'
code += function(main, 'capture_display_snapshot') + r'''

static display_snapshot_t capture(void) {
    display_snapshot_t snapshot;
    memset(&snapshot, 0, sizeof snapshot);
    capture_display_snapshot(&snapshot);
    return snapshot;
}

int main(void) {
    g_demo.weather_condition = MONITOR_WEATHER_CLEAR;
    g_demo.aqi = 42;
    snprintf(g_demo.city_text, sizeof g_demo.city_text, "%s", "Beijing");
    local_weather = (monitor_weather_data_t){.condition = MONITOR_WEATHER_CLEAR, .aqi = 42,
                                             .weather_valid = true, .aqi_valid = true};
    snprintf(local_weather.city, sizeof local_weather.city, "%s", "Beijing");
    local_devices.status = MONITOR_DEVICES_READY;
    local_devices.count = 2;
    local_devices.version = 7;
    snprintf(local_devices.devices[0].name, sizeof local_devices.devices[0].name, "%s", "NAS");
    snprintf(local_devices.devices[1].name, sizeof local_devices.devices[1].name, "%s", "AP");
    local_sensors = (monitor_sensor_data_t){.battery_valid = true, .battery_percent = 80,
                                            .environment_valid = true,
                                            .temperature_tenths_c = 233, .humidity_percent = 51};
    wifi_state = MONITOR_WIFI_CONNECTED;

    display_snapshot_t snapshot = capture();
    assert(snapshot.wifi_state == MONITOR_WIFI_CONNECTED);
    assert(snapshot.weather_condition == MONITOR_WEATHER_CLEAR && snapshot.aqi == 42);
    assert(!strcmp(snapshot.city_text, "Beijing"));
    assert(snapshot.devices.count == 2 && snapshot.devices.version == 7);

    // Takeover while the Client waits for its first slots: keep everything.
    s_companion_overlay = true;
    s_companion_view.weather_pending = s_companion_view.aqi_pending = true;
    s_companion_view.devices_pending = true;
    snapshot = capture();
    assert(snapshot.wifi_state == MONITOR_WIFI_LAN);
    assert(snapshot.weather_condition == MONITOR_WEATHER_CLEAR && snapshot.aqi == 42);
    assert(!strcmp(snapshot.city_text, "Beijing"));
    assert(snapshot.devices.count == 2 && snapshot.devices.version == 7);
    assert(!s_companion_weather_seen && !s_companion_aqi_seen && !s_companion_devices_supplied);

    // LAN uses the same local clock as Standalone, even with an older Client.
    s_companion_view.time_valid = true;
    snprintf(s_companion_view.time_text, sizeof s_companion_view.time_text, "%s", "03:45");
    snprintf(s_companion_view.date_text, sizeof s_companion_view.date_text, "%s", "SUN 2 NOV");
    snapshot = capture();
    assert(!strcmp(snapshot.time_text, "20:15") && !strcmp(snapshot.date_text, "SAT 1 NOV"));
    serial_active = true;
    snapshot = capture();
    assert(!strcmp(snapshot.time_text, "20:15") && !strcmp(snapshot.date_text, "SAT 1 NOV"));
    serial_active = false;

    // First Client data replaces each source.
    s_companion_view.weather_pending = s_companion_view.aqi_pending = false;
    s_companion_view.devices_pending = false;
    s_companion_view.weather_valid = true;
    s_companion_view.weather_condition = MONITOR_WEATHER_FEW_CLOUDS;
    s_companion_view.aqi_valid = true;
    s_companion_view.aqi = 7;
    s_companion_view.devices_valid = true;
    s_companion_view.devices.count = 1;
    s_companion_view.devices.status = MONITOR_DEVICES_READY;
    snprintf(s_companion_view.devices.devices[0].name, sizeof s_companion_view.devices.devices[0].name, "%s", "SRV");
    snapshot = capture();
    assert(snapshot.weather_condition == MONITOR_WEATHER_FEW_CLOUDS && snapshot.aqi == 7);
    assert(snapshot.devices.count == 1 && !strcmp(snapshot.devices.devices[0].name, "SRV"));
    assert(snapshot.devices.version == 1);
    assert(s_companion_weather_seen && s_companion_aqi_seen && s_companion_devices_supplied);

    // After that, an unavailable Client source is authoritative again.
    s_companion_view.weather_valid = s_companion_view.aqi_valid = false;
    memset(&s_companion_view.devices, 0, sizeof s_companion_view.devices);
    s_companion_view.devices.status = MONITOR_DEVICES_UNAVAILABLE;
    snapshot = capture();
    assert(snapshot.weather_condition == MONITOR_WEATHER_UNKNOWN && snapshot.aqi == -1);
    assert(snapshot.devices.count == 0 && snapshot.devices.status == MONITOR_DEVICES_UNAVAILABLE);

    // A Client city replaces the cached one for the session.
    snprintf(s_companion_view.city, sizeof s_companion_view.city, "%s", "Shanghai");
    snapshot = capture();
    assert(!strcmp(snapshot.city_text, "Shanghai"));

    // The Client delivers fresh values again before the session ends.
    s_companion_view.weather_valid = true;
    s_companion_view.weather_condition = MONITOR_WEATHER_OVERCAST;
    s_companion_view.aqi_valid = true;
    s_companion_view.aqi = 55;
    s_companion_view.devices_valid = true;
    s_companion_view.devices.count = 1;
    s_companion_view.devices.status = MONITOR_DEVICES_READY;
    snprintf(s_companion_view.devices.devices[0].name,
             sizeof s_companion_view.devices.devices[0].name, "%s", "SRV");

    // Session end seeds the direct caches before build(NULL) clears the view,
    // so page one keeps the Client's values while polling waits for its slot.
    assert(s_companion_overlay);
    companion_page_cache_t cached;
    capture_companion_page_cache(&cached);
    s_companion_overlay = false;
    s_companion_weather_seen = s_companion_aqi_seen = s_companion_devices_supplied = false;
    s_companion_view.weather_valid = s_companion_view.aqi_valid = false;
    s_companion_view.devices_valid = false;
    memset(&s_companion_view.devices, 0, sizeof s_companion_view.devices);
    s_companion_view.city[0] = '\0';
    adopt_companion_page_cache(&cached);
    snapshot = capture();
    assert(snapshot.wifi_state == MONITOR_WIFI_CONNECTED);
    assert(snapshot.weather_condition == MONITOR_WEATHER_OVERCAST && snapshot.aqi == 55);
    assert(!strcmp(snapshot.city_text, "Shanghai"));
    assert(snapshot.devices.count == 1 && !strcmp(snapshot.devices.devices[0].name, "SRV"));
    assert(snapshot.devices.version == 42);
    puts("Companion handover keeps pending sources cached, adopts Client values, and carries them into Standalone: PASS");
}
'''

run(code, sys.argv[1] if len(sys.argv) > 1 else None)
