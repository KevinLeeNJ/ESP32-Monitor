#pragma once
#include "companion.pb.h"
#include "monitor_devices_data.h"
#include "monitor_ui.h"
#include <stdbool.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct {
    /* Page two. String pointers alias the snapshot and are valid only while
     * the caller keeps that snapshot unchanged. */
    monitor_activity_data_t activity;
    /* Page one proxy overlay. Local battery and environment readings are
     * never replaced. */
    bool weather_valid;
    monitor_weather_condition_t weather_condition;
    /* The proxy worker exists but has not reached its first scheduled fetch.
     * Page one keeps the cached standalone values until the slot arrives. */
    bool weather_pending;
    /* IP-geolocation city name; empty when the client has none. */
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

/* A NULL snapshot clears every companion-owned value. Ages advance from the
 * last complete snapshot; a heartbeat renews only the sources named in its
 * source_updates (and its own time reading), never the whole snapshot. */
void monitor_companion_view_build(const esp32monitor_companion_Snapshot *snapshot,
                                  const esp32monitor_companion_Heartbeat *heartbeat,
                                  uint64_t snapshot_elapsed_ms, uint64_t heartbeat_elapsed_ms,
                                  monitor_companion_view_t *out);

#ifdef __cplusplus
}
#endif
