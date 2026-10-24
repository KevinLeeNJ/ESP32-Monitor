#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "png_writer.h"
#include "monitor_render.h"
#include "monitor_ui.h"
#include "scene_registry.h"
#include "sha256.h"
#include "cJSON.h"
#include "monitor_schedule.h"
#include "monitor_display_refresh.h"
#include "lvgl/src/draw/sw/blend/lv_draw_sw_blend_private.h"
#include "lvgl/src/draw/sw/blend/lv_draw_sw_blend_to_i1.h"

static int failures = 0;

#define CHECK(cond, msg) do { \
    if(!(cond)) { fprintf(stderr, "FAIL: %s\n", msg); failures++; } \
} while(0)

static void test_sha256_vectors(void)
{
    char digest[65];
    monitor_sha256((const uint8_t *)"abc", 3, digest);
    CHECK(strcmp(digest, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad") == 0,
          "SHA-256 abc vector");
    monitor_sha256((const uint8_t *)"", 0, digest);
    CHECK(strcmp(digest, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855") == 0,
          "SHA-256 empty vector");
}

static void test_pack_round_trip(void)
{
    uint8_t *i1 = malloc(MONITOR_FRAME_BYTES);
    uint8_t *packed = malloc(MONITOR_FRAME_BYTES);
    uint16_t *back = malloc(MONITOR_RGB565_BYTES);
    CHECK(i1 && packed && back, "I1 round-trip allocations");
    if(!i1 || !packed || !back) { free(i1); free(packed); free(back); return; }
    for(uint32_t i = 0; i < MONITOR_FRAME_BYTES; ++i) i1[i] = (uint8_t)(i * 37U + 13U);
    monitor_pack_i1_region(i1, MONITOR_DISPLAY_WIDTH / 8U,
                           MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT,
                           0, 0, MONITOR_DISPLAY_WIDTH - 1, MONITOR_DISPLAY_HEIGHT - 1, packed);
    monitor_unpack_to_rgb565(packed, MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT,
                             MONITOR_FRAME_NORMAL, back);
    for(uint32_t i = 0; i < MONITOR_DISPLAY_WIDTH * MONITOR_DISPLAY_HEIGHT; ++i) {
        CHECK(back[i] == ((i1[i / 8U] & (0x80U >> (i % 8U))) ? 0xffffU : 0),
              "I1 MSB-first pixels survive panel mapping");
    }
    free(i1); free(packed); free(back);
}

static void test_pack_layout(void)
{
    /* A padded, offset two-row flush must preserve every unrelated pixel. */
    uint8_t packed[MONITOR_FRAME_BYTES];
    uint16_t *back = malloc(MONITOR_RGB565_BYTES);
    CHECK(back != NULL, "I1 layout allocation");
    if(!back) return;
    memset(packed, 0xff, sizeof(packed));
    const uint8_t rows[] = {0x7f, 0xff, 0x00, 0x00, 0xff, 0xfe, 0x00, 0x00};
    monitor_pack_i1_region(rows, 4, MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT,
                           8, 39, 23, 40, packed);
    monitor_unpack_to_rgb565(packed, MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT,
                             MONITOR_FRAME_NORMAL, back);
    for(uint32_t y = 0; y < MONITOR_DISPLAY_HEIGHT; ++y) {
        for(uint32_t x = 0; x < MONITOR_DISPLAY_WIDTH; ++x) {
            bool black = (x == 8 && y == 39) || (x == 23 && y == 40);
            CHECK(back[y * MONITOR_DISPLAY_WIDTH + x] == (black ? 0 : 0xffffU),
                  "offset I1 region preserves neighbors and ignores padding");
        }
    }
    memset(packed, 0xff, sizeof(packed));
    const uint8_t black = 0;
    monitor_pack_i1_region(&black, 1, MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT,
                           0, 0, 0, 0, packed);
    CHECK(packed[MONITOR_DISPLAY_HEIGHT / 4U - 1U] == 0xfd,
          "top-left black pixel clears panel bit 1");
    CHECK(packed[0] == 0xff, "opposite corner remains white");
    free(back);
}

static void test_pack_regions(void)
{
    /* Compare byte-pair packing to the pixel layout contract over odd/even
     * origins, short tails, row padding and all four vertical bit positions. */
    uint8_t packed[MONITOR_FRAME_BYTES], expected[MONITOR_FRAME_BYTES];
    uint8_t pixels[32 * 9];
    for(unsigned i = 0; i < sizeof(pixels); ++i) pixels[i] = (uint8_t)(i * 37U + 13U);
    for(unsigned x1 = 0; x1 < 10; ++x1) {
        for(unsigned width = 1; width < 65; ++width) {
            for(unsigned y1 = 37; y1 < 41; ++y1) {
                memset(packed, 0xa5, sizeof(packed));
                memcpy(expected, packed, sizeof(expected));
                for(unsigned y = y1; y < y1 + 9; ++y) {
                    for(unsigned x = x1; x < x1 + width; ++x) {
                        unsigned inv_y = MONITOR_DISPLAY_HEIGHT - 1U - y;
                        unsigned index = (x / 2U) * (MONITOR_DISPLAY_HEIGHT / 4U) + inv_y / 4U;
                        uint8_t mask = (uint8_t)(1U << (7U - (inv_y % 4U) * 2U - x % 2U));
                        bool white = pixels[(y - y1) * 32U + (x - x1) / 8U] & (0x80U >> ((x - x1) % 8U));
                        if(white) expected[index] |= mask;
                        else expected[index] &= (uint8_t)~mask;
                    }
                }
                monitor_pack_i1_region(pixels, 32, MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT,
                                       x1, y1, x1 + width - 1, y1 + 8, packed);
                CHECK(!memcmp(packed, expected, sizeof(packed)), "I1 region matches pixel mapping and preserves neighbors");
            }
        }
    }
}

static void test_i1_coverage_blending(void)
{
    /* Exercise LVGL's actual dispatcher, including non-byte-aligned pixels,
       padded rows and both polarities. Fractional coverage must not turn every
       black edge pixel on or every white edge pixel off. */
    for(unsigned inverted = 0; inverted < 2; ++inverted) {
        for(unsigned opacity = 0; opacity < 256; ++opacity) {
            uint8_t pixels[4];
            memset(pixels, inverted ? 0 : 0xff, sizeof(pixels));
            const lv_opa_t mask[] = {64, 192, 0, 0, 127, 128, 0, 0};
            lv_draw_sw_blend_fill_dsc_t dsc = {0};
            dsc.dest_buf = pixels;
            dsc.dest_w = 2;
            dsc.dest_h = 2;
            dsc.dest_stride = 2;
            dsc.mask_buf = mask;
            dsc.mask_stride = 4;
            dsc.color = inverted ? lv_color_white() : lv_color_black();
            dsc.opa = opacity;
            dsc.relative_area.x1 = 3;
            lv_draw_sw_blend_color_to_i1(&dsc);
            for(unsigned y = 0; y < 2; ++y) {
                for(unsigned bit = 0; bit < 16; ++bit) {
                    bool ink = bit >= 3 && bit < 5 &&
                        (mask[y * 4 + bit - 3] * opacity / 255U) >= 128U;
                    bool white = inverted ? ink : !ink;
                    CHECK(((pixels[y * 2 + bit / 8] & (0x80U >> (bit % 8))) != 0) == white,
                          "I1 masked coverage uses midpoint and preserves neighboring bits");
                }
            }
            /* The opacity-only path has the same midpoint behavior. */
            dsc.mask_buf = NULL;
            memset(pixels, inverted ? 0 : 0xff, sizeof(pixels));
            lv_draw_sw_blend_color_to_i1(&dsc);
            bool white = inverted ? opacity >= 128 : opacity < 128;
            CHECK(((pixels[0] & 0x10U) != 0) == white,
                  "I1 opacity-only blend uses midpoint for both polarities");
        }
    }
}

static void test_render_determinism(void)
{
    monitor_renderer_t a, b;
    CHECK(monitor_renderer_init(&a, monitor_ui_create), "init renderer A");
    CHECK(lv_display_get_color_format(a.display) == LV_COLOR_FORMAT_I1, "renderer uses native I1");
    CHECK(monitor_renderer_render(&a), "render A");
    char da[65];
    monitor_sha256(monitor_renderer_packed(&a), MONITOR_FRAME_BYTES, da);
    monitor_renderer_deinit(&a);

    CHECK(monitor_renderer_init(&b, monitor_ui_create), "init renderer B");
    CHECK(monitor_renderer_render(&b), "render B");
    char db[65];
    monitor_sha256(monitor_renderer_packed(&b), MONITOR_FRAME_BYTES, db);
    CHECK(strcmp(da, db) == 0, "fresh simulator renders have identical packed digest");
    monitor_renderer_deinit(&b);
}

static lv_obj_t *create_future_style_scene(lv_obj_t *parent)
{
    lv_obj_t *root = lv_obj_create(parent);
    lv_obj_remove_style_all(root);
    lv_obj_set_size(root, MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT);
    lv_obj_set_style_bg_opa(root, LV_OPA_COVER, 0);
    lv_obj_set_style_bg_color(root, lv_color_hex(0xffffff), 0);

    lv_obj_t *panel = lv_obj_create(root);
    lv_obj_remove_style_all(panel);
    lv_obj_set_pos(panel, 20, 20);
    lv_obj_set_size(panel, 120, 80);
    lv_obj_set_style_bg_opa(panel, LV_OPA_COVER, 0);
    lv_obj_set_style_bg_color(panel, lv_color_hex(0x000000), 0);
    return root;
}

static void test_future_scene_factory(void)
{
    monitor_renderer_t renderer;
    CHECK(monitor_renderer_init(&renderer, create_future_style_scene),
          "init renderer with arbitrary scene factory");
    CHECK(monitor_renderer_render(&renderer), "render arbitrary scene factory");

    const uint8_t *packed = monitor_renderer_packed(&renderer);
    CHECK(packed != NULL, "arbitrary scene has packed frame");
    if(packed != NULL) {
        CHECK(packed[0] == 0xffU, "arbitrary scene preserves background pixels");
        const uint32_t black_pixel_byte = (20U / 2U) * (MONITOR_DISPLAY_HEIGHT / 4U) +
                                          ((MONITOR_DISPLAY_HEIGHT - 1U - 20U) / 4U);
        CHECK((packed[black_pixel_byte] & 0x80U) == 0,
              "arbitrary scene packs a new black widget");
    }
    monitor_renderer_deinit(&renderer);
}

static void test_i1_partial_refresh(void)
{
    monitor_renderer_t renderer;
    CHECK(monitor_renderer_init(&renderer, create_future_style_scene), "init partial I1 renderer");
    CHECK(monitor_renderer_render(&renderer), "initial I1 render");
    uint8_t before[MONITOR_FRAME_BYTES];
    memcpy(before, renderer.packed, sizeof(before));
    lv_obj_t *panel = lv_obj_get_child(renderer.root, 0);
    lv_obj_set_pos(panel, 23, 43);
    lv_obj_set_size(panel, 109, 73);
    CHECK(monitor_renderer_render(&renderer), "partial I1 update across byte and buffer boundaries");
    CHECK(memcmp(before, renderer.packed, sizeof(before)) != 0, "partial update changes frame");
    memcpy(before, renderer.packed, sizeof(before));
    monitor_renderer_invalidate_all(&renderer);
    CHECK(monitor_renderer_render(&renderer), "full I1 reference render");
    CHECK(memcmp(before, renderer.packed, sizeof(before)) == 0,
          "partial I1 flush matches full redraw and clears old widget pixels");
    lv_obj_set_style_bg_color(panel, lv_color_white(), 0);
    CHECK(monitor_renderer_render(&renderer), "erase I1 panel");
    for(uint32_t i = 0; i < MONITOR_FRAME_BYTES; ++i)
        CHECK(renderer.packed[i] == 0xff, "white update clears every previous black bit");
    monitor_renderer_deinit(&renderer);
}

static lv_obj_t *create_rounded_border_scene(lv_obj_t *parent)
{
    lv_obj_t *root = lv_obj_create(parent);
    lv_obj_remove_style_all(root);
    lv_obj_set_size(root, MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT);
    lv_obj_set_style_bg_opa(root, LV_OPA_COVER, 0);
    lv_obj_set_style_bg_color(root, lv_color_white(), 0);
    lv_obj_t *box = lv_obj_create(root);
    lv_obj_remove_style_all(box);
    lv_obj_set_pos(box, 19, 19);
    lv_obj_set_size(box, 81, 63);
    lv_obj_set_style_border_width(box, 2, 0);
    lv_obj_set_style_border_color(box, lv_color_black(), 0);
    lv_obj_set_style_radius(box, 9, 0);
    return root;
}

static void test_i1_rounded_border(void)
{
    monitor_renderer_t renderer;
    CHECK(monitor_renderer_init(&renderer, create_rounded_border_scene), "init rounded I1 scene");
    CHECK(monitor_renderer_render(&renderer), "render black rounded border");
    uint8_t normal[MONITOR_FRAME_BYTES];
    memcpy(normal, renderer.packed, sizeof(normal));
    lv_obj_set_style_bg_color(renderer.root, lv_color_black(), 0);
    lv_obj_set_style_border_color(lv_obj_get_child(renderer.root, 0), lv_color_white(), 0);
    CHECK(monitor_renderer_render(&renderer), "render white rounded border");
    for(uint32_t i = 0; i < MONITOR_FRAME_BYTES; ++i)
        CHECK((normal[i] ^ renderer.packed[i]) == 0xffU,
              "rounded corners and straight strokes have identical coverage in both polarities");
    monitor_renderer_deinit(&renderer);
}

static void test_weather_conditions(void)
{
    static const char *expected[] = {
        "晴", "少云", "多云", "阴天", "小雨", "中雨", "大雨", "雷阵雨",
        "小雪", "中雪", "大雪", "雨夹雪", "冰雹", "雾", "霾", "沙尘暴",
    };
    for(size_t i = 0; i < sizeof(expected) / sizeof(expected[0]); ++i) {
        const monitor_weather_condition_t condition = (monitor_weather_condition_t)i;
        CHECK(strcmp(monitor_weather_condition_name(condition), expected[i]) == 0,
              "weather condition has expected Chinese label");
        CHECK(monitor_weather_condition_icon(condition) != NULL,
              "weather condition has an icon");
    }
}

static void test_scene_registry(void)
{
    const simulator_scene_t *scene = simulator_scene_find("monitor");
    CHECK(scene != NULL, "monitor scene is registered");
    CHECK(scene != NULL && scene->create != NULL, "monitor scene has a factory");
    CHECK(simulator_scene_find("not-a-scene") == NULL, "unknown scene is rejected");
}

static void test_png_signature(void)
{
    const char *path = "unit-test-frame.png";
    uint16_t *rgb = (uint16_t *)calloc(MONITOR_DISPLAY_WIDTH * MONITOR_DISPLAY_HEIGHT, sizeof(uint16_t));
    CHECK(rgb != NULL, "png allocation");
    if(rgb) {
        CHECK(monitor_write_rgb565_png(path, rgb, MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT),
              "write PNG");
        FILE *f = fopen(path, "rb");
        uint8_t sig[8] = {0};
        CHECK(f && fread(sig, 1, 8, f) == 8, "read PNG signature");
        static const uint8_t expected[8] = {137,80,78,71,13,10,26,10};
        CHECK(memcmp(sig, expected, 8) == 0, "PNG signature");
        if(f) fclose(f);
        remove(path);
        free(rgb);
    }
}


static const char *SUMMARY =
    "{\"device_count\":1,\"devices\":[{\"name\":\"NAS\",\"uptime\":\"12d 4h\","
    "\"metric_count\":3,\"metrics\":[{\"name\":\"CPU\",\"value\":\"27%\",\"percent\":27},"
    "{\"name\":\"MEM\",\"value\":\"42%\",\"percent\":42},"
    "{\"name\":\"DISK\",\"value\":\"--\",\"percent\":null}]}]}";

static void test_devices_url(void)
{
    static const char *good[] = {"", "https://baseurl.com:8443/", "https://baseurl.com:443",
        "http://192.168.1.3:8080", "https://monitor.example.com", "http://[::1]:18080/", "https://example.com:65535"};
    static const char *bad[] = {"https://", "ftp://example.com", "https://u:p@example.com:8443",
        "https://example.com/path", "https://example.com?x=1", "https://example.com/#a",
        "https://example.com:0", "https://example.com:65536", "https://example.com:port",
        "https://example.com:", "https://example.com:443:8443", "https://example.com\\path", "https://bad host"};
    char url[256];
    for (size_t i = 0; i < sizeof(good) / sizeof(*good); ++i) {
        strcpy(url, good[i]);
        CHECK(monitor_devices_normalize_url(url), "valid base URL including custom ports");
    }
    strcpy(url, good[1]);
    monitor_devices_normalize_url(url);
    CHECK(strcmp(url, "https://baseurl.com:8443") == 0, "normalization preserves custom HTTPS port");
    for (size_t i = 0; i < sizeof(bad) / sizeof(*bad); ++i) {
        strcpy(url, bad[i]);
        CHECK(!monitor_devices_normalize_url(url), "reject unsafe or malformed base URL");
    }
    CHECK(monitor_devices_valid_token("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"), "accept Server read token");
    CHECK(!monitor_devices_valid_token("token\r\nHeader: value"), "reject header injection token");
    CHECK(!monitor_devices_valid_token(""), "do not allow anonymous Devices requests");
}

static void reject_summary(const char *body, size_t len)
{
    monitor_devices_snapshot_t before, after;
    memset(&before, 0x5a, sizeof(before));
    after = before;
    CHECK(!monitor_devices_parse(body, len, &after), "reject malformed summary");
    CHECK(memcmp(&after, &before, sizeof(after)) == 0, "invalid summary preserves previous snapshot atomically");
}
static void test_devices_parser(void)
{
    monitor_devices_snapshot_t snapshot = {0};
    CHECK(monitor_devices_parse(SUMMARY, strlen(SUMMARY), &snapshot), "parse real Server summary contract");
    CHECK(snapshot.count == 1 && snapshot.devices[0].metric_count == 3, "fixed device and metric counts");
    CHECK(!snapshot.devices[0].metrics[2].percent_valid, "null percent stays distinct from zero");
    CHECK(strcmp(snapshot.devices[0].uptime, "12d 4h") == 0, "copy uptime to owned storage");
    static const char *bad[] = {"{}", "[]", "{\"device_count\":0.5,\"devices\":[]}",
        "{\"device_count\":1,\"devices\":[]}", "{\"device_count\":5,\"devices\":[]}",
        "{\"device_count\":0,\"devices\":[]}junk", "{\"device_count\":0,\"device_count\":0,\"devices\":[]}"};
    for (size_t i = 0; i < sizeof(bad) / sizeof(*bad); ++i) reject_summary(bad[i], strlen(bad[i]));
    for (int variant = 0; variant < 9; ++variant) {
        cJSON *root = cJSON_Parse(SUMMARY);
        cJSON *d = cJSON_GetArrayItem(cJSON_GetObjectItem(root, "devices"), 0);
        cJSON *m = cJSON_GetArrayItem(cJSON_GetObjectItem(d, "metrics"), 0);
        switch (variant) {
            case 0: cJSON_ReplaceItemInObject(d, "name", cJSON_CreateString("123456789")); break;
            case 1: cJSON_ReplaceItemInObject(d, "uptime", cJSON_CreateString("123456789")); break;
            case 2: cJSON_ReplaceItemInObject(d, "metric_count", cJSON_CreateNumber(2)); break;
            case 3: cJSON_ReplaceItemInObject(m, "percent", cJSON_CreateNumber(101)); break;
            case 4: cJSON_ReplaceItemInObject(m, "percent", cJSON_CreateNumber(-1)); break;
            case 5: cJSON_ReplaceItemInObject(m, "percent", cJSON_CreateNumber(2.5)); break;
            case 6: cJSON_ReplaceItemInObject(m, "name", cJSON_CreateString("MEM")); break;
            case 7: cJSON_ReplaceItemInObject(m, "value", cJSON_CreateString("123456789")); break;
            case 8: cJSON_DeleteItemFromObject(m, "percent"); break;
        }
        char *body = cJSON_PrintUnformatted(root);
        reject_summary(body, strlen(body));
        cJSON_free(body);
        cJSON_Delete(root);
    }
    const char *nul = "{\"device_count\":0,\"devices\":[],\"extra\":\"\\u0000\"}";
    reject_summary(nul, strlen(nul));
    char large[4098];
    memset(large, ' ', sizeof(large)); large[4097] = 0;
    reject_summary(large, 4097);
    char deep[129];
    memset(deep, '[', 64); memset(deep + 64, ']', 64); deep[128] = 0;
    reject_summary(deep, 128);
    const char *empty = "{\"device_count\":0,\"devices\":[]}";
    CHECK(monitor_devices_parse(empty, strlen(empty), &snapshot) && !snapshot.count, "successful empty response removes all old devices");
}
static void test_devices_ui_replacement(void)
{
    monitor_renderer_t renderer;
    CHECK(monitor_renderer_init(&renderer, monitor_ui_create), "create Devices UI");
    lv_obj_t *root = monitor_renderer_root(&renderer);
    monitor_devices_snapshot_t snapshot = {0};
    monitor_devices_parse(SUMMARY, strlen(SUMMARY), &snapshot);
    monitor_ui_apply_devices_snapshot(root, &snapshot);
    CHECK(monitor_renderer_render(&renderer), "render live Devices snapshot");
    CHECK(lv_obj_get_child_by_name(root, "device_1") == NULL, "remove extra old device cards");
    lv_obj_t *card = lv_obj_get_child_by_name(root, "device_0");
    CHECK(card != NULL, "create only the new snapshot card");
    CHECK(lv_obj_get_y(lv_obj_get_child(card, 2)) == lv_obj_get_y(lv_obj_get_child(card, 3)), "metric label and value share the same horizontal row");
    strcpy(snapshot.devices[0].name, "PC");
    monitor_ui_apply_devices_snapshot(root, &snapshot);
    card = lv_obj_get_child_by_name(root, "device_0");
    CHECK(strcmp(lv_label_get_text(lv_obj_get_child(card, 0)), "PC") == 0, "same-count membership changes replace cards");
    snapshot.count = 0;
    monitor_ui_apply_devices_snapshot(root, &snapshot);
    CHECK(lv_obj_get_child_by_name(root, "device_0") == NULL, "empty online list removes final device card");
    CHECK(lv_obj_get_child_by_name(root, "empty_devices") != NULL, "empty online list displays message");
    monitor_renderer_deinit(&renderer);
}

static void test_aligned_refresh(void)
{
    const uint32_t all = MONITOR_SCHEDULE_DEVICES | MONITOR_SCHEDULE_SENSORS | MONITOR_SCHEDULE_WEATHER;
    CHECK(monitor_schedule_due(-1, 7) == all, "first valid time initializes every producer");
    CHECK(monitor_schedule_due(29, 30) == (MONITOR_SCHEDULE_DEVICES | MONITOR_SCHEDULE_SENSORS),
          "sensor sample runs at :30 ahead of the minute animation");
    CHECK(monitor_schedule_due(59, 60) == MONITOR_SCHEDULE_DEVICES,
          "Device API keeps its 30-second cadence at the minute boundary");
    CHECK(monitor_schedule_due(899, 900) == (MONITOR_SCHEDULE_DEVICES | MONITOR_SCHEDULE_WEATHER),
          "quarter-hour weather shares the unchanged Device API cadence");
    CHECK(monitor_schedule_due(86399, 86400) == (MONITOR_SCHEDULE_DEVICES | MONITOR_SCHEDULE_WEATHER),
          "midnight weather shares the unchanged Device API cadence");
    CHECK(monitor_schedule_due(900, 901) == 0, "extra wakeup in same slot does not refresh");
    CHECK(monitor_schedule_due(900, 899) == all, "backward time correction realigns every slot");
    CHECK(monitor_schedule_due(10, 1900) == all, "forward correction schedules one batch without catch-up");
    CHECK(monitor_schedule_delay_ms(29123456) == 877, "deadline keeps fractional seconds and rounds up");
    CHECK(monitor_schedule_delay_ms(29999999) == 1, "sub-millisecond boundary never spins");
    CHECK(monitor_schedule_delay_ms(30000000) == 30000, "exact boundary waits for the next slot");

    /* Simulate an hour starting off-boundary, with request completions and
     * unrelated wakeups at every second. Only :00/:30 may commit a normal batch. */
    unsigned devices = 0, sensors = 0, weather = 0, renders = 0;
    for (int64_t now = 8; now <= 3607; ++now) {
        uint32_t due = monitor_schedule_due(now - 1, now);
        devices += (due & MONITOR_SCHEDULE_DEVICES) != 0;
        sensors += (due & MONITOR_SCHEDULE_SENSORS) != 0;
        weather += (due & MONITOR_SCHEDULE_WEATHER) != 0;
        uint32_t reasons = MONITOR_REFRESH_SENSORS | MONITOR_REFRESH_WEATHER | MONITOR_REFRESH_DEVICES;
        if (due) reasons |= MONITOR_REFRESH_CLOCK;
        if (monitor_display_refresh_should_render(reasons, true)) {
            CHECK(now % 30 == 0, "asynchronous completions never cause off-boundary periodic draws");
            renders++;
        }
    }
    CHECK(devices == 120 && sensors == 60 && weather == 4 && renders == 120,
          "one hour keeps 120 API polls and aligns 60 pre-minute sensor samples with every other poll");
    CHECK(monitor_display_refresh_should_render(MONITOR_REFRESH_RECOVER, true),
          "startup, configuration and expiry bypass periodic batching");
    CHECK(monitor_display_refresh_should_render(MONITOR_REFRESH_WIFI, true), "Wi-Fi changes render promptly");
    CHECK(monitor_display_refresh_should_render(MONITOR_REFRESH_SENSORS, false),
          "fallback sampling is visible before time becomes valid");
}

int main(void)
{
    test_aligned_refresh();
    test_devices_url();
    test_devices_parser();
    test_devices_ui_replacement();
    test_sha256_vectors();
    test_pack_round_trip();
    test_pack_layout();
    test_pack_regions();
    test_i1_coverage_blending();
    test_i1_partial_refresh();
    test_i1_rounded_border();
    test_render_determinism();
    test_weather_conditions();
    test_future_scene_factory();
    test_scene_registry();
    test_png_signature();
    if(failures != 0) {
        fprintf(stderr, "%d unit test(s) failed\n", failures);
        return 1;
    }
    printf("all unit tests passed\n");
    return 0;
}
