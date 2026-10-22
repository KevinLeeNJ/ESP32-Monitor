#include "monitor_ui.h"

#include <stdio.h>
#include <string.h>

#define COLOR_BLACK 0x111111U
#define COLOR_WHITE 0xf7f7f7U
#define COLOR_MUTED 0x555555U
#define COLOR_LIGHT 0xd8d8d8U

#define CONTENT_X 15
#define CONTENT_WIDTH 370
#define DEVICE_TOP 112
#define DEVICE_HEIGHT 180
#define DEVICE_GAP 5

extern const lv_image_dsc_t monitor_weather_clear;
extern const lv_image_dsc_t monitor_weather_partly_cloudy;
extern const lv_image_dsc_t monitor_weather_cloudy;
extern const lv_image_dsc_t monitor_weather_rain;
extern const lv_image_dsc_t monitor_weather_thunderstorm;
extern const lv_image_dsc_t monitor_weather_snow;
extern const lv_image_dsc_t monitor_weather_fog;
extern const lv_image_dsc_t monitor_weather_hail;
extern const lv_font_t monitor_weather_zh;
extern const lv_image_dsc_t monitor_metric_thermometer;
extern const lv_image_dsc_t monitor_metric_droplets;

/* The simulator intentionally uses fixed values. Firmware can create the same
 * layout with live values through monitor_ui_create_with_devices(). */
#define DEMO_DEVICE(n, c, m, d) {n, "12d 4h", 3, {{"CPU", #c "%", c, true}, {"MEM", #m "%", m, true}, {"DISK", #d "%", d, true}}}
static const monitor_device_t DEFAULT_DEVICES[] = {
    DEMO_DEVICE("NAS", 37, 62, 84),
    DEMO_DEVICE("PI", 18, 47, 62),
    DEMO_DEVICE("PC", 72, 81, 0),
    DEMO_DEVICE("ROUTER", 29, 54, 100),
};

static char clock_time_text[8] = "12:34";
static char clock_date_text[16] = "MON 18 OCT";

static monitor_ui_demo_data_t demo_data = {
    .wifi_state = MONITOR_WIFI_CONNECTED,
    .board_battery_percent = 78,
    .weather_condition = MONITOR_WEATHER_FEW_CLOUDS,
    .temperature_tenths_c = 245,
    .humidity_percent = 58,
    .aqi = 42,
};

void monitor_ui_set_demo_data(const monitor_ui_demo_data_t *data)
{
    if(data != NULL) demo_data = *data;
}

void monitor_ui_set_clock_text(const char *time_text, const char *date_text)
{
    if(time_text != NULL) snprintf(clock_time_text, sizeof(clock_time_text), "%s", time_text);
    if(date_text != NULL) snprintf(clock_date_text, sizeof(clock_date_text), "%s", date_text);
}

typedef struct {
    lv_obj_t *root;
    lv_obj_t *variant_label;
    monitor_frame_variant_t variant;
    monitor_wifi_state_t wifi_state;
    monitor_weather_condition_t weather_condition;
    lv_obj_t *time_label;
    lv_obj_t *date_label;
    lv_obj_t *wifi_label;
    lv_obj_t *battery_label;
    lv_obj_t *battery_outline;
    lv_obj_t *wifi_slash;
    lv_obj_t *weather_label;
    lv_obj_t *weather_icon;
    lv_obj_t *temperature_label;
    lv_obj_t *humidity_label;
    lv_obj_t *aqi_label;
} monitor_ui_state_t;

static void monitor_ui_state_delete_cb(lv_event_t *event)
{
    lv_obj_t *obj = lv_event_get_target(event);
    monitor_ui_state_t *state = (monitor_ui_state_t *)lv_obj_get_user_data(obj);
    if(state != NULL) {
        lv_free(state);
        lv_obj_set_user_data(obj, NULL);
    }
}

static void clear_object(lv_obj_t *obj)
{
    lv_obj_remove_style_all(obj);
    lv_obj_clear_flag(obj, LV_OBJ_FLAG_SCROLLABLE);
}

static void set_name(lv_obj_t *obj, const char *name)
{
    if(obj != NULL && name != NULL) lv_obj_set_name(obj, name);
}

