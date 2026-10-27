#include "scene_registry.h"
#include "monitor_pages.h"

#include <string.h>

#define SAMPLE(n, c, m, d) {n, "12d 4h", 3, {{"CPU", #c "%", c, true}, {"MEM", #m "%", m, true}, {"DISK", #d "%", d, true}}}
static const monitor_device_t REVIEW_DEVICES[] = {
    SAMPLE("NAS", 37, 62, 84), SAMPLE("PI", 18, 47, 62),
    SAMPLE("PC", 72, 81, 0), SAMPLE("ROUTER", 29, 54, 100),
};

static lv_obj_t *create_monitor_scene(lv_obj_t *parent,
                                      monitor_frame_variant_t variant)
{
    return monitor_ui_create_variant(parent, variant);
}

#define REVIEW_FACTORY(name, devices, count) \
    static lv_obj_t *create_##name(lv_obj_t *parent, monitor_frame_variant_t variant) \
    { \
        return monitor_ui_create_with_devices_variant(parent, devices, count, variant); \
    }

REVIEW_FACTORY(monitor_0_scene, REVIEW_DEVICES, 0)
REVIEW_FACTORY(monitor_1_scene, REVIEW_DEVICES, 1)
REVIEW_FACTORY(monitor_2_scene, REVIEW_DEVICES, 2)
REVIEW_FACTORY(monitor_3_scene, REVIEW_DEVICES, 3)
REVIEW_FACTORY(monitor_4_scene, REVIEW_DEVICES, 4)

static lv_obj_t *create_devices_state(lv_obj_t *parent, monitor_frame_variant_t variant,
                                      monitor_devices_status_t status)
{
    lv_obj_t *root = monitor_ui_create_with_devices_variant(parent, NULL, 0, variant);
    monitor_devices_snapshot_t snapshot = {.count = 4, .status = status};
    memcpy(snapshot.devices, REVIEW_DEVICES, sizeof(REVIEW_DEVICES));
    strcpy(snapshot.devices[3].name, "ABCDEFGH");
    strcpy(snapshot.devices[3].uptime, "999d 23h");
    strcpy(snapshot.devices[3].metrics[2].value, "-123456%");
    snapshot.devices[3].metrics[2].percent_valid = false;
    strcpy(snapshot.devices[1].metrics[1].value, "--");
    snapshot.devices[1].metrics[1].percent_valid = false;
    if (status != MONITOR_DEVICES_READY && status != MONITOR_DEVICES_STALE) snapshot.count = 0;
    monitor_ui_apply_devices_snapshot(root, &snapshot);
    return root;
}
#define STATE_FACTORY(n, status) \
    static lv_obj_t *create_##n(lv_obj_t *parent, monitor_frame_variant_t variant) \
    { return create_devices_state(parent, variant, status); }
STATE_FACTORY(devices_missing, MONITOR_DEVICES_READY)
STATE_FACTORY(devices_stale, MONITOR_DEVICES_STALE)
STATE_FACTORY(devices_unavailable, MONITOR_DEVICES_UNAVAILABLE)
STATE_FACTORY(devices_auth, MONITOR_DEVICES_AUTH_ERROR)
STATE_FACTORY(devices_unconfigured, MONITOR_DEVICES_NOT_CONFIGURED)

static lv_obj_t *create_device_transition(lv_obj_t *parent, monitor_frame_variant_t variant, bool online)
{
    monitor_device_t before[3] = {REVIEW_DEVICES[0], REVIEW_DEVICES[2], REVIEW_DEVICES[3]};
    lv_obj_t *root = monitor_ui_create_with_devices_variant(parent, online ? before : REVIEW_DEVICES,
                                                            online ? 3 : 4, variant);
    monitor_ui_set_animations_enabled(root, true);
    monitor_devices_snapshot_t next = {.count = online ? 4 : 3, .status = MONITOR_DEVICES_READY};
    memcpy(next.devices, online ? REVIEW_DEVICES : before, next.count * sizeof(next.devices[0]));
    monitor_ui_apply_devices_snapshot(root, &next);
    return root;
}
static lv_obj_t *create_online(lv_obj_t *parent, monitor_frame_variant_t variant)
{ return create_device_transition(parent, variant, true); }
static lv_obj_t *create_offline(lv_obj_t *parent, monitor_frame_variant_t variant)
{ return create_device_transition(parent, variant, false); }

