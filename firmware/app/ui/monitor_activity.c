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

typedef struct {
    lv_obj_t *track, *fill;
    int width;
    int fill_px; /* Last applied fill width; skips redundant geometry work. */
} activity_bar_t;

typedef struct {
    activity_state_t wall;
    bool github_available;
    int github_start_year, github_start_month, github_start_day;
    bool playback_available, playback_playing;
    lv_obj_t *wall_obj, *wall_unavailable, *weekday[3];
    lv_obj_t *usage_value[2];
    activity_bar_t usage_bar[2];
    lv_obj_t *task_icon, *task_name;
    lv_obj_t *music_source, *music_title, *music_artist;
    lv_obj_t *playback;
    activity_bar_t music_bar;
    lv_obj_t *music_position, *music_duration;
} activity_view_t;

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

static const lv_font_t *music_font(const char *value, const lv_font_t *latin)
{
    if (value) for (const unsigned char *p = (const unsigned char *)value; *p; ++p)
        if (*p >= 0x80) {
            const lv_font_t *font = monitor_music_font();
            return font ? font : latin;
        }
    return latin;
}

static void set_music_label(lv_obj_t *obj, const char *value, const lv_font_t *latin)
{
    const lv_font_t *font = music_font(value, latin);
    const char *shown = value && value[0] ? value : "--";
    if(strcmp(lv_label_get_text(obj), shown)) lv_label_set_text(obj, shown);
    /* The dynamic CJK font is taller than the Latin default; keep the fixed
     * viewport in sync or glyphs clip after a live update. */
    if(lv_obj_get_style_text_font(obj, LV_PART_MAIN) != font) {
        lv_obj_set_style_text_font(obj, font, 0);
        lv_obj_set_height(obj, font->line_height);
    }
}


static activity_bar_t progress_create(lv_obj_t *parent, int x, int y, int width, int percent, bool inv)
{
    activity_bar_t bar = {.width = width - 4};
    bar.track = box(parent, x, y, width, 6, 2, false, true, inv);
    if(percent > 100) percent = 100;
    int fill = percent > 0 ? bar.width * percent / 100 : 0;
    bar.fill = box(bar.track, 2, 1, fill > 0 ? fill : 1, 4, 0, true, false, inv);
    if(!fill) lv_obj_add_flag(bar.fill, LV_OBJ_FLAG_HIDDEN);
    bar.fill_px = fill;
    return bar;
}

static void progress_apply(activity_bar_t *bar, int percent)
{
    if(percent > 100) percent = 100;
    int fill = percent > 0 ? bar->width * percent / 100 : 0;
    if(fill == bar->fill_px) return;
    bar->fill_px = fill;
    if(fill > 0) {
        lv_obj_set_width(bar->fill, fill);
        lv_obj_clear_flag(bar->fill, LV_OBJ_FLAG_HIDDEN);
    } else {
        lv_obj_add_flag(bar->fill, LV_OBJ_FLAG_HIDDEN);
    }
}

static void format_time(char *out, size_t size, int seconds)
{
    if(seconds < 0) snprintf(out, size, "--:--");
    else snprintf(out, size, "%d:%02d", seconds / 60, seconds % 60);
}

static void usage_value_text(char *text, size_t size, int value)
{
    if(value < 0) snprintf(text, size, "--");
    else snprintf(text, size, "%d%%", value > 100 ? 100 : value);
}

static void usage_apply(activity_view_t *view, int index, int value)
{
    char text[12];
    usage_value_text(text, sizeof(text), value);
    if(strcmp(lv_label_get_text(view->usage_value[index]), text))
        lv_label_set_text(view->usage_value[index], text);
    progress_apply(&view->usage_bar[index], value);
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
        lv_obj_t *first = label(root, months[month - 1], 24, 37, 30, &monitor_misans_12, inv);
        lv_obj_set_name(first, "activity_month");
        last_x = 24;
    }
    for(int i = 0; i < WEEKS * DAYS; ++i) {
        if(i > 0 && day == 1) {
            int x = 24 + (i / DAYS) * 11;
            if(x > 349) x = 349;
            if(x - last_x >= 30) {
                lv_obj_t *marker = label(root, months[month - 1], x, 37, 30, &monitor_misans_12, inv);
                lv_obj_set_name(marker, "activity_month");
                last_x = x;
            }
        }
        if(++day > days_in_month(year, month)) {
            day = 1;
            if(++month > 12) { month = 1; ++year; }
        }
    }
}

static void clear_month_labels(lv_obj_t *root)
{
    lv_obj_t *marker;
    while((marker = lv_obj_get_child_by_name(root, "activity_month")) != NULL)
        lv_obj_delete(marker);
}

