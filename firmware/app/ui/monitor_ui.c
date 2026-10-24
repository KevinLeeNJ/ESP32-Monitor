#include "monitor_ui.h"
#include "monitor_fonts.h"
#include "src/misc/lv_text_private.h"

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
#define ROLL_DURATION_MS 320U

extern const lv_image_dsc_t monitor_weather_clear;
extern const lv_image_dsc_t monitor_weather_partly_cloudy;
extern const lv_image_dsc_t monitor_weather_cloudy;
extern const lv_image_dsc_t monitor_weather_rain;
extern const lv_image_dsc_t monitor_weather_thunderstorm;
extern const lv_image_dsc_t monitor_weather_snow;
extern const lv_image_dsc_t monitor_weather_fog;
extern const lv_image_dsc_t monitor_weather_hail;
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
    .weather_condition = MONITOR_WEATHER_UNKNOWN,
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

typedef struct { int32_t x, y, w, h; } card_rect_t;
typedef struct {
    lv_obj_t *obj;
    card_rect_t from, to;
    bool pop;
} card_motion_t;
typedef enum { MOTION_IDLE, MOTION_REMOVE, MOTION_REFLOW, MOTION_POP } motion_phase_t;

typedef struct {
    lv_obj_t *viewport;
    lv_obj_t *current, *next;
    lv_obj_t *current_icon, *next_icon;
    char target[24];
    uint32_t elapsed;
    bool active;
} info_roll_t;

typedef struct {
    lv_obj_t *root;
    lv_obj_t *variant_label;
    monitor_frame_variant_t variant;
    monitor_wifi_state_t wifi_state;
    monitor_weather_condition_t weather_condition;
    lv_obj_t *time_label;
    lv_obj_t *clock_digits[4];
    lv_obj_t *clock_current[4];
    lv_obj_t *clock_next[4];
    lv_obj_t *clock_fallback;
    char clock_text[8];
    uint8_t clock_mask;
    uint32_t clock_elapsed;
    lv_obj_t *date_label;
    lv_obj_t *wifi_label;
    lv_obj_t *battery_label;
    lv_obj_t *battery_outline;
    lv_obj_t *wifi_slash;
    info_roll_t info[4]; /* weather, temperature, humidity, AQI */
    bool animations_enabled;
    motion_phase_t phase;
    uint32_t motion_elapsed, motion_duration;
    card_motion_t motions[4];
    uint8_t motion_count, motion_index;
    lv_obj_t *device_cards[4];
    bool entering[4];
    monitor_devices_snapshot_t devices, target_devices, pending_devices;
    bool devices_initialized, pending_valid;
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

static bool clock_valid(const char *text)
{
    return strlen(text) == 5 && text[2] == ':' &&
        text[0] >= '0' && text[0] <= '2' && text[1] >= '0' && text[1] <= '9' &&
        (text[0] != '2' || text[1] <= '3') &&
        text[3] >= '0' && text[3] <= '5' && text[4] >= '0' && text[4] <= '9';
}

static void finish_clock(monitor_ui_state_t *state)
{
    for (unsigned i = 0; i < 4; ++i) {
        if (!(state->clock_mask & (1U << i))) continue;
        char digit[] = {state->clock_text[i < 2 ? i : i + 1], 0};
        lv_label_set_text(state->clock_current[i], digit);
        lv_obj_set_y(state->clock_current[i], 0);
        lv_obj_add_flag(state->clock_next[i], LV_OBJ_FLAG_HIDDEN);
    }
    state->clock_mask = 0;
}

static void update_clock(monitor_ui_state_t *state, const char *text)
{
    if (!text || strcmp(state->clock_text, text) == 0) return;
    bool roll = state->animations_enabled &&
        (clock_valid(state->clock_text) || !strcmp(state->clock_text, "--:--")) && clock_valid(text);
    finish_clock(state); /* A correction during a roll starts from its last target. */
    char previous[8];
    memcpy(previous, state->clock_text, sizeof(previous));
    snprintf(state->clock_text, sizeof(state->clock_text), "%s", text);
    lv_label_set_text(state->clock_fallback, text);
    bool valid = clock_valid(text) || !strcmp(text, "--:--");
    if (valid) lv_obj_add_flag(state->clock_fallback, LV_OBJ_FLAG_HIDDEN);
    else lv_obj_clear_flag(state->clock_fallback, LV_OBJ_FLAG_HIDDEN);
    for (unsigned i = 0; i < 4; ++i) {
        if (valid) lv_obj_clear_flag(state->clock_digits[i], LV_OBJ_FLAG_HIDDEN);
        else lv_obj_add_flag(state->clock_digits[i], LV_OBJ_FLAG_HIDDEN);
        unsigned pos = i < 2 ? i : i + 1;
        if (roll && previous[pos] != text[pos]) {
            char digit[] = {text[pos], 0};
            lv_label_set_text(state->clock_next[i], digit);
            lv_obj_set_y(state->clock_next[i], monitor_ndot57_30.line_height);
            lv_obj_clear_flag(state->clock_next[i], LV_OBJ_FLAG_HIDDEN);
            state->clock_mask |= 1U << i;
        } else {
            char digit[] = {valid ? text[pos] : '0', 0};
            if (strcmp(lv_label_get_text(state->clock_current[i]), digit))
                lv_label_set_text(state->clock_current[i], digit);
        }
    }
    lv_obj_t *colon = lv_obj_get_child_by_name(state->time_label, "clock_colon");
    if (valid) lv_obj_clear_flag(colon, LV_OBJ_FLAG_HIDDEN);
    else lv_obj_add_flag(colon, LV_OBJ_FLAG_HIDDEN);
    state->clock_elapsed = 0;
}

static void make_clock(monitor_ui_state_t *state)
{
    lv_obj_t *clock = lv_obj_create(state->root);
    clear_object(clock);
    lv_obj_set_pos(clock, 16, 9);
    lv_obj_set_size(clock, 132, monitor_ndot57_30.line_height);
    set_name(clock, "time");
    state->time_label = clock;
    lv_point_t digit_size, colon_size;
    lv_text_get_size(&digit_size, "0", &monitor_ndot57_30, 0, 0, LV_COORD_MAX, LV_TEXT_FLAG_NONE);
    lv_text_get_size(&colon_size, ":", &monitor_ndot57_30, 0, 0, LV_COORD_MAX, LV_TEXT_FLAG_NONE);
    for (unsigned i = 0; i < 4; ++i) {
        lv_obj_t *slot = lv_obj_create(clock);
        clear_object(slot);
        int32_t x = i * (digit_size.x + 1) + (i >= 2 ? colon_size.x + 1 : 0);
        lv_obj_set_pos(slot, x, 0);
        lv_obj_set_size(slot, digit_size.x, monitor_ndot57_30.line_height);
        char name[24];
        snprintf(name, sizeof(name), "clock_digit_%u", i);
        set_name(slot, name);
        state->clock_digits[i] = slot;
        state->clock_current[i] = make_label(slot, "0", 0, 0, &monitor_ndot57_30, COLOR_BLACK);
        state->clock_next[i] = make_label(slot, "0", 0, 0, &monitor_ndot57_30, COLOR_BLACK);
        /* Fixed-width rollers retain proportional glyphs: narrow digits such
         * as 1 must be centered in both the visible and incoming labels. */
        lv_obj_set_width(state->clock_current[i], digit_size.x);
        lv_obj_set_width(state->clock_next[i], digit_size.x);
        lv_obj_set_style_text_align(state->clock_current[i], LV_TEXT_ALIGN_CENTER, 0);
        lv_obj_set_style_text_align(state->clock_next[i], LV_TEXT_ALIGN_CENTER, 0);
        lv_obj_add_flag(state->clock_next[i], LV_OBJ_FLAG_HIDDEN);
    }
    lv_obj_t *colon = make_label(clock, ":", 2 * (digit_size.x + 1), 0, &monitor_ndot57_30, COLOR_BLACK);
    set_name(colon, "clock_colon");
    state->clock_fallback = make_label(clock, "", 0, 0, &monitor_ndot57_30, COLOR_BLACK);
    lv_obj_set_style_text_letter_space(state->clock_fallback, 1, 0);
    update_clock(state, clock_time_text);
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
        default: return NULL;
    }
}