static lv_obj_t *create_offline_two(lv_obj_t *parent, monitor_frame_variant_t variant)
{
    lv_obj_t *root = monitor_ui_create_with_devices_variant(parent, REVIEW_DEVICES, 4, variant);
    monitor_ui_set_animations_enabled(root, true);
    monitor_devices_snapshot_t next = {.count = 2, .status = MONITOR_DEVICES_READY};
    next.devices[0] = REVIEW_DEVICES[0];
    next.devices[1] = REVIEW_DEVICES[3];
    monitor_ui_apply_devices_snapshot(root, &next);
    return root;
}

static lv_obj_t *create_clock_transition(lv_obj_t *parent, monitor_frame_variant_t variant, bool rollover)
{
    monitor_ui_set_clock_text(rollover ? "19:59" : "19:55", "MON 18 OCT");
    lv_obj_t *root = create_monitor_scene(parent, variant);
    monitor_ui_set_animations_enabled(root, true);
    monitor_ui_snapshot_t next = {.time_text = rollover ? "20:00" : "19:56",
        .wifi_state = MONITOR_WIFI_CONNECTED, .battery_percent = 78,
        .weather_condition = MONITOR_WEATHER_UNKNOWN, .temperature_tenths_c = 245,
        .humidity_percent = 58, .aqi = 42};
    monitor_ui_apply_snapshot(root, &next);
    monitor_ui_set_clock_text("12:34", "MON 18 OCT");
    return root;
}
static lv_obj_t *create_minute(lv_obj_t *parent, monitor_frame_variant_t variant)
{ return create_clock_transition(parent, variant, false); }
static lv_obj_t *create_rollover(lv_obj_t *parent, monitor_frame_variant_t variant)
{ return create_clock_transition(parent, variant, true); }

static lv_obj_t *create_info_update(lv_obj_t *parent, monitor_frame_variant_t variant)
{
    monitor_ui_set_clock_text("19:55", "MON 18 OCT");
    lv_obj_t *root = create_monitor_scene(parent, variant);
    monitor_ui_set_clock_text("12:34", "MON 18 OCT");
    monitor_ui_set_animations_enabled(root, true);
    monitor_ui_snapshot_t next = {.time_text = "19:56", .wifi_state = MONITOR_WIFI_CONNECTED,
        .battery_percent = 78, .weather_condition = MONITOR_WEATHER_LIGHT_RAIN,
        .temperature_tenths_c = 256, .humidity_percent = 63, .aqi = 57};
    monitor_ui_apply_snapshot(root, &next);
    return root;
}

static lv_obj_t *create_waiting(lv_obj_t *parent, monitor_frame_variant_t variant)
{ return monitor_ui_create_waiting(parent, variant); }

static lv_obj_t *create_first_data_count(lv_obj_t *parent, monitor_frame_variant_t variant, uint8_t count)
{
    lv_obj_t *root = create_waiting(parent, variant);
    monitor_ui_snapshot_t next = {.time_text = "19:56", .date_text = "FRI 23 OCT",
        .wifi_state = MONITOR_WIFI_CONNECTED, .battery_percent = 78,
        .weather_condition = MONITOR_WEATHER_LIGHT_RAIN, .temperature_tenths_c = 256,
        .humidity_percent = 63, .aqi = 57};
    monitor_ui_apply_snapshot(root, &next);
    monitor_devices_snapshot_t devices = {.count = count, .status = MONITOR_DEVICES_READY};
    memcpy(devices.devices, REVIEW_DEVICES, sizeof(REVIEW_DEVICES));
    monitor_ui_apply_devices_snapshot(root, &devices);
    return root;
}

#define FIRST_DATA_FACTORY(name, count) \
    static lv_obj_t *create_##name(lv_obj_t *parent, monitor_frame_variant_t variant) \
    { return create_first_data_count(parent, variant, count); }
FIRST_DATA_FACTORY(first_data, 4)
FIRST_DATA_FACTORY(first_data_3, 3)
FIRST_DATA_FACTORY(first_data_2, 2)
FIRST_DATA_FACTORY(first_data_1, 1)

static lv_obj_t *create_configuring(lv_obj_t *parent, monitor_frame_variant_t variant)
{
    lv_obj_t *root = monitor_ui_create_waiting(parent, variant);
    monitor_ui_snapshot_t ui = {.time_text = "19:56", .date_text = "SAT 24 OCT",
        .wifi_state = MONITOR_WIFI_PROVISIONING, .battery_percent = 78,
        .weather_condition = MONITOR_WEATHER_UNKNOWN,
        .temperature_tenths_c = 245, .humidity_percent = 58, .aqi = -1};
    monitor_ui_apply_snapshot(root, &ui);
    return root;
}

