#include "scene_registry.h"

#include <string.h>

/*
 * The review scenes intentionally pass four slots and mark unused slots
 * offline. The shared UI filters those slots before calculating card widths.
 */
static const monitor_device_t REVIEW_DEVICES_0[] = {
    {"NAS", 37, 62, 84, false},
    {"PI", 18, 47, 62, false},
    {"PC", 72, 81, -1, false},
    {"ROUTER", 29, 54, 100, false},
};
static const monitor_device_t REVIEW_DEVICES_1[] = {
    {"NAS", 37, 62, 84, true},
    {"PI", 18, 47, 62, false},
    {"PC", 72, 81, -1, false},
    {"ROUTER", 29, 54, 100, false},
};
static const monitor_device_t REVIEW_DEVICES_2[] = {
    {"NAS", 37, 62, 84, true},
    {"PI", 18, 47, 62, true},
    {"PC", 72, 81, -1, false},
    {"ROUTER", 29, 54, 100, false},
};
static const monitor_device_t REVIEW_DEVICES_3[] = {
    {"NAS", 37, 62, 84, true},
    {"PI", 18, 47, 62, true},
    {"PC", 72, 81, -1, true},
    {"ROUTER", 29, 54, 100, false},
};
static const monitor_device_t REVIEW_DEVICES_4[] = {
    {"NAS", 37, 62, 84, true},
    {"PI", 18, 47, 62, true},
    {"PC", 72, 81, -1, true},
    {"ROUTER", 29, 54, 100, true},
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

REVIEW_FACTORY(monitor_0_scene, REVIEW_DEVICES_0, 4)
REVIEW_FACTORY(monitor_1_scene, REVIEW_DEVICES_1, 4)
REVIEW_FACTORY(monitor_2_scene, REVIEW_DEVICES_2, 4)
REVIEW_FACTORY(monitor_3_scene, REVIEW_DEVICES_3, 4)
REVIEW_FACTORY(monitor_4_scene, REVIEW_DEVICES_4, 4)

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
};

const simulator_scene_t *simulator_scene_default(void)
{
    return &SCENES[0];
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
