#include "monitor_fonts.h"
#include <stddef.h>
#ifndef ESP_PLATFORM
#include <stdio.h>
#include <stdlib.h>
#endif

/* Load the shared LVGL .bin font lazily on first non-ASCII music metadata.
 * The board reads the resource from flash; LVGL owns the decoded font. */
const lv_font_t *monitor_music_font(void)
{
    static lv_font_t *font;
    if (font) return font;
#ifdef ESP_PLATFORM
    extern const unsigned char start[] asm("_binary_music_cjk_bin_start");
    extern const unsigned char end[] asm("_binary_music_cjk_bin_end");
    font = lv_binfont_create_from_buffer((void *)start, (uint32_t)(end - start));
#else
    FILE *file = fopen(MONITOR_MUSIC_FONT_PATH, "rb");
    if (!file) return NULL;
    if (fseek(file, 0, SEEK_END) != 0) { fclose(file); return NULL; }
    long size = ftell(file);
    if (size <= 0 || size > UINT32_MAX || fseek(file, 0, SEEK_SET) != 0) { fclose(file); return NULL; }
    void *data = malloc((size_t)size);
    if (!data) { fclose(file); return NULL; }
    size_t read = fread(data, 1, (size_t)size, file);
    fclose(file);
    if (read == (size_t)size) font = lv_binfont_create_from_buffer(data, (uint32_t)size);
    free(data);
#endif
    return font;
}
