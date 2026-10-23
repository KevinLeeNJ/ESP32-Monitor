#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>

#include "monitor_render.h"
#include "png_writer.h"
#include "scene_registry.h"
#include "sha256.h"

#define SIMULATOR_PATH_MAX 2048
#define SCENE_NAME_MAX 128

static const simulator_scene_t *selected_scene;
static monitor_frame_variant_t selected_variant;

static lv_obj_t *create_selected_scene(lv_obj_t *parent)
{
    return selected_scene->create(parent, selected_variant);
}

static int ensure_directory(const char *path)
{
    if(path == NULL || path[0] == '\0') {
        fprintf(stderr, "output directory must not be empty\n");
        return -1;
    }

    char current[SIMULATOR_PATH_MAX];
    const size_t length = strlen(path);
    if(length >= sizeof(current)) {
        fprintf(stderr, "output path is too long: %s\n", path);
        return -1;
    }
    memcpy(current, path, length + 1U);

    size_t end = length;
    while(end > 1U && current[end - 1U] == '/') current[--end] = '\0';

    for(char *p = current + 1; *p != '\0'; ++p) {
        if(*p != '/') continue;
        *p = '\0';
        if(mkdir(current, 0755) != 0 && errno != EEXIST) {
            fprintf(stderr, "mkdir %s: %s\n", current, strerror(errno));
            return -1;
        }
        *p = '/';
    }

    if(mkdir(current, 0755) != 0 && errno != EEXIST) {
        fprintf(stderr, "mkdir %s: %s\n", current, strerror(errno));
        return -1;
    }
    return 0;
}

static int artifact_path(char *path, size_t capacity, const char *out_dir,
                         const char *stem, const char *suffix)
{
    const int written = snprintf(path, capacity, "%s/%s.%s", out_dir, stem, suffix);
    if(written < 0 || (size_t)written >= capacity) {
        fprintf(stderr, "artifact path is too long\n");
        return -1;
    }
    return 0;
}

static int write_bytes(const char *path, const void *data, size_t len)
{
    FILE *f = fopen(path, "wb");
    if(f == NULL) {
        fprintf(stderr, "open %s: %s\n", path, strerror(errno));
        return -1;
    }
    const size_t written = fwrite(data, 1, len, f);
    const int close_result = fclose(f);
    if(written != len || close_result != 0) {
        fprintf(stderr, "write %s failed\n", path);
        return -1;
    }
    return 0;
}

static int write_text(const char *path, const char *text)
{
    return write_bytes(path, text, strlen(text));
}

static void print_usage(const char *program)
{
    printf("Usage: %s [--scene NAME] [--output DIR] [--variant normal|inverted] [--animate]\n", program);
    printf("\n");
    printf("Render any registered LVGL screen to a pixel-accurate PNG preview.\n");
    printf("Default scene: monitor\n");
    printf("Default output directory: simulator/output/latest\n");
    printf("Default variant: normal\n\n");
    printf("Use --list-scenes to inspect available screens.\n");
    printf("Use --animate to export a deterministic PNG sequence at %u fps.\n", MONITOR_UI_ANIMATION_FPS);
}

static int export_animation(monitor_renderer_t *renderer, const char *out_dir, const char *stem)
{
    uint16_t *preview = malloc(MONITOR_RGB565_BYTES);
    if (!preview) return -1;
    int frames = 0;
    for (;;) {
        char path[SIMULATOR_PATH_MAX];
        int length = snprintf(path, sizeof(path), "%s/%s_frame_%03d.png", out_dir, stem, frames);
        if (length < 0 || (size_t)length >= sizeof(path) || !monitor_renderer_render(renderer)) break;
        monitor_unpack_to_rgb565(renderer->packed, MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT,
                                 MONITOR_FRAME_NORMAL, preview);
        if (!monitor_write_rgb565_png(path, preview, MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT)) break;
        ++frames;
        if (!monitor_ui_animation_active(renderer->root)) { free(preview); return frames; }
        if (frames >= 128) break;
        monitor_ui_advance(renderer->root,
            monitor_animation_frame_time((uint32_t)frames, 1000) -
            monitor_animation_frame_time((uint32_t)frames - 1U, 1000));
    }
    free(preview);
    return -1;
}

static void print_scenes(FILE *stream)
{
    for(size_t i = 0; i < simulator_scene_count(); ++i) {
        const simulator_scene_t *scene = simulator_scene_at(i);
        if(scene != NULL) fprintf(stream, "%-16s %s\n", scene->name, scene->description);
    }
}