static void make_metric(lv_obj_t *card, int32_t card_width, int32_t y,
                        const monitor_device_metric_t *metric, const lv_font_t *font)
{
    lv_point_t name_size, value_size;
    lv_text_get_size(&name_size, metric->name, font, 0, 0, LV_COORD_MAX, LV_TEXT_FLAG_NONE);
    lv_text_get_size(&value_size, metric->value, font, 0, 0, LV_COORD_MAX, LV_TEXT_FLAG_NONE);
    if (name_size.x + value_size.x + 4 > card_width - 14) font = &monitor_misans_8;
    make_label(card, metric->name, 7, y, font, COLOR_MUTED);
    lv_obj_t *value_label = make_label(card, metric->value, 7, y, font, COLOR_BLACK);
    lv_obj_set_width(value_label, card_width - 14);
    lv_obj_set_style_text_align(value_label, LV_TEXT_ALIGN_RIGHT, 0);
    make_bar(card, 7, y + 16, card_width - 14,
             metric->percent_valid ? metric->percent : -1);
}

static int32_t visible_label_center_offset(lv_obj_t *label)
{
    const lv_font_t *font = lv_obj_get_style_text_font(label, 0);
    const char *text = lv_label_get_text(label);
    int32_t first_y = font->line_height;
    int32_t last_y = -1;
    uint32_t index = 0;
    while(text[index]) {
        uint32_t letter = lv_text_encoded_next(text, &index);
        lv_font_glyph_dsc_t glyph;
        if(!lv_font_get_glyph_dsc(font, &glyph, letter, 0) ||
           glyph.box_h == 0 || glyph.box_w == 0) continue;
        const int32_t top = font->line_height - font->base_line - glyph.box_h - glyph.ofs_y;
        const int32_t bottom = top + glyph.box_h - 1;
        if(first_y > top) first_y = top;
        if(last_y < bottom) last_y = bottom;
    }
    /* Font line boxes include baseline padding; center the visible glyphs. */
    return last_y < 0 ? 0 :
        (font->line_height - 1 - first_y - last_y) / 2;
}

