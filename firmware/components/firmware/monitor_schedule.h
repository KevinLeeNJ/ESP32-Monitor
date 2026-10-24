#pragma once

#include <stdbool.h>
#include <stdint.h>

enum {
    MONITOR_SCHEDULE_DEVICES = 1U << 0,
    MONITOR_SCHEDULE_SENSORS = 1U << 1,
    MONITOR_SCHEDULE_WEATHER = 1U << 2,
};

/* Compare wall-clock slots, so wakeups and time corrections never cause
 * duplicate requests within a slot or a burst of catch-up requests. */
static inline uint32_t monitor_schedule_due(int64_t previous_s, int64_t now_s)
{
    uint32_t due = 0;
    if (previous_s >= 0 && now_s < previous_s)
        return MONITOR_SCHEDULE_DEVICES | MONITOR_SCHEDULE_SENSORS | MONITOR_SCHEDULE_WEATHER;
    if (previous_s < 0 || previous_s / 30 != now_s / 30) due |= MONITOR_SCHEDULE_DEVICES;
    /* Sample at :30 to align with one of the API polls and precede the minute roll. */
    if (previous_s < 0 || (previous_s + 30) / 60 != (now_s + 30) / 60)
        due |= MONITOR_SCHEDULE_SENSORS;
    if (previous_s < 0 || previous_s / 900 != now_s / 900) due |= MONITOR_SCHEDULE_WEATHER;
    return due;
}

/* Round up to avoid waking just before the next :00/:30 boundary. */
static inline uint32_t monitor_schedule_delay_ms(int64_t epoch_us)
{
    return (uint32_t)((30000000LL - epoch_us % 30000000LL + 999) / 1000);
}
