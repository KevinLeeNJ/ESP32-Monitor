/* This application uses MiSans Regular.
 * Copyright 2020-2023 Beijing Xiaomi Mobile Software Co.,Ltd.
 * Font source: https://hyperos.mi.com/font/
 * Regenerate with firmware/assets/fonts/generate_misans.py.
 */
/*******************************************************************************
 * Size: 12 px
 * Bpp: 1
 * Opts: --size 12 --bpp 1 --format lvgl --no-compress --font MiSans-Regular.otf --range 0x20-0x7e,0xb0 --symbols 晴少云多阴天小雨中大雷阵雪夹冰雹雾霾沙尘暴 --lv-font-name monitor_misans_12 --output monitor_misans_12.c
 ******************************************************************************/

#ifdef LV_LVGL_H_INCLUDE_SIMPLE
#include "lvgl.h"
#else
#include "lvgl/lvgl.h"
#endif

#ifndef MONITOR_MISANS_12
#define MONITOR_MISANS_12 1
#endif

#if MONITOR_MISANS_12

/*-----------------
 *    BITMAPS
 *----------------*/

/*Store the image of the glyphs*/
static LV_ATTRIBUTE_LARGE_CONST const uint8_t glyph_bitmap[] = {
    /* U+0020 " " */
    0x0,

    /* U+0021 "!" */
    0xfc, 0x40,

    /* U+0022 "\"" */
    0xb6, 0x80,

    /* U+0023 "#" */
    0x12, 0x12, 0x12, 0x7f, 0x24, 0x24, 0xfe, 0x24,
    0x28, 0x48,

    /* U+0024 "$" */
    0x10, 0x73, 0x5c, 0x89, 0xe, 0x7, 0x9, 0x13,
    0xad, 0xf0, 0x80,

    /* U+0025 "%" */
    0x61, 0xa4, 0xc9, 0x22, 0x50, 0x64, 0x2, 0x1,
    0x38, 0x51, 0x24, 0x50, 0xe0,

    /* U+0026 "&" */
    0x38, 0x44, 0x44, 0x4c, 0x38, 0x50, 0x8a, 0x86,
    0xce, 0x7b,

    /* U+0027 "'" */
    0xe0,

    /* U+0028 "(" */
    0x56, 0xaa, 0xa5,

    /* U+0029 ")" */
    0xa9, 0x55, 0x5a,

    /* U+002A "*" */
    0x5f, 0xa0,

    /* U+002B "+" */
    0x21, 0x9, 0xf2, 0x10,

    /* U+002C "," */
    0xe0,

    /* U+002D "-" */
    0xf0,

    /* U+002E "." */
    0x80,

    /* U+002F "/" */
    0x11, 0x12, 0x22, 0x44, 0x44,

    /* U+0030 "0" */
    0x71, 0x28, 0x61, 0x86, 0x18, 0x61, 0x49, 0xe0,

    /* U+0031 "1" */
    0x7c, 0x92, 0x49, 0x24,

    /* U+0032 "2" */
    0x74, 0x42, 0x11, 0x88, 0x88, 0xc7, 0xc0,

    /* U+0033 "3" */
    0xf8, 0x21, 0x8, 0x78, 0x30, 0x41, 0x8d, 0xe0,

    /* U+0034 "4" */
    0x18, 0x62, 0x8a, 0x49, 0x28, 0xbf, 0x8, 0x20,

    /* U+0035 "5" */
    0x7d, 0x4, 0x10, 0x79, 0x30, 0x41, 0x4c, 0xe0,

    /* U+0036 "6" */
    0x10, 0x82, 0x10, 0x7b, 0x38, 0x61, 0xcd, 0xe0,

    /* U+0037 "7" */
    0xf8, 0x44, 0x21, 0x10, 0x84, 0x42, 0x0,

    /* U+0038 "8" */
    0x7b, 0x38, 0x73, 0x7b, 0x38, 0x61, 0xcd, 0xe0,

    /* U+0039 "9" */
    0x7b, 0x38, 0x61, 0xcd, 0xe0, 0x84, 0x10, 0x80,

    /* U+003A ":" */
    0x82,

    /* U+003B ";" */
    0x83, 0x80,

    /* U+003C "<" */
    0xc, 0xcc, 0x30, 0x60, 0x60, 0x40,

    /* U+003D "=" */
    0xf8, 0x1, 0xf0,

    /* U+003E ">" */
    0xc1, 0x81, 0x83, 0x33, 0x8, 0x0,

    /* U+003F "?" */
    0x74, 0x42, 0x11, 0x10, 0x80, 0x1, 0x0,

    /* U+0040 "@" */
    0x1e, 0x30, 0x90, 0x73, 0xda, 0x2d, 0x16, 0x75,
    0x80, 0x60, 0xf, 0x0,

    /* U+0041 "A" */
    0x18, 0x18, 0x18, 0x3c, 0x24, 0x24, 0x7e, 0x42,
    0x42, 0x81,

    /* U+0042 "B" */
    0xfa, 0x18, 0x61, 0x8f, 0xe8, 0x61, 0x87, 0xe0,

    /* U+0043 "C" */
    0x3c, 0x8f, 0x4, 0x8, 0x10, 0x20, 0x60, 0x46,
    0x78,

    /* U+0044 "D" */
    0xf9, 0xa, 0x1c, 0x18, 0x30, 0x60, 0xc3, 0x85,
    0xf0,

    /* U+0045 "E" */
    0xfa, 0x8, 0x20, 0x83, 0xe8, 0x20, 0x83, 0xf0,

    /* U+0046 "F" */
    0xfc, 0x21, 0x8, 0x7e, 0x10, 0x84, 0x0,

    /* U+0047 "G" */
    0x3c, 0x8f, 0x4, 0x8, 0x11, 0xe0, 0xc1, 0x46,
    0x78,

    /* U+0048 "H" */
    0x83, 0x6, 0xc, 0x18, 0x3f, 0xe0, 0xc1, 0x83,
    0x4,

    /* U+0049 "I" */
    0xff, 0xc0,

    /* U+004A "J" */
    0x8, 0x42, 0x10, 0x84, 0x21, 0xcb, 0x80,

    /* U+004B "K" */
    0x85, 0x12, 0x45, 0xe, 0x1a, 0x24, 0x44, 0x8d,
    0x8,

    /* U+004C "L" */
    0x82, 0x8, 0x20, 0x82, 0x8, 0x20, 0x83, 0xf0,

    /* U+004D "M" */
    0x80, 0xe0, 0xf8, 0x74, 0x5b, 0x6c, 0xa6, 0x33,
    0x11, 0x80, 0xc0, 0x40,

    /* U+004E "N" */
    0x83, 0x87, 0x8d, 0x19, 0x32, 0x62, 0xc7, 0x87,
    0x4,

    /* U+004F "O" */
    0x3c, 0x42, 0xc3, 0x81, 0x81, 0x81, 0x81, 0xc3,
    0x42, 0x3c,

    /* U+0050 "P" */
    0xfa, 0x38, 0x61, 0x8f, 0xe8, 0x20, 0x82, 0x0,

    /* U+0051 "Q" */
    0x3c, 0x42, 0xc3, 0x81, 0x81, 0x81, 0x81, 0xcf,
    0x46, 0x3e, 0x1,

    /* U+0052 "R" */
    0xfa, 0x38, 0x61, 0x8f, 0xe9, 0x22, 0x8e, 0x10,

    /* U+0053 "S" */
    0x7b, 0x38, 0x20, 0x60, 0x60, 0x41, 0x8d, 0xe0,

    /* U+0054 "T" */
    0xfe, 0x20, 0x40, 0x81, 0x2, 0x4, 0x8, 0x10,
    0x20,

    /* U+0055 "U" */
    0x83, 0x6, 0xc, 0x18, 0x30, 0x60, 0xc1, 0x44,
    0x70,

    /* U+0056 "V" */
    0x81, 0x42, 0x42, 0x42, 0x24, 0x24, 0x24, 0x18,
    0x18, 0x18,

    /* U+0057 "W" */
    0x84, 0x28, 0xc5, 0x18, 0xa5, 0x24, 0xa4, 0x52,
    0x8a, 0x51, 0x8c, 0x31, 0x86, 0x10,

    /* U+0058 "X" */
    0x42, 0xc8, 0x91, 0xc1, 0x83, 0xa, 0x12, 0x47,
    0x84,

    /* U+0059 "Y" */
    0x82, 0x89, 0x11, 0x42, 0x82, 0x4, 0x8, 0x10,
    0x20,

    /* U+005A "Z" */
    0xfc, 0x30, 0x84, 0x10, 0x82, 0x10, 0xc3, 0xf0,

    /* U+005B "[" */
    0xf2, 0x49, 0x24, 0x92, 0x70,

    /* U+005C "\\" */
    0x84, 0x44, 0x42, 0x22, 0x31,

    /* U+005D "]" */
    0xe4, 0x92, 0x49, 0x24, 0xf0,

    /* U+005E "^" */
    0x21, 0x94, 0x90,

    /* U+005F "_" */
    0xfc,

    /* U+0060 "`" */
    0x10,

    /* U+0061 "a" */
    0x70, 0x42, 0xf8, 0xcd, 0xa0,

    /* U+0062 "b" */
    0x82, 0x8, 0x2e, 0xce, 0x18, 0x61, 0xce, 0xe0,

    /* U+0063 "c" */
    0x76, 0x61, 0x8, 0x65, 0xc0,

    /* U+0064 "d" */
    0x4, 0x10, 0x5d, 0xce, 0x18, 0x61, 0xcd, 0xd0,

    /* U+0065 "e" */
    0x7b, 0x38, 0x7f, 0x83, 0x17, 0x80,

    /* U+0066 "f" */
    0x34, 0x4f, 0x44, 0x44, 0x44,

    /* U+0067 "g" */
    0x77, 0x38, 0x61, 0x87, 0x37, 0x41, 0xcd, 0xe0,

    /* U+0068 "h" */
    0x84, 0x21, 0x6c, 0xc6, 0x31, 0x8c, 0x40,

    /* U+0069 "i" */
    0x9f, 0xc0,

    /* U+006A "j" */
    0x41, 0x55, 0x55, 0xc0,

    /* U+006B "k" */
    0x84, 0x21, 0x3b, 0x73, 0x96, 0x94, 0x40,

    /* U+006C "l" */
    0xaa, 0xaa, 0xb0,

    /* U+006D "m" */
    0xb7, 0x66, 0x62, 0x31, 0x18, 0x8c, 0x46, 0x22,

    /* U+006E "n" */
    0xb6, 0x63, 0x18, 0xc6, 0x20,

    /* U+006F "o" */
    0x7b, 0x38, 0x61, 0x87, 0x37, 0x80,

    /* U+0070 "p" */
    0xbb, 0x38, 0x61, 0x87, 0x3b, 0xa0, 0x82, 0x0,

    /* U+0071 "q" */
    0x77, 0x38, 0x61, 0x87, 0x37, 0x41, 0x4, 0x10,

    /* U+0072 "r" */
    0xbc, 0x88, 0x88, 0x80,

    /* U+0073 "s" */
    0x74, 0x60, 0xe0, 0xc5, 0xc0,

    /* U+0074 "t" */
    0x44, 0x4f, 0x44, 0x44, 0x43,

    /* U+0075 "u" */
    0x8c, 0x63, 0x18, 0xcd, 0xa0,

    /* U+0076 "v" */
    0x85, 0x34, 0x92, 0x30, 0xc3, 0x0,

    /* U+0077 "w" */
    0x88, 0xa6, 0x55, 0x2a, 0xa5, 0x31, 0x98, 0x8c,

    /* U+0078 "x" */
    0x49, 0x23, 0xc, 0x31, 0x2c, 0xc0,

    /* U+0079 "y" */
    0x85, 0x14, 0x92, 0x28, 0xc3, 0x8, 0x20, 0x80,

    /* U+007A "z" */
    0xf8, 0xc4, 0x44, 0x63, 0xe0,

    /* U+007B "{" */
    0x12, 0x22, 0x22, 0xc2, 0x22, 0x21,

    /* U+007C "|" */
    0xff, 0xf0,

    /* U+007D "}" */
    0x84, 0x44, 0x44, 0x34, 0x44, 0x48,

    /* U+007E "~" */
    0xc9, 0x80,

    /* U+00B0 "°" */
    0x69, 0x96,

    /* U+4E2D "中" */
    0x8, 0x4, 0x3f, 0xf1, 0x18, 0x8c, 0x47, 0xfe,
    0x10, 0x8, 0x4, 0x2, 0x0,

    /* U+4E91 "云" */
    0x7f, 0x80, 0x0, 0x0, 0x0, 0xff, 0xc6, 0x1,
    0x20, 0x84, 0x61, 0x1f, 0xe0, 0x4,

    /* U+51B0 "冰" */
    0x1, 0x8, 0x21, 0x4, 0xae, 0xa0, 0x58, 0xb,
    0x12, 0x62, 0x4a, 0x51, 0x34, 0x24, 0x38, 0x0,

    /* U+591A "多" */
    0x8, 0x7, 0xe6, 0x18, 0x58, 0x7c, 0x23, 0xf3,
    0xb, 0x24, 0x6, 0x1e, 0x8, 0x0,

    /* U+5927 "大" */
    0x8, 0x2, 0x0, 0x83, 0xff, 0xc, 0x3, 0x0,
    0xc0, 0x48, 0x21, 0x10, 0x28, 0x4,

    /* U+5929 "天" */
    0x7f, 0x82, 0x0, 0x80, 0x20, 0xff, 0xc3, 0x0,
    0xc0, 0x48, 0x33, 0x18, 0x38, 0x4,

    /* U+5939 "夹" */
    0x8, 0x3f, 0xf0, 0x81, 0x26, 0x29, 0x2, 0xf,
    0xfc, 0x30, 0x1e, 0xc, 0xcc, 0xc,

    /* U+5C0F "小" */
    0x4, 0x0, 0x80, 0x94, 0x12, 0x42, 0x48, 0x89,
    0x91, 0x12, 0x22, 0x84, 0x20, 0x80, 0x70, 0x0,

    /* U+5C11 "少" */
    0x4, 0x0, 0x80, 0x56, 0x12, 0x42, 0x44, 0x88,
    0x71, 0x20, 0x8, 0x6, 0x3, 0x3, 0x80, 0x0,

    /* U+5C18 "尘" */
    0x8, 0xa, 0xc6, 0x93, 0x22, 0x80, 0x40, 0x7,
    0xf8, 0x20, 0x8, 0x2, 0xf, 0xfc,

    /* U+6674 "晴" */
    0x2, 0x1d, 0xfa, 0xbf, 0x51, 0xe, 0xdd, 0x5f,
    0xaa, 0x15, 0x7e, 0xef, 0xd1, 0x8, 0x27, 0x0,

    /* U+66B4 "暴" */
    0x7f, 0x90, 0x27, 0xf9, 0xfe, 0x12, 0x1f, 0xef,
    0xfc, 0xa4, 0xda, 0xde, 0xe1, 0x80,

    /* U+6C99 "沙" */
    0x2, 0xc, 0x50, 0xaa, 0x45, 0x25, 0x24, 0x24,
    0x0, 0xa2, 0x8, 0x42, 0x11, 0x82, 0xc0, 0x0,

    /* U+9634 "阴" */
    0xf7, 0xcc, 0x6a, 0x34, 0xfa, 0x8c, 0xc6, 0x5f,
    0xf1, 0x88, 0xc4, 0x64, 0xe0,

    /* U+9635 "阵" */
    0x2, 0x38, 0x8a, 0xfe, 0x90, 0xc5, 0x32, 0x4a,
    0xfe, 0x84, 0xa1, 0x3b, 0xf8, 0x12, 0x4,

    /* U+96E8 "雨" */
    0xff, 0xc2, 0x0, 0x81, 0xfe, 0x88, 0xaa, 0xa9,
    0x9a, 0xaa, 0x99, 0xa2, 0x28, 0x98,

    /* U+96EA "雪" */
    0xff, 0x82, 0xf, 0xff, 0xad, 0x8, 0xa, 0x8f,
    0xf8, 0x2, 0x7f, 0x80, 0x2f, 0xf8,

    /* U+96F7 "雷" */
    0x7f, 0x81, 0xf, 0xfe, 0xdd, 0x37, 0x0, 0x7,
    0xf9, 0x12, 0x7f, 0x9f, 0xe4, 0x8,

    /* U+96F9 "雹" */
    0x7f, 0x81, 0xf, 0xfe, 0xdd, 0x20, 0x1f, 0xec,
    0x9, 0xfa, 0x7f, 0xd0, 0x13, 0xfc,

    /* U+96FE "雾" */
    0x7f, 0xe0, 0x40, 0x7f, 0xe5, 0x5a, 0x10, 0x3,
    0xfc, 0x4b, 0x7, 0xca, 0x3f, 0xc1, 0x4, 0x67,
    0x80,

    /* U+973E "霾" */
    0x7f, 0xe7, 0xfe, 0x5d, 0xa1, 0x98, 0x73, 0xe3,
    0xbe, 0x75, 0x26, 0xbc, 0x1f, 0xe6, 0x90, 0x3f,
    0xf0
};


