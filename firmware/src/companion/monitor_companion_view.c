#include "monitor_companion_view.h"
#include <math.h>
#include <stdio.h>
#include <string.h>

#define SHOWN_WEEKS 32
#define SHOWN_DAYS 7
#define DAY_SECONDS 86400

typedef struct {
    const esp32monitor_companion_Heartbeat *heartbeat;
    uint32_t snapshot_seq;
    uint64_t snapshot_elapsed_ms;
    uint64_t heartbeat_elapsed_ms;
} view_clock_t;

static bool usable(const char *state, int64_t age, int64_t expire, bool has_data)
{
    if (!has_data || !expire || (strcmp(state, "healthy") && strcmp(state, "degraded"))) return false;
    return age < expire;
}

static int64_t total_age(bool has_age, int64_t age, uint64_t elapsed_ms)
{
    int64_t total = (has_age && age > 0 ? age : 0) + (int64_t)elapsed_ms;
    return total < 0 ? 0 : total;
}

/* Renew one source only when the heartbeat names it for the snapshot we hold.
 * The client repeats the current lease set on every heartbeat and recomputes
 * age_ms at send time, so a repeated lease keeps aging instead of extending
 * freshness. A missing lease leaves the snapshot-based age untouched. */
static int64_t leased_age(const view_clock_t *clock, const char *path, int64_t fallback)
{
    const esp32monitor_companion_Heartbeat *h = clock->heartbeat;
    if (!h || !clock->snapshot_seq) return fallback;
    for (unsigned i = 0; i < h->source_updates_count; i++) {
        const esp32monitor_companion_SourceLease *lease = &h->source_updates[i];
        if (lease->snapshot_seq != clock->snapshot_seq || strcmp(lease->path, path) ||
            lease->age_ms < 0 || lease->observed_at <= 0 || lease->last_success_at <= 0) continue;
        uint64_t total = (uint64_t)lease->age_ms + clock->heartbeat_elapsed_ms;
        if (total > (uint64_t)INT64_MAX) continue;
        if ((int64_t)total < fallback) fallback = (int64_t)total;
    }
    return fallback;
}

static int64_t section_age(const view_clock_t *clock, const char *path, bool has_age, int64_t age)
{
    return leased_age(clock, path, total_age(has_age, age, clock->snapshot_elapsed_ms));
}

static int64_t days_from_civil(int year, unsigned month, unsigned day)
{
    year -= month <= 2;
    const int era = (year >= 0 ? year : year - 399) / 400;
    const unsigned yoe = (unsigned)(year - era * 400);
    const unsigned doy = (153 * (month + (month > 2 ? (unsigned)-3 : 9)) + 2) / 5 + day - 1;
    const unsigned doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    return (int64_t)era * 146097 + (int64_t)doe - 719468;
}

static void civil_from_days(int64_t days, int *year, unsigned *month, unsigned *day)
{
    days += 719468;
    const int64_t era = (days >= 0 ? days : days - 146096) / 146097;
    const unsigned doe = (unsigned)(days - era * 146097);
    const unsigned yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    const int64_t y = (int64_t)yoe + era * 400;
    const unsigned doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    const unsigned mp = (5 * doy + 2) / 153;
    const unsigned d = doy - (153 * mp + 2) / 5 + 1;
    const unsigned m = mp + (mp < 10 ? 3 : (unsigned)-9);
    *year = (int)(y + (m <= 2));
    *month = m;
    *day = d;
}

static bool parse_date(const char *text, int64_t *days)
{
    static const unsigned month_days[] = {31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31};
    int year;
    unsigned month, day;
    if (!text || sscanf(text, "%4d-%2u-%2u", &year, &month, &day) != 3 ||
        year < 1970 || year > 9998 || month < 1 || month > 12 || day < 1) return false;
    unsigned limit = month_days[month - 1];
    if (month == 2 && year % 4 == 0 && (year % 100 != 0 || year % 400 == 0)) limit = 29;
    if (day > limit) return false;
    *days = days_from_civil(year, month, day);
    return true;
}

/* Show the latest 32 Monday-started columns. Future days in the current week
 * stay empty, matching the provider's own contribution graph. */