static void center_info_label(lv_obj_t *label, int32_t x)
{
    const int32_t offset_y = visible_label_center_offset(label);
    lv_obj_align(label, LV_ALIGN_LEFT_MID, x, offset_y);
}

static void center_info_icon(lv_obj_t *image, const lv_image_dsc_t *source)
{
    /* A8 assets have asymmetric transparent margins. Align the visible shape,
     * not just its canvas, with the card's horizontal center line. */
    int32_t first_y = (int32_t)source->header.h;
    int32_t last_y = -1;
    for(uint32_t y = 0; y < source->header.h; ++y) {
        for(uint32_t x = 0; x < source->header.w; ++x) {
            if(source->data[y * source->header.stride + x] < 128) continue;
            if(first_y > (int32_t)y) first_y = (int32_t)y;
            last_y = (int32_t)y;
            break;
        }
    }
    const int32_t offset_y = last_y < 0 ? 0 :
        ((int32_t)source->header.h - 1 - first_y - last_y) / 2;
    lv_obj_set_size(image, source->header.w, source->header.h);
    lv_obj_align(image, LV_ALIGN_LEFT_MID, 5, offset_y);
}

static void set_info_icon(lv_obj_t *image, const lv_image_dsc_t *source)
{
    if(source == NULL) {
        lv_obj_add_flag(image, LV_OBJ_FLAG_HIDDEN);
        return;
    }
    lv_image_set_src(image, source);
    center_info_icon(image, source);
    lv_obj_clear_flag(image, LV_OBJ_FLAG_HIDDEN);
}

static lv_obj_t *make_icon(lv_obj_t *parent, const lv_image_dsc_t *source,
                           const char *name)
{
    lv_obj_t *image = lv_image_create(parent);
    set_info_icon(image, source);
    lv_obj_set_style_image_recolor(image, lv_color_hex(COLOR_BLACK), 0);
    lv_obj_set_style_image_recolor_opa(image, LV_OPA_COVER, 0);
    set_name(image, name);
    return image;
}

static void finish_info_roll(info_roll_t *roll)
{
    if (!roll->active) return;
    lv_obj_t *old = roll->current;
    roll->current = roll->next;
    roll->next = old;
    lv_obj_set_style_translate_y(roll->current, 0, 0);
    lv_obj_add_flag(roll->next, LV_OBJ_FLAG_HIDDEN);
    lv_obj_set_style_translate_y(roll->next, 0, 0);
    if (roll->current_icon) {
        old = roll->current_icon;
        roll->current_icon = roll->next_icon;
        roll->next_icon = old;
        lv_obj_set_style_translate_y(roll->current_icon, 0, 0);
        lv_obj_add_flag(roll->next_icon, LV_OBJ_FLAG_HIDDEN);
        lv_obj_set_style_translate_y(roll->next_icon, 0, 0);
    }
    roll->active = false;
}

static void update_info_roll(info_roll_t *roll, const char *text,
                              const lv_image_dsc_t *icon, bool animate)
{
    if (!strcmp(roll->target, text)) return;
    finish_info_roll(roll); /* A correction starts from the previous target. */
    snprintf(roll->target, sizeof(roll->target), "%s", text);
    lv_obj_t *label = animate ? roll->next : roll->current;
    lv_label_set_text(label, text);
    center_info_label(label, 0);
    if (roll->current_icon) set_info_icon(animate ? roll->next_icon : roll->current_icon, icon);
    if (!animate) return;
    /* All fields use the clock's 320 ms ease-out and the same frame driver.
     * Only the value viewports move; card borders and metric icons stay put. */
    int32_t height = lv_obj_get_style_height(roll->viewport, 0);
    lv_obj_set_style_translate_y(roll->next, height, 0);
    lv_obj_clear_flag(roll->next, LV_OBJ_FLAG_HIDDEN);
    if (roll->next_icon) lv_obj_set_style_translate_y(roll->next_icon, height, 0);
    roll->elapsed = 0;
    roll->active = true;
}

static const char *wifi_state_symbol(monitor_wifi_state_t state)
{
    switch(state) {
        case MONITOR_WIFI_PROVISIONING: return LV_SYMBOL_REFRESH;
        case MONITOR_WIFI_CONNECTED: return LV_SYMBOL_WIFI;
        case MONITOR_WIFI_OFFLINE:
        case MONITOR_WIFI_DISCONNECTED: return LV_SYMBOL_WIFI;
        default: return LV_SYMBOL_WARNING;
    }
}

static void format_temperature(char *text, size_t size, int tenths)
{
    if (tenths == MONITOR_TEMPERATURE_UNAVAILABLE) snprintf(text, size, "--");
    else {
        int magnitude = tenths < 0 ? -tenths : tenths;
        snprintf(text, size, "%s%d.%d°C", tenths < 0 ? "-" : "", magnitude / 10, magnitude % 10);
    }
}

