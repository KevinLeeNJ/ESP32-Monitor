/* Generated from firmware/fonts/Ndot57-Regular.otf and MiSans CJK.
 * 1bpp LVGL resource; regenerate with firmware/assets/fonts/generate_ndot57.py.
 */
/*******************************************************************************
 * Size: 12 px
 * Bpp: 1
 * Opts: --size 12 --bpp 1 --format lvgl --no-compress --font /Users/kevinlee/Documents/ESP32-Monitor/firmware/fonts/Ndot57-Regular.otf --range 0x20-0x7e --font /Users/kevinlee/Library/Fonts/MISANS-REGULAR.OTF --range 0xb0 --symbols 晴少云多阴天小雨中大雷阵雪夹冰雹雾霾沙尘暴 --lv-font-name monitor_ndot57_12 --output monitor_ndot57_12.c
 ******************************************************************************/

#ifdef LV_LVGL_H_INCLUDE_SIMPLE
#include "lvgl.h"
#else
#include "lvgl/lvgl.h"
#endif

#ifndef MONITOR_NDOT57_12
#define MONITOR_NDOT57_12 1
#endif

#if MONITOR_NDOT57_12

/*-----------------
 *    BITMAPS
 *----------------*/

/*Store the image of the glyphs*/
static LV_ATTRIBUTE_LARGE_CONST const uint8_t glyph_bitmap[] = {
    /* U+0020 " " */
    0x0,

    /* U+0021 "!" */
    0xf3,

    /* U+0022 "\"" */
    0xfc,

    /* U+0023 "#" */
    0x51, 0x4f, 0xd4, 0xfc, 0x5, 0x14,

    /* U+0024 "$" */
    0x20, 0x7, 0x2b, 0xa1, 0xc2, 0xc0, 0xad, 0xc2,
    0x0,

    /* U+0025 "%" */
    0x60, 0xa4, 0x49, 0x21, 0xb6, 0x12, 0x40, 0x2,
    0x25, 0x6,

    /* U+0026 "&" */
    0x61, 0x22, 0x43, 0x9, 0x20, 0x23, 0x39,

    /* U+0027 "'" */
    0xe0,

    /* U+0028 "(" */
    0x72, 0x48, 0x23,

    /* U+0029 ")" */
    0xc4, 0x92, 0xe,

    /* U+002A "*" */
    0x5d, 0x0,

    /* U+002B "+" */
    0x20, 0x8f, 0xc8, 0x20,

    /* U+002C "," */
    0x60,

    /* U+002D "-" */
    0xe0,

    /* U+002E "." */
    0xf0,

    /* U+002F "/" */
    0xc, 0x42, 0x10, 0x80,

    /* U+0030 "0" */
    0x72, 0x38, 0xe3, 0x8c, 0x8, 0xdc,

    /* U+0031 "1" */
    0x59, 0x24, 0x17,

    /* U+0032 "2" */
    0x72, 0x30, 0xc4, 0x20, 0x4, 0x3f,

    /* U+0033 "3" */
    0x72, 0x30, 0xcc, 0xc, 0x8, 0xdc,

    /* U+0034 "4" */
    0x10, 0xc5, 0x24, 0xfc, 0x1, 0x4,

    /* U+0035 "5" */
    0xfe, 0x8, 0x3c, 0xc, 0x8, 0xdc,

    /* U+0036 "6" */
    0x3d, 0x8, 0x3c, 0x8c, 0x8, 0xdc,

    /* U+0037 "7" */
    0xfc, 0x31, 0x8, 0x40, 0x4, 0x10,

    /* U+0038 "8" */
    0x72, 0x38, 0xdc, 0x8c, 0x8, 0xdc,

    /* U+0039 "9" */
    0x72, 0x38, 0xdf, 0xc, 0x1, 0x18,

    /* U+003A ":" */
    0xcc,

    /* U+003B ";" */
    0x50, 0x58,

    /* U+003C "<" */
    0x2a, 0x22,

    /* U+003D "=" */
    0xfc, 0xf, 0xc0,

    /* U+003E ">" */
    0x88, 0xa8,

    /* U+003F "?" */
    0x72, 0x30, 0xc4, 0x0, 0x2, 0x8,

    /* U+0040 "@" */
    0x72, 0x3a, 0xeb, 0x2, 0xf8, 0x1c,

    /* U+0041 "A" */
    0x21, 0x48, 0xe3, 0xfc, 0x8, 0xe3,

    /* U+0042 "B" */
    0xf2, 0x38, 0xfc, 0x8c, 0x8, 0xfc,

    /* U+0043 "C" */
    0x72, 0x38, 0x20, 0x80, 0x8, 0xdc,

    /* U+0044 "D" */
    0xf2, 0x38, 0xe3, 0x8c, 0x8, 0xfc,

    /* U+0045 "E" */
    0xfe, 0x8, 0x3c, 0x80, 0x8, 0x3f,

    /* U+0046 "F" */
    0xfe, 0x8, 0x3c, 0x80, 0x8, 0x20,

    /* U+0047 "G" */
    0x72, 0x38, 0x2f, 0x8c, 0x8, 0xdc,

    /* U+0048 "H" */
    0x8e, 0x38, 0xff, 0x8c, 0x8, 0xe3,

    /* U+0049 "I" */
    0xe9, 0x24, 0x17,

    /* U+004A "J" */
    0x3c, 0x41, 0x4, 0x10, 0x9, 0x18,

    /* U+004B "K" */
    0x8e, 0x4a, 0x30, 0xa0, 0x9, 0x23,

    /* U+004C "L" */
    0x82, 0x8, 0x20, 0x80, 0x8, 0x3f,

    /* U+004D "M" */
    0x8f, 0x7a, 0xeb, 0x8c, 0x8, 0xe3,

    /* U+004E "N" */
    0x8e, 0x3c, 0xeb, 0x9c, 0x8, 0xe3,

    /* U+004F "O" */
    0x72, 0x38, 0xe3, 0x8c, 0x8, 0xdc,

    /* U+0050 "P" */
    0xf2, 0x38, 0xfc, 0x80, 0x8, 0x20,

    /* U+0051 "Q" */
    0x72, 0x38, 0xe3, 0x8c, 0x89, 0x1b,

    /* U+0052 "R" */
    0xf2, 0x38, 0xfc, 0xa0, 0x9, 0x23,

    /* U+0053 "S" */
    0x72, 0x38, 0x1c, 0xc, 0x8, 0xdc,

    /* U+0054 "T" */
    0xfc, 0x82, 0x8, 0x20, 0x2, 0x8,

    /* U+0055 "U" */
    0x8e, 0x38, 0xe3, 0x8c, 0x8, 0xdc,

    /* U+0056 "V" */
    0x8e, 0x38, 0xe3, 0x8c, 0x5, 0x8,

    /* U+0057 "W" */
    0x8e, 0x38, 0xeb, 0xac, 0xa, 0xd4,

    /* U+0058 "X" */
    0x8e, 0x35, 0x8, 0x50, 0x8, 0xe3,

    /* U+0059 "Y" */
    0x8e, 0x35, 0x8, 0x20, 0x2, 0x8,

    /* U+005A "Z" */
    0xfc, 0x31, 0x8, 0x40, 0x8, 0x3f,

    /* U+005B "[" */
    0xf2, 0x48, 0x27,

    /* U+005C "\\" */
    0x81, 0x2, 0x4, 0xc,

    /* U+005D "]" */
    0xe4, 0x92, 0xf,

    /* U+005E "^" */
    0x20, 0x5, 0x23,

    /* U+005F "_" */
    0xfc,

    /* U+0060 "`" */
    0x90,

    /* U+0061 "a" */
    0xe1, 0x70, 0x97,

    /* U+0062 "b" */
    0x88, 0xe9, 0x90, 0x9e,

    /* U+0063 "c" */
    0x78, 0x80, 0x87,

    /* U+0064 "d" */
    0x11, 0x79, 0x90, 0x96,

    /* U+0065 "e" */
    0x69, 0xf0, 0x87,

    /* U+0066 "f" */
    0x34, 0xf4, 0x40, 0x44,

    /* U+0067 "g" */
    0x79, 0x99, 0x7, 0x1e,

    /* U+0068 "h" */
    0x88, 0xad, 0x90, 0x99,

    /* U+0069 "i" */
    0x43, 0x24, 0x13,

    /* U+006A "j" */
    0x4d, 0x51, 0x60,

    /* U+006B "k" */
    0x88, 0x9a, 0xc0, 0xa9,

    /* U+006C "l" */
    0xfb,

    /* U+006D "m" */
    0xa2, 0xdd, 0x91, 0x0, 0x91, 0x91,

    /* U+006E "n" */
    0xad, 0x90, 0x99,

    /* U+006F "o" */
    0x69, 0x90, 0x96,

    /* U+0070 "p" */
    0xe9, 0x99, 0xe, 0x88,

    /* U+0071 "q" */
    0x69, 0x99, 0x7, 0x11,

    /* U+0072 "r" */
    0xbc, 0x80, 0x88,

    /* U+0073 "s" */
    0x78, 0x60, 0x1e,

    /* U+0074 "t" */
    0x4f, 0x44, 0x4, 0x30,

    /* U+0075 "u" */
    0x99, 0x90, 0x97,

    /* U+0076 "v" */
    0x99, 0xa0, 0xac,

    /* U+0077 "w" */
    0xae, 0xba, 0xc0, 0xaf, 0x40,

    /* U+0078 "x" */
    0x8d, 0x42, 0x0, 0x52, 0x30,

    /* U+0079 "y" */
    0x99, 0x99, 0x7, 0x16,

    /* U+007A "z" */
    0xf1, 0x60, 0x8f,

    /* U+007B "{" */
    0x34, 0x48, 0x40, 0x43,

    /* U+007C "|" */
    0xfb, 0x80,

    /* U+007D "}" */
    0xc2, 0x21, 0x20, 0x2c,

    /* U+007E "~" */
    0x42, 0xb1, 0x0,

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
    {.bitmap_index = 0, .adv_w = 38, .box_w = 1, .box_h = 1, .ofs_x = 0, .ofs_y = 0},
    {.bitmap_index = 1, .adv_w = 38, .box_w = 1, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 2, .adv_w = 58, .box_w = 2, .box_h = 3, .ofs_x = 1, .ofs_y = 5},
    {.bitmap_index = 3, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 9, .adv_w = 115, .box_w = 6, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 18, .adv_w = 192, .box_w = 10, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 28, .adv_w = 134, .box_w = 7, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 35, .adv_w = 38, .box_w = 1, .box_h = 3, .ofs_x = 1, .ofs_y = 5},
    {.bitmap_index = 36, .adv_w = 77, .box_w = 3, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 39, .adv_w = 77, .box_w = 3, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 42, .adv_w = 77, .box_w = 3, .box_h = 3, .ofs_x = 1, .ofs_y = 5},
    {.bitmap_index = 44, .adv_w = 115, .box_w = 6, .box_h = 5, .ofs_x = 1, .ofs_y = 2},
    {.bitmap_index = 48, .adv_w = 58, .box_w = 2, .box_h = 2, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 49, .adv_w = 77, .box_w = 3, .box_h = 1, .ofs_x = 1, .ofs_y = 4},
    {.bitmap_index = 50, .adv_w = 58, .box_w = 2, .box_h = 2, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 51, .adv_w = 115, .box_w = 6, .box_h = 5, .ofs_x = 1, .ofs_y = 2},
    {.bitmap_index = 55, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 61, .adv_w = 77, .box_w = 3, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 64, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 70, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 76, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 82, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 88, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 94, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 100, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 106, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 112, .adv_w = 38, .box_w = 1, .box_h = 6, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 113, .adv_w = 58, .box_w = 2, .box_h = 7, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 115, .adv_w = 77, .box_w = 3, .box_h = 5, .ofs_x = 1, .ofs_y = 2},
    {.bitmap_index = 117, .adv_w = 115, .box_w = 6, .box_h = 3, .ofs_x = 1, .ofs_y = 3},
    {.bitmap_index = 120, .adv_w = 77, .box_w = 3, .box_h = 5, .ofs_x = 1, .ofs_y = 2},
    {.bitmap_index = 122, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 128, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 134, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 140, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 146, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 152, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 158, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 164, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 170, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 176, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 182, .adv_w = 77, .box_w = 3, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 185, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 191, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 197, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 203, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 209, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 215, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 221, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 227, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 233, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 239, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 245, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 251, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 257, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 263, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 269, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 275, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 281, .adv_w = 115, .box_w = 6, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 287, .adv_w = 77, .box_w = 3, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 290, .adv_w = 115, .box_w = 6, .box_h = 5, .ofs_x = 1, .ofs_y = 2},
    {.bitmap_index = 294, .adv_w = 77, .box_w = 3, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 297, .adv_w = 115, .box_w = 6, .box_h = 4, .ofs_x = 1, .ofs_y = 5},
    {.bitmap_index = 300, .adv_w = 115, .box_w = 6, .box_h = 1, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 301, .adv_w = 58, .box_w = 2, .box_h = 2, .ofs_x = 1, .ofs_y = 7},
    {.bitmap_index = 302, .adv_w = 96, .box_w = 4, .box_h = 6, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 305, .adv_w = 96, .box_w = 4, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 309, .adv_w = 96, .box_w = 4, .box_h = 6, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 312, .adv_w = 96, .box_w = 4, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 316, .adv_w = 96, .box_w = 4, .box_h = 6, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 319, .adv_w = 96, .box_w = 4, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 323, .adv_w = 96, .box_w = 4, .box_h = 8, .ofs_x = 1, .ofs_y = -2},
    {.bitmap_index = 327, .adv_w = 96, .box_w = 4, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 331, .adv_w = 77, .box_w = 3, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 334, .adv_w = 58, .box_w = 2, .box_h = 10, .ofs_x = 1, .ofs_y = -2},
    {.bitmap_index = 337, .adv_w = 96, .box_w = 4, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 341, .adv_w = 38, .box_w = 1, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 342, .adv_w = 154, .box_w = 8, .box_h = 6, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 348, .adv_w = 96, .box_w = 4, .box_h = 6, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 351, .adv_w = 96, .box_w = 4, .box_h = 6, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 354, .adv_w = 96, .box_w = 4, .box_h = 8, .ofs_x = 1, .ofs_y = -2},
    {.bitmap_index = 358, .adv_w = 96, .box_w = 4, .box_h = 8, .ofs_x = 1, .ofs_y = -2},
    {.bitmap_index = 362, .adv_w = 96, .box_w = 4, .box_h = 6, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 365, .adv_w = 96, .box_w = 4, .box_h = 6, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 368, .adv_w = 96, .box_w = 4, .box_h = 7, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 372, .adv_w = 96, .box_w = 4, .box_h = 6, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 375, .adv_w = 96, .box_w = 4, .box_h = 6, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 378, .adv_w = 115, .box_w = 6, .box_h = 6, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 383, .adv_w = 115, .box_w = 6, .box_h = 6, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 388, .adv_w = 96, .box_w = 4, .box_h = 8, .ofs_x = 1, .ofs_y = -2},
    {.bitmap_index = 392, .adv_w = 96, .box_w = 4, .box_h = 6, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 395, .adv_w = 96, .box_w = 4, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 399, .adv_w = 38, .box_w = 1, .box_h = 9, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 401, .adv_w = 96, .box_w = 4, .box_h = 8, .ofs_x = 1, .ofs_y = 0},
    {.bitmap_index = 405, .adv_w = 115, .box_w = 6, .box_h = 3, .ofs_x = 1, .ofs_y = 3},
    {.bitmap_index = 408, .adv_w = 79, .box_w = 4, .box_h = 4, .ofs_x = 1, .ofs_y = 5},
    {.bitmap_index = 410, .adv_w = 192, .box_w = 9, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 423, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 437, .adv_w = 192, .box_w = 11, .box_h = 11, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 453, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 467, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 481, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 495, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 509, .adv_w = 192, .box_w = 11, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 525, .adv_w = 192, .box_w = 11, .box_h = 11, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 541, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 555, .adv_w = 192, .box_w = 11, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 571, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 585, .adv_w = 192, .box_w = 11, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 601, .adv_w = 192, .box_w = 9, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 614, .adv_w = 192, .box_w = 10, .box_h = 12, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 629, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 643, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 657, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 671, .adv_w = 192, .box_w = 10, .box_h = 11, .ofs_x = 1, .ofs_y = -1},
    {.bitmap_index = 685, .adv_w = 192, .box_w = 12, .box_h = 11, .ofs_x = 0, .ofs_y = -1},
    {.bitmap_index = 702, .adv_w = 192, .box_w = 12, .box_h = 11, .ofs_x = 0, .ofs_y = -1}
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
const lv_font_t monitor_ndot57_12 = {
#else
lv_font_t monitor_ndot57_12 = {
#endif
    .get_glyph_dsc = lv_font_get_glyph_dsc_fmt_txt,    /*Function pointer to get glyph's data*/
    .get_glyph_bitmap = lv_font_get_bitmap_fmt_txt,    /*Function pointer to get glyph's bitmap*/
    .line_height = 13,          /*The maximum line height required by the font*/
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



#endif /*#if MONITOR_NDOT57_12*/

