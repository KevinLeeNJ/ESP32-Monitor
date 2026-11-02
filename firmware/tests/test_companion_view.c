#include "monitor_companion_view.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>

static void base(esp32monitor_companion_Snapshot *s)
{
    memset(s, 0, sizeof *s);
    s->protocol_version = 1;
    strcpy(s->host_id, "11111111-1111-4111-8111-111111111111");
    strcpy(s->device_id, "22222222-2222-4222-8222-222222222222");
    strcpy(s->session_id, "33333333-3333-4333-8333-333333333333");
    strcpy(s->config_epoch, "44444444-4444-4444-8444-444444444444");
    s->config_revision = 1;
    s->seq = 1;
    s->sent_at = 1800000000;
    strcpy(s->sections.github.state, "disabled");
    s->sections.github.stale_after_ms = 86400000;
    s->sections.github.expire_after_ms = 604800000;
    strcpy(s->sections.codex.state, "disabled");
    s->sections.codex.stale_after_ms = 15000;
    s->sections.codex.expire_after_ms = 15000;
    strcpy(s->sections.music.state, "disabled");
    s->sections.music.stale_after_ms = 15000;
    s->sections.music.expire_after_ms = 15000;
    strcpy(s->sections.proxy.state, "disabled");
    s->sections.proxy.stale_after_ms = 15000;
    s->sections.proxy.expire_after_ms = 15000;
}

static void github_full_week(void)
{
    esp32monitor_companion_Snapshot s;
    base(&s);
    esp32monitor_companion_GitHubSection *g = &s.sections.github;
    strcpy(g->state, "healthy");
    g->stale_after_ms = 86400000; g->expire_after_ms = 604800000;
    g->has_data = true;
    strcpy(g->data.login, "monitor");
    strcpy(g->data.start_date, "2026-03-16"); /* Monday */
    g->data.days = 224;
    memset(g->data.levels, '0', 224);
    g->data.levels[0] = '4';
    g->data.levels[223] = '2';
    g->data.levels[224] = 0;

    monitor_companion_view_t view;
    monitor_companion_view_build(&s, NULL, 0, 0, &view);
    assert(view.activity.github_available);
    assert(view.activity.github_start_year == 2026);
    assert(view.activity.github_start_month == 3);
    assert(view.activity.github_start_day == 16);
    assert(view.activity.levels[0] == 4);
    assert(view.activity.levels[223] == 2);
}

static void github_partial_week(void)
{
    esp32monitor_companion_Snapshot s;
    base(&s);
    esp32monitor_companion_GitHubSection *g = &s.sections.github;
    strcpy(g->state, "healthy");
    g->has_data = true;
    strcpy(g->data.start_date, "2026-03-16");
    g->data.days = 227; /* ends Wednesday 2026-10-28 */
    memset(g->data.levels, '0', 227);
    g->data.levels[0] = '4';  /* dropped: outside the latest 32 columns */
    g->data.levels[7] = '3';
    g->data.levels[226] = '2';
    g->data.levels[227] = 0;

    monitor_companion_view_t view;
    monitor_companion_view_build(&s, NULL, 0, 0, &view);
    assert(view.activity.github_available);
    assert(view.activity.github_start_year == 2026);
    assert(view.activity.github_start_month == 3);
    assert(view.activity.github_start_day == 23); /* next Monday */
    assert(view.activity.levels[0] == 3);
    assert(view.activity.levels[219] == 2);
    assert(view.activity.levels[220] == 0);
    assert(view.activity.levels[223] == 0);
}