static void build_github(const esp32monitor_companion_GitHubSection *section,
                         const view_clock_t *clock, monitor_activity_data_t *activity)
{
    int64_t age = section_age(clock, "github", section->has_age_ms, section->age_ms);
    if (!usable(section->state, age, section->expire_after_ms, section->has_data)) return;
    const esp32monitor_companion_GitHubData *data = &section->data;
    int64_t start;
    size_t levels = strlen(data->levels);
    if (!parse_date(data->start_date, &start) || !data->days || data->days > 371 ||
        levels != data->days) return;
    int64_t last = start + (int64_t)data->days - 1;
    int last_weekday = (int)((last + 4) % 7); /* 0 = Sunday */
    if (last_weekday < 0) last_weekday += 7;
    int days_since_monday = (last_weekday + 6) % 7;
    int64_t end = last + (6 - days_since_monday); /* Sunday of the last week */
    int64_t first = end - (SHOWN_WEEKS * SHOWN_DAYS - 1);
    for (int i = 0; i < SHOWN_WEEKS * SHOWN_DAYS; i++) {
        int64_t day = first + i;
        activity->levels[i] = day >= start && day <= last
            ? (uint8_t)(data->levels[day - start] - '0') : 0;
    }
    int year;
    unsigned month, day;
    civil_from_days(first, &year, &month, &day);
    activity->github_start_year = year;
    activity->github_start_month = (int)month;
    activity->github_start_day = (int)day;
    activity->github_available = true;
}

static bool percent_of(const esp32monitor_companion_QuotaWindow *w, int *percent)
{
    if (!w->has_used_percent || !isfinite(w->used_percent) ||
        w->used_percent < 0 || w->used_percent > 100) return false;
    *percent = (int)(100.0 - w->used_percent + 0.5);
    return true;
}

static int closest_window(const esp32monitor_companion_QuotaData *q, int64_t minutes, int skip)
{
    int best = -1;
    int64_t best_distance = 0;
    for (unsigned i = 0; i < q->windows_count; i++) {
        if ((int)i == skip || !q->windows[i].has_window_minutes) continue;
        int percent;
        if (!percent_of(&q->windows[i], &percent)) continue;
        int64_t distance = q->windows[i].window_minutes - minutes;
        if (distance < 0) distance = -distance;
        if (best < 0 || distance < best_distance) {
            best = (int)i; best_distance = distance;
        }
    }
    return best >= 0 ? best : -1;
}

static void build_quota(const esp32monitor_companion_QuotaSection *section,
                        const view_clock_t *clock, monitor_activity_data_t *activity)
{
    int64_t age = section_age(clock, "codex.quota", section->has_age_ms, section->age_ms);
    if (!usable(section->state, age, section->expire_after_ms, section->has_data)) return;
    const esp32monitor_companion_QuotaData *q = &section->data;
    if (q->windows_count > 4) return;
    int five = closest_window(q, 300, -1);
    int week = closest_window(q, 10080, five);
    for (unsigned i = 0; i < q->windows_count && five < 0; i++) {
        int percent;
        if (percent_of(&q->windows[i], &percent)) five = (int)i;
    }
    for (unsigned i = 0; i < q->windows_count && week < 0; i++) {
        int percent;
        if ((int)i != five && percent_of(&q->windows[i], &percent)) week = (int)i;
    }
    if (five >= 0) (void)percent_of(&q->windows[five], &activity->usage_5h_percent);
    if (week >= 0) (void)percent_of(&q->windows[week], &activity->usage_week_percent);
}

static monitor_task_state_t task_state(const char *state)
{
    static const char *const names[] = {"unknown", "idle", "running", "waiting_input",
                                        "waiting_approval", "completed", "failed", "cancelled"};
    for (unsigned i = 0; i < sizeof names / sizeof names[0]; i++)
        if (!strcmp(state, names[i])) return (monitor_task_state_t)i;
    return MONITOR_TASK_UNKNOWN;
}

static void build_tasks(const esp32monitor_companion_TasksSection *section,
                        const view_clock_t *clock, monitor_activity_data_t *activity)
{
    int64_t age = section_age(clock, "codex.tasks", section->has_age_ms, section->age_ms);
    if (!usable(section->state, age, section->expire_after_ms, section->has_data)) return;
    activity->agent_state = task_state(section->data.state);
}

