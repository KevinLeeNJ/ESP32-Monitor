#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "monitor_render.h"
#include "monitor_ui.h"
#include "scene_registry.h"

static unsigned failures;
#define CHECK(condition, message) do { if (!(condition)) { \
    fprintf(stderr, "FAIL: %s (line %d)\n", message, __LINE__); ++failures; } } while (0)

static monitor_ui_snapshot_t snapshot(const char *time)
{
    return (monitor_ui_snapshot_t){.time_text = time, .wifi_state = MONITOR_WIFI_CONNECTED,
        .battery_percent = 78, .weather_condition = MONITOR_WEATHER_UNKNOWN,
        .temperature_tenths_c = 245, .humidity_percent = 58, .aqi = 42};
}

static void settle(monitor_renderer_t *renderer)
{
    unsigned frames = 0;
    while (monitor_ui_animation_active(renderer->root) && frames < 100) {
        uint32_t step = monitor_animation_frame_time(frames + 1, 1000) - monitor_animation_frame_time(frames, 1000);
        ++frames;
        CHECK(monitor_ui_advance(renderer->root, step), "active timeline advances");
        CHECK(monitor_renderer_render(renderer), "animation frame renders");
    }
    CHECK(!monitor_ui_animation_active(renderer->root), "finite timeline returns to idle");
}

static void check_local_frame(monitor_renderer_t *renderer, const uint16_t *before,
                               const lv_area_t *areas, unsigned count)
{
    uint16_t *after = malloc(MONITOR_RGB565_BYTES);
    monitor_unpack_to_rgb565(renderer->packed, 400, 300, MONITOR_FRAME_NORMAL, after);
    bool same_outside = true, changed_inside = false;
    for (int y = 0; y < 300; ++y) for (int x = 0; x < 400; ++x) {
        bool inside = false;
        for (unsigned i = 0; i < count; ++i)
            inside |= x >= areas[i].x1 && x <= areas[i].x2 && y >= areas[i].y1 && y <= areas[i].y2;
        if (before[y * 400 + x] == after[y * 400 + x]) continue;
        if (inside) changed_inside = true;
        else same_outside = false;
    }
    CHECK(same_outside, "all pixels outside animated viewports, including colon, stay fixed");
    CHECK(changed_inside, "animated viewports actually render a different frame");
    free(after);
}

static lv_obj_t *clock_factory(lv_obj_t *parent) { return monitor_ui_create(parent); }