static lv_obj_t *make_label(lv_obj_t *parent, const char *text, int32_t x, int32_t y,
                            const lv_font_t *font, uint32_t color)
{
    lv_obj_t *label = lv_label_create(parent);
    lv_label_set_text(label, text);
    lv_obj_set_style_text_font(label, font, 0);
    lv_obj_set_style_text_color(label, lv_color_hex(color), 0);
    lv_obj_set_pos(label, x, y);
    return label;
}

static lv_obj_t *make_box(lv_obj_t *parent, int32_t x, int32_t y, int32_t width,
                          int32_t height, int32_t radius, uint32_t fill,
                          int32_t border_width, uint32_t border)
{
    lv_obj_t *box = lv_obj_create(parent);
    clear_object(box);
    lv_obj_set_pos(box, x, y);
    lv_obj_set_size(box, width, height);
    lv_obj_set_style_bg_opa(box, LV_OPA_COVER, 0);
    lv_obj_set_style_bg_color(box, lv_color_hex(fill), 0);
    lv_obj_set_style_border_width(box, border_width, 0);
    lv_obj_set_style_border_color(box, lv_color_hex(border), 0);
    lv_obj_set_style_radius(box, radius, 0);
    return box;
}

static lv_obj_t *make_ink(lv_obj_t *parent, int32_t x, int32_t y, int32_t width,
                          int32_t height, int32_t radius)
{
    lv_obj_t *ink = make_box(parent, x, y, width, height, radius, COLOR_BLACK, 0, COLOR_BLACK);
    set_name(ink, "ink");
    return ink;
}

static lv_obj_t *make_bar(lv_obj_t *parent, int32_t x, int32_t y, int32_t width,
                          int32_t value)
{
    if(value < 0) value = 0;
    if(value > 100) value = 100;

    lv_obj_t *track = make_box(parent, x, y, width, 7, 3, COLOR_WHITE, 1, COLOR_BLACK);
    set_name(track, "track");
    const int32_t inner_width = width - 4;
    const int32_t fill_width = (inner_width * value) / 100;
    if(fill_width > 0) make_ink(track, 2, 1, fill_width, 5, 2);
    return track;
}

const char *monitor_weather_condition_abbrev(monitor_weather_condition_t condition)
{
    return monitor_weather_condition_name(condition);
}

const char *monitor_weather_condition_name(monitor_weather_condition_t condition)
{
    switch(condition) {
        case MONITOR_WEATHER_CLEAR: return "晴";
        case MONITOR_WEATHER_FEW_CLOUDS: return "少云";
        case MONITOR_WEATHER_SCATTERED_CLOUDS: return "多云";
        case MONITOR_WEATHER_OVERCAST: return "阴天";
        case MONITOR_WEATHER_LIGHT_RAIN: return "小雨";
        case MONITOR_WEATHER_MODERATE_RAIN: return "中雨";
        case MONITOR_WEATHER_HEAVY_RAIN: return "大雨";
        case MONITOR_WEATHER_THUNDERSTORM: return "雷阵雨";
        case MONITOR_WEATHER_LIGHT_SNOW: return "小雪";
        case MONITOR_WEATHER_MODERATE_SNOW: return "中雪";
        case MONITOR_WEATHER_HEAVY_SNOW: return "大雪";
        case MONITOR_WEATHER_SLEET: return "雨夹雪";
        case MONITOR_WEATHER_HAIL: return "冰雹";
        case MONITOR_WEATHER_FOG: return "雾";
        case MONITOR_WEATHER_HAZE: return "霾";
        case MONITOR_WEATHER_DUST_STORM: return "沙尘暴";
        default: return "--";
    }
}

