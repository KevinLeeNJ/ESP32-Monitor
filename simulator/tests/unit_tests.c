#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "png_writer.h"
#include "monitor_render.h"
#include "monitor_ui.h"
#include "scene_registry.h"
#include "sha256.h"

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
    uint16_t *rgb = (uint16_t *)malloc(MONITOR_RGB565_BYTES);
    uint16_t *back = (uint16_t *)malloc(MONITOR_RGB565_BYTES);
    uint8_t *packed = (uint8_t *)malloc(MONITOR_FRAME_BYTES);
    CHECK(rgb && back && packed, "test allocations");
    if(!rgb || !back || !packed) { free(rgb); free(back); free(packed); return; }

    for(uint32_t y = 0; y < MONITOR_DISPLAY_HEIGHT; ++y) {
        for(uint32_t x = 0; x < MONITOR_DISPLAY_WIDTH; ++x) {
            rgb[y * MONITOR_DISPLAY_WIDTH + x] =
                ((x / 20U) + (y / 20U)) % 2U == 0 ? 0x0000U : 0xffffU;
        }
    }

    monitor_pack_rgb565(rgb, MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT,
                        MONITOR_FRAME_NORMAL, packed);
    monitor_unpack_to_rgb565(packed, MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT,
                             MONITOR_FRAME_NORMAL, back);
    CHECK(memcmp(rgb, back, MONITOR_RGB565_BYTES) == 0, "pack/unpack normal round trip");

    monitor_pack_rgb565(rgb, MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT,
                        MONITOR_FRAME_INVERTED, packed);
    monitor_unpack_to_rgb565(packed, MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT,
                             MONITOR_FRAME_INVERTED, back);
    CHECK(memcmp(rgb, back, MONITOR_RGB565_BYTES) == 0, "pack/unpack inverted round trip");

    free(rgb); free(back); free(packed);
}

static void test_pack_layout(void)
{
    uint16_t *rgb = (uint16_t *)malloc(MONITOR_RGB565_BYTES);
    uint8_t *packed = (uint8_t *)malloc(MONITOR_FRAME_BYTES);
    CHECK(rgb && packed, "layout allocations");
    if(!rgb || !packed) { free(rgb); free(packed); return; }

    for(uint32_t i = 0; i < MONITOR_DISPLAY_WIDTH * MONITOR_DISPLAY_HEIGHT; ++i) rgb[i] = 0xffffU;
    rgb[0] = 0x0000U;
    monitor_pack_rgb565(rgb, MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT,
                        MONITOR_FRAME_NORMAL, packed);
    /* 1 means white on the panel. With one black pixel, only the top-left
       corner bit (bit 1 of the final byte in column 0) must be clear. */
    CHECK(packed[(MONITOR_DISPLAY_HEIGHT / 4U) - 1U] == 0xfdU,
          "top-left black pixel clears the corresponding white bit");
    CHECK(packed[0] == 0xffU, "first packed byte remains white for a single top-left black pixel");

    free(rgb); free(packed);
}

static void test_render_determinism(void)
{
    monitor_renderer_t a, b;
    CHECK(monitor_renderer_init(&a, monitor_ui_create), "init renderer A");
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

int main(void)
{
    test_sha256_vectors();
    test_pack_round_trip();
    test_pack_layout();
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
