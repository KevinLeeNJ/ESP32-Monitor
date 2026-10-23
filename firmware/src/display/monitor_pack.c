#include "monitor_ui.h"

#include <stddef.h>

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

void monitor_pack_i1_region(const uint8_t *pixels, uint32_t stride,
                            uint32_t width, uint32_t height,
                            int32_t x1, int32_t y1, int32_t x2, int32_t y2,
                            uint8_t *packed_inout)
{
    if(pixels == NULL || packed_inout == NULL || width == 0 || height == 0 ||
       width % 2U != 0 || height % 4U != 0 ||
       x1 < 0 || y1 < 0 || x1 > x2 || y1 > y2 ||
       x2 >= (int32_t)width || y2 >= (int32_t)height ||
       stride < ((uint32_t)(x2 - x1) + 8U) / 8U) return;

    for(int32_t y = y1; y <= y2; ++y) {
        const uint8_t *row = pixels + (uint32_t)(y - y1) * stride;
        for(int32_t x = x1; x <= x2; ++x) {
            const uint32_t local_x = (uint32_t)(x - x1);
            const bool white = (row[local_x / 8U] & (0x80U >> (local_x % 8U))) != 0;
            const uint32_t index = pack_index((uint32_t)x, (uint32_t)y, height);
            const uint8_t mask = pack_mask((uint32_t)x, (uint32_t)y, height);
            if(white) packed_inout[index] |= mask;
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
