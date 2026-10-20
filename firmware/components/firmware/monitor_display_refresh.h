#pragma once

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

enum {
    MONITOR_REFRESH_CLOCK = 1U << 0,
    MONITOR_REFRESH_SENSORS = 1U << 1,
    MONITOR_REFRESH_WEATHER = 1U << 2,
    MONITOR_REFRESH_WIFI = 1U << 3,
    MONITOR_REFRESH_RECOVER = 1U << 4,
};

/* Register the current task as the sole owner of display rendering. */
void monitor_display_refresh_register(void);
void monitor_display_refresh_request(void);
void monitor_display_refresh_request_reason(uint32_t reason_bits);

#ifdef __cplusplus
}
#endif