static void test_clock(monitor_frame_variant_t variant)
{
    monitor_ui_set_clock_text("19:55", "MON 18 OCT");
    monitor_renderer_t renderer;
    CHECK(monitor_renderer_init(&renderer, clock_factory), "create rolling clock UI");
    lv_obj_t *root = renderer.root;
    monitor_ui_set_variant(root, variant);
    monitor_ui_set_animations_enabled(root, true);
    monitor_renderer_render(&renderer);
    lv_obj_t *clock = lv_obj_get_child_by_name(root, "time");
    lv_obj_t *slots[4];
    lv_area_t areas[4];
    for (unsigned i = 0; i < 4; ++i) {
        char name[24]; snprintf(name, sizeof(name), "clock_digit_%u", i);
        slots[i] = lv_obj_get_child_by_name(clock, name);
        CHECK(slots[i] != NULL, "clock has four independent viewports");
        lv_obj_get_coords(slots[i], &areas[i]);
    }
    uint16_t *before = malloc(MONITOR_RGB565_BYTES);
    monitor_unpack_to_rgb565(renderer.packed, 400, 300, MONITOR_FRAME_NORMAL, before);
    int left = areas[0].x2 + 1, right = areas[0].x1 - 1;
    uint16_t ink = variant == MONITOR_FRAME_NORMAL ? 0 : 0xffff;
    for (int y = areas[0].y1; y <= areas[0].y2; ++y)
        for (int x = areas[0].x1; x <= areas[0].x2; ++x) {
            if (before[y * 400 + x] != ink) continue;
            if (x < left) left = x;
            if (x > right) right = x;
        }
    CHECK(left <= right && abs((left - areas[0].x1) - (areas[0].x2 - right)) <= 1,
          "narrow digit 1 has equal visible margins in its roller");
    monitor_ui_snapshot_t ui = snapshot("19:56");
    monitor_ui_apply_snapshot(root, &ui);
    for (unsigned i = 0; i < 4; ++i)
        CHECK(lv_obj_has_flag(lv_obj_get_child(slots[i], 1), LV_OBJ_FLAG_HIDDEN) == (i != 3),
              "19:55 to 19:56 animates only the last digit");
    for (unsigned i = 0; i < (320U * MONITOR_UI_ANIMATION_FPS + 999U) / 1000U; ++i) {
        monitor_ui_advance(root, monitor_animation_frame_time(i + 1, 1000) -
                                 monitor_animation_frame_time(i, 1000));
        monitor_renderer_render(&renderer);
        check_local_frame(&renderer, before, &areas[3], 1);
        monitor_ui_apply_snapshot(root, &ui); /* Repeated data cannot restart the roll. */
    }
    CHECK(!monitor_ui_animation_active(root), "same time does not restart a completed roll");
    CHECK(!strcmp(lv_label_get_text(lv_obj_get_child(slots[3], 0)), "6"), "last digit lands on 6");
    ui.time_text = "19:59"; monitor_ui_apply_snapshot(root, &ui); settle(&renderer);
    monitor_unpack_to_rgb565(renderer.packed, 400, 300, MONITOR_FRAME_NORMAL, before);
    ui.time_text = "20:00"; monitor_ui_apply_snapshot(root, &ui);
    for (unsigned i = 0; i < 4; ++i)
        CHECK(!lv_obj_has_flag(lv_obj_get_child(slots[i], 1), LV_OBJ_FLAG_HIDDEN), "all four carry digits roll");
    monitor_ui_advance(root, 96); monitor_renderer_render(&renderer);
    for (unsigned i = 1; i < 4; ++i)
        CHECK(lv_obj_get_y(lv_obj_get_child(slots[i], 0)) == lv_obj_get_y(lv_obj_get_child(slots[0], 0)),
              "carry digits share the same animation phase");
    check_local_frame(&renderer, before, areas, 4);
    ui.time_text = "20:01"; monitor_ui_apply_snapshot(root, &ui); settle(&renderer);
    CHECK(!strcmp(lv_label_get_text(lv_obj_get_child(slots[3], 0)), "1"), "mid-roll correction lands on latest target");
    ui.time_text = "--:--"; monitor_ui_apply_snapshot(root, &ui);
    CHECK(!monitor_ui_animation_active(root), "invalid clock snaps without a roll");
    ui.time_text = "23:59"; monitor_ui_apply_snapshot(root, &ui);
    CHECK(monitor_ui_animation_active(root), "first valid clock rolls in from placeholder dashes");
    settle(&renderer);
    ui.time_text = "00:00"; monitor_ui_apply_snapshot(root, &ui); settle(&renderer);
    uint8_t frame[MONITOR_FRAME_BYTES]; memcpy(frame, renderer.packed, sizeof(frame));
    CHECK(!monitor_ui_advance(root, 60000), "idle UI requests no animation frame");
    monitor_renderer_render(&renderer);
    CHECK(!memcmp(frame, renderer.packed, sizeof(frame)), "idle clock has no blinking or moving content");
    free(before);
    monitor_renderer_deinit(&renderer);
    monitor_ui_set_clock_text("12:34", "MON 18 OCT");
}