const lv_image_dsc_t *monitor_weather_condition_icon(monitor_weather_condition_t condition)
{
    switch(condition) {
        case MONITOR_WEATHER_CLEAR: return &monitor_weather_clear;
        case MONITOR_WEATHER_FEW_CLOUDS: return &monitor_weather_partly_cloudy;
        case MONITOR_WEATHER_SCATTERED_CLOUDS: return &monitor_weather_cloudy;
        case MONITOR_WEATHER_OVERCAST: return &monitor_weather_cloudy;
        case MONITOR_WEATHER_LIGHT_RAIN:
        case MONITOR_WEATHER_MODERATE_RAIN:
        case MONITOR_WEATHER_HEAVY_RAIN: return &monitor_weather_rain;
        case MONITOR_WEATHER_THUNDERSTORM: return &monitor_weather_thunderstorm;
        case MONITOR_WEATHER_LIGHT_SNOW:
        case MONITOR_WEATHER_MODERATE_SNOW:
        case MONITOR_WEATHER_HEAVY_SNOW: return &monitor_weather_snow;
        case MONITOR_WEATHER_SLEET: return &monitor_weather_hail;
        case MONITOR_WEATHER_HAIL: return &monitor_weather_hail;
        case MONITOR_WEATHER_FOG:
        case MONITOR_WEATHER_HAZE: return &monitor_weather_fog;
        case MONITOR_WEATHER_DUST_STORM: return &monitor_weather_hail;
        default: return &monitor_weather_cloudy;
    }
}

static void make_metric(lv_obj_t *card, int32_t card_width, int32_t y,
                        const monitor_device_metric_t *metric, const lv_font_t *font)
{
    lv_point_t name_size, value_size;
    lv_text_get_size(&name_size, metric->name, font, 0, 0, LV_COORD_MAX, LV_TEXT_FLAG_NONE);
    lv_text_get_size(&value_size, metric->value, font, 0, 0, LV_COORD_MAX, LV_TEXT_FLAG_NONE);
    if (name_size.x + value_size.x + 4 > card_width - 14) font = &lv_font_montserrat_8;
    make_label(card, metric->name, 7, y, font, COLOR_MUTED);
    lv_obj_t *value_label = make_label(card, metric->value, 7, y, font, COLOR_BLACK);
    lv_obj_set_width(value_label, card_width - 14);
    lv_obj_set_style_text_align(value_label, LV_TEXT_ALIGN_RIGHT, 0);
    make_bar(card, 7, y + 16, card_width - 14,
             metric->percent_valid ? metric->percent : -1);
}

static lv_obj_t *make_icon(lv_obj_t *parent, const lv_image_dsc_t *source,
                           int32_t x, int32_t y, int32_t size, const char *name)
{
    lv_obj_t *image = lv_image_create(parent);
    lv_image_set_src(image, source);
    lv_obj_set_pos(image, x, y);
    lv_obj_set_size(image, size, size);
    lv_obj_set_style_image_recolor(image, lv_color_hex(COLOR_BLACK), 0);
    lv_obj_set_style_image_recolor_opa(image, LV_OPA_COVER, 0);
    set_name(image, name);
    return image;
}

static const char *wifi_state_symbol(monitor_wifi_state_t state)
{
    switch(state) {
        case MONITOR_WIFI_PROVISIONING: return LV_SYMBOL_REFRESH;
        case MONITOR_WIFI_CONNECTED: return LV_SYMBOL_WIFI;
        case MONITOR_WIFI_DISCONNECTED: return LV_SYMBOL_WIFI;
        default: return LV_SYMBOL_WARNING;
    }
}

