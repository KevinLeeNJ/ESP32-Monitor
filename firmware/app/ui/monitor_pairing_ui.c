#include "monitor_pairing_ui.h"
#include "monitor_fonts.h"
#include <string.h>

static void move_y(void *obj, int32_t y) { lv_obj_set_y(obj, y); }

static lv_obj_t *text(lv_obj_t *parent, const char *value, const lv_font_t *font,
                      int y, lv_color_t color)
{
    lv_obj_t *label = lv_label_create(parent);
    lv_label_set_text(label, value);
    lv_obj_set_width(label, 220);
    lv_obj_set_pos(label, 0, y);
    lv_obj_set_style_text_align(label, LV_TEXT_ALIGN_CENTER, 0);
    lv_obj_set_style_text_font(label, font, 0);
    lv_obj_set_style_text_color(label, color, 0);
    return label;
}

lv_obj_t *monitor_pairing_show(lv_obj_t *parent, monitor_frame_variant_t variant,
                               const char *code, bool animate)
{
    if (!parent || !code || strlen(code) != 6) return NULL;
    for (unsigned i = 0; i < 6; i++) if (code[i] < '0' || code[i] > '9') return NULL;
    bool inv = variant == MONITOR_FRAME_INVERTED;
    lv_color_t ink = lv_color_hex(inv ? 0xf7f7f7 : 0x111111);
    lv_color_t paper = lv_color_hex(inv ? 0x111111 : 0xf7f7f7);
    lv_obj_t *panel = lv_obj_create(parent);
    if (!panel) return NULL;
    lv_obj_remove_style_all(panel);
    lv_obj_clear_flag(panel, LV_OBJ_FLAG_SCROLLABLE);
    lv_obj_set_size(panel, 220, 126);
    lv_obj_set_pos(panel, 90, animate ? 60 : 87);
    lv_obj_set_style_bg_color(panel, paper, 0);
    lv_obj_set_style_bg_opa(panel, LV_OPA_COVER, 0);
    lv_obj_set_style_border_color(panel, ink, 0);
    lv_obj_set_style_border_width(panel, 2, 0);
    lv_obj_set_style_radius(panel, 8, 0);
    lv_obj_set_name(panel, "pairing_popup");
    text(panel, "PAIR DEVICE", &monitor_misans_14, 12, ink);
    text(panel, code, &monitor_ndot57_30, 39, ink);
    text(panel, "Check code on Client", &monitor_misans_12, 85, ink);
    text(panel, "KEY: confirm   BOOT: cancel", &monitor_misans_12, 104, ink);
    if (animate) {
        lv_anim_t anim;
        lv_anim_init(&anim);
        lv_anim_set_var(&anim, panel);
        lv_anim_set_exec_cb(&anim, move_y);
        lv_anim_set_values(&anim, 60, 87);
        lv_anim_set_duration(&anim, 260);
        lv_anim_set_path_cb(&anim, lv_anim_path_ease_out);
        lv_anim_start(&anim);
    }
    return panel;
}

void monitor_pairing_close(lv_obj_t *popup)
{
    if (popup) lv_obj_delete(popup);
}

bool monitor_pairing_animating(lv_obj_t *popup)
{
    return popup && lv_anim_get(popup, move_y) != NULL;
}

void monitor_pairing_pause(lv_obj_t *popup)
{
    lv_anim_t *anim = popup ? lv_anim_get(popup, move_y) : NULL;
    if (anim) lv_anim_pause(anim);
}

void monitor_pairing_resume(lv_obj_t *popup)
{
    lv_anim_t *anim = popup ? lv_anim_get(popup, move_y) : NULL;
    if (anim && lv_anim_is_paused(anim)) { lv_anim_refr_now(); lv_anim_resume(anim); }
}

void monitor_pairing_settle(lv_obj_t *popup)
{
    if (!popup) return;
    lv_anim_delete(popup, move_y);
    lv_obj_set_y(popup, 87);
}
