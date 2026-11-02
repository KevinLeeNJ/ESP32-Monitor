#pragma once
#include "monitor_ui.h"

/* Display-owner only. A six-digit code is copied into the popup. */
lv_obj_t *monitor_pairing_show(lv_obj_t *parent, monitor_frame_variant_t variant,
                               const char *code, bool animate);
void monitor_pairing_close(lv_obj_t *popup);
bool monitor_pairing_animating(lv_obj_t *popup);
void monitor_pairing_pause(lv_obj_t *popup);
void monitor_pairing_resume(lv_obj_t *popup);
void monitor_pairing_settle(lv_obj_t *popup);
