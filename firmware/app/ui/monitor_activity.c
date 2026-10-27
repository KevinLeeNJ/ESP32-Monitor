#include "monitor_ui.h"
#include "monitor_fonts.h"

#include <stdio.h>
#include <string.h>

#define WEEKS 32
#define DAYS 7
#define BLACK 0x111111U
#define WHITE 0xf7f7f7U

extern const lv_image_dsc_t activity_openai, activity_github, activity_circle_question_mark;
extern const lv_image_dsc_t activity_circle_minus, activity_loader_circle;
extern const lv_image_dsc_t activity_message_circle, activity_shield_alert;
extern const lv_image_dsc_t activity_circle_check, activity_circle_x, activity_circle_slash;

typedef struct { const char *name; const lv_image_dsc_t *icon; } task_style_t;
static const task_style_t TASK_STYLES[] = {
    {"Unknown", &activity_circle_question_mark}, {"Idle", &activity_circle_minus},
    {"Running", &activity_loader_circle}, {"Waiting input", &activity_message_circle},
    {"Waiting approval", &activity_shield_alert}, {"Completed", &activity_circle_check},
    {"Failed", &activity_circle_x}, {"Cancelled", &activity_circle_slash},
};

static lv_obj_t *icon(lv_obj_t *parent, const lv_image_dsc_t *source, int x, int y, bool inv)
{
    lv_obj_t *obj = lv_image_create(parent);
    lv_image_set_src(obj, source);
    lv_obj_set_pos(obj, x, y);
    lv_obj_set_style_image_recolor(obj, lv_color_hex(inv ? WHITE : BLACK), 0);
    lv_obj_set_style_image_recolor_opa(obj, LV_OPA_COVER, 0);
    return obj;
}


typedef struct {
    uint8_t levels[WEEKS * DAYS];
    bool inverted;
} activity_state_t;

static lv_obj_t *box(lv_obj_t *parent, int x, int y, int w, int h, int radius,
                     bool ink, bool border, bool inverted)
{
    lv_obj_t *obj = lv_obj_create(parent);
    lv_obj_remove_style_all(obj);
    lv_obj_clear_flag(obj, LV_OBJ_FLAG_SCROLLABLE);
    lv_obj_set_pos(obj, x, y);
    lv_obj_set_size(obj, w, h);
    lv_obj_set_style_bg_opa(obj, LV_OPA_COVER, 0);
    lv_obj_set_style_bg_color(obj, lv_color_hex((ink != inverted) ? BLACK : WHITE), 0);
    lv_obj_set_style_border_color(obj, lv_color_hex(inverted ? WHITE : BLACK), 0);
    lv_obj_set_style_border_width(obj, border ? 1 : 0, 0);
    lv_obj_set_style_radius(obj, radius, 0);
    return obj;
}

static lv_obj_t *label(lv_obj_t *parent, const char *text, int x, int y, int width,
                       const lv_font_t *font, bool inverted)
{
    lv_obj_t *obj = lv_label_create(parent);
    lv_label_set_text(obj, text ? text : "--");
    lv_label_set_long_mode(obj, LV_LABEL_LONG_DOT);
    lv_obj_set_pos(obj, x, y);
    lv_obj_set_size(obj, width, font->line_height);
    lv_obj_set_style_text_font(obj, font, 0);
    lv_obj_set_style_text_color(obj, lv_color_hex(inverted ? WHITE : BLACK), 0);
    return obj;
}


static void progress(lv_obj_t *parent, int x, int y, int width, int percent, bool inv)
{
    lv_obj_t *track = box(parent, x, y, width, 6, 2, false, true, inv);
    if(percent > 100) percent = 100;
    if(percent > 0) {
        int fill = (width - 4) * percent / 100;
        if(fill > 0) box(track, 2, 1, fill, 4, 0, true, false, inv);
    }
}

static void format_time(char *out, size_t size, int seconds)
{
    if(seconds < 0) snprintf(out, size, "--:--");
    else snprintf(out, size, "%d:%02d", seconds / 60, seconds % 60);
}

static void usage_row(lv_obj_t *parent, const char *name, int y, int value, bool inv)
{
    char text[12];
    if(value < 0) snprintf(text, sizeof(text), "--");
    else snprintf(text, sizeof(text), "%d%%", value > 100 ? 100 : value);
    label(parent, name, 9, y, 105, &monitor_misans_12, inv);
    lv_obj_t *amount = label(parent, text, 115, y, 54, &monitor_misans_12, inv);
    lv_obj_set_style_text_align(amount, LV_TEXT_ALIGN_RIGHT, 0);
    progress(parent, 9, y + 15, 160, value, inv);
}