static void build_music(const esp32monitor_companion_MusicSection *section,
                        const view_clock_t *clock, const esp32monitor_companion_Snapshot *snapshot,
                        monitor_activity_data_t *activity)
{
    int64_t age = section_age(clock, "music", section->has_age_ms, section->age_ms);
    if (!usable(section->state, age, section->expire_after_ms, section->has_data)) return;
    const esp32monitor_companion_MusicData *data = &section->data;
    if (!data->has_current) return;
    const esp32monitor_companion_CurrentMusic *m = &data->current;
    bool playing = !strcmp(m->state, "playing");
    if (!playing && strcmp(m->state, "not_playing")) return;
    activity->music_available = true;
    activity->music_playing = playing;
    activity->music_title = m->has_title ? m->title : NULL;
    activity->music_artist = m->has_artist ? m->artist : NULL;
    activity->music_source = m->has_source ? m->source : NULL;
    if (m->has_duration && isfinite(m->duration) && m->duration >= 0)
        activity->music_duration_seconds = (int)m->duration;
    if (m->has_position && isfinite(m->position) && m->position >= 0) {
        double position = m->position;
        int64_t now = snapshot->sent_at + (int64_t)(clock->snapshot_elapsed_ms / 1000);
        if (playing && m->has_position_observed_at && now > m->position_observed_at)
            position += (double)(now - m->position_observed_at);
        if (m->has_duration && isfinite(m->duration) && position > m->duration)
            position = m->duration;
        activity->music_position_seconds = (int)position;
    }
}

static void build_weather(const esp32monitor_companion_WeatherSection *section,
                          const view_clock_t *clock, monitor_companion_view_t *out)
{
    if (!strcmp(section->state, "starting")) {
        out->weather_pending = true;
        return;
    }
    int64_t age = section_age(clock, "proxy.weather", section->has_age_ms, section->age_ms);
    if (!usable(section->state, age, section->expire_after_ms, section->has_data)) return;
    if (section->data.has_city)
        snprintf(out->city, sizeof out->city, "%s", section->data.city);
    int condition = section->data.condition;
    if (condition < 0 || condition > 15) return;
    out->weather_valid = true;
    out->weather_condition = (monitor_weather_condition_t)condition;
}

static void build_aqi(const esp32monitor_companion_AQISection *section,
                      const view_clock_t *clock, monitor_companion_view_t *out)
{
    if (!strcmp(section->state, "starting")) {
        out->aqi_pending = true;
        return;
    }
    int64_t age = section_age(clock, "proxy.aqi", section->has_age_ms, section->age_ms);
    if (!usable(section->state, age, section->expire_after_ms, section->has_data)) return;
    if (section->data.value > 500) return;
    out->aqi_valid = true;
    out->aqi = (int)section->data.value;
}

static void build_time(const esp32monitor_companion_TimeSection *section,
                       const view_clock_t *clock, monitor_companion_view_t *out)
{
    static const char *const weekdays[] = {"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"};
    static const char *const months[] = {"JAN", "FEB", "MAR", "APR", "MAY", "JUN",
                                         "JUL", "AUG", "SEP", "OCT", "NOV", "DEC"};
    if (!section->has_data || (strcmp(section->state, "healthy") && strcmp(section->state, "degraded")))
        return;
    int64_t unix_ms, offset_minutes;
    const esp32monitor_companion_HeartbeatTime *now =
        clock->heartbeat && clock->heartbeat->has_time ? &clock->heartbeat->time : NULL;
    if (now && now->utc_unix_ms > 0 && now->utc_offset_minutes >= -720 && now->utc_offset_minutes <= 840) {
        /* The heartbeat carries a current clock reading, so time does not need
         * a full snapshot to stay valid between pushes. */
        unix_ms = now->utc_unix_ms + (int64_t)clock->heartbeat_elapsed_ms;
        offset_minutes = now->utc_offset_minutes;
    } else {
        int64_t age = section_age(clock, "proxy.time", section->has_age_ms, section->age_ms);
        if (!usable(section->state, age, section->expire_after_ms, true)) return;
        unix_ms = section->data.utc_unix_ms + (int64_t)clock->snapshot_elapsed_ms;
        offset_minutes = section->data.utc_offset_minutes;
        if (section->data.utc_unix_ms <= 0 ||
            offset_minutes < -720 || offset_minutes > 840) return;
    }
    int64_t seconds = unix_ms / 1000 + offset_minutes * 60;
    int64_t days = seconds / DAY_SECONDS;
    int64_t rest = seconds % DAY_SECONDS;
    if (rest < 0) { rest += DAY_SECONDS; days--; }
    int year;
    unsigned month, day;
    civil_from_days(days, &year, &month, &day);
    int weekday = (int)((days + 4) % 7);
    if (weekday < 0) weekday += 7;
    snprintf(out->time_text, sizeof out->time_text, "%02d:%02d",
             (int)(rest / 3600), (int)((rest % 3600) / 60));
    snprintf(out->date_text, sizeof out->date_text, "%s %02u %s",
             weekdays[weekday], day, months[month - 1]);
    out->time_utc_ms = unix_ms;
    out->time_valid = true;
}