#define TEST_DEVICE(n, c, m, d) {n, "12d 4h", 3, {{"CPU", #c "%", c, true}, {"MEM", #m "%", m, true}, {"DISK", #d "%", d, true}}}
static const monitor_device_t DEVICES[] = {
    TEST_DEVICE("NAS", 37, 62, 84), TEST_DEVICE("PI", 18, 47, 62),
    TEST_DEVICE("PC", 72, 81, 0), TEST_DEVICE("ROUTER", 29, 54, 100),
};
static lv_obj_t *devices_factory(lv_obj_t *parent)
{
    monitor_device_t initial[] = {DEVICES[0], DEVICES[2]};
    return monitor_ui_create_with_devices(parent, initial, 2);
}
static lv_obj_t *card(lv_obj_t *root, unsigned i)
{
    char name[24]; snprintf(name, sizeof(name), "device_%u", i);
    return lv_obj_get_child_by_name(root, name);
}
static void test_devices(void)
{
    monitor_renderer_t r;
    CHECK(monitor_renderer_init(&r, devices_factory), "create topology test UI");
    monitor_renderer_render(&r);
    monitor_ui_set_animations_enabled(r.root, true);
    lv_obj_t *nas = card(r.root, 0), *pc = card(r.root, 1);
    monitor_devices_snapshot_t next = {.count = 3, .status = MONITOR_DEVICES_READY};
    memcpy(next.devices, DEVICES, 3 * sizeof(DEVICES[0]));
    monitor_ui_apply_devices_snapshot(r.root, &next);
    CHECK(card(r.root, 0) == nas && card(r.root, 2) == pc, "online transition preserves survivor identity");
    lv_obj_t *pi = card(r.root, 1);
    CHECK(lv_obj_has_flag(pi, LV_OBJ_FLAG_HIDDEN), "new device stays hidden while survivors make room");
    monitor_ui_advance(r.root, 120); monitor_renderer_render(&r);
    CHECK(lv_obj_get_width(pc) < 183 && lv_obj_get_x(pc) > 202,
          "both survivors resize and move together");
    CHECK(lv_obj_get_width(nas) < 182 && lv_obj_get_width(nas) > 120, "survivor resizes gradually");
    CHECK(lv_obj_has_flag(pi, LV_OBJ_FLAG_HIDDEN), "new device has not popped before reflow finishes");
    monitor_ui_advance(r.root, 120); monitor_renderer_render(&r);
    CHECK(!lv_obj_has_flag(pi, LV_OBJ_FLAG_HIDDEN) && lv_obj_get_width(pi) == 2,
          "new device starts its pop after reflow");
    settle(&r);
    CHECK(lv_obj_get_x(pc) == 265 && lv_obj_get_width(nas) == 120, "three cards land at their final geometry");
    lv_obj_t *name = lv_obj_get_child(nas, 0);
    next.status = MONITOR_DEVICES_STALE;
    monitor_ui_apply_devices_snapshot(r.root, &next);
    CHECK(!monitor_ui_animation_active(r.root) && lv_obj_get_child(nas, 0) == name,
          "metadata refresh keeps existing card contents and does not animate");
    next.devices[1] = DEVICES[2]; next.count = 2;
    monitor_ui_apply_devices_snapshot(r.root, &next);
    monitor_ui_advance(r.root, 80); monitor_renderer_render(&r);
    CHECK(lv_obj_get_width(pi) < 120 && lv_obj_get_x(pc) == 265 && lv_obj_get_width(nas) == 120,
          "offline target shrinks before survivors fill the space");
    settle(&r);
    CHECK(card(r.root, 0) == nas && card(r.root, 1) == pc && !card(r.root, 2), "middle removal retains both survivors");
    next.devices[0] = DEVICES[2]; next.devices[1] = DEVICES[0];
    monitor_ui_apply_devices_snapshot(r.root, &next); settle(&r);
    CHECK(card(r.root, 0) == pc && card(r.root, 1) == nas, "server reorder moves existing objects");
    next.devices[0] = DEVICES[1];
    monitor_ui_apply_devices_snapshot(r.root, &next); settle(&r);
    CHECK(!strcmp(lv_label_get_text(lv_obj_get_child(card(r.root, 0), 0)), "PI"), "same-count replacement updates membership");
    next.count = 0; monitor_ui_apply_devices_snapshot(r.root, &next);
    next.count = 4; memcpy(next.devices, DEVICES, sizeof(DEVICES)); monitor_ui_apply_devices_snapshot(r.root, &next);
    next.count = 1; monitor_ui_apply_devices_snapshot(r.root, &next); settle(&r);
    CHECK(card(r.root, 0) && !card(r.root, 1), "burst updates coalesce to latest membership");
    next.count = 0; monitor_ui_apply_devices_snapshot(r.root, &next); settle(&r);
    CHECK(lv_obj_get_child_by_name(r.root, "empty_devices") && !card(r.root, 0), "last device shrinks before empty state");
    next.count = 2; next.devices[1] = next.devices[0];
    monitor_ui_apply_devices_snapshot(r.root, &next); settle(&r);
    CHECK(card(r.root, 0) != card(r.root, 1), "duplicate truncated names get distinct objects");
    next.count = 1; monitor_ui_apply_devices_snapshot(r.root, &next); settle(&r);
    CHECK(card(r.root, 0) && !card(r.root, 1), "duplicate name removal cannot double-delete");
    next.count = 4; memcpy(next.devices, DEVICES, sizeof(DEVICES)); monitor_ui_apply_devices_snapshot(r.root, &next);
    monitor_ui_set_animations_enabled(r.root, false); monitor_renderer_render(&r);
    CHECK(!monitor_ui_animation_active(r.root) && card(r.root, 3), "disabling motion settles latest data immediately");
    monitor_renderer_deinit(&r);
}