static void delete_state(lv_event_t *event)
{
    lv_free(lv_event_get_user_data(event));
}

monitor_activity_data_t monitor_activity_demo(void)
{
    monitor_activity_data_t data = {
        .github_available = true, .github_start_year = 2026, .github_start_month = 3, .github_start_day = 16,
        .agent_state = MONITOR_TASK_RUNNING, .usage_5h_percent = 36, .usage_week_percent = 24,
        .music_available = true, .music_playing = true,
        .music_title = "Blue", .music_artist = "Yung Kai", .music_source = "Spotify",
        .music_position_seconds = 88, .music_duration_seconds = 225
    };
    for(unsigned week = 0; week < MONITOR_ACTIVITY_WEEKS; ++week)
        for(unsigned day = 0; day < MONITOR_ACTIVITY_DAYS; ++day) {
            unsigned n = (week * 17 + day * 11 + week * day * 3) % 13;
            data.levels[week * MONITOR_ACTIVITY_DAYS + day] = n < 5 ? 0 : n < 8 ? 1 : n < 10 ? 2 : n < 12 ? 3 : 4;
        }
    return data;
}

static void draw_playback(activity_view_t *view, bool available, bool playing)
{
    bool inv = view->wall.inverted;
    lv_obj_clean(view->playback);
    if(!available) {
        label(view->playback, "--", 13, 13, 18, &monitor_misans_12, inv);
    } else if(playing) {
        box(view->playback, 11, 9, 5, 20, 1, true, false, inv);
        box(view->playback, 22, 9, 5, 20, 1, true, false, inv);
    } else {
        /* Rows form a right-pointing triangle, centered optically in the ring. */
        for(int row = 0; row < 19; ++row) {
            int width = 1 + (9 - (row < 9 ? 9 - row : row - 9)) * 15 / 9;
            box(view->playback, 13, 9 + row, width, 1, 0, true, false, inv);
        }
    }
}

static void apply_github(lv_obj_t *root, activity_view_t *view, const monitor_activity_data_t *data)
{
    /* Heartbeats re-publish an unchanged wall every second; only a semantic
     * change may repaint the 353x78 grid or rebuild the month labels. */
    if(data->github_available == view->github_available &&
       data->github_start_year == view->github_start_year &&
       data->github_start_month == view->github_start_month &&
       data->github_start_day == view->github_start_day &&
       memcmp(view->wall.levels, data->levels, sizeof(view->wall.levels)) == 0) return;
    view->github_available = data->github_available;
    view->github_start_year = data->github_start_year;
    view->github_start_month = data->github_start_month;
    view->github_start_day = data->github_start_day;
    memcpy(view->wall.levels, data->levels, sizeof(view->wall.levels));
    lv_obj_invalidate(view->wall_obj);
    clear_month_labels(root);
    if(data->github_available) month_labels(root, data, view->wall.inverted);
    for(unsigned i = 0; i < 3; i++) {
        if(data->github_available) lv_obj_clear_flag(view->weekday[i], LV_OBJ_FLAG_HIDDEN);
        else lv_obj_add_flag(view->weekday[i], LV_OBJ_FLAG_HIDDEN);
    }
    if(data->github_available) {
        lv_obj_clear_flag(view->wall_obj, LV_OBJ_FLAG_HIDDEN);
        lv_obj_add_flag(view->wall_unavailable, LV_OBJ_FLAG_HIDDEN);
    } else {
        lv_obj_add_flag(view->wall_obj, LV_OBJ_FLAG_HIDDEN);
        lv_obj_clear_flag(view->wall_unavailable, LV_OBJ_FLAG_HIDDEN);
    }
}

static void apply_music(activity_view_t *view, const monitor_activity_data_t *data)
{
    set_music_label(view->music_source,
                    data->music_available && data->music_source && data->music_source[0]
                        ? data->music_source : "--",
                    &monitor_misans_12);
    set_music_label(view->music_title, data->music_title, &monitor_misans_20);
    set_music_label(view->music_artist, data->music_artist, &monitor_misans_14);
    if(view->playback_available != data->music_available ||
       view->playback_playing != data->music_playing) {
        view->playback_available = data->music_available;
        view->playback_playing = data->music_playing;
        draw_playback(view, data->music_available, data->music_playing);
    }
    int percent = -1;
    if(data->music_available && data->music_position_seconds >= 0 && data->music_duration_seconds > 0) {
        int pos = data->music_position_seconds;
        if(pos > data->music_duration_seconds) pos = data->music_duration_seconds;
        percent = (int)((int64_t)pos * 100 / data->music_duration_seconds);
    }
    progress_apply(&view->music_bar, percent);
    char position[16], duration[16];
    format_time(position, sizeof(position), data->music_available ? data->music_position_seconds : -1);
    format_time(duration, sizeof(duration), data->music_available ? data->music_duration_seconds : -1);
    if(strcmp(lv_label_get_text(view->music_position), position))
        lv_label_set_text(view->music_position, position);
    if(strcmp(lv_label_get_text(view->music_duration), duration))
        lv_label_set_text(view->music_duration, duration);
}

