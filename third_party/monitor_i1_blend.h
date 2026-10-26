#pragma once

/* Uses LVGL 9.4 private descriptor layout via its supported custom blend hook.
 * Recheck this layout and I1 rendering regressions before upgrading LVGL. */
#include "lvgl/src/draw/sw/blend/lv_draw_sw_blend_private.h"

/* LVGL 9.4 mixes 0/1 values with integer division in its I1 fill fallback.
 * Any fractional black coverage then becomes black, widening rounded borders
 * and glyphs. Mix in 8-bit luminance and quantize only the final pixel instead.
 * Use LVGL's custom blend hooks so the pinned submodule stays unchanged. */
static inline lv_result_t monitor_i1_blend_fill(lv_draw_sw_blend_fill_dsc_t *dsc)
{
    const uint32_t source = lv_color_luminance(dsc->color);
    const int32_t bit_offset = dsc->relative_area.x1 % 8;
    uint8_t *row = dsc->dest_buf;
    const lv_opa_t *mask = dsc->mask_buf;

    for(int32_t y = 0; y < dsc->dest_h; ++y) {
        for(int32_t x = 0; x < dsc->dest_w; ++x) {
            const uint32_t opacity = mask ? ((uint32_t)mask[x] * dsc->opa) / 255U : dsc->opa;
            if(opacity == 0) continue;
            const int32_t bit = x + bit_offset;
            uint8_t *pixel = &row[bit / 8];
            const uint8_t flag = (uint8_t)(0x80U >> (bit % 8));
            const uint32_t background = (*pixel & flag) ? 255U : 0U;
            const uint32_t luminance = (source * opacity + background * (255U - opacity) + 127U) / 255U;
            if(luminance > LV_DRAW_SW_I1_LUM_THRESHOLD) *pixel |= flag;
            else *pixel &= (uint8_t)~flag;
        }
        row += dsc->dest_stride;
        if(mask) mask += dsc->mask_stride;
    }
    return LV_RESULT_OK;
}

#define LV_DRAW_SW_COLOR_BLEND_TO_I1_WITH_OPA(dsc) monitor_i1_blend_fill(dsc)
#define LV_DRAW_SW_COLOR_BLEND_TO_I1_WITH_MASK(dsc) monitor_i1_blend_fill(dsc)
#define LV_DRAW_SW_COLOR_BLEND_TO_I1_MIX_MASK_OPA(dsc) monitor_i1_blend_fill(dsc)
