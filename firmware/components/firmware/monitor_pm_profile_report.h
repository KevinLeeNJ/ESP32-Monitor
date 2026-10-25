#pragma once

#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>

#define MONITOR_PM_MAX_ROWS 64

typedef struct {
    char name[16];
    char type[16];
    int arg;
    unsigned active;
    uint32_t count;
    int64_t time_us;
} monitor_pm_row_t;

typedef struct {
    int64_t uptime_us;
    uint32_t sleep_count;
    uint32_t reject_count;
    unsigned lock_count;
    unsigned mode_count;
    monitor_pm_row_t locks[MONITOR_PM_MAX_ROWS];
    monitor_pm_row_t modes[4];
} monitor_pm_snapshot_t;

/* Parse the current IDF dump format. Reject incomplete or overflowing dumps. */
bool monitor_pm_snapshot_parse(const char *text, monitor_pm_snapshot_t *out);
bool monitor_pm_report_write(FILE *out, const monitor_pm_snapshot_t *start,
                             const monitor_pm_snapshot_t *end);
