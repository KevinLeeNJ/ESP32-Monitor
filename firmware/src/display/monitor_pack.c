#include "monitor_ui.h"

#include <stddef.h>
#include <string.h>

/*
 * The RLCD examples use a 1bpp panel layout:
 *   landscape 400x300: for each x column, 4 vertically mirrored pixels per byte,
 *                       with x selecting the byte column and bit order y/x.
 * The simulator uses this exact mapping so its preview exercises the same data
 * path as the board instead of only comparing a widget screenshot.
 */
static inline uint32_t pack_index(uint32_t x, uint32_t y, uint32_t height)
{
    const uint32_t inv_y = height - 1U - y;
    return (x >> 1U) * (height >> 2U) + (inv_y >> 2U);
}

static inline uint8_t pack_mask(uint32_t x, uint32_t y, uint32_t height)
{
    const uint32_t inv_y = height - 1U - y;
    const uint32_t local_x = x & 1U;
    const uint32_t local_y = inv_y & 3U;
    return (uint8_t)(1U << (7U - ((local_y << 1U) | local_x)));
}

static inline bool rgb565_is_white(uint16_t pixel)
{
    /* The Waveshare RLCD example maps a displayed white pixel to bit 1:
       ColorBlack = 0, ColorWhite = 0xff. Keep that panel polarity here.
       White RGB565 is 0xffff; values above the midpoint are treated as white. */
    return pixel > 0x7fffU;
}

void monitor_pack_rgb565(const uint16_t *rgb565, uint32_t width, uint32_t height,
                         monitor_frame_variant_t variant, uint8_t *out)
{
    const uint32_t bytes = (width * height) / 8U;
    memset(out, 0, bytes);

    for(uint32_t y = 0; y < height; ++y) {
        for(uint32_t x = 0; x < width; ++x) {
            bool white = rgb565_is_white(rgb565[y * width + x]);
            if(variant == MONITOR_FRAME_INVERTED) {
                white = !white;
            }
            /* 1 = white, 0 = black, matching ColorWhite/ColorBlack in the
               Waveshare reference display driver. */
            if(white) {
                out[pack_index(x, y, height)] |= pack_mask(x, y, height);
            }
        }
    }
}

void monitor_pack_rgb565_region(const uint16_t *rgb565, uint32_t width, uint32_t height,
                                int32_t x1, int32_t y1, int32_t x2, int32_t y2,
                                monitor_frame_variant_t variant, uint8_t *packed_inout)
{
    if (rgb565 == NULL || packed_inout == NULL || width == 0 || height == 0) return;
    if (x1 < 0) x1 = 0;
    if (y1 < 0) y1 = 0;
    if (x2 >= (int32_t)width) x2 = (int32_t)width - 1;
    if (y2 >= (int32_t)height) y2 = (int32_t)height - 1;
    if (x1 > x2 || y1 > y2) return;
    for (int32_t y = y1; y <= y2; ++y) {
        for (int32_t x = x1; x <= x2; ++x) {
            bool white = rgb565_is_white(rgb565[(uint32_t)y * width + (uint32_t)x]);
            if (variant == MONITOR_FRAME_INVERTED) white = !white;
            const uint32_t index = pack_index((uint32_t)x, (uint32_t)y, height);
            const uint8_t mask = pack_mask((uint32_t)x, (uint32_t)y, height);
            if (white) packed_inout[index] |= mask;
            else packed_inout[index] &= (uint8_t)~mask;
        }
    }
}

void monitor_unpack_to_rgb565(const uint8_t *packed, uint32_t width, uint32_t height,
                              monitor_frame_variant_t variant, uint16_t *out)
{
    for(uint32_t y = 0; y < height; ++y) {
        for(uint32_t x = 0; x < width; ++x) {
            bool white = (packed[pack_index(x, y, height)] & pack_mask(x, y, height)) != 0;
            if(variant == MONITOR_FRAME_INVERTED) {
                white = !white;
            }
            out[y * width + x] = white ? 0xffffU : 0x0000U;
        }
    }
}