void monitor_ui_apply_snapshot(lv_obj_t *root, const monitor_ui_snapshot_t *snapshot)
{
    if (root == NULL || snapshot == NULL) return;
    monitor_ui_state_t *state = (monitor_ui_state_t *)lv_obj_get_user_data(root);
    if (state == NULL) return;
    char text[24];
    update_clock(state, snapshot->time_text);
    const char *date = snapshot->wifi_state == MONITOR_WIFI_PROVISIONING ? "Configuring" :
                       snapshot->wifi_state == MONITOR_WIFI_OFFLINE ? "Offline" :
                       snapshot->wifi_state == MONITOR_WIFI_DISCONNECTED ? "Connecting" : snapshot->date_text;
    if (date && strcmp(lv_label_get_text(state->date_label), date) != 0)
        lv_label_set_text(state->date_label, date);
    if (state->wifi_state != snapshot->wifi_state) {
        state->wifi_state = snapshot->wifi_state;
        lv_label_set_text(state->wifi_label, wifi_state_symbol(snapshot->wifi_state));
        if (snapshot->wifi_state == MONITOR_WIFI_DISCONNECTED || snapshot->wifi_state == MONITOR_WIFI_OFFLINE) lv_obj_clear_flag(state->wifi_slash, LV_OBJ_FLAG_HIDDEN);
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
        update_info_roll(&state->info[0], monitor_weather_condition_abbrev(snapshot->weather_condition),
                          monitor_weather_condition_icon(snapshot->weather_condition), state->animations_enabled);
    }
    format_temperature(text, sizeof(text), snapshot->temperature_tenths_c);
    update_info_roll(&state->info[1], text, NULL, state->animations_enabled);
    snprintf(text, sizeof(text), snapshot->humidity_percent < 0 ? "--" : "%d%%", snapshot->humidity_percent);
    update_info_roll(&state->info[2], text, NULL, state->animations_enabled);
    snprintf(text, sizeof(text), snapshot->aqi < 0 ? "--" : "%d", snapshot->aqi);
    update_info_roll(&state->info[3], text, NULL, state->animations_enabled);
}