static void sections(void)
{
    esp32monitor_companion_Snapshot s;
    base(&s);
    esp32monitor_companion_CodexSection *cx = &s.sections.codex;
    strcpy(cx->state, "healthy");
    cx->has_data = true;
    esp32monitor_companion_QuotaSection *q = &cx->data.quota;
    strcpy(q->state, "healthy");
    q->stale_after_ms = 180000; q->expire_after_ms = 300000;
    q->has_data = true;
    q->data.windows_count = 3;
    strcpy(q->data.windows[0].bucket_id, "codex");
    q->data.windows[0].has_window_minutes = true;
    q->data.windows[0].window_minutes = 10080;
    q->data.windows[0].has_used_percent = true;
    q->data.windows[0].used_percent = 24.4;
    strcpy(q->data.windows[1].bucket_id, "codex");
    q->data.windows[1].has_window_minutes = true;
    q->data.windows[1].window_minutes = 300;
    q->data.windows[1].has_used_percent = true;
    q->data.windows[1].used_percent = 36.6;
    strcpy(q->data.windows[2].bucket_id, "extra");
    q->data.windows[2].has_window_minutes = true;
    q->data.windows[2].window_minutes = 60;
    esp32monitor_companion_TasksSection *t = &cx->data.tasks;
    strcpy(t->state, "healthy");
    t->stale_after_ms = 15000; t->expire_after_ms = 15000;
    t->has_data = true;
    strcpy(t->data.adapter, "codex");
    strcpy(t->data.scope, "local");
    strcpy(t->data.state, "waiting_approval");

    esp32monitor_companion_MusicSection *m = &s.sections.music;
    strcpy(m->state, "healthy");
    m->has_data = true;
    strcpy(m->data.adapter, "spotify");
    strcpy(m->data.coverage, "full");
    m->data.has_current = true;
    strcpy(m->data.current.state, "playing");
    m->data.current.has_title = true;
    strcpy(m->data.current.title, "Blue");
    m->data.current.has_artist = true;
    strcpy(m->data.current.artist, "Yung Kai");
    m->data.current.has_source = true;
    strcpy(m->data.current.source, "Spotify");
    m->data.current.has_duration = true;
    m->data.current.duration = 225;
    m->data.current.has_position = true;
    m->data.current.position = 88;
    m->data.current.has_position_observed_at = true;
    m->data.current.position_observed_at = s.sent_at - 10;
    m->has_age_ms = true;
    m->age_ms = 14000;

    monitor_companion_view_t view;
    monitor_companion_view_build(&s, NULL, 0, 0, &view);
    assert(view.activity.usage_5h_percent == 63);
    assert(view.activity.usage_week_percent == 76);
    assert(view.activity.agent_state == MONITOR_TASK_WAITING_APPROVAL);
    assert(view.activity.music_available && view.activity.music_playing);
    assert(!strcmp(view.activity.music_title, "Blue"));
    assert(!strcmp(view.activity.music_artist, "Yung Kai"));
    assert(!strcmp(view.activity.music_source, "Spotify"));
    assert(view.activity.music_duration_seconds == 225);
    assert(view.activity.music_position_seconds == 98);

    q->data.windows[0].used_percent = 0;
    q->data.windows[1].used_percent = 100;
    monitor_companion_view_build(&s, NULL, 0, 0, &view);
    assert(view.activity.usage_5h_percent == 0);
    assert(view.activity.usage_week_percent == 100);

    /* Expiry advances with elapsed time and must not be revived by a heartbeat. */
    monitor_companion_view_build(&s, NULL, 2000, 0, &view);
    assert(!view.activity.music_available);
    assert(view.activity.music_position_seconds == -1);

    /* A missing health value keeps the card empty rather than inventing data. */
    m->has_data = false;
    monitor_companion_view_build(&s, NULL, 0, 0, &view);
    assert(!view.activity.music_available);
}

