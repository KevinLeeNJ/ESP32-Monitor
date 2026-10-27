#pragma once

#include <stdint.h>
#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

enum {
    MONITOR_REFRESH_CLOCK = 1U << 0,
    MONITOR_REFRESH_SENSORS = 1U << 1,
    MONITOR_REFRESH_WEATHER = 1U << 2,
    MONITOR_REFRESH_WIFI = 1U << 3,
    MONITOR_REFRESH_DEVICES = 1U << 5,
    MONITOR_REFRESH_RECOVER = 1U << 4,
    MONITOR_REFRESH_LVGL = 1U << 6,
    MONITOR_REFRESH_PAGE_NEXT = 1U << 7,
    MONITOR_REFRESH_PAGE_PREVIOUS = 1U << 8,
};

/* Periodic data stays cached until the next shared clock boundary. Recovery
 * includes initial data, configuration changes and snapshot expiration. */
static inline bool monitor_display_refresh_should_render(uint32_t reasons, bool clock_valid)
{
    return !clock_valid || (reasons & (MONITOR_REFRESH_CLOCK | MONITOR_REFRESH_WIFI |
                                      MONITOR_REFRESH_RECOVER | MONITOR_REFRESH_LVGL |
                                      MONITOR_REFRESH_PAGE_NEXT | MONITOR_REFRESH_PAGE_PREVIOUS)) != 0;
}

/* Register the current task as the sole owner of display rendering. */
void monitor_display_refresh_register(void);
void monitor_display_refresh_request(void);
void monitor_display_refresh_request_reason(uint32_t reason_bits);

#ifdef __cplusplus
}
#endif