static lv_obj_t *make_wifi_status(lv_obj_t *parent, monitor_wifi_state_t state)
{
    lv_obj_t *icon = make_label(parent, wifi_state_symbol(state), 18, 63,
                                &monitor_misans_14,
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
    if(state != MONITOR_WIFI_DISCONNECTED && state != MONITOR_WIFI_OFFLINE) lv_obj_add_flag(slash, LV_OBJ_FLAG_HIDDEN);
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
    lv_obj_t *label = make_label(parent, text, 0, 0,
                                 &monitor_misans_12,
                                 value >= 0 ? COLOR_BLACK : COLOR_MUTED);
    const lv_font_t *font = lv_obj_get_style_text_font(label, 0);
    const int32_t line_y = y + (11 - font->line_height) / 2;
    lv_obj_set_pos(label, x + 27, line_y + visible_label_center_offset(label));
    set_name(label, "board_battery_value");
}

static void make_info_card(lv_obj_t *parent, int32_t x, int32_t y,
                            const lv_image_dsc_t *icon, const char *value,
                            const char *name, const char *icon_name, info_roll_t *roll)
{
    lv_obj_t *card = make_box(parent, x, y, 115, 36, 6, COLOR_WHITE, 1, COLOR_BLACK);
    set_name(card, name);
    const bool weather = strcmp(name, "weather_card") == 0;
    const bool aqi = strcmp(name, "aqi_card") == 0;
    if (weather) {
        lv_obj_t *clip = lv_obj_create(card);
        clear_object(clip);
        lv_obj_set_pos(clip, 0, 1);
        lv_obj_set_size(clip, 38, 34);
        roll->current_icon = make_icon(clip, icon, icon_name);
        roll->next_icon = make_icon(clip, icon, icon_name);
        lv_obj_add_flag(roll->next_icon, LV_OBJ_FLAG_HIDDEN);
    } else if (!aqi) {
        make_icon(card, icon, icon_name);
    } else {
        lv_obj_t *title = make_label(card, "AQI", 8, 0, &monitor_misans_12, COLOR_MUTED);
        center_info_label(title, 8);
    }
    roll->viewport = lv_obj_create(card);
    clear_object(roll->viewport);
    const int32_t value_x = aqi ? 58 : 39;
    lv_obj_set_pos(roll->viewport, value_x, 1);
    lv_obj_set_size(roll->viewport, 115 - value_x - 1, 34);
    set_name(roll->viewport, "value_viewport");
    roll->current = make_label(roll->viewport, value, 0, 0, &monitor_ndot57_20, COLOR_BLACK);
    roll->next = make_label(roll->viewport, value, 0, 0, &monitor_ndot57_20, COLOR_BLACK);
    for (unsigned i = 0; i < 2; ++i) {
        lv_obj_t *label = i ? roll->next : roll->current;
        lv_obj_set_width(label, 115 - value_x - 1);
        lv_label_set_long_mode(label, LV_LABEL_LONG_CLIP);
        center_info_label(label, 0);
    }
    lv_obj_add_flag(roll->next, LV_OBJ_FLAG_HIDDEN);
    snprintf(roll->target, sizeof(roll->target), "%s", value);
}

static void fill_device_card(lv_obj_t *card, const monitor_device_t *device,
                             uint8_t device_count, int32_t width)
{
    const bool compact = device_count >= 4;
    const lv_font_t *name_font = compact ? &monitor_misans_12 : &monitor_misans_14;
    const lv_font_t *metric_font = compact ? &monitor_misans_12 : &monitor_misans_14;
    const int32_t metric_y = compact ? 58 : 60;
    const int32_t metric_step = compact ? 39 : 40;

    lv_obj_clean(card);
    make_label(card, device->name, 7, 7, name_font, COLOR_BLACK);
    make_label(card, device->uptime, 7, 29, &monitor_misans_12, COLOR_MUTED);
    for (uint8_t i = 0; i < device->metric_count && i < 3; ++i)
        make_metric(card, width, metric_y + metric_step * i, &device->metrics[i], metric_font);
}

static lv_obj_t *make_device_card(lv_obj_t *parent, const monitor_device_t *device,
                                  uint8_t count, int32_t x, int32_t width)
{
    lv_obj_t *card = make_box(parent, x, DEVICE_TOP, width, DEVICE_HEIGHT, 8,
                              COLOR_WHITE, 2, COLOR_BLACK);
    fill_device_card(card, device, count, width);
    return card;
}

static void resize_device_contents(lv_obj_t *card, const monitor_device_t *device, int32_t width)
{
    /* Keep right-aligned values and bars attached to the moving right edge.
     * Recreating children per frame would add heap churn during the animation. */
    int32_t content_width = width > 14 ? width - 14 : 0;
    int32_t inner_width = width > 18 ? width - 18 : 0;
    for (unsigned i = 0; i < device->metric_count && i < 3; ++i) {
        lv_obj_set_width(lv_obj_get_child(card, 3 + i * 3), content_width);
        lv_obj_t *track = lv_obj_get_child(card, 4 + i * 3);
        lv_obj_set_width(track, content_width);
        if (lv_obj_get_child_count(track)) {
            int percent = device->metrics[i].percent_valid ? device->metrics[i].percent : 0;
            if (percent < 0) percent = 0;
            if (percent > 100) percent = 100;
            lv_obj_set_width(lv_obj_get_child(track, 0), inner_width * percent / 100);
        }
    }
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

static card_rect_t card_rect(lv_obj_t *obj)
{
    return (card_rect_t){lv_obj_get_x(obj), lv_obj_get_y(obj),
                         lv_obj_get_width(obj), lv_obj_get_height(obj)};
}

static void set_rect(lv_obj_t *obj, card_rect_t rect)
{
    lv_obj_set_pos(obj, rect.x, rect.y);
    lv_obj_set_size(obj, rect.w, rect.h);
}

static card_rect_t device_rect(unsigned count, unsigned index)
{
    int32_t width = (CONTENT_WIDTH - DEVICE_GAP * (count - 1)) / count;
    return (card_rect_t){CONTENT_X + index * (width + DEVICE_GAP), DEVICE_TOP,
                         width, DEVICE_HEIGHT};
}

static card_rect_t collapsed(card_rect_t rect)
{
    return (card_rect_t){rect.x + rect.w / 2 - 1, rect.y + rect.h / 2 - 1, 2, 2};
}

/* Display names can collide after the API's eight-character limit. Match
 * each occurrence once so duplicate names never alias or double-delete a card. */
static int device_match(const monitor_devices_snapshot_t *from, unsigned index,
                         const monitor_devices_snapshot_t *to)
{
    const char *name = from->devices[index].name;
    unsigned occurrence = 0;
    for (unsigned i = 0; i < index; ++i)
        if (!strcmp(from->devices[i].name, name)) ++occurrence;
    for (unsigned i = 0; i < to->count; ++i) {
        if (strcmp(to->devices[i].name, name)) continue;
        if (!occurrence) return (int)i;
        --occurrence;
    }
    return -1;
}

static void begin_phase(monitor_ui_state_t *state, motion_phase_t phase, uint32_t duration)
{
    state->phase = phase;
    state->motion_elapsed = 0;
    state->motion_duration = duration;
    state->motion_count = 0;
    state->motion_index = 0;
}

static void add_motion(monitor_ui_state_t *state, lv_obj_t *obj, card_rect_t from,
                        card_rect_t to, bool pop)
{
    state->motions[state->motion_count++] = (card_motion_t){obj, from, to, pop};
    set_rect(obj, from);
}

static void devices_chrome(monitor_ui_state_t *state)
{
    const monitor_devices_snapshot_t *snapshot = &state->devices;
    lv_obj_t *root = state->root;
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
    if (strcmp(lv_label_get_text(count), title)) lv_label_set_text(count, title);
    if (!snapshot->count) {
        lv_obj_t *empty = make_label(root, snapshot->status == MONITOR_DEVICES_AUTH_ERROR ? "DATA UNAVAILABLE" :
                                     (*status ? status : "NO ONLINE DEVICES"), 0, 177,
                                     &monitor_misans_14, COLOR_MUTED);
        lv_obj_set_width(empty, MONITOR_DISPLAY_WIDTH);
        lv_obj_set_style_text_align(empty, LV_TEXT_ALIGN_CENTER, 0);
        set_name(empty, "empty_devices");
    }
    if (state->variant != MONITOR_FRAME_NORMAL) apply_variant_recursive(root, true);
}

static void finish_motion(monitor_ui_state_t *state)
{
    state->phase = MOTION_IDLE;
    state->motion_count = 0;
    if (state->pending_valid) {
        monitor_devices_snapshot_t pending = state->pending_devices;
        state->pending_valid = false;
        monitor_ui_apply_devices_snapshot(state->root, &pending);
    }
}

/* Reuse survivors by name, even when the server changes order or replaces a
 * device without changing the count. No index is used as device identity. */
static void begin_reflow(monitor_ui_state_t *state, bool animate)
{
    lv_obj_t *next[4] = {0};
    lv_obj_update_layout(state->root);
    for (unsigned i = 0; i < state->devices.count; ++i) {
        if (device_match(&state->devices, i, &state->target_devices) < 0)
            lv_obj_delete(state->device_cards[i]);
        else
            set_name(state->device_cards[i], "device_moving");
    }
    begin_phase(state, MOTION_REFLOW, 240);
    for (unsigned i = 0; i < state->target_devices.count; ++i) {
        card_rect_t to = device_rect(state->target_devices.count, i);
        int old = device_match(&state->target_devices, i, &state->devices);
        state->entering[i] = old < 0;
        if (old >= 0) {
            next[i] = state->device_cards[old];
            card_rect_t from = card_rect(next[i]);
            if (from.w != to.w ||
                memcmp(&state->devices.devices[old], &state->target_devices.devices[i], sizeof(monitor_device_t)))
                fill_device_card(next[i], &state->target_devices.devices[i], state->target_devices.count, to.w);
            if (animate && (from.x != to.x || from.w != to.w))
                add_motion(state, next[i], from, to, false);
            else set_rect(next[i], to);
        } else {
            next[i] = make_device_card(state->root, &state->target_devices.devices[i],
                                       state->target_devices.count, to.x, to.w);
            if (animate) lv_obj_add_flag(next[i], LV_OBJ_FLAG_HIDDEN);
        }
        char name[24];
        snprintf(name, sizeof(name), "device_%u", i);
        set_name(next[i], name);
    }
    memcpy(state->device_cards, next, sizeof(next));
    state->devices = state->target_devices;
    state->devices_initialized = true;
    for (unsigned i = 0; i < state->devices.count; ++i)
        resize_device_contents(state->device_cards[i], &state->devices.devices[i],
                                lv_obj_get_style_width(state->device_cards[i], 0));
    devices_chrome(state);
    if (!animate) finish_motion(state);
}

static void begin_pop(monitor_ui_state_t *state)
{
    begin_phase(state, MOTION_POP, 240);
    for (unsigned i = 0; i < state->devices.count; ++i) {
        if (!state->entering[i]) continue;
        lv_obj_t *card = state->device_cards[i];
        card_rect_t to = device_rect(state->devices.count, i);
        add_motion(state, card, collapsed(to), to, true);
        lv_obj_clear_flag(card, LV_OBJ_FLAG_HIDDEN);
        state->entering[i] = false;
        break; /* Reuse the add animation, revealing one new card at a time. */
    }
    if (!state->motion_count) finish_motion(state);
}

void monitor_ui_apply_devices_snapshot(lv_obj_t *root, const monitor_devices_snapshot_t *snapshot)
{
    if (!root || !snapshot || snapshot->count > 4) return;
    monitor_ui_state_t *state = lv_obj_get_user_data(root);
    if (!state) return;
    bool values_active = state->clock_mask != 0;
    for (unsigned i = 0; i < 4; ++i) values_active |= state->info[i].active;
    if (state->phase != MOTION_IDLE || values_active) {
        state->pending_devices = *snapshot; /* Coalesce bursts to the latest data. */
        state->pending_valid = true;
        return;
    }
    bool topology = state->devices.count != snapshot->count;
    for (unsigned i = 0; !topology && i < snapshot->count; ++i)
        topology = strcmp(state->devices.devices[i].name, snapshot->devices[i].name) != 0;
    bool animate = state->animations_enabled && state->devices_initialized && topology;
    state->target_devices = *snapshot;
    if (!animate) {
        begin_reflow(state, false);
        return;
    }
    lv_obj_update_layout(root);
    begin_phase(state, MOTION_REMOVE, 160);
    for (unsigned i = 0; i < state->devices.count; ++i) {
        if (device_match(&state->devices, i, snapshot) >= 0) continue;
        lv_obj_t *card = state->device_cards[i];
        card_rect_t from = card_rect(card);
        add_motion(state, card, from, collapsed(from), false);
    }
    if (!state->motion_count) {
        begin_reflow(state, true);
        if (!state->motion_count) begin_pop(state);
    }
}

bool monitor_ui_animation_active(lv_obj_t *root)
{
    monitor_ui_state_t *state = root ? lv_obj_get_user_data(root) : NULL;
    if (!state) return false;
    if (state->phase != MOTION_IDLE || state->clock_mask || state->pending_valid) return true;
    for (unsigned i = 0; i < 4; ++i) if (state->info[i].active) return true;
    return false;
}

/* Fixed-point cubic ease-out. A short, single overshoot gives new cards a pop
 * without repeating/bouncing continuously. No opacity effects on a 1bpp panel. */
static int32_t motion_progress(uint32_t elapsed, uint32_t duration, bool pop)
{
    if (elapsed >= duration) return 1024;
    int32_t t = (int32_t)(elapsed * 1024 / duration);
    if (pop) {
        if (t < 768) return t * 1080 / 768;
        return 1080 - (t - 768) * 56 / 256;
    }
    int32_t remain = 1024 - t;
    return 1024 - (int32_t)((int64_t)remain * remain * remain / (1024 * 1024));
}

static int32_t lerp(int32_t from, int32_t to, int32_t t)
{
    return from + (to - from) * t / 1024;
}

static void advance_cards(monitor_ui_state_t *state, uint32_t elapsed)
{
    while (state->phase != MOTION_IDLE) {
        uint32_t total = state->motion_duration;
        uint32_t remaining = total - state->motion_elapsed;
        uint32_t step = elapsed < remaining ? elapsed : remaining;
        state->motion_elapsed += step;
        elapsed -= step;
        /* Collapse departing cards one at a time, then reflow survivors one
         * at a time. A full-width dirty region must not combine several cards. */
        if (state->motion_index < state->motion_count) {
            card_motion_t *motion = &state->motions[state->motion_index];
            int32_t p = motion_progress(state->motion_elapsed, state->motion_duration, motion->pop);
            card_rect_t r = {lerp(motion->from.x, motion->to.x, p), lerp(motion->from.y, motion->to.y, p),
                              lerp(motion->from.w, motion->to.w, p), lerp(motion->from.h, motion->to.h, p)};
            bool width_changed = lv_obj_get_style_width(motion->obj, 0) != r.w;
            set_rect(motion->obj, r);
            if (width_changed) {
                for (unsigned j = 0; j < state->devices.count; ++j)
                    if (motion->obj == state->device_cards[j])
                        resize_device_contents(motion->obj, &state->devices.devices[j], r.w);
            }
        }
        if (state->motion_elapsed < total) break;
        if (++state->motion_index < state->motion_count) {
            state->motion_elapsed = 0;
            if (!elapsed) break;
            continue;
        }
        switch (state->phase) {
            case MOTION_REMOVE:
                begin_reflow(state, true);
                if (!state->motion_count) begin_pop(state);
                break;
            case MOTION_REFLOW: begin_pop(state); break;
            case MOTION_POP: begin_pop(state); break;
            default: finish_motion(state); break;
        }
        if (!elapsed) break;
    }
}

bool monitor_ui_advance(lv_obj_t *root, uint32_t elapsed_ms)
{
    monitor_ui_state_t *state = root ? lv_obj_get_user_data(root) : NULL;
    if (!state || !elapsed_ms || !monitor_ui_animation_active(root)) return false;
    bool values_active = state->clock_mask != 0;
    for (unsigned i = 0; i < 4; ++i) values_active |= state->info[i].active;
    if (state->clock_mask) {
        uint32_t remaining = ROLL_DURATION_MS - state->clock_elapsed;
        state->clock_elapsed += elapsed_ms < remaining ? elapsed_ms : remaining;
        int32_t offset = motion_progress(state->clock_elapsed, ROLL_DURATION_MS, false) * monitor_ndot57_30.line_height / 1024;
        for (unsigned i = 0; i < 4; ++i) {
            if (!(state->clock_mask & (1U << i))) continue;
            lv_obj_set_y(state->clock_current[i], -offset);
            lv_obj_set_y(state->clock_next[i], monitor_ndot57_30.line_height - offset);
        }
        if (state->clock_elapsed == ROLL_DURATION_MS) finish_clock(state);
    }
    for (unsigned i = 0; i < 4; ++i) {
        info_roll_t *roll = &state->info[i];
        if (!roll->active) continue;
        uint32_t remaining = ROLL_DURATION_MS - roll->elapsed;
        roll->elapsed += elapsed_ms < remaining ? elapsed_ms : remaining;
        int32_t height = lv_obj_get_style_height(roll->viewport, 0);
        int32_t offset = motion_progress(roll->elapsed, ROLL_DURATION_MS, false) * height / 1024;
        lv_obj_set_style_translate_y(roll->current, -offset, 0);
        lv_obj_set_style_translate_y(roll->next, height - offset, 0);
        if (roll->current_icon) {
            lv_obj_set_style_translate_y(roll->current_icon, -offset, 0);
            lv_obj_set_style_translate_y(roll->next_icon, height - offset, 0);
        }
        if (roll->elapsed == ROLL_DURATION_MS) finish_info_roll(roll);
    }
    /* Value rolls take priority (including a minute boundary arriving during
     * a card transition). Card geometry stays still until the rolls settle. */
    if (!values_active) advance_cards(state, elapsed_ms);
    bool values_finished = state->clock_mask == 0;
    for (unsigned i = 0; i < 4; ++i) values_finished &= !state->info[i].active;
    if (values_finished && state->phase == MOTION_IDLE && state->pending_valid) {
        monitor_devices_snapshot_t pending = state->pending_devices;
        state->pending_valid = false;
        monitor_ui_apply_devices_snapshot(root, &pending);
    }
    return true;
}

void monitor_ui_set_animations_enabled(lv_obj_t *root, bool enabled)
{
    monitor_ui_state_t *state = root ? lv_obj_get_user_data(root) : NULL;
    if (!state) return;
    state->animations_enabled = enabled;
    if (!enabled) {
        /* Finish all finite phases and any coalesced snapshot, retaining data. */
        while (monitor_ui_animation_active(root)) monitor_ui_advance(root, 2000);
    }
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

    monitor_ui_state_t *state = (monitor_ui_state_t *)lv_zalloc(sizeof(*state));
    if(state == NULL) return NULL;

    lv_obj_t *root = lv_obj_create(parent);
    clear_object(root);
    lv_obj_set_pos(root, 0, 0);
    lv_obj_set_size(root, MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT);
    lv_obj_set_style_bg_opa(root, LV_OPA_COVER, 0);
    lv_obj_set_style_bg_color(root, lv_color_hex(COLOR_WHITE), 0);
    lv_obj_set_style_text_font(root, &monitor_misans_14, 0);
    lv_obj_set_user_data(root, state);
    lv_obj_add_event_cb(root, monitor_ui_state_delete_cb, LV_EVENT_DELETE, NULL);

    state->root = root;
    state->variant = variant;
    state->wifi_state = wifi_state;
    state->weather_condition = demo_data.weather_condition;

    make_box(root, 5, 5, 390, 290, 9, COLOR_WHITE, 2, COLOR_BLACK);

    /* Header: time and four compact rounded data cards. Weather has no text
     * label; temperature and humidity are identified by sourced icons. */
    make_clock(state);
    state->date_label = make_label(root, clock_date_text, 19, 43, &monitor_misans_12, COLOR_MUTED);
    set_name(state->date_label, "clock_date");
    state->wifi_label = make_wifi_status(root, wifi_state);
    state->wifi_slash = lv_obj_get_child_by_name(root, "wifi_slash");
    make_board_battery(root, 38, 65, demo_data.board_battery_percent);
    state->battery_label = lv_obj_get_child_by_name(root, "board_battery_value");
    state->battery_outline = lv_obj_get_child_by_name(root, "board_battery_outline");

    /* Keep the four data cards together as a 2x2 block in the upper-right. */
    const monitor_weather_condition_t weather_condition = demo_data.weather_condition;
    make_info_card(root, 150, 8, monitor_weather_condition_icon(weather_condition),
                   monitor_weather_condition_abbrev(weather_condition), "weather_card",
                   "weather_icon", &state->info[0]);
    char temperature_text[16];
    format_temperature(temperature_text, sizeof(temperature_text), demo_data.temperature_tenths_c);
    char humidity_text[12];
    snprintf(humidity_text, sizeof(humidity_text), demo_data.humidity_percent < 0 ? "--" : "%d%%", demo_data.humidity_percent);
    char aqi_text[12];
    if(demo_data.aqi < 0) snprintf(aqi_text, sizeof(aqi_text), "--");
    else snprintf(aqi_text, sizeof(aqi_text), "%d", demo_data.aqi);
    make_info_card(root, 270, 8, &monitor_metric_thermometer, temperature_text,
                   "temperature_card", "metric_icon", &state->info[1]);
    make_info_card(root, 150, 48, &monitor_metric_droplets, humidity_text,
                   "humidity_card", "metric_icon", &state->info[2]);
    make_info_card(root, 270, 48, NULL, aqi_text, "aqi_card", NULL, &state->info[3]);

    make_box(root, 15, 91, 370, 1, 0, COLOR_BLACK, 0, COLOR_BLACK);
    make_label(root, "DEVICES", 16, 95, &monitor_misans_14, COLOR_BLACK);
    lv_obj_t *count_label = make_label(root, "0 ONLINE", 320, 97, &monitor_misans_12, COLOR_MUTED);
    lv_obj_set_style_text_align(count_label, LV_TEXT_ALIGN_RIGHT, 0);
    set_name(count_label, "device_count");

    monitor_devices_snapshot_t initial = {.count = device_count, .status = MONITOR_DEVICES_READY};
    if (device_count) memcpy(initial.devices, devices, device_count * sizeof(*devices));
    monitor_ui_apply_devices_snapshot(root, &initial);

    state->variant_label = make_label(root, "", 0, 0, &monitor_misans_12, COLOR_MUTED);
    lv_obj_add_flag(state->variant_label, LV_OBJ_FLAG_HIDDEN);
    if(variant != MONITOR_FRAME_NORMAL) monitor_ui_set_variant(root, variant);
    return root;
}

lv_obj_t *monitor_ui_create_waiting(lv_obj_t *parent, monitor_frame_variant_t variant)
{
    lv_obj_t *root = monitor_ui_create_with_devices_battery_wifi_variant(
        parent, NULL, 0, demo_data.board_battery_percent, demo_data.wifi_state, variant);
    if (!root) return NULL;
    monitor_ui_snapshot_t pending = {
        .time_text = "--:--", .date_text = "--", .wifi_state = demo_data.wifi_state,
        .battery_percent = demo_data.board_battery_percent, .weather_condition = MONITOR_WEATHER_UNKNOWN,
        .temperature_tenths_c = MONITOR_TEMPERATURE_UNAVAILABLE, .humidity_percent = -1, .aqi = -1,
    };
    monitor_ui_apply_snapshot(root, &pending);
    monitor_devices_snapshot_t devices = {.status = MONITOR_DEVICES_WAITING};
    monitor_ui_apply_devices_snapshot(root, &devices);
    monitor_ui_set_animations_enabled(root, true);
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