/* Exercise the requested 1,2,3 -> 1,4,2,3 sequence frame by frame. */
static void test_middle_insert_edges(monitor_frame_variant_t variant)
{
    monitor_renderer_t r;
    CHECK(monitor_renderer_init(&r, monitor_ui_create), "create edge constraint UI");
    monitor_devices_snapshot_t three = {.count = 3, .status = MONITOR_DEVICES_READY};
    memcpy(three.devices, DEVICES, 3 * sizeof(DEVICES[0]));
    monitor_ui_apply_devices_snapshot(r.root, &three);
    monitor_ui_set_variant(r.root, variant);
    monitor_renderer_render(&r);
    monitor_ui_set_animations_enabled(r.root, true);
    lv_obj_t *one = card(r.root, 0), *two = card(r.root, 1), *three_card = card(r.root, 2);
    monitor_devices_snapshot_t four = {.count = 4, .status = MONITOR_DEVICES_READY};
    four.devices[0] = DEVICES[0]; four.devices[1] = DEVICES[3];
    four.devices[2] = DEVICES[1]; four.devices[3] = DEVICES[2];
    monitor_ui_apply_devices_snapshot(r.root, &four);
    lv_obj_t *added = card(r.root, 1);
    int32_t previous_width = 120, previous_x = 140;
    for (unsigned t = 15; t <= 240; t += 15) {
        monitor_ui_advance(r.root, 15); monitor_renderer_render(&r);
        CHECK(lv_obj_get_x(one) == 15, "first card left edge remains anchored during insertion");
        CHECK(lv_obj_get_x(three_card) + lv_obj_get_width(three_card) == 385,
              "last card right edge remains anchored during insertion");
        CHECK(lv_obj_get_x(three_card) - lv_obj_get_x(two) - lv_obj_get_width(two) == 5,
              "adjacent survivors preserve their gap at every insertion frame");
        CHECK(lv_obj_get_width(two) <= previous_width && lv_obj_get_x(two) >= previous_x,
              "middle survivor moves right while shrinking");
        CHECK(lv_obj_has_flag(added, LV_OBJ_FLAG_HIDDEN) == (t < 240),
              "new card starts growing only after all survivors settle");
        previous_width = lv_obj_get_width(two); previous_x = lv_obj_get_x(two);
    }
    previous_width = 2;
    for (unsigned t = 15; t <= 240; t += 15) {
        monitor_ui_advance(r.root, 15); monitor_renderer_render(&r);
        CHECK(lv_obj_get_width(added) >= previous_width && lv_obj_get_width(added) <= 88,
              "new card grows monotonically within its reserved slot");
        CHECK(lv_obj_get_x(one) == 15 && lv_obj_get_width(one) == 88 && lv_obj_get_x(two) == 201,
              "survivors stay still throughout the new card reveal");
        previous_width = lv_obj_get_width(added);
    }
    CHECK(!monitor_ui_animation_active(r.root), "insertion completes in two shared phases");
    monitor_ui_apply_devices_snapshot(r.root, &three);
    for (unsigned t = 10; t <= 160; t += 10) {
        monitor_ui_advance(r.root, 10); monitor_renderer_render(&r);
        CHECK(lv_obj_get_width(one) == 88 && lv_obj_get_x(two) == 201,
              "survivors wait while the removed card collapses");
    }
    for (unsigned t = 15; t <= 240; t += 15) {
        monitor_ui_advance(r.root, 15); monitor_renderer_render(&r);
        CHECK(lv_obj_get_x(one) == 15 &&
              lv_obj_get_x(three_card) + lv_obj_get_width(three_card) == 385,
              "outer survivor edges remain anchored during deletion");
        CHECK(lv_obj_get_x(three_card) - lv_obj_get_x(two) - lv_obj_get_width(two) == 5,
              "adjacent survivors preserve their gap at every deletion frame");
    }
    CHECK(!monitor_ui_animation_active(r.root) && lv_obj_get_width(one) == 120 &&
          card(r.root, 1) == two && card(r.root, 2) == three_card,
          "deletion restores the original layout and card identities");
    monitor_renderer_deinit(&r);
}

