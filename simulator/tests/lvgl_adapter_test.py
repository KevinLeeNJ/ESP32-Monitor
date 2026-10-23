"""Exercise production I1 panel callbacks and frame commits with a fake adapter.

The real adapter's registration and DMA/TE wiring are checked on the board.
"""
from pathlib import Path
import re
import subprocess
import sys
import tempfile

repo = Path(__file__).resolve().parents[2]
backend = (repo / "firmware/components/firmware/monitor_lvgl.c").read_text()
packing = (repo / "firmware/src/display/monitor_pack.c").read_text()


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
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#define MONITOR_DISPLAY_WIDTH 400
#define MONITOR_DISPLAY_HEIGHT 300
#define MONITOR_FRAME_BYTES 15000
#define LV_COLOR_FORMAT_I1 1
#define ESP_OK 0
#define ESP_ERR_INVALID_ARG -1
typedef int esp_err_t;
typedef struct { int unused; } esp_lcd_panel_t;
typedef struct { int unused; } lv_display_t;
typedef struct { int unused; } rlcd_port_t;
typedef struct {
    esp_lcd_panel_t panel;
    rlcd_port_t *port;
    lv_display_t *display;
    uint8_t *packed;
} monitor_lvgl_t;
static unsigned ready, transfers, chunks;
static int refresh_error, transfer_error;
static monitor_lvgl_t *current;
static uint32_t lv_draw_buf_width_to_stride(uint32_t width, int format) {
    assert(format == LV_COLOR_FORMAT_I1);
    return ((width + 7) / 8 + 3) & ~3U;
}
static void lv_display_flush_ready(lv_display_t *display) {
    assert(display == current->display);
    ++ready;
}
static esp_err_t esp_lv_adapter_refresh_now(lv_display_t *display);
static esp_err_t rlcd_port_draw_diff(rlcd_port_t *port, const uint8_t *packed, uint32_t size) {
    assert(port == current->port && packed == current->packed && size == MONITOR_FRAME_BYTES);
    assert(ready == chunks); /* Every partial region was consumed before TE/SPI. */
    ++transfers;
    return transfer_error;
}
'''
for name in ["pack_index", "pack_mask", "monitor_pack_i1_region"]:
    code += function(packing, name) + "\n"
for name in ["draw_bitmap", "monitor_lvgl_render"]:
    code += function(backend, name) + "\n"
code += r'''
static esp_err_t esp_lv_adapter_refresh_now(lv_display_t *display) {
    assert(display == current->display);
    if (refresh_error) return refresh_error;
    /* I1 payloads here have no palette, and each source row has padding. */
    const uint8_t first[] = {0xff, 0xff, 0xaa, 0xaa, 0xf0, 0x0f, 0xaa, 0xaa};
    const uint8_t overlap[] = {0, 0xaa, 0xaa, 0xaa};
    if (chunks > 0) assert(draw_bitmap(&current->panel, 8, 10, 24, 12, first) == ESP_OK);
    if (chunks > 1) assert(draw_bitmap(&current->panel, 8, 10, 16, 11, overlap) == ESP_OK);
    assert(transfers == 0); /* No SPI transfer for individual LVGL areas. */
    return ESP_OK;
}
int main(void) {
    uint8_t packed[MONITOR_FRAME_BYTES] = {0};
    lv_display_t display = {0};
    rlcd_port_t port = {0};
    monitor_lvgl_t renderer = {.display=&display, .port=&port, .packed=packed};
    current = &renderer;
    chunks = 2;
    assert(monitor_lvgl_render(&renderer) == ESP_OK && ready == 2 && transfers == 1);
    for (uint32_t y=0; y<300; ++y) for (uint32_t x=0; x<400; ++x) {
        bool expected = (y == 10 && x >= 16 && x < 24) ||
                        (y == 11 && ((x >= 8 && x < 12) || (x >= 20 && x < 24)));
        assert(!!(packed[pack_index(x,y,300)] & pack_mask(x,y,300)) == expected);
    }
    puts("Partial I1 stride, palette-free pixels, overlap and single frame commit: PASS");
    ready = transfers = chunks = 0;
    assert(monitor_lvgl_render(&renderer) == ESP_OK && transfers == 1);
    puts("Unchanged frame still reaches the driver's TE pacing path: PASS");
    ready = transfers = 0;
    refresh_error = -20;
    assert(monitor_lvgl_render(&renderer) == -20 && transfers == 0);
    refresh_error = 0;
    transfer_error = -30;
    assert(monitor_lvgl_render(&renderer) == -30 && transfers == 1);
    assert(monitor_lvgl_render(NULL) == ESP_ERR_INVALID_ARG);
    renderer.display = NULL;
    assert(monitor_lvgl_render(&renderer) == ESP_ERR_INVALID_ARG);
    puts("Adapter and physical transfer failures reach the existing recovery path: PASS");
}
'''
with tempfile.TemporaryDirectory(prefix="monitor-lvgl-adapter-") as directory:
    source = Path(directory) / "test.c"
    binary = Path(directory) / "test"
    source.write_text(code)
    compiler = sys.argv[1] if len(sys.argv) > 1 else "cc"
    subprocess.run([compiler, "-std=c11", "-Wall", "-Wextra", "-Werror", str(source), "-o", str(binary)], check=True)
    subprocess.run([str(binary)], check=True)
