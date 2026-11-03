#include "monitor_fonts.h"

/* Generated 1-bpp fonts are linked as read-only data. On the device,
 * CONFIG_SPIRAM_RODATA maps this single copy into PSRAM at boot. */
const lv_font_t *monitor_music_font(void)
{
    return &monitor_music_18;
}

const lv_font_t *monitor_music_small_font(void)
{
    return &monitor_music_12;
}
