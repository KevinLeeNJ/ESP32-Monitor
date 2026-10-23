/* This application uses MiSans Regular.
 * Copyright 2020-2023 Beijing Xiaomi Mobile Software Co.,Ltd.
 * Font source: https://hyperos.mi.com/font/
 * Regenerate with firmware/assets/fonts/generate_misans.py.
 */
/*******************************************************************************
 * Size: 8 px
 * Bpp: 1
 * Opts: --size 8 --bpp 1 --format lvgl --no-compress --font MiSans-Regular.otf --range 0x20-0x7e,0xb0 --symbols 晴少云多阴天小雨中大雷阵雪夹冰雹雾霾沙尘暴 --lv-font-name monitor_misans_8 --output monitor_misans_8.c
 ******************************************************************************/

#ifdef LV_LVGL_H_INCLUDE_SIMPLE
#include "lvgl.h"
#else
#include "lvgl/lvgl.h"
#endif

#ifndef MONITOR_MISANS_8
#define MONITOR_MISANS_8 1
#endif

#if MONITOR_MISANS_8

/*-----------------
 *    BITMAPS
 *----------------*/

/*Store the image of the glyphs*/
static LV_ATTRIBUTE_LARGE_CONST const uint8_t glyph_bitmap[] = {
    /* U+0020 " " */
    0x0,

    /* U+0021 "!" */
    0xf8,

    /* U+0022 "\"" */
    0xf0,

    /* U+0023 "#" */
    0x33, 0xd5, 0xf5, 0x0,

    /* U+0024 "$" */
    0x23, 0xa8, 0xc3, 0xd5, 0xc0,

    /* U+0025 "%" */
    0xe9, 0xd0, 0x71, 0x95, 0xe0,

    /* U+0026 "&" */
    0x72, 0x99, 0x3f, 0x80,

    /* U+0027 "'" */
    0xc0,

    /* U+0028 "(" */
    0x6a, 0xa4,

    /* U+0029 ")" */
    0xa5, 0x58,

    /* U+002A "*" */
    0x70,

    /* U+002B "+" */
    0x5d, 0x0,

    /* U+002C "," */
    0xc0,

    /* U+002D "-" */
    0xe0,

    /* U+002E "." */
    0x0,

    /* U+002F "/" */
    0x29, 0x28,

    /* U+0030 "0" */
    0x69, 0x99, 0x60,

    /* U+0031 "1" */
    0xd5, 0x40,

    /* U+0032 "2" */
    0xf1, 0x24, 0xf0,

    /* U+0033 "3" */
    0xf4, 0x71, 0xf0,

    /* U+0034 "4" */
    0x32, 0xa5, 0xf1, 0x0,

    /* U+0035 "5" */
    0xfe, 0x19, 0x60,

    /* U+0036 "6" */
    0x46, 0x99, 0xe0,

    /* U+0037 "7" */
    0xf2, 0x24, 0x40,

    /* U+0038 "8" */
    0xf9, 0x69, 0xf0,

    /* U+0039 "9" */
    0xe9, 0x72, 0x40,

    /* U+003A ":" */
    0x0,

    /* U+003B ";" */
    0x98,

    /* U+003C "<" */
    0x16, 0x86,

    /* U+003D "=" */
    0xfc,

    /* U+003E ">" */
    0x86, 0x3c,

    /* U+003F "?" */
    0xe5, 0x4,

    /* U+0040 "@" */
    0x7b, 0xfc, 0xee, 0x78,

    /* U+0041 "A" */
    0x22, 0x9d, 0x18, 0x80,

    /* U+0042 "B" */
    0xf9, 0xe9, 0xf0,

    /* U+0043 "C" */
    0x7c, 0x21, 0x7, 0x80,

    /* U+0044 "D" */
    0xf4, 0x63, 0x1f, 0x0,

    /* U+0045 "E" */
    0xf8, 0xf8, 0xf0,

    /* U+0046 "F" */
    0xf8, 0xf8, 0x80,

    /* U+0047 "G" */
    0x74, 0x27, 0x17, 0x80,

    /* U+0048 "H" */
    0x8c, 0x7f, 0x18, 0x80,

    /* U+0049 "I" */
    0xf8,

    /* U+004A "J" */
    0x24, 0x9e,

    /* U+004B "K" */
    0x95, 0x39, 0x29, 0x0,

    /* U+004C "L" */
    0x88, 0x88, 0xf0,

    /* U+004D "M" */
    0x87, 0x3b, 0x61, 0x84,

    /* U+004E "N" */
    0x8e, 0x6b, 0x38, 0x80,

    /* U+004F "O" */
    0x7a, 0x18, 0x61, 0x78,

    /* U+0050 "P" */
    0xf9, 0xe8, 0x80,

    /* U+0051 "Q" */
    0x7a, 0x18, 0x61, 0x78, 0x10,

    /* U+0052 "R" */
    0xf9, 0xea, 0x90,

    /* U+0053 "S" */
    0xf8, 0x61, 0xf0,

    /* U+0054 "T" */
    0xf9, 0x8, 0x42, 0x0,

    /* U+0055 "U" */
    0x8c, 0x63, 0x17, 0x0,

    /* U+0056 "V" */
    0x8a, 0x54, 0xa2, 0x0,

    /* U+0057 "W" */
    0x92, 0x9a, 0x6a, 0x6c, 0x24,

    /* U+0058 "X" */
    0x52, 0x88, 0xa8, 0x80,

    /* U+0059 "Y" */
    0x8a, 0x88, 0x42, 0x0,

    /* U+005A "Z" */
    0xf2, 0x44, 0xf0,

    /* U+005B "[" */
    0xea, 0xac,

    /* U+005C "\\" */
    0x91, 0x24,

    /* U+005D "]" */
    0xd5, 0x5c,

    /* U+005E "^" */
    0x54,

    /* U+005F "_" */
    0xf0,

    /* U+0060 "`" */
    0x10,

    /* U+0061 "a" */
    0x77, 0x9f,

    /* U+0062 "b" */
    0x8e, 0x99, 0xe0,

    /* U+0063 "c" */
    0x78, 0x87,

    /* U+0064 "d" */
    0x17, 0x99, 0x70,

    /* U+0065 "e" */
    0x6e, 0x86,

    /* U+0066 "f" */
    0x7d, 0x24,

    /* U+0067 "g" */
    0x79, 0x97, 0x16,

    /* U+0068 "h" */
    0x8f, 0x99, 0x90,

    /* U+0069 "i" */
    0x78,

    /* U+006A "j" */
    0x15, 0x5c,

    /* U+006B "k" */
    0x8a, 0xca, 0xa0,

    /* U+006C "l" */
    0xaa, 0xc0,

    /* U+006D "m" */
    0xfd, 0x6b, 0x50,

    /* U+006E "n" */
    0xf9, 0x99,

    /* U+006F "o" */
    0x69, 0x96,

    /* U+0070 "p" */
    0xe9, 0x9e, 0x88,

    /* U+0071 "q" */
    0x79, 0x97, 0x11,

    /* U+0072 "r" */
    0xf2, 0x40,

    /* U+0073 "s" */
    0xf1, 0xf0,

    /* U+0074 "t" */
    0x4b, 0xa4, 0xc0,

    /* U+0075 "u" */
    0x99, 0x9f,

    /* U+0076 "v" */
    0x9a, 0x66,

    /* U+0077 "w" */
    0x96, 0xd7, 0x92,

    /* U+0078 "x" */
    0xa6, 0x69,

    /* U+0079 "y" */
    0x99, 0x66, 0x44,

    /* U+007A "z" */
    0xe9, 0x70,

    /* U+007B "{" */
    0x69, 0x44, 0x88,

    /* U+007C "|" */
    0xfe,

    /* U+007D "}" */
    0xc9, 0x14, 0xa0,

    /* U+007E "~" */
    0x70,

    /* U+00B0 "°" */
    0x55, 0x0,

    /* U+4E2D "中" */
    0x10, 0x23, 0xfc, 0x9f, 0xe2, 0x4, 0x8,

    /* U+4E91 "云" */
    0x7c, 0x3, 0xf9, 0x4, 0x88, 0x8e, 0x80,

    /* U+51B0 "冰" */
    0x8, 0x88, 0x7a, 0x1c, 0xac, 0xcb, 0x18,

    /* U+591A "多" */
    0x21, 0xfc, 0x9c, 0xbd, 0x1b, 0xb8,

    /* U+5927 "大" */
    0x10, 0x23, 0xf8, 0x83, 0x5, 0x11, 0x41,

    /* U+5929 "天" */
    0xfe, 0x23, 0xf8, 0x82, 0x88, 0xa0, 0x80,

    /* U+5939 "夹" */
    0x11, 0xfc, 0x50, 0x8f, 0xe2, 0xa, 0x63,

    /* U+5C0F "小" */
    0x10, 0xa9, 0x54, 0xa9, 0x22, 0x5c, 0x0,

    /* U+5C11 "少" */
    0x10, 0xaa, 0x4c, 0xa0, 0x86, 0x30, 0x0,

    /* U+5C18 "尘" */
    0x10, 0xaa, 0x48, 0x87, 0xc2, 0x3f, 0x80,

    /* U+6674 "晴" */
    0x9, 0xba, 0xfd, 0xfd, 0xdb, 0xb7, 0x0,

    /* U+66B4 "暴" */
    0x7c, 0xfb, 0xff, 0xf7, 0xdf, 0xec, 0x80,

    /* U+6C99 "沙" */
    0x88, 0xda, 0xa8, 0x58, 0x53, 0x28, 0x0,

    /* U+9634 "阴" */
    0xff, 0xbf, 0x4d, 0xfd, 0x34, 0xe8, 0x0,

    /* U+9635 "阵" */
    0x9, 0xfe, 0xc6, 0xca, 0xdf, 0xe2, 0x44,

    /* U+96E8 "雨" */
    0xff, 0xfe, 0x6c, 0x9b, 0x32, 0xc0,

    /* U+96EA "雪" */
    0xff, 0xf5, 0x84, 0xff, 0xf0, 0x40,

    /* U+96F7 "雷" */
    0x7d, 0xf5, 0xe3, 0xe7, 0xcf, 0xa1, 0x0,

    /* U+96F9 "雹" */
    0xff, 0xf5, 0xe4, 0x27, 0xef, 0xc0,

    /* U+96FE "雾" */
    0xff, 0xfc, 0x47, 0xc7, 0xdb, 0x80,

    /* U+973E "霾" */
    0xff, 0xff, 0x7d, 0xf5, 0xff, 0xc0
};