static void build_devices(const esp32monitor_companion_DevicesSection *section,
                          const view_clock_t *clock, monitor_companion_view_t *out)
{
    monitor_devices_snapshot_t *devices = &out->devices;
    devices->status = MONITOR_DEVICES_UNAVAILABLE;
    if (!strcmp(section->state, "auth_required") || !strcmp(section->state, "permission_denied")) {
        devices->status = MONITOR_DEVICES_AUTH_ERROR;
        return;
    }
    if (!strcmp(section->state, "disabled")) {
        devices->status = MONITOR_DEVICES_NOT_CONFIGURED;
        return;
    }
    if (!strcmp(section->state, "starting")) {
        devices->status = MONITOR_DEVICES_WAITING;
        out->devices_pending = true;
        return;
    }
    if (!section->has_data || section->data.device_count < 0 ||
        section->data.device_count > MONITOR_DEVICES_MAX ||
        section->data.device_count != (int)section->data.devices_count) return;
    int64_t age = section_age(clock, "proxy.devices", section->has_age_ms, section->age_ms);
    if (!usable(section->state, age, section->expire_after_ms, true)) {
        if (!strcmp(section->state, "error")) devices->status = MONITOR_DEVICES_STALE;
        return;
    }
    devices->count = (uint8_t)section->data.device_count;
    out->devices_valid = true;
    devices->status = age >= section->stale_after_ms ? MONITOR_DEVICES_STALE : MONITOR_DEVICES_READY;
    for (unsigned i = 0; i < devices->count; i++) {
        const esp32monitor_companion_DeviceCard *card = &section->data.devices[i];
        monitor_device_t *target = &devices->devices[i];
        snprintf(target->name, sizeof target->name, "%s", card->name);
        snprintf(target->uptime, sizeof target->uptime, "%s", card->uptime);
        if (card->metric_count < 0 || card->metric_count > 3 ||
            card->metric_count != (int)card->metrics_count) continue;
        target->metric_count = (uint8_t)card->metric_count;
        for (unsigned j = 0; j < target->metric_count; j++) {
            const esp32monitor_companion_Metric *metric = &card->metrics[j];
            monitor_device_metric_t *item = &target->metrics[j];
            snprintf(item->name, sizeof item->name, "%s", metric->name);
            snprintf(item->value, sizeof item->value, "%s", metric->value);
            if (metric->has_percent && metric->percent >= 0 && metric->percent <= 100) {
                item->percent_valid = true;
                item->percent = metric->percent;
            }
        }
    }
}

static void empty_activity(monitor_activity_data_t *activity)
{
    memset(activity, 0, sizeof *activity);
    activity->agent_state = MONITOR_TASK_UNKNOWN;
    activity->usage_5h_percent = -1;
    activity->usage_week_percent = -1;
    activity->music_position_seconds = -1;
    activity->music_duration_seconds = -1;
}

void monitor_companion_view_build(const esp32monitor_companion_Snapshot *snapshot,
                                  const esp32monitor_companion_Heartbeat *heartbeat,
                                  uint64_t snapshot_elapsed_ms, uint64_t heartbeat_elapsed_ms,
                                  monitor_companion_view_t *out)
{
    if (!out) return;
    memset(out, 0, sizeof *out);
    empty_activity(&out->activity);
    out->weather_condition = MONITOR_WEATHER_UNKNOWN;
    out->aqi = -1;
    snprintf(out->time_text, sizeof out->time_text, "--:--");
    snprintf(out->date_text, sizeof out->date_text, "SYNCING");
    out->devices.status = MONITOR_DEVICES_WAITING;
    if (!snapshot || !snapshot->protocol_version) return;
    view_clock_t clock = { heartbeat, snapshot->seq, snapshot_elapsed_ms, heartbeat_elapsed_ms };

    build_github(&snapshot->sections.github, &clock, &out->activity);
    if (snapshot->sections.codex.has_data) {
        build_quota(&snapshot->sections.codex.data.quota, &clock, &out->activity);
        build_tasks(&snapshot->sections.codex.data.tasks, &clock, &out->activity);
    }
    build_music(&snapshot->sections.music, &clock, snapshot, &out->activity);
    if (!snapshot->sections.proxy.has_data ||
        !strcmp(snapshot->sections.proxy.state, "disabled")) return;
    /* Each proxy source owns its own age; one expired source must not hide the
     * others, so the aggregate age no longer gates the whole overlay. */
    out->proxy_owned = true;
    const esp32monitor_companion_ProxyData *proxy = &snapshot->sections.proxy.data;
    build_weather(&proxy->weather, &clock, out);
    build_aqi(&proxy->aqi, &clock, out);
    build_time(&proxy->time, &clock, out);
    build_devices(&proxy->devices, &clock, out);
}