void monitor_activity_apply(lv_obj_t *root, const monitor_activity_data_t *data)
{
    if(!root || !data) return;
    /* The page root's user_data belongs to monitor_ui state, so the activity
     * view lives on a private marker child instead. */
    lv_obj_t *holder = lv_obj_get_child_by_name(root, "activity_view");
    activity_view_t *view = holder ? lv_obj_get_user_data(holder) : NULL;
    if(!view) return;
    apply_github(root, view, data);
    usage_apply(view, 0, data->usage_5h_percent);
    usage_apply(view, 1, data->usage_week_percent);
    unsigned task = (unsigned)data->agent_state;
    if(task >= sizeof(TASK_STYLES) / sizeof(TASK_STYLES[0])) task = MONITOR_TASK_UNKNOWN;
    if(lv_image_get_src(view->task_icon) != TASK_STYLES[task].icon)
        lv_image_set_src(view->task_icon, TASK_STYLES[task].icon);
    if(strcmp(lv_label_get_text(view->task_name), TASK_STYLES[task].name)) {
        lv_label_set_text(view->task_name, TASK_STYLES[task].name);
        lv_obj_set_style_text_font(view->task_name,
            task == MONITOR_TASK_WAITING_APPROVAL ? &monitor_misans_12 : &monitor_misans_14, 0);
    }
    apply_music(view, data);
}