/*---------------------
 *  GLYPH DESCRIPTION
 *--------------------*/

static const lv_font_fmt_txt_glyph_dsc_t glyph_dsc[] = {
    {.bitmap_index = 0, .adv_w = 0, .box_w = 0, .box_h = 0, .ofs_x = 0, .ofs_y = 0} /* id = 0 reserved */,
    {.bitmap_index = 0, .adv_w = 37, .box_w = 1, .box_h = 1, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 1, .adv_w = 34, .box_w = 1, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 2, .adv_w = 43, .box_w = 2, .box_h = 2, .ofs_x = 0, .ofs_y = 4},
    {.bitmap_index = 3, .adv_w = 90, .box_w = 5, .box_h = 5, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 7, .adv_w = 74, .box_w = 5, .box_h = 7, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 12, .adv_w = 105, .box_w = 7, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 17, .adv_w = 91, .box_w = 5, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 21, .adv_w = 24, .box_w = 1, .box_h = 2, .ofs_x = 0, .ofs_y = 4},
    {.bitmap_index = 22, .adv_w = 40, .box_w = 2, .box_h = 7, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 24, .adv_w = 40, .box_w = 2, .box_h = 7, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 26, .adv_w = 57, .box_w = 2, .box_h = 2, .ofs_x = 1, .ofs_y = 3},
    {.bitmap_index = 27, .adv_w = 78, .box_w = 3, .box_h = 3, .ofs_x = 1, .ofs_y = 1},
    {.bitmap_index = 29, .adv_w = 33, .box_w = 1, .box_h = 2, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 30, .adv_w = 58, .box_w = 3, .box_h = 1, .ofs_x = 0, .ofs_y = 2},
    {.bitmap_index = 31, .adv_w = 32, .box_w = 1, .box_h = 1, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 32, .adv_w = 51, .box_w = 3, .box_h = 5, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 34, .adv_w = 81, .box_w = 4, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 37, .adv_w = 52, .box_w = 2, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 39, .adv_w = 72, .box_w = 4, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 42, .adv_w = 76, .box_w = 4, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 45, .adv_w = 76, .box_w = 5, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 49, .adv_w = 77, .box_w = 4, .box_h = 5, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 52, .adv_w = 78, .box_w = 4, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 55, .adv_w = 67, .box_w = 4, .box_h = 5, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 58, .adv_w = 81, .box_w = 4, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 61, .adv_w = 77, .box_w = 4, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 64, .adv_w = 32, .box_w = 1, .box_h = 4, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 65, .adv_w = 33, .box_w = 1, .box_h = 5, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 66, .adv_w = 78, .box_w = 4, .box_h = 4, .ofs_x = 1, .ofs_y = 1},
    {.bitmap_index = 68, .adv_w = 78, .box_w = 3, .box_h = 2, .ofs_x = 1, .ofs_y = 2},
    {.bitmap_index = 69, .adv_w = 78, .box_w = 4, .box_h = 4, .ofs_x = 1, .ofs_y = 1},
    {.bitmap_index = 71, .adv_w = 63, .box_w = 3, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 73, .adv_w = 105, .box_w = 6, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 77, .adv_w = 85, .box_w = 5, .box_h = 5, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 81, .adv_w = 86, .box_w = 4, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 84, .adv_w = 90, .box_w = 5, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 88, .adv_w = 95, .box_w = 5, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 92, .adv_w = 76, .box_w = 4, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 95, .adv_w = 74, .box_w = 4, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 98, .adv_w = 94, .box_w = 5, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 102, .adv_w = 92, .box_w = 5, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 106, .adv_w = 31, .box_w = 1, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 107, .adv_w = 62, .box_w = 3, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 109, .adv_w = 84, .box_w = 5, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 113, .adv_w = 72, .box_w = 4, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 116, .adv_w = 112, .box_w = 6, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 120, .adv_w = 94, .box_w = 5, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 124, .adv_w = 101, .box_w = 6, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 128, .adv_w = 80, .box_w = 4, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 131, .adv_w = 101, .box_w = 6, .box_h = 6, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 136, .adv_w = 81, .box_w = 4, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 139, .adv_w = 77, .box_w = 4, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 142, .adv_w = 77, .box_w = 5, .box_h = 5, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 146, .adv_w = 90, .box_w = 5, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 150, .adv_w = 85, .box_w = 5, .box_h = 5, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 154, .adv_w = 123, .box_w = 8, .box_h = 5, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 159, .adv_w = 81, .box_w = 5, .box_h = 5, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 163, .adv_w = 80, .box_w = 5, .box_h = 5, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 167, .adv_w = 76, .box_w = 4, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 170, .adv_w = 45, .box_w = 2, .box_h = 7, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 172, .adv_w = 43, .box_w = 3, .box_h = 5, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 174, .adv_w = 45, .box_w = 2, .box_h = 7, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 176, .adv_w = 57, .box_w = 3, .box_h = 2, .ofs_x = 0, .ofs_y = 4},
    {.bitmap_index = 177, .adv_w = 59, .box_w = 4, .box_h = 1, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 178, .adv_w = 33, .box_w = 2, .box_h = 2, .ofs_x = 0, .ofs_y = 4},
    {.bitmap_index = 179, .adv_w = 69, .box_w = 4, .box_h = 4, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 181, .adv_w = 77, .box_w = 4, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 184, .adv_w = 68, .box_w = 4, .box_h = 4, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 186, .adv_w = 77, .box_w = 4, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 189, .adv_w = 71, .box_w = 4, .box_h = 4, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 191, .adv_w = 45, .box_w = 3, .box_h = 5, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 193, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 1, .ofs_y = -2},
    {.bitmap_index = 196, .adv_w = 74, .box_w = 4, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 199, .adv_w = 32, .box_w = 1, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 200, .adv_w = 32, .box_w = 2, .box_h = 7, .ofs_x = 0, .ofs_y = -2},
    {.bitmap_index = 202, .adv_w = 65, .box_w = 4, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 205, .adv_w = 32, .box_w = 2, .box_h = 5, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 207, .adv_w = 113, .box_w = 5, .box_h = 4, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 210, .adv_w = 74, .box_w = 4, .box_h = 4, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 212, .adv_w = 74, .box_w = 4, .box_h = 4, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 214, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 1, .ofs_y = -2},
    {.bitmap_index = 217, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 1, .ofs_y = -2},
    {.bitmap_index = 220, .adv_w = 48, .box_w = 3, .box_h = 4, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 222, .adv_w = 60, .box_w = 3, .box_h = 4, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 224, .adv_w = 46, .box_w = 3, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 227, .adv_w = 74, .box_w = 4, .box_h = 4, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 229, .adv_w = 65, .box_w = 4, .box_h = 4, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 231, .adv_w = 102, .box_w = 6, .box_h = 4, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 234, .adv_w = 63, .box_w = 4, .box_h = 4, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 236, .adv_w = 66, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = -2},
    {.bitmap_index = 239, .adv_w = 61, .box_w = 3, .box_h = 4, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 241, .adv_w = 45, .box_w = 3, .box_h = 7, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 244, .adv_w = 29, .box_w = 1, .box_h = 7, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 245, .adv_w = 45, .box_w = 3, .box_h = 7, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 248, .adv_w = 67, .box_w = 4, .box_h = 1, .ofs_x = 0, .ofs_y = 2},
    {.bitmap_index = 249, .adv_w = 52, .box_w = 3, .box_h = 3, .ofs_x = 1, .ofs_y = 3},
    {.bitmap_index = 251, .adv_w = 128, .box_w = 7, .box_h = 8, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 258, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 265, .adv_w = 128, .box_w = 8, .box_h = 7, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 272, .adv_w = 128, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 278, .adv_w = 128, .box_w = 7, .box_h = 8, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 285, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 292, .adv_w = 128, .box_w = 7, .box_h = 8, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 299, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 306, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 313, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 320, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 327, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 334, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 341, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 348, .adv_w = 128, .box_w = 7, .box_h = 8, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 355, .adv_w = 128, .box_w = 7, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 361, .adv_w = 128, .box_w = 6, .box_h = 7, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 367, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 374, .adv_w = 128, .box_w = 7, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 380, .adv_w = 128, .box_w = 7, .box_h = 6, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 386, .adv_w = 128, .box_w = 7, .box_h = 6, .ofs_x = 1, .ofs_y = 0}
};