void monitor_ui_apply_snapshot(lv_obj_t *root, const monitor_ui_snapshot_t *snapshot)
{
    if (root == NULL || snapshot == NULL) return;
    monitor_ui_state_t *state = (monitor_ui_state_t *)lv_obj_get_user_data(root);
    if (state == NULL) return;
    char text[24];
    if (snapshot->time_text && strcmp(lv_label_get_text(state->time_label), snapshot->time_text) != 0)
        lv_label_set_text(state->time_label, snapshot->time_text);
    if (snapshot->date_text && strcmp(lv_label_get_text(state->date_label), snapshot->date_text) != 0)
        lv_label_set_text(state->date_label, snapshot->date_text);
    if (state->wifi_state != snapshot->wifi_state) {
        state->wifi_state = snapshot->wifi_state;
        lv_label_set_text(state->wifi_label, wifi_state_symbol(snapshot->wifi_state));
        if (snapshot->wifi_state == MONITOR_WIFI_DISCONNECTED) lv_obj_clear_flag(state->wifi_slash, LV_OBJ_FLAG_HIDDEN);
        else lv_obj_add_flag(state->wifi_slash, LV_OBJ_FLAG_HIDDEN);
        lv_obj_set_style_text_color(state->wifi_label,
            lv_color_hex(snapshot->wifi_state == MONITOR_WIFI_PROVISIONING ? COLOR_MUTED : COLOR_BLACK), 0);
    }
    snprintf(text, sizeof(text), snapshot->battery_percent < 0 ? "--" : "%d%%", snapshot->battery_percent);
    if (strcmp(lv_label_get_text(state->battery_label), text) != 0) {
        lv_label_set_text(state->battery_label, text);
        lv_obj_clean(state->battery_outline);
        if (snapshot->battery_percent >= 0) {
            int value = snapshot->battery_percent > 100 ? 100 : snapshot->battery_percent;
            int32_t fill_width = (15 * value) / 100;
            if (fill_width > 0) make_ink(state->battery_outline, 2, 2, fill_width, 7, 1);
        }
    }
    if (state->weather_condition != snapshot->weather_condition) {
        state->weather_condition = snapshot->weather_condition;
        lv_image_set_src(state->weather_icon, monitor_weather_condition_icon(snapshot->weather_condition));
        lv_label_set_text(state->weather_label, monitor_weather_condition_abbrev(snapshot->weather_condition));
    }
    snprintf(text, sizeof(text), "%d.%d°C", snapshot->temperature_tenths_c / 10,
        snapshot->temperature_tenths_c < 0 ? -(snapshot->temperature_tenths_c % 10) : snapshot->temperature_tenths_c % 10);
    if (strcmp(lv_label_get_text(state->temperature_label), text) != 0)
        lv_label_set_text(state->temperature_label, text);
    snprintf(text, sizeof(text), "%d%%", snapshot->humidity_percent);
    if (strcmp(lv_label_get_text(state->humidity_label), text) != 0)
        lv_label_set_text(state->humidity_label, text);
    snprintf(text, sizeof(text), snapshot->aqi < 0 ? "--" : "%d", snapshot->aqi);
    if (strcmp(lv_label_get_text(state->aqi_label), text) != 0)
        lv_label_set_text(state->aqi_label, text);
}

static lv_obj_t *make_wifi_status(lv_obj_t *parent, monitor_wifi_state_t state)
{
    lv_obj_t *icon = make_label(parent, wifi_state_symbol(state), 18, 63,
                                &lv_font_montserrat_14,
                                state == MONITOR_WIFI_PROVISIONING ? COLOR_MUTED : COLOR_BLACK);
    set_name(icon, "wifi_status");

    static const lv_point_precise_t slash_points[] = {{1, 1}, {14, 14}};
    lv_obj_t *slash = lv_line_create(parent);
    lv_line_set_points(slash, slash_points, 2);
    lv_obj_set_pos(slash, 17, 62);
    lv_obj_set_size(slash, 16, 16);
    lv_obj_set_style_line_width(slash, 3, 0);
    lv_obj_set_style_line_color(slash, lv_color_hex(COLOR_BLACK), 0);
    lv_obj_set_style_line_rounded(slash, true, 0);
    set_name(slash, "wifi_slash");
    if(state != MONITOR_WIFI_DISCONNECTED) lv_obj_add_flag(slash, LV_OBJ_FLAG_HIDDEN);
    return icon;
}

static void make_board_battery(lv_obj_t *parent, int32_t x, int32_t y, int value)
{
    if(value > 100) value = 100;

    /* A compact outline icon keeps the board status in the left header without
     * competing with the 2x2 weather/metrics block on the right. */
    lv_obj_t *outline = make_box(parent, x, y, 19, 11, 2, COLOR_WHITE, 1, COLOR_BLACK);
    set_name(outline, "board_battery_outline");
    make_box(parent, x + 19, y + 3, 3, 5, 1, COLOR_BLACK, 0, COLOR_BLACK);

    if(value >= 0) {
        const int32_t fill_width = ((19 - 4) * value) / 100;
        if(fill_width > 0) make_ink(outline, 2, 2, fill_width, 7, 1);
    }

    char text[8];
    if(value < 0) snprintf(text, sizeof(text), "--");
    else snprintf(text, sizeof(text), "%d%%", value);
    lv_obj_t *label = make_label(parent, text, x + 27, y - 3,
                                 &lv_font_montserrat_12,
                                 value >= 0 ? COLOR_BLACK : COLOR_MUTED);
    set_name(label, "board_battery_value");
}