int main(int argc, char **argv)
{
    const char *out_dir = "simulator/output/latest";
    const char *scene_name = "monitor";
    monitor_frame_variant_t variant = MONITOR_FRAME_NORMAL;
    const char *variant_name = "normal";
    bool animate = false;

    for(int i = 1; i < argc; ++i) {
        if(strcmp(argv[i], "--scene") == 0) {
            if(++i >= argc) {
                fprintf(stderr, "--scene requires a name\n");
                return 2;
            }
            scene_name = argv[i];
        } else if(strcmp(argv[i], "--output") == 0) {
            if(++i >= argc) {
                fprintf(stderr, "--output requires a directory\n");
                return 2;
            }
            out_dir = argv[i];
        } else if(strcmp(argv[i], "--variant") == 0) {
            if(++i >= argc) {
                fprintf(stderr, "--variant requires normal or inverted\n");
                return 2;
            }
            if(strcmp(argv[i], "normal") == 0) {
                variant = MONITOR_FRAME_NORMAL;
                variant_name = "normal";
            } else if(strcmp(argv[i], "inverted") == 0) {
                variant = MONITOR_FRAME_INVERTED;
                variant_name = "inverted";
            } else {
                fprintf(stderr, "unknown variant: %s\n", argv[i]);
                return 2;
            }
        } else if(strcmp(argv[i], "--animate") == 0) {
            animate = true;
        } else if(strcmp(argv[i], "--list-scenes") == 0) {
            print_scenes(stdout);
            return 0;
        } else if(strcmp(argv[i], "--help") == 0 || strcmp(argv[i], "-h") == 0) {
            print_usage(argv[0]);
            return 0;
        } else {
            fprintf(stderr, "unknown argument: %s\n", argv[i]);
            print_usage(argv[0]);
            return 2;
        }
    }

    selected_scene = simulator_scene_find(scene_name);
    if(selected_scene == NULL) {
        fprintf(stderr, "unknown scene: %s\nAvailable scenes:\n", scene_name);
        print_scenes(stderr);
        return 2;
    }
    selected_variant = variant;

    if(ensure_directory(out_dir) != 0) return 1;

    char stem[SCENE_NAME_MAX + 16];
    if(strcmp(selected_scene->name, "monitor") == 0) {
        snprintf(stem, sizeof(stem), "%s", variant_name);
    } else {
        snprintf(stem, sizeof(stem), "%s_%s", selected_scene->name, variant_name);
    }

    char rgb_path[SIMULATOR_PATH_MAX];
    char packed_path[SIMULATOR_PATH_MAX];
    char png_path[SIMULATOR_PATH_MAX];
    char digest_path[SIMULATOR_PATH_MAX];
    char meta_path[SIMULATOR_PATH_MAX];
    if(artifact_path(rgb_path, sizeof(rgb_path), out_dir, stem, "rgb565") != 0 ||
       artifact_path(packed_path, sizeof(packed_path), out_dir, stem, "packed") != 0 ||
       artifact_path(png_path, sizeof(png_path), out_dir, stem, "png") != 0 ||
       artifact_path(digest_path, sizeof(digest_path), out_dir, stem, "sha256") != 0 ||
       artifact_path(meta_path, sizeof(meta_path), out_dir, stem, "json") != 0) {
        return 1;
    }

    monitor_renderer_t renderer;
    if(!monitor_renderer_init(&renderer, create_selected_scene)) {
        fprintf(stderr, "LVGL renderer initialization failed\n");
        return 1;
    }
    int animation_frames = 0;
    if (animate) {
        animation_frames = export_animation(&renderer, out_dir, stem);
        if (animation_frames < 0) {
            fprintf(stderr, "animation export failed or timeline did not settle\n");
            monitor_renderer_deinit(&renderer);
            return 1;
        }
    } else {
        monitor_ui_set_animations_enabled(renderer.root, false);
    }
    if(!monitor_renderer_render(&renderer)) {
        fprintf(stderr, "LVGL render failed\n");
        monitor_renderer_deinit(&renderer);
        return 1;
    }

    char digest[65];
    monitor_sha256(monitor_renderer_packed(&renderer), MONITOR_FRAME_BYTES, digest);

    uint16_t *preview = (uint16_t *)malloc(MONITOR_RGB565_BYTES);
    if(preview == NULL) {
        fprintf(stderr, "preview allocation failed\n");
        monitor_renderer_deinit(&renderer);
        return 1;
    }
    monitor_unpack_to_rgb565(monitor_renderer_packed(&renderer),
                             MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT,
                             MONITOR_FRAME_NORMAL, preview);

    /* RGB565 is a desktop export reconstructed from the final monochrome frame. */
    int ok = 1;
    ok = write_bytes(rgb_path, preview, MONITOR_RGB565_BYTES) == 0 && ok;
    ok = write_bytes(packed_path, monitor_renderer_packed(&renderer), MONITOR_FRAME_BYTES) == 0 && ok;
    ok = monitor_write_rgb565_png(png_path, preview,
                                  MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT) && ok;
    ok = write_text(digest_path, digest) == 0 && ok;
    free(preview);

    char meta[1536];
    snprintf(meta, sizeof(meta),
             "{\n"
             "  \"scene\": \"%s\",\n"
             "  \"width\": %d,\n"
             "  \"height\": %d,\n"
             "  \"color_format\": \"I1\",\n"
             "  \"panel_format\": \"1bpp\",\n"
             "  \"panel_polarity\": \"1=white, 0=black\",\n"
             "  \"variant\": \"%s\",\n"
             "  \"animation_frames\": %d,\n"
             "  \"animation_fps\": %u,\n"
             "  \"animation_frame_ms\": %.6f,\n"
             "  \"png\": \"%s.png\",\n"
             "  \"packed\": \"%s.packed\",\n"
             "  \"rgb565\": \"%s.rgb565\",\n"
             "  \"sha256\": \"%s\"\n"
             "}\n",
             selected_scene->name, MONITOR_DISPLAY_WIDTH, MONITOR_DISPLAY_HEIGHT,
             variant_name, animation_frames, MONITOR_UI_ANIMATION_FPS,
             1000.0 / MONITOR_UI_ANIMATION_FPS, stem, stem, stem, digest);
    ok = write_text(meta_path, meta) == 0 && ok;
    monitor_renderer_deinit(&renderer);

    if(!ok) {
        fprintf(stderr, "failed to write one or more simulator artifacts\n");
        return 1;
    }

    printf("scene:   %s\n", selected_scene->name);
    printf("preview: %s\n", png_path);
    printf("packed:  %s\n", packed_path);
    printf("sha256:  %s\n", digest);
    return 0;
}