/*---------------------
 *  CHARACTER MAPPING
 *--------------------*/

static const uint16_t unicode_list_1[] = {
    0x0, 0x4d7d, 0x4de1, 0x5100, 0x586a, 0x5877, 0x5879, 0x5889,
    0x5b5f, 0x5b61, 0x5b68, 0x65c4, 0x6604, 0x6be9, 0x9584, 0x9585,
    0x9638, 0x963a, 0x9647, 0x9649, 0x964e, 0x968e
};

/*Collect the unicode lists and glyph_id offsets*/
static const lv_font_fmt_txt_cmap_t cmaps[] =
{
    {
        .range_start = 32, .range_length = 95, .glyph_id_start = 1,
        .unicode_list = NULL, .glyph_id_ofs_list = NULL, .list_length = 0, .type = LV_FONT_FMT_TXT_CMAP_FORMAT0_TINY
    },
    {
        .range_start = 176, .range_length = 38543, .glyph_id_start = 96,
        .unicode_list = unicode_list_1, .glyph_id_ofs_list = NULL, .list_length = 22, .type = LV_FONT_FMT_TXT_CMAP_SPARSE_TINY
    }
};

/*-----------------
 *    KERNING
 *----------------*/


/*Map glyph_ids to kern left classes*/
static const uint8_t kern_left_class_mapping[] =
{
    0, 0, 0, 1, 0, 0, 0, 2,
    1, 0, 0, 0, 0, 3, 4, 3,
    5, 6, 0, 7, 8, 9, 10, 11,
    12, 13, 14, 0, 0, 0, 0, 0,
    15, 0, 16, 17, 18, 19, 20, 21,
    22, 0, 0, 23, 24, 25, 0, 0,
    19, 26, 27, 28, 29, 30, 31, 32,
    33, 34, 35, 36, 0, 0, 0, 0,
    0, 0, 37, 38, 39, 0, 40, 41,
    42, 43, 0, 0, 44, 45, 43, 43,
    38, 38, 42, 46, 47, 48, 42, 49,
    50, 51, 49, 52, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0
};

