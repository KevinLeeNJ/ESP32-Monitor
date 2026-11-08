#pragma once

#include <stdbool.h>
#include "esp_err.h"
#include "monitor_ui.h"

#ifdef __cplusplus
extern "C" {
#endif

typedef struct {
    monitor_weather_condition_t condition;
    int aqi;
    float latitude;
    float longitude;
    /* IP-geolocation city name; empty for manual coordinates or before the
     * first successful lookup. */
    char city[32];
    bool location_valid;
    bool weather_valid;
    bool aqi_valid;
} monitor_weather_data_t;

esp_err_t monitor_weather_start(void);
void monitor_weather_request_refresh(void);
void monitor_weather_on_wifi_connected(void);
void monitor_weather_on_wifi_disconnected(void);
void monitor_weather_on_time_valid(void);
/* While an authenticated companion session owns page one, pause the direct
 * HTTPS producer without starting any location or weather work. Resuming in
 * Standalone stays silent and lets the next wall-clock 900 s slot (:00, :15,
 * :30, :45) fetch; Wi-Fi or time recovery cannot pull that first update
 * forward past the slot. */
void monitor_weather_set_companion_active(bool active);
/* Adopt the Client's last page-one values when a companion session ends, so
 * page one keeps them while the direct producer waits for its next slot. A
 * negative condition or AQI and an empty city keep the standalone value. */
void monitor_weather_adopt_cached(monitor_weather_condition_t condition, int aqi,
                                  const char *city);
/* Milliseconds until the next wall-clock 900 s slot when the current slot was
 * already serviced, otherwise 0 (fetch now). Lets a companion session keep the
 * standalone refresh cadence. */
uint32_t monitor_weather_refresh_delay_ms(void);
void monitor_weather_get(monitor_weather_data_t *out);

#ifdef __cplusplus
}
#endif
