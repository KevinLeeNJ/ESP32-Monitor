"""Exercise production native LVGL frame commits with a fake shared renderer.

I1 partial packing is exercised by unit_tests.c; DMA/TE needs board validation.
"""
from pathlib import Path
import re
import subprocess
import sys
import tempfile

repo = Path(__file__).resolve().parents[2]
backend = (repo / "firmware/components/firmware/monitor_lvgl.c").read_text()


def function(source, name):
    match = re.search(r"^(?:static )?(?:inline )?\w+ " + name + r"\(", source, re.M)
    assert match, name
    start = match.start()
    end = source.index("{", start) + 1
    depth = 1
    while depth:
        depth += (source[end] == "{") - (source[end] == "}")
        end += 1
    return source[start:end]


code = r'''
#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#define MONITOR_FRAME_BYTES 15000
#define ESP_OK 0
#define ESP_FAIL -1
#define ESP_ERR_INVALID_ARG -2
typedef int esp_err_t;
typedef struct { int unused; } lv_display_t;
typedef struct { int unused; } rlcd_port_t;
typedef struct { lv_display_t *display; uint8_t *packed; } monitor_renderer_t;
typedef struct { rlcd_port_t *port; monitor_renderer_t renderer; } monitor_lvgl_t;
static unsigned renders, transfers;
static bool render_ok = true;
static int transfer_error;
static monitor_lvgl_t *current;
static bool monitor_renderer_render(monitor_renderer_t *renderer) {
    assert(renderer == &current->renderer);
    assert(transfers == 0); ++renders; return render_ok;
}
static const uint8_t *monitor_renderer_packed(const monitor_renderer_t *renderer) {
    assert(renderer == &current->renderer && renders == 1); return renderer->packed;
}
static esp_err_t rlcd_port_draw_diff(rlcd_port_t *port, const uint8_t *packed, uint32_t size) {
    assert(port == current->port && packed == current->renderer.packed && size == MONITOR_FRAME_BYTES);
    assert(renders == 1); ++transfers; return transfer_error;
}
'''
code += function(backend, "monitor_lvgl_render") + "\n"
code += r'''
int main(void) {
    uint8_t packed[MONITOR_FRAME_BYTES] = {0};
    lv_display_t display = {0}; rlcd_port_t port = {0};
    monitor_lvgl_t renderer = {.port=&port, .renderer={.display=&display, .packed=packed}};
    current = &renderer;
    assert(monitor_lvgl_render(&renderer) == ESP_OK && renders == 1 && transfers == 1);
    renders = transfers = 0;
    /* Even an unchanged frame reaches draw_diff, which owns TE pacing. */
    assert(monitor_lvgl_render(&renderer) == ESP_OK && renders == 1 && transfers == 1);
    renders = transfers = 0; render_ok = false;
    assert(monitor_lvgl_render(&renderer) == ESP_FAIL && renders == 1 && transfers == 0);
    renders = 0; render_ok = true; transfer_error = -30;
    assert(monitor_lvgl_render(&renderer) == -30 && renders == 1 && transfers == 1);
    renders = transfers = 0;
    assert(monitor_lvgl_render(NULL) == ESP_ERR_INVALID_ARG);
    renderer.renderer.display = NULL;
    assert(monitor_lvgl_render(&renderer) == ESP_ERR_INVALID_ARG && !renders && !transfers);
    puts("Native shared renderer precedes exactly one frame commit; unchanged frames, invalid inputs and both failure paths: PASS");
}
'''
with tempfile.TemporaryDirectory(prefix="monitor-lvgl-adapter-") as directory:
    source = Path(directory) / "test.c"
    binary = Path(directory) / "test"
    source.write_text(code)
    compiler = sys.argv[1] if len(sys.argv) > 1 else "cc"
    subprocess.run([compiler, "-std=c11", "-Wall", "-Wextra", "-Werror", str(source), "-o", str(binary)], check=True)
    subprocess.run([str(binary)], check=True)