static lv_obj_t *make_info_card(lv_obj_t *parent, int32_t x, int32_t y,
                                const lv_image_dsc_t *icon, const char *value,
                                bool show_icon, bool large_value, const char *icon_name,
                                lv_obj_t **icon_out)
{
    const int32_t width = 115;
    lv_obj_t *card = make_box(parent, x, y, width, 36, 6, COLOR_WHITE, 1, COLOR_BLACK);
    if(icon_out != NULL) *icon_out = NULL;
    if(show_icon) {
        const bool weather_icon = icon_name != NULL && strcmp(icon_name, "weather_icon") == 0;
        /* Use the weather mask at its native 30px size. The taller card gives
         * it a real display area instead of clipping the source silhouette. */
        lv_obj_t *image = make_icon(card, icon, 5, weather_icon ? 2 : 6,
                                    weather_icon ? 30 : 23, icon_name);
        if(icon_out != NULL) *icon_out = image;
        const int32_t value_x = 39;
        const lv_font_t *value_font = weather_icon ? &monitor_weather_zh :
                                      &lv_font_montserrat_20;
        /* All large values use the same visual baseline as AQI. The 20 px
         * Chinese subset fixes the missing-glyph issue for weather labels. */
        const int32_t value_y = weather_icon ? 5 : 5;
        lv_obj_t *value_label = make_label(card, value, value_x, value_y,
                                           value_font, COLOR_BLACK);
        if(weather_icon) lv_obj_set_width(value_label, 75);
        return value_label;
    } else {
        make_label(card, "AQI", 8, 5, &lv_font_montserrat_12, COLOR_MUTED);
        return make_label(card, value, 58, 5, &lv_font_montserrat_20, COLOR_BLACK);
    }
}

static void make_device_card(lv_obj_t *parent, const monitor_device_t *device,
                             uint8_t device_count, uint8_t index, int32_t x,
                             int32_t width)
{
    const bool compact = device_count >= 4;
    const lv_font_t *name_font = compact ? &lv_font_montserrat_12 : &lv_font_montserrat_14;
    const lv_font_t *metric_font = compact ? &lv_font_montserrat_12 : &lv_font_montserrat_14;
    const int32_t metric_y = compact ? 58 : 60;
    const int32_t metric_step = compact ? 39 : 40;

    lv_obj_t *card = make_box(parent, x, DEVICE_TOP, width, DEVICE_HEIGHT, 8,
                              COLOR_WHITE, 2, COLOR_BLACK);
    char card_name[24];
    snprintf(card_name, sizeof(card_name), "device_%u", (unsigned)index);
    set_name(card, card_name);

    make_label(card, device->name, 7, 7, name_font, COLOR_BLACK);
    make_label(card, device->uptime, 7, 29, &lv_font_montserrat_12, COLOR_MUTED);
    for (uint8_t i = 0; i < device->metric_count && i < 3; ++i)
        make_metric(card, width, metric_y + metric_step * i, &device->metrics[i], metric_font);
}

