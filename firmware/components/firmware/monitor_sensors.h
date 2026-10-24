#pragma once

#include <stdbool.h>
#include "esp_err.h"

#ifdef __cplusplus
extern "C" {
#endif

typedef struct {
    int battery_percent;
    int temperature_tenths_c;
    int humidity_percent;
    float battery_voltage;
    bool battery_valid;
    bool environment_valid;
} monitor_sensor_data_t;

esp_err_t monitor_sensors_start(void);
void monitor_sensors_request_sample(void);
void monitor_sensors_request_sample_deferred(void);
void monitor_sensors_get(monitor_sensor_data_t *out);

#ifdef __cplusplus
}
#endif
