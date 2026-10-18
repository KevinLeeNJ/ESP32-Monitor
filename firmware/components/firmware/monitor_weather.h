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
    bool location_valid;
    bool weather_valid;
    bool aqi_valid;
} monitor_weather_data_t;

esp_err_t monitor_weather_start(void);
void monitor_weather_on_wifi_connected(void);
void monitor_weather_get(monitor_weather_data_t *out);

#ifdef __cplusplus
}
#endif