static void apply_variant_recursive(lv_obj_t *obj, bool inverted)
{
    const lv_color_t foreground = lv_color_hex(inverted ? 0xffffffU : COLOR_BLACK);
    const lv_color_t background = lv_color_hex(inverted ? 0x000000U : COLOR_WHITE);
    const lv_color_t muted = lv_color_hex(inverted ? 0xb0b0b0U : COLOR_MUTED);
    const lv_color_t ink = lv_color_hex(inverted ? 0xffffffU : COLOR_BLACK);

    lv_obj_set_style_text_color(obj, foreground, 0);
    lv_obj_set_style_border_color(obj, foreground, 0);

    const char *name = lv_obj_get_name(obj);
    if(name != NULL && strcmp(name, "ink") == 0) {
        lv_obj_set_style_bg_color(obj, ink, 0);
    } else if(lv_obj_get_style_bg_opa(obj, 0) != LV_OPA_TRANSP) {
        lv_obj_set_style_bg_color(obj, background, 0);
    }
    if(name != NULL && strcmp(name, "wifi_status") == 0) {
        monitor_ui_state_t *state = (monitor_ui_state_t *)lv_obj_get_user_data(lv_obj_get_parent(obj));
        const bool provisioning = state != NULL && state->wifi_state == MONITOR_WIFI_PROVISIONING;
        lv_obj_set_style_text_color(obj, provisioning ? muted : foreground, 0);
    }
    if(name != NULL && strcmp(name, "wifi_slash") == 0) {
        lv_obj_set_style_line_color(obj, foreground, 0);
    }
    if(name != NULL && (strcmp(name, "weather_icon") == 0 ||
                        strcmp(name, "metric_icon") == 0)) {
        lv_obj_set_style_image_recolor(obj, foreground, 0);
        lv_obj_set_style_image_recolor_opa(obj, LV_OPA_COVER, 0);
    }

    for(uint32_t i = 0; i < lv_obj_get_child_count(obj); ++i) {
        apply_variant_recursive(lv_obj_get_child(obj, i), inverted);
    }
}

void monitor_ui_apply_devices_snapshot(lv_obj_t *root, const monitor_devices_snapshot_t *snapshot)
{
    if (!root || !snapshot || snapshot->count > 4) return;
    for (int i = 0; i < 4; ++i) {
        char name[24];
        snprintf(name, sizeof(name), "device_%d", i);
        lv_obj_t *old = lv_obj_get_child_by_name(root, name);
        if (old) lv_obj_delete(old);
    }
    lv_obj_t *old = lv_obj_get_child_by_name(root, "empty_devices");
    if (old) lv_obj_delete(old);
    const char *status = "";
    switch (snapshot->status) {
        case MONITOR_DEVICES_NOT_CONFIGURED: status = "NOT CONFIGURED"; break;
        case MONITOR_DEVICES_WAITING: status = "WAITING FOR DATA"; break;
        case MONITOR_DEVICES_STALE: status = "STALE"; break;
        case MONITOR_DEVICES_UNAVAILABLE: status = "DATA UNAVAILABLE"; break;
        case MONITOR_DEVICES_AUTH_ERROR: status = snapshot->count ? "AUTH ERROR / STALE" : "AUTH ERROR"; break;
        default: break;
    }
    char title[40];
    snprintf(title, sizeof(title), "%u%s%s", snapshot->count, *status ? "  " : "", status);
    lv_obj_t *count = lv_obj_get_child_by_name(root, "device_count");
    lv_obj_set_pos(count, 130, 97);
    lv_obj_set_width(count, 254);
    lv_label_set_text(count, title);
    if (!snapshot->count) {
        lv_obj_t *empty = make_label(root, snapshot->status == MONITOR_DEVICES_AUTH_ERROR ? "DATA UNAVAILABLE" :
                                     (*status ? status : "NO ONLINE DEVICES"), 0, 177,
                                     &lv_font_montserrat_14, COLOR_MUTED);
        lv_obj_set_width(empty, MONITOR_DISPLAY_WIDTH);
        lv_obj_set_style_text_align(empty, LV_TEXT_ALIGN_CENTER, 0);
        set_name(empty, "empty_devices");
    } else {
        int32_t width = (CONTENT_WIDTH - DEVICE_GAP * (snapshot->count - 1)) / snapshot->count;
        for (uint8_t i = 0; i < snapshot->count; ++i)
            make_device_card(root, &snapshot->devices[i], snapshot->count, i,
                             CONTENT_X + i * (width + DEVICE_GAP), width);
    }
    monitor_ui_state_t *state = lv_obj_get_user_data(root);
    if (state && state->variant != MONITOR_FRAME_NORMAL) apply_variant_recursive(root, true);
}

lv_obj_t *monitor_ui_create_with_devices(lv_obj_t *parent,
                                          const monitor_device_t *devices,
                                          uint8_t device_count)
{
    return monitor_ui_create_with_devices_battery(parent, devices, device_count, 78);
}