static void proxy(void)
{
    esp32monitor_companion_Snapshot s;
    base(&s);
    strcpy(s.sections.proxy.state, "healthy");
    s.sections.proxy.has_data = true;
    esp32monitor_companion_ProxyData *p = &s.sections.proxy.data;
    strcpy(p->config_epoch, s.config_epoch);
    p->config_revision = 1;

    strcpy(p->weather.state, "healthy");
    p->weather.stale_after_ms = 1800000; p->weather.expire_after_ms = 7200000;
    p->weather.has_data = true;
    p->weather.data.condition = 4;
    strcpy(p->aqi.state, "healthy");
    p->aqi.stale_after_ms = 1800000; p->aqi.expire_after_ms = 7200000;
    p->aqi.has_data = true;
    p->aqi.data.value = 42;

    strcpy(p->time.state, "healthy");
    p->time.stale_after_ms = 45000; p->time.expire_after_ms = 60000;
    p->time.has_data = true;
    p->time.data.utc_unix_ms = 1800000000000LL;
    p->time.data.utc_offset_minutes = 480;
    strcpy(p->time.data.source, "system_clock");

    strcpy(p->devices.state, "healthy");
    p->devices.stale_after_ms = 45000; p->devices.expire_after_ms = 90000;
    p->devices.has_data = true;
    p->devices.data.device_count = 2;
    p->devices.data.devices_count = 2;
    strcpy(p->devices.data.devices[0].name, "NAS");
    strcpy(p->devices.data.devices[0].uptime, "3d 4h");
    p->devices.data.devices[0].metric_count = 3;
    p->devices.data.devices[0].metrics_count = 3;
    strcpy(p->devices.data.devices[0].metrics[0].name, "CPU");
    strcpy(p->devices.data.devices[0].metrics[0].value, "12%");
    p->devices.data.devices[0].metrics[0].has_percent = true;
    p->devices.data.devices[0].metrics[0].percent = 12;
    strcpy(p->devices.data.devices[1].name, "PI");
    strcpy(p->devices.data.devices[1].uptime, "6h");
    p->devices.data.devices[1].metric_count = 3;
    p->devices.data.devices[1].metrics_count = 3;

    monitor_companion_view_t view;
    monitor_companion_view_build(&s, NULL, 0, 0, &view);
    assert(view.proxy_owned);
    assert(view.weather_valid && view.weather_condition == MONITOR_WEATHER_LIGHT_RAIN);
    assert(view.aqi_valid && view.aqi == 42);
    assert(view.time_valid);
    assert(!strcmp(view.time_text, "16:00"));
    assert(!strcmp(view.date_text, "FRI 15 JAN"));
    assert(view.devices.count == 2);
    assert(view.devices.status == MONITOR_DEVICES_READY);
    assert(!strcmp(view.devices.devices[0].name, "NAS"));
    assert(view.devices.devices[0].metric_count == 3);
    assert(view.devices.devices[0].metrics[0].percent_valid);
    assert(view.devices.devices[0].metrics[0].percent == 12);
    assert(!strcmp(view.devices.devices[1].name, "PI"));

    strcpy(p->devices.state, "auth_required");
    p->devices.has_data = false;
    monitor_companion_view_build(&s, NULL, 0, 0, &view);
    assert(view.proxy_owned);
    assert(view.devices.status == MONITOR_DEVICES_AUTH_ERROR);

    /* Expired devices data must clear rather than stay displayed. */
    strcpy(p->devices.state, "healthy");
    p->devices.has_data = true;
    p->devices.stale_after_ms = 45000;
    p->devices.expire_after_ms = 90000;
    p->devices.has_age_ms = true;
    p->devices.age_ms = 89000;
    monitor_companion_view_build(&s, NULL, 2000, 0, &view);
    assert(view.devices.count == 0);
    assert(view.devices.status == MONITOR_DEVICES_UNAVAILABLE);
}

static void leases(void)
{
    esp32monitor_companion_Snapshot s;
    base(&s);
    esp32monitor_companion_MusicSection *m = &s.sections.music;
    strcpy(m->state, "healthy");
    m->stale_after_ms = 15000; m->expire_after_ms = 15000;
    m->has_data = true;
    strcpy(m->data.adapter, "spotify");
    strcpy(m->data.coverage, "full");
    m->data.has_current = true;
    strcpy(m->data.current.state, "playing");
    m->data.current.has_title = true;
    strcpy(m->data.current.title, "Blue");
    m->data.current.has_duration = true;
    m->data.current.duration = 225;
    m->data.current.has_position = true;
    m->data.current.position = 88;
    m->data.current.has_position_observed_at = true;
    m->data.current.position_observed_at = s.sent_at - 10;
    m->has_age_ms = true;
    m->age_ms = 14000;

    esp32monitor_companion_Heartbeat hb;
    memset(&hb, 0, sizeof hb);
    hb.protocol_version = 1;
    strcpy(hb.host_id, s.host_id);
    strcpy(hb.device_id, s.device_id);
    strcpy(hb.session_id, s.session_id);
    hb.seq = 10;
    hb.has_last_snapshot_seq = true;
    hb.last_snapshot_seq = s.seq;
    hb.source_updates_count = 1;
    strcpy(hb.source_updates[0].path, "music");
    hb.source_updates[0].snapshot_seq = s.seq;
    hb.source_updates[0].observed_at = s.sent_at - 1;
    hb.source_updates[0].last_success_at = s.sent_at - 1;
    hb.source_updates[0].age_ms = 1000;

    monitor_companion_view_t view;
    monitor_companion_view_build(&s, NULL, 2000, 0, &view);
    assert(!view.activity.music_available);
    monitor_companion_view_build(&s, &hb, 2000, 0, &view);
    assert(view.activity.music_available && view.activity.music_playing);
    assert(view.activity.music_position_seconds == 100);

    /* A lease only renews the snapshot it names. */
    hb.source_updates[0].snapshot_seq = s.seq + 1;
    monitor_companion_view_build(&s, &hb, 2000, 0, &view);
    assert(!view.activity.music_available);
    hb.source_updates[0].snapshot_seq = s.seq;

    /* Zero-filled timestamps from a malformed lease are ignored. */
    hb.source_updates[0].last_success_at = 0;
    monitor_companion_view_build(&s, &hb, 2000, 0, &view);
    assert(!view.activity.music_available);
    hb.source_updates[0].last_success_at = s.sent_at - 1;

    /* The leased age keeps advancing with heartbeat time. */
    monitor_companion_view_build(&s, &hb, 14000, 14000, &view);
    assert(!view.activity.music_available);
    monitor_companion_view_build(&s, &hb, 14000, 13000, &view);
    assert(view.activity.music_available);
}