lv_obj_t *monitor_activity_create(lv_obj_t *parent, const monitor_activity_data_t *data,
                                  monitor_frame_variant_t variant)
{
    if(!parent || !data) return NULL;
    activity_view_t *view = lv_zalloc(sizeof(*view));
    if(!view) return NULL;
    memcpy(view->wall.levels, data->levels, sizeof(view->wall.levels));
    view->wall.inverted = variant == MONITOR_FRAME_INVERTED;
    view->github_available = data->github_available;
    view->github_start_year = data->github_start_year;
    view->github_start_month = data->github_start_month;
    view->github_start_day = data->github_start_day;
    view->playback_available = data->music_available;
    view->playback_playing = data->music_playing;
    bool inv = view->wall.inverted;
    lv_obj_t *root = box(parent, 0, 0, 400, 300, 0, false, false, inv);
    lv_obj_t *holder = box(root, 0, 0, 0, 0, 0, false, false, inv);
    lv_obj_set_name(holder, "activity_view");
    lv_obj_set_user_data(holder, view);
    lv_obj_add_flag(holder, LV_OBJ_FLAG_HIDDEN);
    lv_obj_add_event_cb(root, delete_state, LV_EVENT_DELETE, view);
    lv_obj_t *frame = box(root, 5, 5, 390, 290, 9, false, true, inv);
    lv_obj_set_style_border_width(frame, 2, 0);

    icon(root, &activity_github, 24, 10, inv);
    label(root, "Contributions", 53, 11, 200, &monitor_misans_20, inv);
    if(data->github_available) month_labels(root, data, inv);
    view->weekday[0] = label(root, "M", 11, 54, 10, &monitor_misans_8, inv);
    view->weekday[1] = label(root, "W", 11, 76, 10, &monitor_misans_8, inv);
    view->weekday[2] = label(root, "F", 11, 98, 10, &monitor_misans_8, inv);
    for(unsigned i = 0; i < 3; i++)
        if(!data->github_available) lv_obj_add_flag(view->weekday[i], LV_OBJ_FLAG_HIDDEN);
    view->wall_obj = lv_obj_create(root);
    lv_obj_remove_style_all(view->wall_obj);
    lv_obj_clear_flag(view->wall_obj, LV_OBJ_FLAG_SCROLLABLE);
    lv_obj_set_pos(view->wall_obj, 24, 54);
    lv_obj_set_size(view->wall_obj, 353, 78);
    lv_obj_add_event_cb(view->wall_obj, draw_wall, LV_EVENT_DRAW_MAIN, &view->wall);
    view->wall_unavailable = label(root, "DATA UNAVAILABLE", 30, 77, 352, &monitor_misans_14, inv);
    lv_obj_set_style_text_align(view->wall_unavailable, LV_TEXT_ALIGN_CENTER, 0);
    if(data->github_available) lv_obj_add_flag(view->wall_unavailable, LV_OBJ_FLAG_HIDDEN);
    else lv_obj_add_flag(view->wall_obj, LV_OBJ_FLAG_HIDDEN);
    box(root, 15, 142, 370, 1, 0, true, false, inv);

    lv_obj_t *agent = box(root, 15, 150, 180, 134, 8, false, true, inv);
    icon(agent, &activity_openai, 8, 3, inv);
    label(agent, "Codex", 37, 4, 130, &monitor_misans_20, inv);
    char value_text[12];
    usage_value_text(value_text, sizeof(value_text), data->usage_5h_percent);
    label(agent, "5h Left", 9, 31, 105, &monitor_misans_12, inv);
    view->usage_value[0] = label(agent, value_text, 115, 31, 54, &monitor_misans_12, inv);
    view->usage_bar[0] = progress_create(agent, 9, 46, 160, data->usage_5h_percent, inv);
    usage_value_text(value_text, sizeof(value_text), data->usage_week_percent);
    label(agent, "Week Left", 9, 58, 105, &monitor_misans_12, inv);
    view->usage_value[1] = label(agent, value_text, 115, 58, 54, &monitor_misans_12, inv);
    view->usage_bar[1] = progress_create(agent, 9, 73, 160, data->usage_week_percent, inv);
    lv_obj_set_style_text_align(view->usage_value[0], LV_TEXT_ALIGN_RIGHT, 0);
    lv_obj_set_style_text_align(view->usage_value[1], LV_TEXT_ALIGN_RIGHT, 0);
    box(agent, 1, 87, 178, 1, 0, true, false, inv);
    unsigned task = (unsigned)data->agent_state;
    if(task >= sizeof(TASK_STYLES) / sizeof(TASK_STYLES[0])) task = MONITOR_TASK_UNKNOWN;
    view->task_icon = icon(agent, TASK_STYLES[task].icon, 10, 99, inv);
    label(agent, "Current Task", 42, 92, 127, &monitor_misans_12, inv);
    const lv_font_t *task_font = task == MONITOR_TASK_WAITING_APPROVAL ? &monitor_misans_12 : &monitor_misans_14;
    view->task_name = label(agent, TASK_STYLES[task].name, 42, 109, 127, task_font, inv);

    lv_obj_t *music = box(root, 205, 150, 180, 134, 8, false, true, inv);
    label(music, LV_SYMBOL_AUDIO, 12, 7, 18, &lv_font_montserrat_14, inv);
    label(music, "Music", 37, 4, 68, &monitor_misans_20, inv);
    const char *source = data->music_available && data->music_source && data->music_source[0]
        ? data->music_source : "--";
    view->music_source = label(music, source, 105, 10, 63, music_font(source, &monitor_misans_12), inv);
    lv_obj_set_style_text_align(view->music_source, LV_TEXT_ALIGN_RIGHT, 0);
    /* Pixel-aligned symbols stay crisp after conversion to the 1-bit panel. */
    view->playback = box(music, 127, 35, 42, 42, 21, false, true, inv);
    lv_obj_set_style_border_width(view->playback, 2, 0);
    draw_playback(view, data->music_available, data->music_playing);
    view->music_title = label(music, data->music_title, 10, 31, 115,
                              music_font(data->music_title, &monitor_misans_20), inv);
    lv_obj_set_name(view->music_title, "music_title");
    view->music_artist = label(music, data->music_artist, 10, 58, 115,
                               music_font(data->music_artist, &monitor_misans_14), inv);
    int percent = -1;
    if(data->music_available && data->music_position_seconds >= 0 && data->music_duration_seconds > 0) {
        int pos = data->music_position_seconds;
        if(pos > data->music_duration_seconds) pos = data->music_duration_seconds;
        percent = (int)((int64_t)pos * 100 / data->music_duration_seconds);
    }
    view->music_bar = progress_create(music, 10, 99, 158, percent, inv);
    char position[16], duration[16];
    format_time(position, sizeof(position), data->music_available ? data->music_position_seconds : -1);
    format_time(duration, sizeof(duration), data->music_available ? data->music_duration_seconds : -1);
    view->music_position = label(music, position, 10, 109, 60, &monitor_misans_12, inv);
    view->music_duration = label(music, duration, 108, 109, 60, &monitor_misans_12, inv);
    lv_obj_set_style_text_align(view->music_duration, LV_TEXT_ALIGN_RIGHT, 0);
    return root;
}