lv_obj_t *monitor_ui_create_with_devices_battery(lv_obj_t *parent,
                                                 const monitor_device_t *devices,
                                                 uint8_t device_count,
                                                 int board_battery_percent)
{
    return monitor_ui_create_with_devices_battery_wifi_variant(parent, devices, device_count,
                                                               board_battery_percent,
                                                               MONITOR_WIFI_CONNECTED,
                                                               MONITOR_FRAME_NORMAL);
}

lv_obj_t *monitor_ui_create_with_devices_battery_variant(lv_obj_t *parent,
                                                         const monitor_device_t *devices,
                                                         uint8_t device_count,
                                                         int board_battery_percent,
                                                         monitor_frame_variant_t variant)
{
    return monitor_ui_create_with_devices_battery_wifi_variant(parent, devices, device_count,
                                                               board_battery_percent,
                                                               MONITOR_WIFI_CONNECTED,
                                                               variant);
}

lv_obj_t *monitor_ui_create_with_devices_battery_wifi(lv_obj_t *parent,
                                                     const monitor_device_t *devices,
                                                     uint8_t device_count,
                                                     int board_battery_percent,
                                                     monitor_wifi_state_t wifi_state)
{
    return monitor_ui_create_with_devices_battery_wifi_variant(parent, devices, device_count,
                                                               board_battery_percent, wifi_state,
                                                               MONITOR_FRAME_NORMAL);
}

lv_obj_t *monitor_ui_create_with_devices_battery_wifi_variant(lv_obj_t *parent,
                                                              const monitor_device_t *devices,
                                                              uint8_t device_count,
                                                              int board_battery_percent,
                                                              monitor_wifi_state_t wifi_state,
                                                              monitor_frame_variant_t variant)
{
    if(parent == NULL || device_count > 4) return NULL;
    if(device_count > 0 && devices == NULL) return NULL;

    monitor_ui_state_t *state = (monitor_ui_state_t *)lv_malloc(sizeof(*state));
    if(state == NULL) return NULL;

    lv_obj_t *root = lv_obj_create(parent);
    clear_object(root);
    lv_obj_set_pos(root, 0, 0);
    lv_obj_set_size(root, MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT);
    lv_obj_set_style_bg_opa(root, LV_OPA_COVER, 0);
    lv_obj_set_style_bg_color(root, lv_color_hex(COLOR_WHITE), 0);
    lv_obj_set_user_data(root, state);
    lv_obj_add_event_cb(root, monitor_ui_state_delete_cb, LV_EVENT_DELETE, NULL);

    state->root = root;
    state->variant = variant;
    state->wifi_state = wifi_state;
    state->weather_condition = demo_data.weather_condition;

    make_box(root, 5, 5, 390, 290, 9, COLOR_WHITE, 2, COLOR_BLACK);

    /* Header: time and four compact rounded data cards. Weather has no text
     * label; temperature and humidity are identified by sourced icons. */
    state->time_label = make_label(root, clock_time_text, 16, 9, &lv_font_montserrat_30, COLOR_BLACK);
    lv_obj_set_style_text_letter_space(state->time_label, 1, 0);
    set_name(state->time_label, "time");
    state->date_label = make_label(root, clock_date_text, 19, 43, &lv_font_montserrat_12, COLOR_MUTED);
    state->wifi_label = make_wifi_status(root, wifi_state);
    state->wifi_slash = lv_obj_get_child_by_name(root, "wifi_slash");
    make_board_battery(root, 38, 65, demo_data.board_battery_percent);
    state->battery_label = lv_obj_get_child_by_name(root, "board_battery_value");
    state->battery_outline = lv_obj_get_child_by_name(root, "board_battery_outline");

    /* Keep the four data cards together as a 2x2 block in the upper-right. */
    const monitor_weather_condition_t weather_condition = demo_data.weather_condition;
    state->weather_label = make_info_card(root, 150, 8, monitor_weather_condition_icon(weather_condition),
                   monitor_weather_condition_abbrev(weather_condition), true, false,
                   "weather_icon", &state->weather_icon);
    char temperature_text[16];
    snprintf(temperature_text, sizeof(temperature_text), "%d.%d°C",
             demo_data.temperature_tenths_c / 10,
             demo_data.temperature_tenths_c < 0 ? -(demo_data.temperature_tenths_c % 10) :
             demo_data.temperature_tenths_c % 10);
    char humidity_text[12];
    snprintf(humidity_text, sizeof(humidity_text), "%d%%", demo_data.humidity_percent);
    char aqi_text[12];
    if(demo_data.aqi < 0) snprintf(aqi_text, sizeof(aqi_text), "--");
    else snprintf(aqi_text, sizeof(aqi_text), "%d", demo_data.aqi);
    state->temperature_label = make_info_card(root, 270, 8, &monitor_metric_thermometer, temperature_text, true, true,
                   "metric_icon", NULL);
    state->humidity_label = make_info_card(root, 150, 48, &monitor_metric_droplets, humidity_text, true, true,
                   "metric_icon", NULL);
    state->aqi_label = make_info_card(root, 270, 48, NULL, aqi_text, false, true, NULL, NULL);

    make_box(root, 15, 91, 370, 1, 0, COLOR_BLACK, 0, COLOR_BLACK);
    make_label(root, "DEVICES", 16, 95, &lv_font_montserrat_14, COLOR_BLACK);
    lv_obj_t *count_label = make_label(root, "0 ONLINE", 320, 97, &lv_font_montserrat_12, COLOR_MUTED);
    lv_obj_set_style_text_align(count_label, LV_TEXT_ALIGN_RIGHT, 0);
    set_name(count_label, "device_count");

    monitor_devices_snapshot_t initial = {.count = device_count, .status = MONITOR_DEVICES_READY};
    if (device_count) memcpy(initial.devices, devices, device_count * sizeof(*devices));
    monitor_ui_apply_devices_snapshot(root, &initial);

    state->variant_label = make_label(root, "", 0, 0, &lv_font_montserrat_12, COLOR_MUTED);
    lv_obj_add_flag(state->variant_label, LV_OBJ_FLAG_HIDDEN);
    if(variant != MONITOR_FRAME_NORMAL) monitor_ui_set_variant(root, variant);
    return root;
}

