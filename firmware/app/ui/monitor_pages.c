#include "monitor_pages.h"
#include <string.h>

#ifdef ESP_PLATFORM
#include "esp_heap_caps.h"
static void *frame_alloc(size_t n) {
    void *p = heap_caps_malloc(n, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    return p ? p : heap_caps_malloc(n, MALLOC_CAP_8BIT);
}
#define frame_free heap_caps_free
#else
#define frame_alloc lv_malloc
#define frame_free lv_free
#endif

#define TRANSITION_MS 600U
#define PAGE_WIDTH MONITOR_DISPLAY_WIDTH
#define PAGE_HEIGHT MONITOR_DISPLAY_HEIGHT

struct monitor_pages {
    lv_obj_t *first, *second, *first_view, *second_view, *stripe;
    uint8_t *first_packed, *second_packed;
    bool active, second_visible, target_second, next, first_frame;
    int32_t position, previous_position;
};

/* LVGL computes easing; drawing is done in the panel's packed 1-bit layout. */
static void stripe_step(void *var, int32_t position)
{
    monitor_pages_t *p = var;
    if(position < 0) position = 0;
    if(position > PAGE_WIDTH) position = PAGE_WIDTH;
    p->position = position;
    int marker = position < 1 ? 1 : position > PAGE_WIDTH - 3 ? PAGE_WIDTH - 3 : position - 1;
    lv_obj_set_x(p->stripe, marker); /* Hidden; useful for simulator inspection. */
}

static void release_snapshots(monitor_pages_t *p)
{
    if(p->first_packed) { frame_free(p->first_packed); p->first_packed = NULL; }
    if(p->second_packed) { frame_free(p->second_packed); p->second_packed = NULL; }
}

static lv_draw_buf_t *snapshot(lv_obj_t *page)
{
    lv_draw_buf_t *buf = lv_snapshot_create_draw_buf(page, LV_COLOR_FORMAT_I1);
    if(!buf) return NULL;
    lv_draw_buf_set_palette(buf, 0, lv_color32_make(0, 0, 0, 255));
    lv_draw_buf_set_palette(buf, 1, lv_color32_make(255, 255, 255, 255));
    if(lv_snapshot_take_to_draw_buf(page, LV_COLOR_FORMAT_I1, buf) != LV_RESULT_OK) {
        lv_draw_buf_destroy(buf);
        return NULL;
    }
    return buf;
}

static bool pack_snapshot(lv_obj_t *page, uint8_t *packed)
{
    lv_draw_buf_t *buf = snapshot(page);
    if(!buf) return false;
    bool valid = buf->header.w == PAGE_WIDTH && buf->header.h == PAGE_HEIGHT;
    if(valid) monitor_pack_i1_region(buf->data + 8, buf->header.stride, PAGE_WIDTH, PAGE_HEIGHT,
                                     0, 0, PAGE_WIDTH - 1, PAGE_HEIGHT - 1, packed);
    lv_draw_buf_destroy(buf);
    return valid;
}

static bool prepare_snapshots(monitor_pages_t *p)
{
    p->first_packed = frame_alloc(MONITOR_FRAME_BYTES);
    p->second_packed = frame_alloc(MONITOR_FRAME_BYTES);
    if(!p->first_packed || !p->second_packed ||
       !pack_snapshot(p->first, p->first_packed) || !pack_snapshot(p->second, p->second_packed)) {
        release_snapshots(p);
        return false;
    }
    return true;
}

static void show_target(monitor_pages_t *p)
{
    lv_obj_t *view = p->target_second ? p->second_view : p->first_view;
    lv_obj_t *page = p->target_second ? p->second : p->first;
    lv_obj_t *old = p->target_second ? p->first_view : p->second_view;
    lv_obj_set_pos(view, 0, 0);
    lv_obj_set_size(view, PAGE_WIDTH, PAGE_HEIGHT);
    lv_obj_set_pos(page, 0, 0);
    release_snapshots(p);
    lv_obj_clear_flag(view, LV_OBJ_FLAG_HIDDEN);
    lv_obj_add_flag(old, LV_OBJ_FLAG_HIDDEN);
    p->second_visible = p->target_second;
}

static void transition_done(lv_anim_t *animation)
{
    monitor_pages_t *p = animation->var;
    /* The final packed frame must be composed before snapshots are freed. */
    p->active = false;
}

static void first_delete(lv_event_t *event)
{
    monitor_pages_t *p = lv_event_get_user_data(event);
    p->first = NULL;
    release_snapshots(p);
    lv_obj_t *second_view = p->second_view;
    p->second_view = NULL;
    p->second = NULL;
    if(second_view) lv_obj_delete(second_view);
    lv_obj_delete(p->stripe);
}

static void marker_delete(lv_event_t *event)
{
    monitor_pages_t *p = lv_event_get_user_data(event);
    lv_anim_delete(p, stripe_step);
    release_snapshots(p);
    if(p->first) lv_obj_remove_event_cb_with_user_data(p->first, first_delete, p);
    if(p->second_view) lv_obj_delete(p->second_view);
    lv_free(p);
}

static lv_obj_t *viewport(lv_obj_t *parent)
{
    lv_obj_t *view = lv_obj_create(parent);
    if(!view) return NULL;
    lv_obj_remove_style_all(view);
    lv_obj_clear_flag(view, LV_OBJ_FLAG_SCROLLABLE);
    lv_obj_set_pos(view, 0, 0);
    lv_obj_set_size(view, PAGE_WIDTH, PAGE_HEIGHT);
    return view;
}

monitor_pages_t *monitor_pages_create(lv_obj_t *first, monitor_frame_variant_t variant,
                                      const monitor_activity_data_t *activity)
{
    if(!first || !lv_obj_get_parent(first) || monitor_pages_from_first(first)) return NULL;
    monitor_pages_t *p = lv_zalloc(sizeof(*p));
    if(!p) return NULL;
    lv_obj_t *screen = lv_obj_get_parent(first);
    p->first = first;
    p->first_view = viewport(screen);
    p->second_view = viewport(screen);
    if(!p->first_view || !p->second_view) {
        if(p->first_view) lv_obj_delete(p->first_view);
        if(p->second_view) lv_obj_delete(p->second_view);
        lv_free(p); return NULL;
    }
    monitor_activity_data_t empty = {.usage_5h_percent = -1, .usage_week_percent = -1,
        .agent_state = MONITOR_TASK_UNKNOWN, .music_position_seconds = -1,
        .music_duration_seconds = -1};
    p->second = monitor_activity_create(p->second_view, activity ? activity : &empty, variant);
    if(!p->second) { lv_obj_delete(p->first_view); lv_obj_delete(p->second_view); lv_free(p); return NULL; }
    lv_obj_add_flag(p->second_view, LV_OBJ_FLAG_HIDDEN);
    p->stripe = lv_obj_create(screen);
    if(!p->stripe) { lv_obj_delete(p->first_view); lv_obj_delete(p->second_view); lv_free(p); return NULL; }
    lv_obj_remove_style_all(p->stripe);
    lv_obj_clear_flag(p->stripe, LV_OBJ_FLAG_SCROLLABLE);
    lv_obj_set_name(p->stripe, "page_transition_marker");
    lv_obj_set_user_data(p->stripe, p);
    lv_obj_set_size(p->stripe, 2, PAGE_HEIGHT - 10);
    lv_obj_set_y(p->stripe, 5);
    lv_obj_set_style_bg_opa(p->stripe, LV_OPA_COVER, 0);
    lv_obj_set_style_bg_color(p->stripe, lv_color_hex(variant == MONITOR_FRAME_INVERTED ? 0xffffff : 0x111111), 0);
    lv_obj_add_flag(p->stripe, LV_OBJ_FLAG_HIDDEN);
    lv_obj_set_parent(first, p->first_view);
    lv_obj_set_pos(first, 0, 0);
    lv_obj_add_event_cb(p->stripe, marker_delete, LV_EVENT_DELETE, p);
    lv_obj_add_event_cb(first, first_delete, LV_EVENT_DELETE, p);
    return p;
}

monitor_pages_t *monitor_pages_from_first(lv_obj_t *first)
{
    lv_obj_t *parent = first ? lv_obj_get_parent(first) : NULL;
    lv_obj_t *marker = parent ? lv_obj_get_child_by_name(parent, "page_transition_marker") : NULL;
    lv_obj_t *screen = parent ? lv_obj_get_parent(parent) : NULL;
    if(!marker && screen) marker = lv_obj_get_child_by_name(screen, "page_transition_marker");
    return marker ? lv_obj_get_user_data(marker) : NULL;
}

bool monitor_pages_switch(monitor_pages_t *p, bool next)
{
    if(!p || p->active) return false;
    if(!prepare_snapshots(p)) return false;
    p->next = next;
    p->target_second = !p->second_visible;
    p->active = true;
    p->first_frame = true;
    lv_obj_move_foreground(p->stripe);
    /* The hidden marker tracks the reveal edge without drawing a line. */
    stripe_step(p, next ? PAGE_WIDTH : 0);
    lv_anim_t animation;
    lv_anim_init(&animation);
    lv_anim_set_var(&animation, p);
    lv_anim_set_exec_cb(&animation, stripe_step);
    lv_anim_set_values(&animation, next ? PAGE_WIDTH : 0, next ? 0 : PAGE_WIDTH);
    lv_anim_set_duration(&animation, TRANSITION_MS);
    /* A flatter opening than LVGL's default ease-in-out, with a gentle stop. */
    lv_anim_set_path_cb(&animation, lv_anim_path_custom_bezier3);
    lv_anim_set_bezier3_param(&animation, LV_BEZIER_VAL_FLOAT(0.65), 0,
                              LV_BEZIER_VAL_FLOAT(0.35), LV_BEZIER_VAL_FLOAT(1));
    lv_anim_set_completed_cb(&animation, transition_done);
    lv_anim_t *started = lv_anim_start(&animation);
    if(!started) {
        show_target(p);
        lv_obj_add_flag(p->stripe, LV_OBJ_FLAG_HIDDEN);
        p->active = false;
        return true; /* The destination still needs one settled commit. */
    }
    lv_anim_pause(started); /* HPM preparation must not consume the transition. */
    return true;
}

void monitor_pages_resume(monitor_pages_t *p)
{
    if(!p || !p->active) return;
    lv_anim_t *animation = lv_anim_get(p, stripe_step);
    if(animation && lv_anim_is_paused(animation)) {
        /* Let LVGL account for elapsed wall time while still paused. */
        lv_anim_refr_now();
        lv_anim_resume(animation);
    }
}
int32_t monitor_pages_position(const monitor_pages_t *p) { return p ? p->position : -1; }

bool monitor_pages_compose(monitor_pages_t *p, uint8_t *packed, monitor_damage_t *dirty)
{
    if(!p || !packed || !dirty || !p->first_packed || !p->second_packed) return false;
    const uint8_t *target = p->target_second ? p->second_packed : p->first_packed;
    const uint8_t *source = p->target_second ? p->first_packed : p->second_packed;
    const uint8_t *left = p->next ? source : target;
    const uint8_t *right = p->next ? target : source;
    unsigned column = (unsigned)p->position / 2U;
    unsigned offset = column * (PAGE_HEIGHT / 4U);
    memcpy(packed, left, offset);
    memcpy(packed + offset, right + offset, MONITOR_FRAME_BYTES - offset);
    if((p->position & 1) && offset < MONITOR_FRAME_BYTES) {
        for(unsigned y = 0; y < PAGE_HEIGHT / 4U; ++y)
            packed[offset + y] = (uint8_t)((left[offset + y] & 0xaaU) | (right[offset + y] & 0x55U));
    }
    if(p->first_frame) {
        monitor_damage_mark(dirty, 0, 0, PAGE_WIDTH - 1, PAGE_HEIGHT - 1);
        p->first_frame = false;
    } else if(p->position != p->previous_position) {
        int lo = p->position < p->previous_position ? p->position : p->previous_position;
        int hi = p->position > p->previous_position ? p->position : p->previous_position;
        if(lo < PAGE_WIDTH) monitor_damage_mark(dirty, lo, 0, hi < PAGE_WIDTH ? hi : PAGE_WIDTH - 1, PAGE_HEIGHT - 1);
    }
    p->previous_position = p->position;
    if(!p->active) show_target(p);
    return true;
}

bool monitor_pages_active(const monitor_pages_t *p) { return p && p->active; }
bool monitor_pages_is_second(const monitor_pages_t *p) { return p && p->second_visible; }

void monitor_pages_settle(monitor_pages_t *p)
{
    if(!p || !p->active) return;
    lv_anim_delete(p, stripe_step);
    show_target(p);
    lv_obj_add_flag(p->stripe, LV_OBJ_FLAG_HIDDEN);
    p->active = false;
}