/*Map glyph_ids to kern right classes*/
static const uint8_t kern_right_class_mapping[] =
{
    0, 0, 0, 1, 0, 0, 0, 2,
    1, 0, 0, 0, 0, 3, 4, 3,
    5, 6, 7, 8, 9, 10, 11, 12,
    13, 14, 15, 0, 0, 0, 0, 0,
    16, 0, 17, 0, 18, 0, 0, 0,
    18, 0, 0, 19, 0, 0, 0, 0,
    18, 0, 18, 0, 20, 21, 22, 23,
    24, 25, 26, 27, 0, 0, 0, 0,
    0, 0, 28, 0, 29, 29, 29, 30,
    29, 0, 31, 31, 0, 32, 33, 33,
    29, 33, 29, 33, 34, 35, 36, 37,
    38, 39, 37, 40, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0
};

/*Kern values between classes*/
static const int8_t kern_class_values[] =
{
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -12, 0, -8, 3, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -1, -4, -2, -12, -4, -12, -8,
    0, -13, 0, -2, -1, 0, 0, -3,
    0, 0, -4, 0, -5, -4, 0, 0,
    0, 0, 0, 0, 0, -4, -11, 0,
    0, -6, -3, -3, -4, -1, -13, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, -4,
    0, 0, 0, 0, -6, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -9, 0, -6, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, -4, 0, 0, 0, 0, -4,
    -2, 0, 0, 0, -3, 0, 0, 0,
    -3, 0, -4, 0, -5, 0, -3, -2,
    0, -7, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, -1, 0, -2, 0, 0,
    -3, -4, 0, -3, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, -2, 0,
    0, -3, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, -2, 0, 0, 0, 0, -3,
    0, 0, 0, 0, 0, 0, -1, -3,
    0, 0, 0, 0, 0, 0, 0, 0,
    -1, -3, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    -1, 0, 0, 0, 0, 0, -4, 0,
    -3, 0, 0, 0, -4, 0, -3, -4,
    0, 0, 0, 0, -3, 0, -5, 0,
    0, -5, 1, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, -3, 0, 0, 0, -4, 0,
    0, 0, 0, 0, -2, 0, 0, -6,
    0, 0, 0, 0, 0, 0, -3, 0,
    -4, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    -9, 0, -3, 0, 0, 0, -8, 0,
    0, 0, 0, 0, -7, 0, -1, -8,
    0, 0, 0, 0, -13, 0, -10, 0,
    -2, -13, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -5, 0, -4, -3,
    3, 0, -18, -4, -12, -3, 0, 0,
    0, -9, 0, -9, 1, -4, 0, 1,
    -12, 0, -14, 0, 0, 0, 0, 0,
    0, 0, 0, -11, -11, -4, 0, 0,
    -6, -8, 0, -5, 0, 0, -5, -5,
    0, 0, -1, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -3, 0, -3, -3,
    0, 0, 0, 0, -3, 0, -2, 0,
    0, -5, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, -3, 0,
    0, 0, -14, 0, -6, 0, 0, 0,
    0, -4, 0, -3, 0, -3, 0, 0,
    -9, 0, -11, 0, 0, 0, -1, 0,
    -6, -3, 0, -5, -4, 1, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -7, -3, -4, 1, -3, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    -11, -1, 0, -5, 0, -3, -8, 0,
    0, 0, 0, 0, 0, 0, -9, -10,
    0, -4, 0, 0, -12, -2, -12, -9,
    0, -12, 0, 0, -2, -1, 0, 0,
    0, 0, -3, -2, -6, -4, 0, 0,
    -4, 0, -2, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, -3,
    -3, 0, -1, 0, -3, 0, -5, -2,
    -3, -5, -2, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -1, 0, -1, -1,
    0, 0, 0, -5, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -3, 0, 0, 0, 0, 0, 0,
    -3, -4, -1, -2, -3, 0, 0, 0,
    0, 0, 0, 0, -3, -3, -1, 0,
    -3, 0, -5, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, -3,
    -4, 0, -8, 0, -4, 0, -5, -2,
    -5, -6, -4, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, -2,
    0, -3, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -2, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -3, -2, 0, 0,
    2, -3, -9, 0, 0, 0, 0, 0,
    0, -3, 0, -4, 0, 0, 0, 0,
    -8, 0, -20, -2, 0, 0, 0, 0,
    -3, 0, 0, -7, -4, 0, 0, 0,
    -3, -2, 0, -3, -1, -1, -5, -3,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, -3,
    -3, 0, 0, 0, -4, 0, -5, -2,
    -2, -6, -2, 0, 0, 0, 0, 0,
    0, -1, 0, 0, -3, -4, -3, 0,
    0, 0, -2, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    -2, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -5, 2, -12, 0, -6, -4, 0,
    0, -3, 0, 0, 0, 0, -6, -4,
    0, -7, -3, -3, 0, -6, 0, 0,
    0, 0, 0, -4, -7, -4, 0, -3,
    -1, -1, -6, -6, -9, -8, 2, 0,
    -12, 0, 3, -12, 0, 0, -9, 0,
    0, 0, 0, 0, -3, 0, -15, -12,
    0, -5, 0, 0, -20, -3, -17, -11,
    0, -22, 0, 0, -2, -3, 0, 0,
    0, 0, -5, -3, -10, -6, 0, 0,
    0, -5, -13, 0, 0, 0, 0, -1,
    0, -4, 0, 0, 0, 0, 0, 0,
    -10, 0, -22, 0, -3, 0, -3, -2,
    -3, -4, -3, -4, -3, 3, 0, 0,
    0, 0, 3, 0, 4, 4, 1, 2,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    -4, 0, 0, 0, -4, 0, -3, -3,
    -6, -6, -1, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -2, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -1, -4, 0, -2, 0, -2, -2,
    0, -3, 0, -2, -3, 0, 0, 0,
    0, 0, 0, -3, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -1, 0, -1, 0,
    -1, -4, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -3, -3, -1, -2,
    3, -9, -14, -10, 0, -5, 0, 0,
    0, -13, 0, -10, 0, -3, 0, 0,
    -12, -4, -18, -2, 0, 0, -3, 0,
    -4, 0, 0, -15, -15, -5, 0, 0,
    -12, -14, -3, -10, -14, -13, -14, -12,
    0, 0, -5, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    -2, 0, -5, 0, 0, 0, 0, 0,
    -2, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -9, -17, -5, 0, -3, 0, 0,
    0, -8, -3, -7, 0, -2, 0, 0,
    -12, -5, -20, 0, 0, 0, 0, 0,
    0, 0, 0, -11, -9, -2, 0, 0,
    -6, -8, -1, -6, -4, -4, -5, -4,
    0, -5, -11, -3, 0, -2, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    -9, -2, -13, 1, 0, 0, 0, 0,
    0, 0, 0, -8, -7, -2, 0, 0,
    -5, -6, 0, -6, -1, -3, -4, -3,
    0, -4, 1, -10, 0, 0, -4, 0,
    -1, -2, -3, -3, 0, 0, -6, -3,
    0, -5, -1, -1, -4, -2, 0, 0,
    0, 0, 0, -2, -4, -2, 0, -1,
    0, -4, -5, -6, -7, -8, 0, 0,
    0, -10, -15, -8, 0, -7, 0, -3,
    -1, -13, 0, -11, 0, -5, -2, 0,
    -12, -6, -22, -3, 0, 0, 0, 0,
    0, 0, 0, -14, -14, -5, 0, 0,
    -10, -12, -4, -11, -6, -6, -7, -8,
    0, -5, 0, -6, 0, 0, 0, 0,
    0, -6, 0, 0, 0, 0, 0, 0,
    0, -6, 0, 0, 0, -2, 0, 0,
    0, 0, 0, 0, -2, 0, 0, 0,
    0, 0, -4, -4, -5, -5, 0, 0,
    -5, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -6, 0, -2, -7,
    0, 0, 0, 0, -14, 0, -9, -8,
    0, -12, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -2, -2, 0, 0,
    -5, 0, -3, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -7, 0, -3, -9,
    -2, 0, 0, 0, -15, 0, -9, -7,
    -4, -14, -1, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -2, -1, -3, -1,
    -2, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, -4,
    0, 0, 0, 0, -14, 0, -7, -4,
    -2, -14, 0, -1, -2, 0, -1, -1,
    0, 0, 0, 0, -1, 0, -3, 0,
    0, 0, -1, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -7, 0, 0, -7,
    0, 0, 0, 0, -13, 0, -8, -5,
    -4, -10, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -1, -1, -2, 0,
    4, -4, -8, 0, 0, 0, 0, 0,
    0, -5, 0, 0, 3, 0, 2, 0,
    -7, 0, -9, 0, 0, 0, 0, 0,
    0, 0, 0, -2, -2, 0, 0, 0,
    0, -2, 1, -1, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -4, 0, 0, -4,
    0, 0, 0, 0, -12, 0, -6, -5,
    -1, -10, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    -4, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -5, 0, -2, -6,
    0, 0, 0, 0, -15, 0, -11, -8,
    -2, -13, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -2, -1, -1, 0,
    -3, -2, 1, -3, 0, 0, 0, 0,
    0, -4, 0, -3, 0, -3, -1, -2,
    0, -3, 0, 0, -13, -2, -6, -3,
    0, -10, 0, -1, -3, 0, 0, -1,
    0, -1, -1, -2, 0, 0, 0, 0,
    0, -2, 2, -2, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, -3,
    0, -2, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 1, 0, 0, 0, 0,
    1, 0, 0, 1, -1, -1, 0, 0,
    0, -6, -14, 0, 0, 0, 0, -2,
    -6, -6, 0, -3, 0, 0, 2, 0,
    0, 0, 0, 0, -12, 0, 0, 0,
    -5, -4, -6, -3, -2, 2, 0, 0,
    0, -1, 2, -2, 2, 3, 1, 0,
    -4, 0, 0, 0, 0, 0, -4, 0,
    0, 0, 0, 0, -4, 0, 0, -4,
    0, 0, 0, 0, -13, 0, -7, -6,
    -3, -10, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, -3, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -2, 0, 0, 0, 0, 2, 0,
    0, 0, 0, 0, -3, 0, -2, 0,
    0, -6, 0, 0, -1, 1, 0, 0,
    0, -1, 0, 0, 0, 0, 0, 0,
    0, -5, -10, 0, 0, 0, 0, 0,
    0, -3, 0, -4, 0, 0, 0, 0,
    -6, 0, -12, 0, -14, 0, -4, -1,
    -7, -6, -7, -3, -2, 0, 0, 0,
    0, -1, 0, 0, 0, 0, 0, 0,
    0, -3, -7, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    -4, 0, -9, 0, -13, 0, -4, -3,
    -8, -6, -5, -2, -1, 0, 0, 0,
    0, -1, 0, 0, 0, 0, 0, 0,
    0, -2, 0, -3, 0, 0, 0, 0,
    0, -5, 0, -4, -4, -3, 0, 0,
    0, -1, 0, 0, -14, 0, -5, -4,
    2, -7, 0, -2, -3, 0, 0, -1,
    0, -3, 0, -2, 0, 0, 0, 0,
    0, -4, 0, -2, 0, 0, 0, 0,
    0, -5, 0, -3, -3, 0, -1, 0,
    0, -4, 0, 0, -12, 0, -6, -3,
    0, -8, 0, 0, -2, 0, 0, 0,
    0, -1, 0, -1, 0, 0, 0, 0
};


