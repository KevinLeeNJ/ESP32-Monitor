#include "png_writer.h"

#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static uint32_t crc32_update(uint32_t crc, const uint8_t *data, size_t len)
{
    static uint32_t table[256];
    static int initialized = 0;
    if(!initialized) {
        for(uint32_t i = 0; i < 256; ++i) {
            uint32_t c = i;
            for(int k = 0; k < 8; ++k) c = (c & 1U) ? (0xedb88320U ^ (c >> 1U)) : (c >> 1U);
            table[i] = c;
        }
        initialized = 1;
    }
    crc = crc ^ 0xffffffffU;
    for(size_t i = 0; i < len; ++i) crc = table[(crc ^ data[i]) & 0xffU] ^ (crc >> 8U);
    return crc ^ 0xffffffffU;
}

static void put_be32(uint8_t *p, uint32_t v)
{
    p[0] = (uint8_t)(v >> 24U); p[1] = (uint8_t)(v >> 16U);
    p[2] = (uint8_t)(v >> 8U); p[3] = (uint8_t)v;
}

static int write_chunk(FILE *f, const char type[4], const uint8_t *data, uint32_t len)
{
    uint8_t header[8];
    uint8_t crc_data[4];
    uint8_t crc[4];
    put_be32(header, len);
    memcpy(header + 4, type, 4);
    uint32_t c = crc32_update(0, (const uint8_t *)type, 4);
    if(len > 0 && data != NULL) c = crc32_update(c, data, len);
    put_be32(crc_data, c);
    (void)crc;
    if(fwrite(header, 1, sizeof(header), f) != sizeof(header)) return 0;
    if(len > 0 && fwrite(data, 1, len, f) != len) return 0;
    if(fwrite(crc_data, 1, sizeof(crc_data), f) != sizeof(crc_data)) return 0;
    return 1;
}

bool monitor_write_rgb565_png(const char *path, const uint16_t *pixels,
                              uint32_t width, uint32_t height)
{
    if(path == NULL || pixels == NULL || width == 0 || height == 0) return false;

    const size_t row_bytes = (size_t)width * 3U;
    const size_t raw_size = (row_bytes + 1U) * height;
    uint8_t *raw = (uint8_t *)malloc(raw_size);
    if(raw == NULL) return false;

    for(uint32_t y = 0; y < height; ++y) {
        uint8_t *row = raw + (row_bytes + 1U) * y;
        row[0] = 0;
        for(uint32_t x = 0; x < width; ++x) {
            uint16_t p = pixels[y * width + x];
            uint8_t r = (uint8_t)(((p >> 11U) & 0x1fU) * 255U / 31U);
            uint8_t g = (uint8_t)(((p >> 5U) & 0x3fU) * 255U / 63U);
            uint8_t b = (uint8_t)((p & 0x1fU) * 255U / 31U);
            row[1U + (size_t)x * 3U] = r;
            row[2U + (size_t)x * 3U] = g;
            row[3U + (size_t)x * 3U] = b;
        }
    }

    const size_t z_size = raw_size + raw_size / 65535U + 16U;
    uint8_t *z = (uint8_t *)malloc(z_size);
    if(z == NULL) { free(raw); return false; }

    /* A tiny fixed-Huffman DEFLATE stream: stored blocks are enough for test artifacts. */
    size_t zpos = 0;
    z[zpos++] = 0x78; z[zpos++] = 0x01;
    size_t remaining = raw_size;
    size_t offset = 0;
    while(remaining > 0) {
        uint16_t block = (uint16_t)(remaining > 65535U ? 65535U : remaining);
        z[zpos++] = (uint8_t)(remaining <= 65535U ? 1U : 0U);
        z[zpos++] = (uint8_t)(block & 0xffU);
        z[zpos++] = (uint8_t)(block >> 8U);
        uint16_t nlen = (uint16_t)~block;
        z[zpos++] = (uint8_t)(nlen & 0xffU);
        z[zpos++] = (uint8_t)(nlen >> 8U);
        memcpy(z + zpos, raw + offset, block);
        zpos += block;
        offset += block;
        remaining -= block;
    }
    uint32_t a = 1, b = 0;
    for(size_t i = 0; i < raw_size; ++i) { a = (a + raw[i]) % 65521U; b = (b + a) % 65521U; }
    uint32_t adler = (b << 16U) | a;
    z[zpos++] = (uint8_t)(adler >> 24U); z[zpos++] = (uint8_t)(adler >> 16U);
    z[zpos++] = (uint8_t)(adler >> 8U); z[zpos++] = (uint8_t)adler;
    free(raw);

    FILE *f = fopen(path, "wb");
    if(f == NULL) { free(z); return false; }
    static const uint8_t signature[8] = {137,80,78,71,13,10,26,10};
    int ok = fwrite(signature, 1, sizeof(signature), f) == sizeof(signature);
    uint8_t ihdr[13];
    put_be32(ihdr, width); put_be32(ihdr + 4, height);
    ihdr[8] = 8; ihdr[9] = 2; ihdr[10] = 0; ihdr[11] = 0; ihdr[12] = 0;
    ok = ok && write_chunk(f, "IHDR", ihdr, sizeof(ihdr));
    ok = ok && write_chunk(f, "IDAT", z, (uint32_t)zpos);
    ok = ok && write_chunk(f, "IEND", NULL, 0);
    ok = ok && fflush(f) == 0;
    ok = (fclose(f) == 0) && ok;
    free(z);
    return ok != 0;
}
