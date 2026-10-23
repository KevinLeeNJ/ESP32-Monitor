#pragma once

#include <stdbool.h>
#include <stdint.h>

#include "lvgl.h"
#include "monitor_devices_data.h"

#ifdef __cplusplus
extern "C" {
#endif

#define MONITOR_DISPLAY_WIDTH  400
#define MONITOR_DISPLAY_HEIGHT 300
#define MONITOR_FRAME_BYTES    (MONITOR_DISPLAY_WIDTH * MONITOR_DISPLAY_HEIGHT / 8U)
#define MONITOR_RGB565_BYTES   (MONITOR_DISPLAY_WIDTH * MONITOR_DISPLAY_HEIGHT * 2U)

typedef enum {
    MONITOR_FRAME_NORMAL = 0,
    MONITOR_FRAME_INVERTED = 1,
} monitor_frame_variant_t;

typedef enum {
    MONITOR_WIFI_PROVISIONING = 0,
    MONITOR_WIFI_CONNECTED,
    MONITOR_WIFI_DISCONNECTED,
} monitor_wifi_state_t;

typedef enum {
    MONITOR_WEATHER_UNKNOWN = -1,       /* 尚未获得有效天气 */
    MONITOR_WEATHER_CLEAR = 0,          /* 晴 */
    MONITOR_WEATHER_FEW_CLOUDS,         /* 少云 */
    MONITOR_WEATHER_SCATTERED_CLOUDS,   /* 多云 */
    MONITOR_WEATHER_OVERCAST,           /* 阴天 */
    MONITOR_WEATHER_LIGHT_RAIN,         /* 小雨 */
    MONITOR_WEATHER_MODERATE_RAIN,      /* 中雨 */
    MONITOR_WEATHER_HEAVY_RAIN,         /* 大雨 */
    MONITOR_WEATHER_THUNDERSTORM,       /* 雷阵雨 */
    MONITOR_WEATHER_LIGHT_SNOW,         /* 小雪 */
    MONITOR_WEATHER_MODERATE_SNOW,      /* 中雪 */
    MONITOR_WEATHER_HEAVY_SNOW,         /* 大雪 */
    MONITOR_WEATHER_SLEET,               /* 雨夹雪 */
    MONITOR_WEATHER_HAIL,                /* 冰雹 */
    MONITOR_WEATHER_FOG,                 /* 雾 */
    MONITOR_WEATHER_HAZE,                /* 霾 */
    MONITOR_WEATHER_DUST_STORM,          /* 沙尘暴 */
} monitor_weather_condition_t;

typedef struct {
    monitor_wifi_state_t wifi_state;
    int board_battery_percent;
    monitor_weather_condition_t weather_condition;
    int temperature_tenths_c;
    int humidity_percent;
    int aqi;
} monitor_ui_demo_data_t;

typedef struct {
    const char *time_text;
    const char *date_text;
    monitor_wifi_state_t wifi_state;
    int battery_percent;
    monitor_weather_condition_t weather_condition;
    int temperature_tenths_c;
    int humidity_percent;
    int aqi;
} monitor_ui_snapshot_t;

typedef struct {
    uint8_t *frame;
    uint32_t width;
    uint32_t height;
} monitor_frame_t;

/* The demo is intentionally deterministic: no clock, randomness or animation. */
const char *monitor_weather_condition_abbrev(monitor_weather_condition_t condition);
const char *monitor_weather_condition_name(monitor_weather_condition_t condition);
const lv_image_dsc_t *monitor_weather_condition_icon(monitor_weather_condition_t condition);
void monitor_ui_set_demo_data(const monitor_ui_demo_data_t *data);
/* Override the deterministic simulator clock with live firmware values. */
void monitor_ui_set_clock_text(const char *time_text, const char *date_text);
void monitor_ui_apply_devices_snapshot(lv_obj_t *root, const monitor_devices_snapshot_t *snapshot);
void monitor_ui_apply_snapshot(lv_obj_t *root, const monitor_ui_snapshot_t *snapshot);

lv_obj_t *monitor_ui_create(lv_obj_t *parent);
lv_obj_t *monitor_ui_create_with_devices(lv_obj_t *parent,
                                          const monitor_device_t *devices,
                                          uint8_t device_count);
lv_obj_t *monitor_ui_create_with_devices_variant(lv_obj_t *parent,
                                                 const monitor_device_t *devices,
                                                 uint8_t device_count,
                                                 monitor_frame_variant_t variant);
/* Create the monitor with a board-level battery percentage. Pass -1 when the
 * board battery is unavailable; the indicator then renders as "--". */
lv_obj_t *monitor_ui_create_with_devices_battery(lv_obj_t *parent,
                                                 const monitor_device_t *devices,
                                                 uint8_t device_count,
                                                 int board_battery_percent);
lv_obj_t *monitor_ui_create_with_devices_battery_variant(lv_obj_t *parent,
                                                         const monitor_device_t *devices,
                                                         uint8_t device_count,
                                                         int board_battery_percent,
                                                         monitor_frame_variant_t variant);
lv_obj_t *monitor_ui_create_with_devices_battery_wifi(lv_obj_t *parent,
                                                     const monitor_device_t *devices,
                                                     uint8_t device_count,
                                                     int board_battery_percent,
                                                     monitor_wifi_state_t wifi_state);
lv_obj_t *monitor_ui_create_with_devices_battery_wifi_variant(lv_obj_t *parent,
                                                              const monitor_device_t *devices,
                                                              uint8_t device_count,
                                                              int board_battery_percent,
                                                              monitor_wifi_state_t wifi_state,
                                                              monitor_frame_variant_t variant);
lv_obj_t *monitor_ui_create_variant(lv_obj_t *parent, monitor_frame_variant_t variant);
void monitor_ui_set_variant(lv_obj_t *root, monitor_frame_variant_t variant);
monitor_frame_variant_t monitor_ui_get_variant(lv_obj_t *root);

/* Convert a full-screen RGB565 frame to the panel's packed 1bpp format. */
void monitor_pack_rgb565(const uint16_t *rgb565, uint32_t width, uint32_t height,
                         monitor_frame_variant_t variant, uint8_t *out);
void monitor_pack_rgb565_region(const uint16_t *rgb565, uint32_t width, uint32_t height,
                                int32_t x1, int32_t y1, int32_t x2, int32_t y2,
                                monitor_frame_variant_t variant, uint8_t *packed_inout);

/* Reconstruct RGB565 from the packed panel frame for a pixel-accurate preview. */
void monitor_unpack_to_rgb565(const uint8_t *packed, uint32_t width, uint32_t height,
                              monitor_frame_variant_t variant, uint16_t *out);

#ifdef __cplusplus
}
#endif
