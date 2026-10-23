/* Generated from firmware/fonts/Ndot57-Regular.otf and MiSans CJK.
 * 1bpp LVGL resource; regenerate with firmware/assets/fonts/generate_ndot57.py.
 */
/*******************************************************************************
 * Size: 8 px
 * Bpp: 1
 * Opts: --size 8 --bpp 1 --format lvgl --no-compress --font /Users/kevinlee/Documents/ESP32-Monitor/firmware/fonts/Ndot57-Regular.otf --range 0x20-0x7e --font /Users/kevinlee/Library/Fonts/MISANS-REGULAR.OTF --range 0xb0 --symbols 晴少云多阴天小雨中大雷阵雪夹冰雹雾霾沙尘暴 --lv-font-name monitor_ndot57_8 --output monitor_ndot57_8.c
 ******************************************************************************/

#ifdef LV_LVGL_H_INCLUDE_SIMPLE
#include "lvgl.h"
#else
#include "lvgl/lvgl.h"
#endif

#ifndef MONITOR_NDOT57_8
#define MONITOR_NDOT57_8 1
#endif

#if MONITOR_NDOT57_8

/*-----------------
 *    BITMAPS
 *----------------*/

/*Store the image of the glyphs*/
static LV_ATTRIBUTE_LARGE_CONST const uint8_t glyph_bitmap[] = {
    /* U+0020 " " */
    0x0,

    /* U+0021 "!" */
    0xfc,

    /* U+0022 "\"" */
    0xfc,

    /* U+0023 "#" */
    0x55, 0xff, 0x55,

    /* U+0024 "$" */
    0x27, 0xba, 0x7b, 0x72,

    /* U+0025 "%" */
    0x62, 0x94, 0x98, 0x7f, 0x29, 0x46,

    /* U+0026 "&" */
    0x64, 0xa5, 0xf9, 0x3c,

    /* U+0027 "'" */
    0xe0,

    /* U+0028 "(" */
    0x72, 0x48, 0xc0,

    /* U+0029 ")" */
    0xc4, 0x93, 0x80,

    /* U+002A "*" */
    0x5d, 0x0,

    /* U+002B "+" */
    0x22, 0xf2, 0x20,

    /* U+002C "," */
    0x60,

    /* U+002D "-" */
    0xe0,

    /* U+002E "." */
    0xf0,

    /* U+002F "/" */
    0x1, 0x24, 0x80,

    /* U+0030 "0" */
    0x79, 0x99, 0x97,

    /* U+0031 "1" */
    0x59, 0x25, 0xc0,

    /* U+0032 "2" */
    0x79, 0x13, 0x4f,

    /* U+0033 "3" */
    0x79, 0x13, 0x97,

    /* U+0034 "4" */
    0x13, 0x5f, 0x11,

    /* U+0035 "5" */
    0xf8, 0x8f, 0x97,

    /* U+0036 "6" */
    0x34, 0x8f, 0x97,

    /* U+0037 "7" */
    0xf1, 0x16, 0x44,

    /* U+0038 "8" */
    0x79, 0x9f, 0x97,

    /* U+0039 "9" */
    0x79, 0x97, 0x16,

    /* U+003A ":" */
    0xf0,

    /* U+003B ";" */
    0x55, 0x80,

    /* U+003C "<" */
    0x2a, 0x22,

    /* U+003D "=" */
    0xf0, 0xf0,

    /* U+003E ">" */
    0x88, 0xa8,

    /* U+003F "?" */
    0x79, 0x11, 0x22,

    /* U+0040 "@" */
    0x79, 0xbb, 0x87,

    /* U+0041 "A" */
    0x25, 0x9f, 0x99,

    /* U+0042 "B" */
    0xf9, 0x9f, 0x9f,

    /* U+0043 "C" */
    0x79, 0x88, 0x97,

    /* U+0044 "D" */
    0xf9, 0x99, 0x9f,

    /* U+0045 "E" */
    0xf8, 0x8f, 0x8f,

    /* U+0046 "F" */
    0xf8, 0x8f, 0x88,

    /* U+0047 "G" */
    0x79, 0x8b, 0x97,

    /* U+0048 "H" */
    0x99, 0x9f, 0x99,

    /* U+0049 "I" */
    0xe9, 0x25, 0xc0,

    /* U+004A "J" */
    0x31, 0x11, 0x96,

    /* U+004B "K" */
    0x99, 0xae, 0x99,

    /* U+004C "L" */
    0x88, 0x88, 0x8f,

    /* U+004D "M" */
    0x9d, 0xbb, 0x99,

    /* U+004E "N" */
    0x99, 0xdb, 0x99,

    /* U+004F "O" */
    0x79, 0x99, 0x97,

    /* U+0050 "P" */
    0xf9, 0x9f, 0x88,

    /* U+0051 "Q" */
    0x79, 0x99, 0xb7,

    /* U+0052 "R" */
    0xf9, 0x9f, 0x99,

    /* U+0053 "S" */
    0x79, 0x87, 0x97,

    /* U+0054 "T" */
    0xf2, 0x22, 0x22,

    /* U+0055 "U" */
    0x99, 0x99, 0x97,

    /* U+0056 "V" */
    0x99, 0x99, 0x52,

    /* U+0057 "W" */
    0x99, 0x9b, 0xb5,

    /* U+0058 "X" */
    0x99, 0x57, 0x99,

    /* U+0059 "Y" */
    0x99, 0x52, 0x22,

    /* U+005A "Z" */
    0xf1, 0x16, 0x8f,

    /* U+005B "[" */
    0xf2, 0x49, 0xc0,

    /* U+005C "\\" */
    0x84, 0x21, 0x10,

    /* U+005D "]" */
    0xe4, 0x93, 0xc0,

    /* U+005E "^" */
    0x79,

    /* U+005F "_" */
    0xf0,

    /* U+0060 "`" */
    0x90,

    /* U+0061 "a" */
    0xe7, 0x97,

    /* U+0062 "b" */
    0x88, 0xe9, 0x9e,

    /* U+0063 "c" */
    0x78, 0x87,

    /* U+0064 "d" */
    0x11, 0x79, 0x96,

    /* U+0065 "e" */
    0x6f, 0x87,

    /* U+0066 "f" */
    0x34, 0xf4, 0x44,

    /* U+0067 "g" */
    0x79, 0x9f, 0x1e,

    /* U+0068 "h" */
    0x88, 0xad, 0x99,

    /* U+0069 "i" */
    0x43, 0x24, 0xc0,

    /* U+006A "j" */
    0x4d, 0x56,

    /* U+006B "k" */
    0x88, 0x9e, 0xa9,

    /* U+006C "l" */
    0xfc,

    /* U+006D "m" */
    0xab, 0x59, 0x65,

    /* U+006E "n" */
    0xad, 0x99,

    /* U+006F "o" */
    0x69, 0x96,

    /* U+0070 "p" */
    0xe9, 0x9f, 0x88,

    /* U+0071 "q" */
    0x69, 0x9f, 0x11,

    /* U+0072 "r" */
    0xbc, 0x88,

    /* U+0073 "s" */
    0x7e, 0x1e,

    /* U+0074 "t" */
    0x4f, 0x44, 0x30,

    /* U+0075 "u" */
    0x99, 0x97,

    /* U+0076 "v" */
    0x9b, 0xac,

    /* U+0077 "w" */
    0xbb, 0xbd,

    /* U+0078 "x" */
    0x97, 0x59,

    /* U+0079 "y" */
    0x99, 0x9f, 0x16,

    /* U+007A "z" */
    0xf7, 0x8f,

    /* U+007B "{" */
    0x34, 0x4c, 0x43,

    /* U+007C "|" */
    0xfe,

    /* U+007D "}" */
    0xc2, 0x23, 0x2c,

    /* U+007E "~" */
    0x4b, 0x10,

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
    {.bitmap_index = 0, .adv_w = 26, .box_w = 1, .box_h = 1, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 1, .adv_w = 26, .box_w = 1, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 2, .adv_w = 38, .box_w = 2, .box_h = 3, .ofs_x = 0, .ofs_y = 3},
    {.bitmap_index = 3, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 6, .adv_w = 77, .box_w = 4, .box_h = 8, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 10, .adv_w = 128, .box_w = 8, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 16, .adv_w = 90, .box_w = 5, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 20, .adv_w = 26, .box_w = 1, .box_h = 3, .ofs_x = 0, .ofs_y = 3},
    {.bitmap_index = 21, .adv_w = 51, .box_w = 3, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 24, .adv_w = 51, .box_w = 3, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 27, .adv_w = 51, .box_w = 3, .box_h = 3, .ofs_x = 0, .ofs_y = 3},
    {.bitmap_index = 29, .adv_w = 77, .box_w = 4, .box_h = 5, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 32, .adv_w = 38, .box_w = 2, .box_h = 2, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 33, .adv_w = 51, .box_w = 3, .box_h = 1, .ofs_x = 0, .ofs_y = 2},
    {.bitmap_index = 34, .adv_w = 38, .box_w = 2, .box_h = 2, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 35, .adv_w = 77, .box_w = 4, .box_h = 5, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 38, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 41, .adv_w = 51, .box_w = 3, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 44, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 47, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 50, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 53, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 56, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 59, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 62, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 65, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 68, .adv_w = 26, .box_w = 1, .box_h = 4, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 69, .adv_w = 38, .box_w = 2, .box_h = 5, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 71, .adv_w = 51, .box_w = 3, .box_h = 5, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 73, .adv_w = 77, .box_w = 4, .box_h = 3, .ofs_x = 0, .ofs_y = 1},
    {.bitmap_index = 75, .adv_w = 51, .box_w = 3, .box_h = 5, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 77, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 80, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 83, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 86, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 89, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 92, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 95, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 98, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 101, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 104, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 107, .adv_w = 51, .box_w = 3, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 110, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 113, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 116, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 119, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 122, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 125, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 128, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 131, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 134, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 137, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 140, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 143, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 146, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 149, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 152, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 155, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 158, .adv_w = 77, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 161, .adv_w = 51, .box_w = 3, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 164, .adv_w = 77, .box_w = 4, .box_h = 5, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 167, .adv_w = 51, .box_w = 3, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 170, .adv_w = 77, .box_w = 4, .box_h = 2, .ofs_x = 0, .ofs_y = 3},
    {.bitmap_index = 171, .adv_w = 77, .box_w = 4, .box_h = 1, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 172, .adv_w = 38, .box_w = 2, .box_h = 2, .ofs_x = 0, .ofs_y = 5},
    {.bitmap_index = 173, .adv_w = 64, .box_w = 4, .box_h = 4, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 175, .adv_w = 64, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 178, .adv_w = 64, .box_w = 4, .box_h = 4, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 180, .adv_w = 64, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 183, .adv_w = 64, .box_w = 4, .box_h = 4, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 185, .adv_w = 64, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 188, .adv_w = 64, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = -2},
    {.bitmap_index = 191, .adv_w = 64, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 194, .adv_w = 51, .box_w = 3, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 197, .adv_w = 38, .box_w = 2, .box_h = 8, .ofs_x = 0, .ofs_y = -2},
    {.bitmap_index = 199, .adv_w = 64, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 202, .adv_w = 26, .box_w = 1, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 203, .adv_w = 102, .box_w = 6, .box_h = 4, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 206, .adv_w = 64, .box_w = 4, .box_h = 4, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 208, .adv_w = 64, .box_w = 4, .box_h = 4, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 210, .adv_w = 64, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = -2},
    {.bitmap_index = 213, .adv_w = 64, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = -2},
    {.bitmap_index = 216, .adv_w = 64, .box_w = 4, .box_h = 4, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 218, .adv_w = 64, .box_w = 4, .box_h = 4, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 220, .adv_w = 64, .box_w = 4, .box_h = 5, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 223, .adv_w = 64, .box_w = 4, .box_h = 4, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 225, .adv_w = 64, .box_w = 4, .box_h = 4, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 227, .adv_w = 77, .box_w = 4, .box_h = 4, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 229, .adv_w = 77, .box_w = 4, .box_h = 4, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 231, .adv_w = 64, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = -2},
    {.bitmap_index = 234, .adv_w = 64, .box_w = 4, .box_h = 4, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 236, .adv_w = 64, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 239, .adv_w = 26, .box_w = 1, .box_h = 7, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 240, .adv_w = 64, .box_w = 4, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 243, .adv_w = 77, .box_w = 4, .box_h = 3, .ofs_x = 0, .ofs_y = 2},
    {.bitmap_index = 245, .adv_w = 52, .box_w = 3, .box_h = 3, .ofs_x = 1, .ofs_y = 3},
    {.bitmap_index = 247, .adv_w = 128, .box_w = 7, .box_h = 8, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 254, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 261, .adv_w = 128, .box_w = 8, .box_h = 7, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 268, .adv_w = 128, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 274, .adv_w = 128, .box_w = 7, .box_h = 8, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 281, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 288, .adv_w = 128, .box_w = 7, .box_h = 8, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 295, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 302, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 309, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 316, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 323, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 330, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 337, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 344, .adv_w = 128, .box_w = 7, .box_h = 8, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 351, .adv_w = 128, .box_w = 7, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 357, .adv_w = 128, .box_w = 6, .box_h = 7, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 363, .adv_w = 128, .box_w = 7, .box_h = 7, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 370, .adv_w = 128, .box_w = 7, .box_h = 6, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 376, .adv_w = 128, .box_w = 7, .box_h = 6, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 382, .adv_w = 128, .box_w = 7, .box_h = 6, .ofs_x = 1, .ofs_y = 0}
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
    .kern_dsc = NULL,
    .kern_scale = 0,
    .cmap_num = 2,
    .bpp = 1,
    .kern_classes = 0,
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
const lv_font_t monitor_ndot57_8 = {
#else
lv_font_t monitor_ndot57_8 = {
#endif
    .get_glyph_dsc = lv_font_get_glyph_dsc_fmt_txt,    /*Function pointer to get glyph's data*/
    .get_glyph_bitmap = lv_font_get_bitmap_fmt_txt,    /*Function pointer to get glyph's bitmap*/
    .line_height = 9,          /*The maximum line height required by the font*/
    .base_line = 2,             /*Baseline measured from the bottom of the line*/
#if !(LVGL_VERSION_MAJOR == 6 && LVGL_VERSION_MINOR == 0)
    .subpx = LV_FONT_SUBPX_NONE,
#endif
#if LV_VERSION_CHECK(7, 4, 0) || LVGL_VERSION_MAJOR >= 8
    .underline_position = 0,
    .underline_thickness = 0,
#endif
    .dsc = &font_dsc,          /*The custom font data. Will be accessed by `get_glyph_bitmap/dsc` */
#if LV_VERSION_CHECK(8, 2, 0) || LVGL_VERSION_MAJOR >= 9
    .fallback = NULL,
#endif
    .user_data = NULL,
};



#endif /*#if MONITOR_NDOT57_8*/