static void draw_wall(lv_event_t *event)
{
    lv_obj_t *obj = lv_event_get_target(event);
    activity_state_t *state = lv_event_get_user_data(event);
    lv_layer_t *layer = lv_event_get_layer(event);
    lv_area_t coords;
    lv_obj_get_coords(obj, &coords);
    lv_draw_rect_dsc_t dsc;
    lv_draw_rect_dsc_init(&dsc);
    dsc.bg_opa = LV_OPA_COVER;
    dsc.bg_color = lv_color_hex(state->inverted ? WHITE : BLACK);
    /* Shared one-pixel grid lines bound the centered solid squares. */
    for(int week = 0; week <= WEEKS; ++week) {
        int x = coords.x1 + week * 11;
        lv_area_t line = {x, coords.y1, x, coords.y1 + DAYS * 11};
        lv_draw_rect(layer, &dsc, &line);
    }
    for(int day = 0; day <= DAYS; ++day) {
        int y = coords.y1 + day * 11;
        lv_area_t line = {coords.x1, y, coords.x1 + WEEKS * 11, y};
        lv_draw_rect(layer, &dsc, &line);
    }
    static const uint8_t sizes[] = {0, 4, 6, 8, 10};
    for(int week = 0; week < WEEKS; ++week) {
        for(int day = 0; day < DAYS; ++day) {
            unsigned level = state->levels[week * DAYS + day];
            if(level > 4) level = 4;
            int size = sizes[level];
            if(!size) continue;
            int x = coords.x1 + week * 11 + 1 + (10 - size) / 2;
            int y = coords.y1 + day * 11 + 1 + (10 - size) / 2;
            lv_area_t square = {x, y, x + size - 1, y + size - 1};
            lv_draw_rect(layer, &dsc, &square);
        }
    }
}

static int days_in_month(int year, int month)
{
    static const uint8_t days[] = {31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31};
    if(month < 1 || month > 12) return 0;
    int count = days[month - 1];
    if(month == 2 && year % 4 == 0 && (year % 100 != 0 || year % 400 == 0)) ++count;
    return count;
}

static void month_labels(lv_obj_t *root, const monitor_activity_data_t *data, bool inv)
{
    static const char *const months[] = {"JAN", "FEB", "MAR", "APR", "MAY", "JUN",
                                          "JUL", "AUG", "SEP", "OCT", "NOV", "DEC"};
    int year = data->github_start_year, month = data->github_start_month;
    int day = data->github_start_day;
    if(year < 1 || year > 9998 || day < 1 || day > days_in_month(year, month)) return;
    /* Avoid a misleading overlapping label when the range starts just before
     * a month boundary; otherwise the first partial month is shown. */
    int next_boundary = days_in_month(year, month) - day + 1;
    int last_x = -100;
    if(next_boundary >= 21) {
        label(root, months[month - 1], 24, 37, 30, &monitor_misans_12, inv);
        last_x = 24;
    }
    for(int i = 0; i < WEEKS * DAYS; ++i) {
        if(i > 0 && day == 1) {
            int x = 24 + (i / DAYS) * 11;
            if(x > 349) x = 349;
            if(x - last_x >= 30) {
                label(root, months[month - 1], x, 37, 30, &monitor_misans_12, inv);
                last_x = x;
            }
        }
        if(++day > days_in_month(year, month)) {
            day = 1;
            if(++month > 12) { month = 1; ++year; }
        }
    }
}

static void delete_state(lv_event_t *event)
{
    lv_free(lv_event_get_user_data(event));
}

