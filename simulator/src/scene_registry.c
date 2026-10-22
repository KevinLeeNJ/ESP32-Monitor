#include "scene_registry.h"

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

static const simulator_scene_t SCENES[] = {
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