static void test_serial_card_removal(void)
{
    monitor_ui_set_clock_text("12:34", "FRI 23 OCT");
    monitor_renderer_t r;
    CHECK(monitor_renderer_init(&r, monitor_ui_create), "create sequential removal UI");
    monitor_ui_set_animations_enabled(r.root, true);
    monitor_renderer_render(&r);
    lv_obj_t *nas = card(r.root, 0), *pi = card(r.root, 1);
    lv_obj_t *pc = card(r.root, 2), *router = card(r.root, 3);
    monitor_ui_snapshot_t ui = snapshot("12:35");
    monitor_ui_apply_snapshot(r.root, &ui);
    monitor_devices_snapshot_t next = {.count=1, .status=MONITOR_DEVICES_READY};
    next.devices[0] = DEVICES[0];
    monitor_ui_apply_devices_snapshot(r.root, &next);
    monitor_ui_advance(r.root, 160); monitor_renderer_render(&r);
    CHECK(lv_obj_get_width(nas)==88 && lv_obj_get_width(pi)==88 &&
          lv_obj_get_width(pc)==88 && lv_obj_get_width(router)==91,
          "card topology waits while clock and information roll");
    monitor_ui_advance(r.root, 160); monitor_renderer_render(&r);
    CHECK(lv_obj_get_width(pi)==88 && lv_obj_get_width(pc)==88,
          "finishing values starts removal without advancing its fresh timeline");
    monitor_ui_advance(r.root, 80); monitor_renderer_render(&r);
    CHECK(lv_obj_get_width(pi)<88 && lv_obj_get_width(pc)==88 &&
          lv_obj_get_width(router)==91 && lv_obj_get_width(nas)==88,
          "only the first departing card collapses");
    monitor_ui_advance(r.root, 80); monitor_renderer_render(&r);
    CHECK(lv_obj_get_width(pi)==2 && lv_obj_get_width(pc)==88,
          "the next removal starts only after the first is collapsed");
    monitor_ui_advance(r.root, 80); monitor_renderer_render(&r);
    CHECK(lv_obj_get_width(pc)<88 && lv_obj_get_width(router)==91 && lv_obj_get_width(nas)==88,
          "second removal leaves last departing card and survivor still");
    monitor_ui_advance(r.root, 240); monitor_renderer_render(&r);
    CHECK(card(r.root,0)==nas && !card(r.root,1) && lv_obj_get_width(nas)==88,
          "survivor reflow begins after every departing card is removed");
    monitor_ui_advance(r.root, 120); monitor_renderer_render(&r);
    CHECK(lv_obj_get_width(nas)>88 && lv_obj_get_width(nas)<370,
          "survivor uses the existing eased reflow after removal");
    settle(&r);
    CHECK(lv_obj_get_width(nas)==370 && lv_obj_get_x(nas)==15,
          "sequential transition settles at the complete final layout");
    monitor_renderer_deinit(&r);
    monitor_ui_set_clock_text("12:34", "MON 18 OCT");
}