lv_obj_t *monitor_activity_create(lv_obj_t *parent, const monitor_activity_data_t *data,
                                  monitor_frame_variant_t variant)
{
    if(!parent || !data) return NULL;
    activity_state_t *state = lv_zalloc(sizeof(*state));
    if(!state) return NULL;
    memcpy(state->levels, data->levels, sizeof(state->levels));
    state->inverted = variant == MONITOR_FRAME_INVERTED;
    bool inv = state->inverted;
    lv_obj_t *root = box(parent, 0, 0, 400, 300, 0, false, false, inv);
    lv_obj_add_event_cb(root, delete_state, LV_EVENT_DELETE, state);
    lv_obj_t *frame = box(root, 5, 5, 390, 290, 9, false, true, inv);
    lv_obj_set_style_border_width(frame, 2, 0);

    icon(root, &activity_github, 24, 10, inv);
    label(root, "Contributions", 53, 11, 200, &monitor_misans_20, inv);
    month_labels(root, data, inv);
    label(root, "M", 11, 54, 10, &monitor_misans_8, inv);
    label(root, "W", 11, 76, 10, &monitor_misans_8, inv);
    label(root, "F", 11, 98, 10, &monitor_misans_8, inv);
    lv_obj_t *wall = lv_obj_create(root);
    lv_obj_remove_style_all(wall);
    lv_obj_clear_flag(wall, LV_OBJ_FLAG_SCROLLABLE);
    lv_obj_set_pos(wall, 24, 54);
    lv_obj_set_size(wall, 353, 78);
    lv_obj_add_event_cb(wall, draw_wall, LV_EVENT_DRAW_MAIN, state);
    box(root, 15, 142, 370, 1, 0, true, false, inv);

    lv_obj_t *agent = box(root, 15, 150, 180, 134, 8, false, true, inv);
    icon(agent, &activity_openai, 8, 3, inv);
    label(agent, "Codex", 37, 4, 130, &monitor_misans_20, inv);
    usage_row(agent, "5h Usage", 31, data->usage_5h_percent, inv);
    usage_row(agent, "Weekly Usage", 58, data->usage_week_percent, inv);
    box(agent, 1, 87, 178, 1, 0, true, false, inv);
    unsigned task = (unsigned)data->agent_state;
    if(task >= sizeof(TASK_STYLES) / sizeof(TASK_STYLES[0])) task = MONITOR_TASK_UNKNOWN;
    icon(agent, TASK_STYLES[task].icon, 10, 99, inv);
    label(agent, "Current Task", 42, 92, 127, &monitor_misans_12, inv);
    const lv_font_t *task_font = task == MONITOR_TASK_WAITING_APPROVAL ? &monitor_misans_12 : &monitor_misans_14;
    label(agent, TASK_STYLES[task].name, 42, 109, 127, task_font, inv);

    lv_obj_t *music = box(root, 205, 150, 180, 134, 8, false, true, inv);
    label(music, LV_SYMBOL_AUDIO, 12, 7, 18, &lv_font_montserrat_14, inv);
    label(music, "Music", 37, 4, 68, &monitor_misans_20, inv);
    const char *source = data->music_available && data->music_source && data->music_source[0]
        ? data->music_source : "--";
    lv_obj_t *source_label = label(music, source, 105, 10, 63, &monitor_misans_12, inv);
    lv_obj_set_style_text_align(source_label, LV_TEXT_ALIGN_RIGHT, 0);
    /* Pixel-aligned symbols stay crisp after conversion to the 1-bit panel. */
    lv_obj_t *playback = box(music, 127, 35, 42, 42, 21, false, true, inv);
    lv_obj_set_style_border_width(playback, 2, 0);
    if(!data->music_available) {
        label(playback, "--", 13, 13, 18, &monitor_misans_12, inv);
    } else if(data->music_playing) {
        box(playback, 11, 9, 5, 20, 1, true, false, inv);
        box(playback, 22, 9, 5, 20, 1, true, false, inv);
    } else {
        /* Rows form a right-pointing triangle, centered optically in the ring. */
        for(int row = 0; row < 19; ++row) {
            int width = 1 + (9 - (row < 9 ? 9 - row : row - 9)) * 15 / 9;
            box(playback, 13, 9 + row, width, 1, 0, true, false, inv);
        }
    }
    label(music, data->music_title, 10, 31, 115, &monitor_misans_20, inv);
    label(music, data->music_artist, 10, 58, 115, &monitor_misans_14, inv);
    int percent = -1;
    if(data->music_available && data->music_position_seconds >= 0 && data->music_duration_seconds > 0) {
        int pos = data->music_position_seconds;
        if(pos > data->music_duration_seconds) pos = data->music_duration_seconds;
        percent = (int)((int64_t)pos * 100 / data->music_duration_seconds);
    }
    progress(music, 10, 99, 158, percent, inv);
    char position[16], duration[16];
    format_time(position, sizeof(position), data->music_available ? data->music_position_seconds : -1);
    format_time(duration, sizeof(duration), data->music_available ? data->music_duration_seconds : -1);
    label(music, position, 10, 109, 60, &monitor_misans_12, inv);
    lv_obj_t *end = label(music, duration, 108, 109, 60, &monitor_misans_12, inv);
    lv_obj_set_style_text_align(end, LV_TEXT_ALIGN_RIGHT, 0);
    return root;
}
