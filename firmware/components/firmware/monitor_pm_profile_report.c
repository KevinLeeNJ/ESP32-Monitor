#include "monitor_pm_profile_report.h"

#include <inttypes.h>
#include <string.h>

bool monitor_pm_snapshot_parse(const char *text, monitor_pm_snapshot_t *out)
{
    if (!text || !out) return false;
    memset(out, 0, sizeof(*out));
    enum { HEADER, LOCKS, MODES, SLEEP } section = HEADER;
    bool have_time = false, have_sleep = false;
    while (*text) {
        const char *newline = strchr(text, '\n');
        size_t length = newline ? (size_t)(newline - text) : strlen(text);
        char line[192];
        if (length >= sizeof(line)) return false;
        memcpy(line, text, length);
        line[length] = '\0';
        text += length + (newline != NULL);
        if (!length) continue;
        if (sscanf(line, "Time since boot up: %" SCNd64 " us", &out->uptime_us) == 1) {
            have_time = true;
        } else if (!strcmp(line, "Lock stats:")) section = LOCKS;
        else if (!strcmp(line, "Mode stats:")) section = MODES;
        else if (!strcmp(line, "Sleep stats:")) section = SLEEP;
        else if (section == LOCKS && strncmp(line, "Name ", 5)) {
            if (out->lock_count == MONITOR_PM_MAX_ROWS) return false;
            monitor_pm_row_t *row = &out->locks[out->lock_count];
            if (sscanf(line, "%15s %15s %d %u %" SCNu32 " %" SCNd64,
                       row->name, row->type, &row->arg, &row->active,
                       &row->count, &row->time_us) != 6 || row->time_us < 0) return false;
            ++out->lock_count;
        } else if (section == MODES && strncmp(line, "Mode ", 5)) {
            if (out->mode_count == 4) return false;
            monitor_pm_row_t *row = &out->modes[out->mode_count];
            if (sscanf(line, "%15s %d M %" SCNd64, row->name, &row->arg, &row->time_us) != 3 ||
                row->time_us < 0) return false;
            ++out->mode_count;
        } else if (section == SLEEP) {
            if (sscanf(line, "light_sleep_counts:%" SCNu32 " light_sleep_reject_counts:%" SCNu32,
                       &out->sleep_count, &out->reject_count) != 2) return false;
            have_sleep = true;
        }
    }
    return have_time && have_sleep && out->uptime_us > 0 && out->mode_count == 4;
}

static bool same_row(const monitor_pm_row_t *a, const monitor_pm_row_t *b)
{
    return a->arg == b->arg && !strcmp(a->name, b->name) && !strcmp(a->type, b->type);
}

static unsigned matches(const monitor_pm_row_t *rows, unsigned count,
                        const monitor_pm_row_t *key, const monitor_pm_row_t **match)
{
    unsigned found = 0;
    for (unsigned i = 0; i < count; ++i) if (same_row(&rows[i], key)) {
        *match = &rows[i];
        ++found;
    }
    return found;
}

bool monitor_pm_report_write(FILE *out, const monitor_pm_snapshot_t *start,
                             const monitor_pm_snapshot_t *end)
{
    const int64_t duration = end->uptime_us - start->uptime_us;
    if (!out || duration <= 0) return false;
    fprintf(out, "=== PM PROFILE: frozen 5-minute window ===\n"
                 "Window(us): %" PRId64 "  start_uptime(us): %" PRId64 "  end_uptime(us): %" PRId64 "\n"
                 "light_sleep_counts: %" PRIu32 "  light_sleep_reject_counts: %" PRIu32 "\n"
                 "PM mode deltas (SLEEP is the PM mode, not measured hardware sleep residency):\n"
                 "Mode            CPU(MHz) Time(us)         Time(%%)\n",
            duration, start->uptime_us, end->uptime_us,
            end->sleep_count - start->sleep_count, end->reject_count - start->reject_count);
    for (unsigned i = 0; i < end->mode_count; ++i) {
        const monitor_pm_row_t *row = &end->modes[i], *before = NULL;
        if (matches(start->modes, start->mode_count, row, &before) != 1 ||
            row->time_us < before->time_us) return false;
        int64_t us = row->time_us - before->time_us;
        fprintf(out, "%-15s %-8d %-16" PRId64 " %.2f\n", row->name, row->arg, us, 100.0 * us / duration);
    }
    fprintf(out, "Lock deltas (overlapping locks must not be added together):\n"
                 "Name            Type            Arg Active Taken      Held(us)         Held(%%) Status\n");
    for (unsigned i = 0; i < end->lock_count; ++i) {
        const monitor_pm_row_t *row = &end->locks[i], *before = NULL, *duplicate = NULL;
        unsigned n = matches(start->locks, start->lock_count, row, &before);
        bool unique = matches(end->locks, end->lock_count, row, &duplicate) == 1 && n <= 1;
        bool valid = unique && (!before || (row->count >= before->count && row->time_us >= before->time_us));
        fprintf(out, "%-15s %-15s %-3d %-6u ", row->name, row->type, row->arg, row->active);
        if (valid) {
            int64_t us = row->time_us - (before ? before->time_us : 0);
            fprintf(out, "%-10" PRIu32 " %-16" PRId64 " %.2f %s\n",
                    row->count - (before ? before->count : 0), us, 100.0 * us / duration,
                    before ? "matched" : "new");
        } else fprintf(out, "N/A        N/A              N/A ambiguous-or-recreated\n");
    }
    for (unsigned i = 0; i < start->lock_count; ++i) {
        const monitor_pm_row_t *unused = NULL;
        if (!matches(end->locks, end->lock_count, &start->locks[i], &unused))
            fprintf(out, "%s %s: removed during window; final counters unavailable\n",
                    start->locks[i].name, start->locks[i].type);
    }
    fprintf(out, "Lock names are truncated by IDF; duplicate identities are not subtracted.\n"
                 "Locks deleted/recreated between snapshots may be unobservable.\n"
                 "Snapshots are sequential, not atomic across all locks/modes.\n"
                 "No periodic profiling output or flash writes inside the window.\n"
                 "=== END PM PROFILE ===\n");
    return !ferror(out);
}