static lv_obj_t *create_local_offline(lv_obj_t *parent, monitor_frame_variant_t variant)
{
    lv_obj_t *root = create_configuring(parent, variant);
    /* Local readings have already appeared during configuration. */
    monitor_ui_set_animations_enabled(root, false);
    monitor_ui_set_animations_enabled(root, true);
    monitor_ui_snapshot_t ui = {.time_text = "19:56", .date_text = "SAT 24 OCT",
        .wifi_state = MONITOR_WIFI_OFFLINE, .battery_percent = 78,
        .weather_condition = MONITOR_WEATHER_UNKNOWN,
        .temperature_tenths_c = 245, .humidity_percent = 58, .aqi = -1};
    monitor_ui_apply_snapshot(root, &ui);
    return root;
}

static lv_obj_t *create_profiling(lv_obj_t *parent, monitor_frame_variant_t variant)
{
    lv_obj_t *root = create_monitor_scene(parent, variant);
    monitor_ui_snapshot_t ui = {.time_text = "12:34", .date_text = "SUN 25 OCT",
        .wifi_state = MONITOR_WIFI_CONNECTED, .battery_percent = 78,
        .weather_condition = MONITOR_WEATHER_CLEAR,
        .temperature_tenths_c = 245, .humidity_percent = 58, .aqi = 42, .profiling = true};
    monitor_ui_apply_snapshot(root, &ui);
    return root;
}

static monitor_activity_data_t activity_fixture(monitor_task_state_t state, bool playing)
{
    monitor_activity_data_t data = monitor_activity_demo();
    data.agent_state = state;
    data.music_playing = playing;
    return data;
}

static lv_obj_t *create_activity_state(lv_obj_t *parent, monitor_frame_variant_t variant, monitor_task_state_t state, bool playing)
{
    monitor_activity_data_t data = activity_fixture(state, playing);
    return monitor_activity_create(parent, &data, variant);
}

static lv_obj_t *create_page_transition(lv_obj_t *parent, monitor_frame_variant_t variant, bool next)
{
    lv_obj_t *first = create_monitor_scene(parent, variant);
    if(!first) return NULL;
    monitor_activity_data_t data = activity_fixture(MONITOR_TASK_RUNNING, true);
    monitor_pages_t *pages = monitor_pages_create(first, variant, &data);
    if(!pages) { lv_obj_delete(first); return NULL; }
    if(!next) {
        if(!monitor_pages_switch(pages, true)) { lv_obj_delete(first); return NULL; }
        monitor_pages_settle(pages);
    }
    if(!monitor_pages_switch(pages, next)) { lv_obj_delete(first); return NULL; }
    monitor_pages_resume(pages);
    return first;
}
static lv_obj_t *create_page_next(lv_obj_t *parent, monitor_frame_variant_t variant)
{ return create_page_transition(parent, variant, true); }
static lv_obj_t *create_page_previous(lv_obj_t *parent, monitor_frame_variant_t variant)
{ return create_page_transition(parent, variant, false); }

#define ACTIVITY_FACTORY(name, state) \
    static lv_obj_t *create_activity_##name(lv_obj_t *parent, monitor_frame_variant_t variant) \
    { return create_activity_state(parent, variant, state, true); }
ACTIVITY_FACTORY(running, MONITOR_TASK_RUNNING)
ACTIVITY_FACTORY(idle, MONITOR_TASK_IDLE)
ACTIVITY_FACTORY(waiting_input, MONITOR_TASK_WAITING_INPUT)
ACTIVITY_FACTORY(waiting_approval, MONITOR_TASK_WAITING_APPROVAL)
ACTIVITY_FACTORY(completed, MONITOR_TASK_COMPLETED)
ACTIVITY_FACTORY(failed, MONITOR_TASK_FAILED)
ACTIVITY_FACTORY(cancelled, MONITOR_TASK_CANCELLED)
ACTIVITY_FACTORY(unknown, MONITOR_TASK_UNKNOWN)

static lv_obj_t *create_activity_paused(lv_obj_t *parent, monitor_frame_variant_t variant)
{ return create_activity_state(parent, variant, MONITOR_TASK_RUNNING, false); }