/*---------------------
 *  GLYPH DESCRIPTION
 *--------------------*/

static const lv_font_fmt_txt_glyph_dsc_t glyph_dsc[] = {
    {.bitmap_index = 0, .adv_w = 0, .box_w = 0, .box_h = 0, .ofs_x = 0, .ofs_y = 0} /* id = 0 reserved */,
    {.bitmap_index = 0, .adv_w = 56, .box_w = 1, .box_h = 1, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 1, .adv_w = 51, .box_w = 1, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 3, .adv_w = 65, .box_w = 3, .box_h = 3, .ofs_x = 1, .ofs_y = 8},
    {.bitmap_index = 5, .adv_w = 136, .box_w = 8, .box_h = 10, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 15, .adv_w = 111, .box_w = 7, .box_h = 12, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 26, .adv_w = 157, .box_w = 10, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 39, .adv_w = 137, .box_w = 8, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 49, .adv_w = 36, .box_w = 1, .box_h = 3, .ofs_x = 1, .ofs_y = 8},
    {.bitmap_index = 50, .adv_w = 59, .box_w = 2, .box_h = 12, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 53, .adv_w = 59, .box_w = 2, .box_h = 12, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 56, .adv_w = 86, .box_w = 3, .box_h = 4, .ofs_x = 1, .ofs_y = 6},
    {.bitmap_index = 58, .adv_w = 118, .box_w = 5, .box_h = 6, .ofs_x = 1, .ofs_y = 2},
    {.bitmap_index = 62, .adv_w = 50, .box_w = 1, .box_h = 3, .ofs_x = 1, .ofs_y = -2},
    {.bitmap_index = 63, .adv_w = 87, .box_w = 4, .box_h = 1, .ofs_x = 1, .ofs_y = 5},
    {.bitmap_index = 64, .adv_w = 48, .box_w = 1, .box_h = 1, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 65, .adv_w = 77, .box_w = 4, .box_h = 10, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 70, .adv_w = 122, .box_w = 6, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 78, .adv_w = 77, .box_w = 3, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 82, .adv_w = 109, .box_w = 5, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 89, .adv_w = 114, .box_w = 6, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 97, .adv_w = 113, .box_w = 6, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 105, .adv_w = 116, .box_w = 6, .box_h = 10, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 113, .adv_w = 116, .box_w = 6, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 121, .adv_w = 101, .box_w = 5, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 128, .adv_w = 121, .box_w = 6, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 136, .adv_w = 115, .box_w = 6, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 144, .adv_w = 48, .box_w = 1, .box_h = 7, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 145, .adv_w = 50, .box_w = 1, .box_h = 9, .ofs_x = 1, .ofs_y = -2},
    {.bitmap_index = 147, .adv_w = 118, .box_w = 6, .box_h = 7, .ofs_x = 1, .ofs_y = 1},
    {.bitmap_index = 153, .adv_w = 118, .box_w = 5, .box_h = 4, .ofs_x = 1, .ofs_y = 3},
    {.bitmap_index = 156, .adv_w = 118, .box_w = 6, .box_h = 7, .ofs_x = 1, .ofs_y = 1},
    {.bitmap_index = 162, .adv_w = 94, .box_w = 5, .box_h = 10, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 169, .adv_w = 158, .box_w = 9, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 181, .adv_w = 128, .box_w = 8, .box_h = 10, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 191, .adv_w = 128, .box_w = 6, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 199, .adv_w = 135, .box_w = 7, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 208, .adv_w = 143, .box_w = 7, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 217, .adv_w = 115, .box_w = 6, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 225, .adv_w = 110, .box_w = 5, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 232, .adv_w = 142, .box_w = 7, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 241, .adv_w = 138, .box_w = 7, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 250, .adv_w = 47, .box_w = 1, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 252, .adv_w = 93, .box_w = 5, .box_h = 10, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 259, .adv_w = 125, .box_w = 7, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 268, .adv_w = 108, .box_w = 6, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 276, .adv_w = 169, .box_w = 9, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 288, .adv_w = 141, .box_w = 7, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 297, .adv_w = 151, .box_w = 8, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 307, .adv_w = 120, .box_w = 6, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 315, .adv_w = 151, .box_w = 8, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 326, .adv_w = 122, .box_w = 6, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 334, .adv_w = 116, .box_w = 6, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 342, .adv_w = 116, .box_w = 7, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 351, .adv_w = 135, .box_w = 7, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 360, .adv_w = 128, .box_w = 8, .box_h = 10, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 370, .adv_w = 184, .box_w = 11, .box_h = 10, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 384, .adv_w = 122, .box_w = 7, .box_h = 10, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 393, .adv_w = 120, .box_w = 7, .box_h = 10, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 402, .adv_w = 114, .box_w = 6, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 410, .adv_w = 67, .box_w = 3, .box_h = 12, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 415, .adv_w = 65, .box_w = 4, .box_h = 10, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 420, .adv_w = 67, .box_w = 3, .box_h = 12, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 425, .adv_w = 85, .box_w = 5, .box_h = 4, .ofs_x = 0, .ofs_y = 7},
    {.bitmap_index = 428, .adv_w = 88, .box_w = 6, .box_h = 1, .ofs_x = 0, .ofs_y = -2},
    {.bitmap_index = 429, .adv_w = 50, .box_w = 2, .box_h = 3, .ofs_x = 0, .ofs_y = 7},
    {.bitmap_index = 430, .adv_w = 104, .box_w = 5, .box_h = 7, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 435, .adv_w = 115, .box_w = 6, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 443, .adv_w = 102, .box_w = 5, .box_h = 7, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 448, .adv_w = 115, .box_w = 6, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 456, .adv_w = 107, .box_w = 6, .box_h = 7, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 462, .adv_w = 67, .box_w = 4, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 467, .adv_w = 115, .box_w = 6, .box_h = 10, .ofs_x = 1, .ofs_y = -3},
    {.bitmap_index = 475, .adv_w = 111, .box_w = 5, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 482, .adv_w = 48, .box_w = 1, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 484, .adv_w = 48, .box_w = 2, .box_h = 13, .ofs_x = 0, .ofs_y = -3},
    {.bitmap_index = 488, .adv_w = 97, .box_w = 5, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 495, .adv_w = 48, .box_w = 2, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 498, .adv_w = 169, .box_w = 9, .box_h = 7, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 506, .adv_w = 111, .box_w = 5, .box_h = 7, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 511, .adv_w = 111, .box_w = 6, .box_h = 7, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 517, .adv_w = 115, .box_w = 6, .box_h = 10, .ofs_x = 1, .ofs_y = -3},
    {.bitmap_index = 525, .adv_w = 115, .box_w = 6, .box_h = 10, .ofs_x = 1, .ofs_y = -3},
    {.bitmap_index = 533, .adv_w = 72, .box_w = 4, .box_h = 7, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 537, .adv_w = 91, .box_w = 5, .box_h = 7, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 542, .adv_w = 69, .box_w = 4, .box_h = 10, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 547, .adv_w = 111, .box_w = 5, .box_h = 7, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 552, .adv_w = 98, .box_w = 6, .box_h = 7, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 558, .adv_w = 153, .box_w = 9, .box_h = 7, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 566, .adv_w = 95, .box_w = 6, .box_h = 7, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 572, .adv_w = 99, .box_w = 6, .box_h = 10, .ofs_x = 0, .ofs_y = -3},
    {.bitmap_index = 580, .adv_w = 92, .box_w = 5, .box_h = 7, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 585, .adv_w = 67, .box_w = 4, .box_h = 12, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 591, .adv_w = 44, .box_w = 1, .box_h = 12, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 593, .adv_w = 67, .box_w = 4, .box_h = 12, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 599, .adv_w = 100, .box_w = 5, .box_h = 2, .ofs_x = 1, .ofs_y = 4},
    {.bitmap_index = 601, .adv_w = 79, .box_w = 4, .box_h = 4, .ofs_x = 1, .ofs_y = 5},
    {.bitmap_index = 603, .adv_w = 192, .box_w = 9, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 616, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 630, .adv_w = 192, .box_w = 11, .box_h = 11, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 646, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 660, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 674, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 688, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 702, .adv_w = 192, .box_w = 11, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 718, .adv_w = 192, .box_w = 11, .box_h = 11, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 734, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 748, .adv_w = 192, .box_w = 11, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 764, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 778, .adv_w = 192, .box_w = 11, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 794, .adv_w = 192, .box_w = 9, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 807, .adv_w = 192, .box_w = 10, .box_h = 12, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 822, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 836, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 850, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 864, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 878, .adv_w = 192, .box_w = 12, .box_h = 11, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 895, .adv_w = 192, .box_w = 12, .box_h = 11, .ofs_x = 0, .ofs_y = -1}
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
    0, -17, 0, -12, 4, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -2, -6, -4, -18, -6, -18, -12,
    0, -20, 0, -3, -2, 0, 0, -5,
    0, 0, -6, 0, -7, -6, 0, 0,
    0, 0, 0, 0, 0, -7, -17, 0,
    0, -9, -4, -4, -6, -2, -20, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, -6,
    0, 0, 0, 0, -9, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -13, 0, -10, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, -7, 0, 0, 0, 0, -6,
    -3, 0, 0, 0, -4, 0, 0, 0,
    -5, 0, -5, 0, -7, 0, -5, -3,
    0, -10, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, -2, 0, -3, 0, 0,
    -5, -6, 0, -4, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, -2, 0,
    0, -4, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, -4, 0, 0, 0, 0, -4,
    0, 0, 0, 0, 0, 0, -2, -5,
    0, 0, 0, 0, 0, 0, 0, 0,
    -2, -4, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    -2, 0, 0, 0, 0, 0, -5, 0,
    -4, 0, 0, 0, -5, 0, -4, -6,
    0, 0, 0, 0, -5, 0, -8, 0,
    0, -8, 2, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, -4, 0, 0, 0, -5, 0,
    0, 0, 0, 0, -4, 0, 0, -8,
    0, 0, 0, 0, 0, 0, -5, 0,
    -5, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    -13, 0, -4, 0, 0, 0, -12, 0,
    0, 0, 0, 0, -11, 0, -2, -12,
    0, 0, 0, 0, -19, 0, -15, 0,
    -3, -20, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -7, 0, -5, -4,
    4, 0, -27, -5, -17, -4, 0, 0,
    0, -14, 0, -14, 1, -6, 0, 2,
    -18, 0, -21, 0, 0, 0, 0, 0,
    0, 0, 0, -17, -16, -5, 0, 0,
    -9, -11, 0, -8, 0, 0, -8, -8,
    0, 0, -2, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -4, 0, -4, -4,
    0, 0, 0, 0, -5, 0, -3, 0,
    0, -8, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, -4, 0,
    0, 0, -21, 0, -10, 0, 0, 0,
    0, -6, 0, -5, 0, -4, 0, 0,
    -14, 0, -16, 0, 0, 0, -2, 0,
    -8, -4, 0, -8, -6, 2, 0, 0,
    0, 0, 1, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -10, -4, -6, 2, -4, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    -17, -2, 0, -8, 0, -5, -11, 0,
    0, 0, 0, 0, 0, 0, -13, -15,
    0, -7, 0, 0, -18, -3, -19, -13,
    0, -19, 0, 0, -3, -2, 0, -1,
    0, 0, -5, -2, -9, -7, 0, 0,
    -6, 0, -3, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, -4,
    -4, 0, -2, 0, -5, 0, -8, -4,
    -4, -7, -2, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -2, 0, -1, -2,
    0, 0, 0, -8, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -4, 0, 0, -1, 0, 0, 0,
    -4, -6, -2, -2, -4, 0, 0, 0,
    0, 0, 0, 0, -4, -4, -2, 0,
    -4, 0, -8, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, -5,
    -7, 0, -12, 0, -6, 0, -7, -3,
    -8, -10, -7, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, -2,
    0, -4, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -3, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -4, -2, 0, 0,
    4, -5, -13, 0, 0, 0, 0, 0,
    0, -5, 0, -6, 0, 0, 0, 0,
    -12, 0, -30, -2, 0, 0, 0, 0,
    -4, 0, 0, -10, -7, 0, 0, 0,
    -4, -3, 0, -4, -2, -2, -8, -4,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, -5,
    -4, 0, 0, 0, -6, 0, -8, -3,
    -3, -9, -2, 0, 0, 0, 0, 0,
    0, -2, 0, 0, -5, -6, -4, 0,
    0, 0, -3, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    -3, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -7, 4, -17, 0, -9, -6, 0,
    0, -5, 0, 0, 0, 0, -9, -7,
    0, -11, -5, -4, 0, -10, 0, 0,
    0, 0, 0, -5, -10, -6, 0, -4,
    -2, -2, -9, -10, -13, -12, 4, 0,
    -17, 0, 5, -17, 0, 0, -13, 0,
    0, 0, 0, 0, -5, 0, -22, -17,
    0, -8, 0, 0, -30, -5, -26, -16,
    0, -33, 0, 0, -4, -4, 0, 0,
    0, 0, -7, -4, -15, -10, 0, 0,
    0, -8, -19, 0, 0, 0, 0, -2,
    0, -5, 0, 0, 0, 0, 0, 0,
    -15, 0, -34, 0, -4, 0, -5, -4,
    -5, -6, -4, -7, -5, 4, 0, 0,
    0, 0, 4, 0, 6, 6, 1, 2,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    -5, 0, 0, 0, -6, 0, -4, -4,
    -8, -10, -2, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -3, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -1, -6, 0, -4, 0, -3, -2,
    0, -4, 0, -3, -4, 0, 0, 0,
    0, 0, 0, -4, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -2, 0, -2, 1,
    -2, -5, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -4, -4, -2, -3,
    4, -13, -21, -15, 0, -7, 0, 0,
    0, -19, 0, -15, 0, -5, 0, 0,
    -18, -6, -26, -3, 0, 0, -4, 0,
    -6, 0, 0, -23, -23, -8, 0, 0,
    -17, -21, -5, -16, -21, -19, -21, -18,
    0, 0, -7, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    -3, 0, -7, 0, 0, 0, 0, 0,
    -3, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -13, -25, -8, 0, -5, 0, 0,
    0, -12, -4, -10, 0, -3, 0, 0,
    -19, -7, -29, 0, 0, 0, 0, 0,
    0, 0, 0, -17, -14, -3, 0, 0,
    -9, -12, -2, -8, -6, -6, -8, -6,
    0, -7, -16, -5, 0, -3, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    -13, -3, -20, 2, 0, 0, 0, 0,
    0, 0, 0, -12, -10, -2, 0, 0,
    -7, -9, 0, -8, -2, -4, -6, -4,
    0, -6, 2, -15, 0, 0, -6, 0,
    -2, -3, -5, -4, 0, 0, -9, -4,
    0, -8, -1, -2, -6, -3, 0, 0,
    0, 0, 0, -3, -6, -4, 0, -1,
    0, -6, -7, -9, -10, -12, 0, 0,
    0, -15, -22, -13, 0, -10, 0, -4,
    -2, -20, 0, -17, 0, -8, -3, 0,
    -19, -10, -33, -4, 0, 0, 0, 0,
    0, 0, 0, -21, -21, -7, 0, 0,
    -15, -17, -6, -16, -8, -9, -10, -12,
    0, -8, 0, -10, 0, 0, 0, 0,
    0, -9, 0, 0, 0, 0, 0, 0,
    0, -10, 0, 0, 0, -3, 0, 0,
    0, 0, 0, 0, -3, 0, 0, 0,
    0, 0, -6, -6, -8, -8, 0, 0,
    -8, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -10, 0, -3, -10,
    0, 0, 0, 0, -21, 0, -14, -12,
    0, -18, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -2, -2, 0, 0,
    -8, 0, -4, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -10, 0, -5, -14,
    -3, 0, 0, 0, -23, 0, -14, -10,
    -6, -21, -2, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -2, -2, -4, -1,
    -2, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, -6,
    0, 0, 0, 0, -21, 0, -11, -7,
    -3, -21, 0, -2, -2, 0, -2, -1,
    0, 0, 0, 0, -2, 0, -4, 0,
    0, 0, -2, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -10, 0, 0, -10,
    0, 0, 0, 0, -20, 0, -12, -8,
    -7, -16, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -2, -1, -3, 0,
    6, -6, -13, 0, 0, 0, 0, 0,
    0, -8, 0, 0, 4, 0, 4, 0,
    -11, 0, -14, 0, 0, 0, 0, 0,
    0, 0, 0, -3, -3, 0, 0, 0,
    0, -3, 2, -2, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -6, 0, 0, -6,
    0, 0, 0, 0, -17, 0, -9, -7,
    -2, -15, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    -6, 0, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -8, 0, -3, -9,
    0, 0, 0, 0, -23, 0, -16, -12,
    -3, -20, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, -2, -1, -2, 0,
    -4, -3, 2, -5, 0, 0, 0, 0,
    0, -6, 0, -4, 0, -5, -2, -4,
    0, -4, 0, 0, -19, -2, -9, -4,
    0, -15, 0, -2, -5, 0, 0, -1,
    0, -2, -2, -3, 0, 0, 0, 0,
    0, -3, 4, -3, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, -4,
    0, -2, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 2, 0, 0, 0, 0,
    2, 0, 0, 1, -2, -1, 0, 0,
    0, -9, -21, 0, 0, 0, 0, -4,
    -10, -10, 0, -5, 0, 0, 2, 0,
    0, 0, 0, 0, -17, 0, 0, 0,
    -7, -6, -10, -4, -2, 2, 0, 0,
    0, -2, 3, -3, 4, 4, 2, 0,
    -6, 0, 0, 0, 0, 0, -6, 0,
    0, 0, 0, 0, -6, 0, 0, -6,
    0, 0, 0, 0, -20, 0, -11, -8,
    -5, -15, 0, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, -4, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    0, -4, 0, 0, 0, 0, 3, 0,
    0, 0, 0, 0, -4, 0, -3, 0,
    0, -9, 0, 0, -1, 2, 0, 0,
    0, -1, 0, -1, 0, 0, 0, 0,
    0, -8, -15, 0, 0, 0, 0, 0,
    0, -5, 0, -6, 0, 0, 0, 0,
    -9, 0, -17, 0, -21, 0, -6, -2,
    -10, -8, -10, -5, -2, 0, 0, 0,
    0, -2, 0, 0, 0, 0, 0, 0,
    0, -4, -10, 0, 0, 0, 0, 0,
    0, 0, 0, 0, 0, 0, 0, 0,
    -7, 0, -14, 0, -19, 0, -6, -4,
    -12, -9, -8, -3, -2, 0, 0, 0,
    0, -1, 0, 0, 0, 0, 0, 0,
    0, -2, 0, -4, 0, 0, 0, 0,
    0, -7, 0, -5, -6, -4, 0, 0,
    0, -2, 0, 0, -21, 0, -8, -6,
    2, -10, 0, -3, -4, 0, 0, -2,
    0, -4, 0, -2, 0, 0, 0, 0,
    0, -6, 0, -3, 0, 0, 0, 0,
    0, -8, 0, -4, -4, 0, -1, 0,
    0, -6, 0, 0, -17, 0, -8, -5,
    0, -12, 0, -1, -3, 0, 0, 0,
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
const lv_font_t monitor_misans_12 = {
#else
lv_font_t monitor_misans_12 = {
#endif
    .get_glyph_dsc = lv_font_get_glyph_dsc_fmt_txt,    /*Function pointer to get glyph's data*/
    .get_glyph_bitmap = lv_font_get_bitmap_fmt_txt,    /*Function pointer to get glyph's bitmap*/
    .line_height = 14,          /*The maximum line height required by the font*/
    .base_line = 3,             /*Baseline measured from the bottom of the line*/
#if !(LVGL_VERSION_MAJOR == 6 && LVGL_VERSION_MINOR == 0)
    .subpx = LV_FONT_SUBPX_NONE,
#endif
#if LV_VERSION_CHECK(7, 4, 0) || LVGL_VERSION_MAJOR >= 8
    .underline_position = -1,
    .underline_thickness = 1,
#endif
    .dsc = &font_dsc,          /*The custom font data. Will be accessed by `get_glyph_bitmap/dsc` */
#if LV_VERSION_CHECK(8, 2, 0) || LVGL_VERSION_MAJOR >= 9
    .fallback = NULL,
#endif
    .user_data = NULL,
};



#endif /*#if MONITOR_MISANS_12*/