static void test_info_rolls(monitor_frame_variant_t variant)
{
    monitor_ui_set_clock_text("19:55", "MON 18 OCT");
    monitor_renderer_t r;
    CHECK(monitor_renderer_init(&r, monitor_ui_create), "create static monitor without startup animation");
    CHECK(!monitor_ui_animation_active(r.root), "initial cards appear in place without a startup timeline");
    monitor_ui_set_variant(r.root, variant);
    monitor_ui_set_animations_enabled(r.root, true);
    CHECK(!monitor_ui_animation_active(r.root), "enabling animation does not animate initial content");
    monitor_renderer_render(&r);
    lv_obj_t *clock = lv_obj_get_child_by_name(r.root, "time");
    lv_obj_t *digit = lv_obj_get_child_by_name(clock, "clock_digit_3");
    const char *names[] = {"weather_card", "temperature_card", "humidity_card", "aqi_card"};
    lv_obj_t *clips[4], *incoming[4];
    lv_area_t areas[6];
    lv_obj_get_coords(digit, &areas[0]);
    for (unsigned i = 0; i < 4; ++i) {
        lv_obj_t *card = lv_obj_get_child_by_name(r.root, names[i]);
        clips[i] = lv_obj_get_child_by_name(card, "value_viewport");
        incoming[i] = lv_obj_get_child(clips[i], 1);
        lv_obj_get_coords(clips[i], &areas[1 + i]);
        if (!i) lv_obj_get_coords(lv_obj_get_child(card, 0), &areas[5]);
    }
    uint16_t *before = malloc(MONITOR_RGB565_BYTES);
    monitor_unpack_to_rgb565(r.packed, 400, 300, MONITOR_FRAME_NORMAL, before);
    monitor_ui_snapshot_t ui = snapshot("19:56");
    ui.weather_condition = MONITOR_WEATHER_LIGHT_RAIN;
    ui.temperature_tenths_c = 256; ui.humidity_percent = 63; ui.aqi = 57;
    monitor_ui_apply_snapshot(r.root, &ui);
    for (unsigned frame = 0; frame < 10; ++frame) {
        monitor_ui_advance(r.root, 32); monitor_renderer_render(&r);
        check_local_frame(&r, before, areas, 6);
        for (unsigned i = 1; i < 4; ++i)
            CHECK(lv_obj_get_style_translate_y(incoming[0], 0) == lv_obj_get_style_translate_y(incoming[i], 0),
                  "weather, temperature, humidity and AQI share the same rolling phase");
        CHECK(monitor_ui_animation_active(r.root) == (frame < 9), "clock and information finish together at 320 ms");
        monitor_ui_apply_snapshot(r.root, &ui);
    }
    const char *expected[] = {"小雨", "25.6°C", "63%", "57"};
    for (unsigned i = 0; i < 4; ++i) {
        CHECK(!strcmp(lv_label_get_text(incoming[i]), expected[i]), "information roll lands on the new value");
        CHECK(!lv_obj_has_flag(incoming[i], LV_OBJ_FLAG_HIDDEN), "new information remains visible after settling");
    }
    CHECK(!monitor_ui_animation_active(r.root), "unchanged data does not restart information animations");
    ui.aqi = 58; monitor_ui_apply_snapshot(r.root, &ui);
    monitor_ui_advance(r.root, 96);
    ui.aqi = 61; ui.temperature_tenths_c = -5; monitor_ui_apply_snapshot(r.root, &ui);
    settle(&r);
    ui.weather_condition = MONITOR_WEATHER_CLEAR; monitor_ui_apply_snapshot(r.root, &ui); settle(&r);
    ui.weather_condition = MONITOR_WEATHER_UNKNOWN; ui.aqi = -1;
    monitor_ui_apply_snapshot(r.root, &ui);
    monitor_ui_set_animations_enabled(r.root, false); monitor_renderer_render(&r);
    CHECK(!monitor_ui_animation_active(r.root), "disabling animations settles corrections and unavailable values");
    lv_obj_t *weather = lv_obj_get_child_by_name(r.root, "weather_card");
    lv_obj_t *icon_clip = lv_obj_get_child(weather, 0);
    CHECK(lv_obj_has_flag(lv_obj_get_child(icon_clip, 0), LV_OBJ_FLAG_HIDDEN) &&
          lv_obj_has_flag(lv_obj_get_child(icon_clip, 1), LV_OBJ_FLAG_HIDDEN),
          "unavailable weather hides both icon buffers");
    uint8_t final[MONITOR_FRAME_BYTES]; memcpy(final, r.packed, sizeof(final));
    CHECK(!monitor_ui_advance(r.root, 60000), "settled information requests no idle frame");
    monitor_renderer_render(&r);
    CHECK(!memcmp(final, r.packed, sizeof(final)), "settled information stays static");
    monitor_ui_set_animations_enabled(r.root, true);
    ui.aqi = 62; monitor_ui_apply_snapshot(r.root, &ui); monitor_ui_advance(r.root, 64);
    CHECK(monitor_renderer_rebuild(&r, monitor_ui_create), "delete and rebuild safely during an information roll");
    CHECK(!monitor_ui_animation_active(r.root), "rebuilt UI has no old animation state");
    free(before);
    monitor_renderer_deinit(&r);
    monitor_ui_set_clock_text("12:34", "MON 18 OCT");
}

static lv_obj_t *waiting_factory(lv_obj_t *parent)
{ return monitor_ui_create_waiting(parent, MONITOR_FRAME_NORMAL); }