/*Collect the kern class' data in one place*/
static const lv_font_fmt_txt_kern_classes_t kern_classes =
{
    .class_pair_values   = kern_class_values,
    .left_class_mapping  = kern_left_class_mapping,
    .right_class_mapping = kern_right_class_mapping,
    .left_class_cnt      = 52,
    .right_class_cnt     = 40,
};

/*--------------------
 *  ALL CUSTOM DATA
 *--------------------*/

#if LVGL_VERSION_MAJOR == 8
/*Store all the custom data of the font*/
static  lv_font_fmt_txt_glyph_cache_t cache;
#endif

#if LVGL_VERSION_MAJOR >= 8
static const lv_font_fmt_txt_dsc_t font_dsc = {
#else
static lv_font_fmt_txt_dsc_t font_dsc = {
#endif
    .glyph_bitmap = glyph_bitmap,
    .glyph_dsc = glyph_dsc,
    .cmaps = cmaps,
    .kern_dsc = &kern_classes,
    .kern_scale = 16,
    .cmap_num = 2,
    .bpp = 1,
    .kern_classes = 1,
    .bitmap_format = 0,
#if LVGL_VERSION_MAJOR == 8
    .cache = &cache
#endif
};



/*-----------------
 *  PUBLIC FONT
 *----------------*/

/*Initialize a public general font descriptor*/
#if LVGL_VERSION_MAJOR >= 8
const lv_font_t monitor_misans_8 = {
#else
lv_font_t monitor_misans_8 = {
#endif
    .get_glyph_dsc = lv_font_get_glyph_dsc_fmt_txt,    /*Function pointer to get glyph's data*/
    .get_glyph_bitmap = lv_font_get_bitmap_fmt_txt,    /*Function pointer to get glyph's bitmap*/
    .line_height = 9,          /*The maximum line height required by the font*/
    .base_line = 2,             /*Baseline measured from the bottom of the line*/
#if !(LVGL_VERSION_MAJOR == 6 && LVGL_VERSION_MINOR == 0)
    .subpx = LV_FONT_SUBPX_NONE,
#endif
#if LV_VERSION_CHECK(7, 4, 0) || LVGL_VERSION_MAJOR >= 8
    .underline_position = -1,
    .underline_thickness = 0,
#endif
    .dsc = &font_dsc,          /*The custom font data. Will be accessed by `get_glyph_bitmap/dsc` */
#if LV_VERSION_CHECK(8, 2, 0) || LVGL_VERSION_MAJOR >= 9
    .fallback = NULL,
#endif
    .user_data = NULL,
};



#endif /*#if MONITOR_MISANS_8*/