static const simulator_scene_t SCENES[] = {
    { .name = "page-next", .description = "First to second page transition", .create = create_page_next },
    { .name = "page-previous", .description = "Second to first page transition", .create = create_page_previous },
    { .name = "activity-paused", .description = "Music is not playing", .create = create_activity_paused },
    { .name = "activity", .description = "GitHub, agent and playback second page", .create = create_activity_running },
    { .name = "activity-idle", .description = "Codex idle", .create = create_activity_idle },
    { .name = "activity-waiting-input", .description = "Codex waiting for input", .create = create_activity_waiting_input },
    { .name = "activity-waiting-approval", .description = "Codex waiting for approval", .create = create_activity_waiting_approval },
    { .name = "activity-completed", .description = "Codex task completed", .create = create_activity_completed },
    { .name = "activity-failed", .description = "Codex task failed", .create = create_activity_failed },
    { .name = "activity-cancelled", .description = "Codex task cancelled", .create = create_activity_cancelled },
    { .name = "activity-unknown", .description = "Codex task state unknown", .create = create_activity_unknown },
    { .name = "profiling", .description = "PM profiling replaces the date for five minutes", .create = create_profiling },
    { .name = "configuring", .description = "Configuration session status", .create = create_configuring },
    { .name = "offline", .description = "Offline retains local data after configuration", .create = create_local_offline },
    { .name = "monitor-waiting", .description = "Live boot placeholders before first data", .create = create_waiting },
    { .name = "animation-load-3", .description = "First snapshot with three Devices", .create = create_first_data_3 },
    { .name = "animation-load-2", .description = "First snapshot with two Devices", .create = create_first_data_2 },
    { .name = "animation-load-1", .description = "First snapshot with one Device", .create = create_first_data_1 },
    { .name = "animation-load", .description = "First data rolls in and Devices pop in one by one", .create = create_first_data },
    { .name = "animation-info", .description = "Clock, weather, temperature, humidity and AQI roll together", .create = create_info_update },
    { .name = "animation-online", .description = "Existing cards make room, new card pops in", .create = create_online },
    { .name = "animation-offline", .description = "Middle card shrinks, survivors fill the gap", .create = create_offline },
    { .name = "animation-offline-two", .description = "PI and PC leave in one snapshot; NAS and ROUTER remain", .create = create_offline_two },
    { .name = "animation-minute", .description = "19:55 to 19:56, last digit rolls", .create = create_minute },
    { .name = "animation-rollover", .description = "19:59 to 20:00, four digits roll together", .create = create_rollover },
    {
        .name = "monitor",
        .description = "Current ESP32-S3 monitor verification UI",
        .create = create_monitor_scene,
    },
    {
        .name = "monitor-0",
        .description = "Review layout with zero online devices",
        .create = create_monitor_0_scene,
    },
    {
        .name = "monitor-1",
        .description = "Review layout with one online device",
        .create = create_monitor_1_scene,
    },
    {
        .name = "monitor-2",
        .description = "Review layout with two online devices",
        .create = create_monitor_2_scene,
    },
    {
        .name = "monitor-3",
        .description = "Review layout with three online devices",
        .create = create_monitor_3_scene,
    },
    {
        .name = "monitor-4",
        .description = "Review layout with four online devices",
        .create = create_monitor_4_scene,
    },
    { .name = "devices-missing", .description = "Maximum labels and missing metrics", .create = create_devices_missing },
    { .name = "devices-stale", .description = "Retained stale snapshot", .create = create_devices_stale },
    { .name = "devices-unavailable", .description = "Expired snapshot", .create = create_devices_unavailable },
    { .name = "devices-auth", .description = "Authentication failure", .create = create_devices_auth },
    { .name = "devices-unconfigured", .description = "No Devices configuration", .create = create_devices_unconfigured },
};

const simulator_scene_t *simulator_scene_default(void)
{
    return simulator_scene_find("monitor");
}

const simulator_scene_t *simulator_scene_find(const char *name)
{
    if(name == NULL) return NULL;
    for(size_t i = 0; i < simulator_scene_count(); ++i) {
        if(strcmp(SCENES[i].name, name) == 0) return &SCENES[i];
    }
    return NULL;
}

size_t simulator_scene_count(void)
{
    return sizeof(SCENES) / sizeof(SCENES[0]);
}

const simulator_scene_t *simulator_scene_at(size_t index)
{
    return index < simulator_scene_count() ? &SCENES[index] : NULL;
}