static void test_first_data(monitor_frame_variant_t variant, unsigned device_count)
{
    monitor_renderer_t r;
    CHECK(monitor_renderer_init(&r, waiting_factory), "create live boot placeholder UI");
    monitor_ui_set_variant(r.root, variant);
    monitor_renderer_render(&r);
    CHECK(!monitor_ui_animation_active(r.root) && !card(r.root, 0), "waiting screen is static with no invented Devices");
    lv_obj_t *clock = lv_obj_get_child_by_name(r.root, "time");
    for (unsigned i = 0; i < 4; ++i) {
        char name[24]; snprintf(name, sizeof(name), "clock_digit_%u", i);
        lv_obj_t *slot = lv_obj_get_child_by_name(clock, name);
        CHECK(!lv_obj_has_flag(slot, LV_OBJ_FLAG_HIDDEN) &&
              !strcmp(lv_label_get_text(lv_obj_get_child(slot, 0)), "-"), "all four clock rollers initially show dashes");
    }
    const char *names[] = {"weather_card", "temperature_card", "humidity_card", "aqi_card"};
    lv_obj_t *clips[4];
    for (unsigned i = 0; i < 4; ++i) {
        clips[i] = lv_obj_get_child_by_name(lv_obj_get_child_by_name(r.root, names[i]), "value_viewport");
        CHECK(!strcmp(lv_label_get_text(lv_obj_get_child(clips[i], 0)), "--"), "all information fields start with placeholders");
    }
    uint8_t initial[MONITOR_FRAME_BYTES]; memcpy(initial, r.packed, sizeof(initial));
    CHECK(!monitor_ui_advance(r.root, 60000), "waiting for data does not create periodic animation frames");
    monitor_renderer_render(&r);
    CHECK(!memcmp(initial, r.packed, sizeof(initial)), "waiting placeholders remain stable");
    monitor_ui_snapshot_t ui = snapshot("19:56");
    ui.temperature_tenths_c = MONITOR_TEMPERATURE_UNAVAILABLE; ui.humidity_percent = -1; ui.aqi = -1;
    monitor_ui_apply_snapshot(r.root, &ui);
    CHECK(monitor_ui_animation_active(r.root), "first valid time begins a roll");
    monitor_ui_advance(r.root, 160); monitor_renderer_render(&r);
    for (unsigned i = 0; i < 4; ++i) {
        char name[24]; snprintf(name, sizeof(name), "clock_digit_%u", i);
        lv_obj_t *slot = lv_obj_get_child_by_name(clock, name);
        CHECK(lv_obj_get_y(lv_obj_get_child(slot, 0)) < 0 &&
              !lv_obj_has_flag(lv_obj_get_child(slot, 1), LV_OBJ_FLAG_HIDDEN), "placeholder clock digits roll into the first time");
        CHECK(!strcmp(lv_label_get_text(lv_obj_get_child(clips[i], 0)), "--"), "late or failed information stays unavailable");
    }
    settle(&r);
    ui.temperature_tenths_c = 256; ui.humidity_percent = 63; ui.aqi = 57;
    ui.weather_condition = MONITOR_WEATHER_LIGHT_RAIN;
    monitor_ui_apply_snapshot(r.root, &ui);
    for (unsigned i = 0; i < 4; ++i)
        CHECK(!lv_obj_has_flag(lv_obj_get_child(clips[i], 1), LV_OBJ_FLAG_HIDDEN), "first information values roll in from dashes");
    settle(&r);
    monitor_devices_snapshot_t devices = {.count = device_count, .status = MONITOR_DEVICES_READY};
    memcpy(devices.devices, DEVICES, sizeof(DEVICES));
    monitor_ui_apply_devices_snapshot(r.root, &devices);
    for (unsigned stage = 0; stage < device_count; ++stage) {
        monitor_renderer_render(&r);
        CHECK(card(r.root, device_count) == NULL, "first snapshot never creates extra device cards");
        for (unsigned i = 0; i < device_count; ++i)
            CHECK(lv_obj_has_flag(card(r.root, i), LV_OBJ_FLAG_HIDDEN) == (i > stage),
                  "initial Devices reveal exactly one additional card per pop");
        CHECK(monitor_ui_animation_active(r.root), "current card has a finite pop timeline");
        monitor_ui_advance(r.root, 240);
    }
    CHECK(!monitor_ui_animation_active(r.root), "first snapshot settles after exactly one pop per device");
    monitor_renderer_render(&r);
    const int32_t widths[] = {0, 370, 182, 120, 88};
    for (unsigned i = 0; i < device_count; ++i) {
        CHECK(lv_obj_get_width(card(r.root, i)) ==
              (i + 1 == device_count ? 385 - lv_obj_get_x(card(r.root, i)) : widths[device_count]), "settled card width matches the number of Devices");
        CHECK(lv_obj_get_x(card(r.root, i)) == 15 + i * (widths[device_count] + 5) &&
              lv_obj_get_y(card(r.root, i)) == 112 && lv_obj_get_height(card(r.root, i)) == 180,
              "settled device cards have correct positions and do not overlap");
    }
    uint8_t final[MONITOR_FRAME_BYTES]; memcpy(final, r.packed, sizeof(final));
    monitor_ui_apply_snapshot(r.root, &ui); monitor_ui_apply_devices_snapshot(r.root, &devices);
    CHECK(!monitor_ui_animation_active(r.root), "repeated first data cannot replay the reveal");
    monitor_renderer_render(&r);
    CHECK(!memcmp(final, r.packed, sizeof(final)), "repeated data retains the settled frame");
    monitor_renderer_deinit(&r);

    CHECK(monitor_renderer_init(&r, waiting_factory), "create static reference for first batch");
    monitor_ui_set_variant(r.root, variant);
    monitor_ui_set_animations_enabled(r.root, false);
    monitor_ui_apply_snapshot(r.root, &ui);
    monitor_ui_apply_devices_snapshot(r.root, &devices);
    monitor_renderer_render(&r);
    CHECK(!memcmp(final, r.packed, sizeof(final)), "animated first batch matches the static reference pixel for pixel");
    monitor_renderer_deinit(&r);

    CHECK(monitor_renderer_init(&r, waiting_factory), "create fresh screen for interrupted first batch");
    monitor_ui_apply_devices_snapshot(r.root, &devices);
    monitor_ui_advance(r.root, 100);
    devices.count = 1; monitor_ui_apply_devices_snapshot(r.root, &devices);
    settle(&r);
    CHECK(card(r.root, 0) && !card(r.root, 1), "new snapshot during first reveal eventually displays latest membership");
    monitor_renderer_deinit(&r);
}