static void devices_lease(void)
{
    esp32monitor_companion_Snapshot s;
    base(&s);
    strcpy(s.sections.proxy.state, "healthy");
    s.sections.proxy.has_data = true;
    esp32monitor_companion_ProxyData *p = &s.sections.proxy.data;
    strcpy(p->config_epoch, s.config_epoch);
    p->config_revision = 1;
    strcpy(p->devices.state, "healthy");
    p->devices.stale_after_ms = 45000;
    p->devices.expire_after_ms = 90000;
    p->devices.has_data = true;
    p->devices.has_age_ms = true;
    p->devices.age_ms = 89000;
    p->devices.data.device_count = 1;
    p->devices.data.devices_count = 1;
    strcpy(p->devices.data.devices[0].name, "NAS");
    strcpy(p->devices.data.devices[0].uptime, "3d");

    esp32monitor_companion_Heartbeat hb;
    memset(&hb, 0, sizeof hb);
    hb.protocol_version = 1;
    hb.source_updates_count = 1;
    strcpy(hb.source_updates[0].path, "proxy.devices");
    hb.source_updates[0].snapshot_seq = s.seq;
    hb.source_updates[0].observed_at = s.sent_at;
    hb.source_updates[0].last_success_at = s.sent_at;
    hb.source_updates[0].age_ms = 30000;

    monitor_companion_view_t view;
    monitor_companion_view_build(&s, NULL, 2000, 0, &view);
    assert(view.devices.count == 0);
    monitor_companion_view_build(&s, &hb, 2000, 0, &view);
    assert(view.devices.count == 1 && view.devices.status == MONITOR_DEVICES_READY);
    hb.source_updates[0].age_ms = 46000;
    monitor_companion_view_build(&s, &hb, 2000, 0, &view);
    assert(view.devices.count == 1 && view.devices.status == MONITOR_DEVICES_STALE);
}

static void heartbeat_time(void)
{
    esp32monitor_companion_Snapshot s;
    base(&s);
    strcpy(s.sections.proxy.state, "healthy");
    s.sections.proxy.has_data = true;
    esp32monitor_companion_ProxyData *p = &s.sections.proxy.data;
    strcpy(p->config_epoch, s.config_epoch);
    p->config_revision = 1;
    strcpy(p->time.state, "healthy");
    p->time.stale_after_ms = 45000; p->time.expire_after_ms = 60000;
    p->time.has_data = true;
    p->time.data.utc_unix_ms = 1800000000000LL;
    p->time.data.utc_offset_minutes = 480;
    p->time.has_age_ms = true;
    p->time.age_ms = 59000;

    monitor_companion_view_t view;
    monitor_companion_view_build(&s, NULL, 2000, 0, &view);
    assert(view.proxy_owned && !view.time_valid);

    esp32monitor_companion_Heartbeat hb;
    memset(&hb, 0, sizeof hb);
    hb.protocol_version = 1;
    hb.has_time = true;
    hb.time.utc_unix_ms = 1800000061000LL;
    hb.time.utc_offset_minutes = 480;
    monitor_companion_view_build(&s, &hb, 2000, 0, &view);
    assert(view.proxy_owned && view.time_valid);
    assert(!strcmp(view.time_text, "16:01"));
    assert(!strcmp(view.date_text, "FRI 15 JAN"));
}

static void cleared(void)
{
    monitor_companion_view_t view;
    monitor_companion_view_build(NULL, NULL, 0, 0, &view);
    assert(!view.proxy_owned && !view.activity.github_available);
    assert(view.activity.usage_5h_percent == -1);
    assert(view.activity.usage_week_percent == -1);
    assert(view.activity.music_position_seconds == -1);
    assert(view.activity.agent_state == MONITOR_TASK_UNKNOWN);
    assert(view.devices.count == 0);
}

int main(void)
{
    github_full_week();
    github_partial_week();
    sections();
    proxy();
    leases();
    devices_lease();
    heartbeat_time();
    cleared();
    puts("companion view conversion tests passed");
    return 0;
}