lv_obj_t *monitor_ui_create(lv_obj_t *parent)
{
    return monitor_ui_create_with_devices(parent, DEFAULT_DEVICES,
                                           (uint8_t)(sizeof(DEFAULT_DEVICES) /
                                                     sizeof(DEFAULT_DEVICES[0])));
}

lv_obj_t *monitor_ui_create_with_devices_variant(lv_obj_t *parent,
                                                 const monitor_device_t *devices,
                                                 uint8_t device_count,
                                                 monitor_frame_variant_t variant)
{
    return monitor_ui_create_with_devices_battery_variant(parent, devices, device_count,
                                                           78, variant);
}

lv_obj_t *monitor_ui_create_variant(lv_obj_t *parent, monitor_frame_variant_t variant)
{
    return monitor_ui_create_with_devices_variant(
        parent, DEFAULT_DEVICES,
        (uint8_t)(sizeof(DEFAULT_DEVICES) / sizeof(DEFAULT_DEVICES[0])), variant);
}

void monitor_ui_set_variant(lv_obj_t *root, monitor_frame_variant_t variant)
{
    if(root == NULL) return;
    monitor_ui_state_t *state = (monitor_ui_state_t *)lv_obj_get_user_data(root);
    if(state == NULL) return;

    state->variant = variant;
    if(state->variant_label != NULL) {
        lv_label_set_text(state->variant_label,
                          variant == MONITOR_FRAME_INVERTED ? "INVERT" : "NORMAL");
    }

    const bool inverted = variant == MONITOR_FRAME_INVERTED;
    apply_variant_recursive(root, inverted);
    lv_obj_set_style_bg_color(root, lv_color_hex(inverted ? 0x000000U : COLOR_WHITE), 0);
    lv_obj_invalidate(root);
}

monitor_frame_variant_t monitor_ui_get_variant(lv_obj_t *root)
{
    if(root == NULL) return MONITOR_FRAME_NORMAL;
    monitor_ui_state_t *state = (monitor_ui_state_t *)lv_obj_get_user_data(root);
    return state == NULL ? MONITOR_FRAME_NORMAL : state->variant;
}