static lv_obj_t *configuring_factory(lv_obj_t *parent)
{
    return simulator_scene_find("configuring")->create(parent, MONITOR_FRAME_NORMAL);
}

static void test_configuring_to_offline(void)
{
    monitor_renderer_t r;
    const simulator_scene_t *scene = simulator_scene_find("configuring");
    CHECK(scene && monitor_renderer_init(&r, configuring_factory), "create configuring scene");
    monitor_ui_snapshot_t ui = snapshot("19:56");
    ui.date_text = "SAT 24 OCT";
    ui.wifi_state = MONITOR_WIFI_PROVISIONING;
    ui.aqi = -1;
    monitor_ui_apply_snapshot(r.root, &ui);
    CHECK(monitor_ui_animation_active(r.root), "local data first appears while configuring");
    settle(&r);
    lv_obj_t *date = lv_obj_get_child_by_name(r.root, "clock_date");
    CHECK(date && !strcmp(lv_label_get_text(date), "Configuring"), "configuration status overrides date");
    lv_obj_t *temp = lv_obj_get_child_by_name(r.root, "temperature_card");
    lv_obj_t *clip = lv_obj_get_child_by_name(temp, "value_viewport");
    lv_obj_t *visible = lv_obj_get_child(clip, lv_obj_has_flag(lv_obj_get_child(clip, 0), LV_OBJ_FLAG_HIDDEN) ? 1 : 0);
    CHECK(!strcmp(lv_label_get_text(visible), "24.5°C"), "configuring displays local temperature");
    ui.wifi_state = MONITOR_WIFI_OFFLINE;
    monitor_ui_apply_snapshot(r.root, &ui);
    CHECK(!strcmp(lv_label_get_text(date), "Offline"), "offline status overrides even valid RTC date");
    CHECK(!monitor_ui_animation_active(r.root), "configuration exit does not replay local data animation");
    CHECK(!lv_obj_has_flag(visible, LV_OBJ_FLAG_HIDDEN) &&
          !strcmp(lv_label_get_text(visible), "24.5°C"), "configuration exit preserves the visible local value");
    CHECK(!monitor_ui_advance(r.root, 1000), "configuration exit has no deferred animation");
    monitor_ui_apply_snapshot(r.root, &ui);
    CHECK(!monitor_ui_animation_active(r.root), "repeated offline snapshots stay settled");
    ui.temperature_tenths_c = 251;
    monitor_ui_apply_snapshot(r.root, &ui);
    CHECK(monitor_ui_animation_active(r.root), "fresh sensor readings still animate normally offline");
    settle(&r);
    ui.wifi_state = MONITOR_WIFI_CONNECTED;
    monitor_ui_apply_snapshot(r.root, &ui);
    CHECK(!strcmp(lv_label_get_text(date), "SAT 24 OCT"), "connection restores date");
    monitor_renderer_deinit(&r);
}

int main(void)
{
    test_clock(MONITOR_FRAME_NORMAL);
    test_clock(MONITOR_FRAME_INVERTED);
    test_devices();
    test_middle_insert_edges(MONITOR_FRAME_NORMAL);
    test_middle_insert_edges(MONITOR_FRAME_INVERTED);
    test_configuring_to_offline();
    test_serial_card_removal();
    for (unsigned count = 1; count <= 4; ++count) {
        unsigned before = failures;
        test_first_data(MONITOR_FRAME_NORMAL, count);
        test_first_data(MONITOR_FRAME_INVERTED, count);
        printf("first snapshot %u device(s), normal + inverted: %s\n", count,
               failures == before ? "PASS" : "FAIL");
    }
    test_info_rolls(MONITOR_FRAME_NORMAL);
    test_info_rolls(MONITOR_FRAME_INVERTED);
    if (failures) { fprintf(stderr, "%u animation checks failed\n", failures); return 1; }
    puts("all animation checks passed");
    return 0;
}
