#pragma once

#include <stdbool.h>
#include <stdint.h>
#include "esp_err.h"
#include "driver/i2c_master.h"

#ifdef __cplusplus
extern "C" {
#endif

typedef struct {
    bool present;
    bool time_valid;
    bool oscillator_stopped;
    bool running;
    int64_t utc_epoch_s;
} monitor_rtc_sample_t;

esp_err_t monitor_rtc_init(i2c_master_bus_handle_t bus);
esp_err_t monitor_rtc_read(monitor_rtc_sample_t *out);
esp_err_t monitor_rtc_write_utc(int64_t utc_epoch_s);

#ifdef __cplusplus
}
#endif
