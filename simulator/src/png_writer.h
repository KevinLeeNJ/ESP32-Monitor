#pragma once

#include <stdbool.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/* Write an RGB565 frame as an 8-bit RGB PNG without external image libraries. */
bool monitor_write_rgb565_png(const char *path, const uint16_t *pixels,
                              uint32_t width, uint32_t height);

#ifdef __cplusplus
}
#endif
