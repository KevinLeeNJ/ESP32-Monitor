#pragma once
#include <stdbool.h>
#include "monitor_ui.h"
#include "monitor_damage.h"

#ifdef __cplusplus
extern "C" {
#endif

typedef struct monitor_pages monitor_pages_t;
/* All functions are called by the LVGL display owner. The second page uses
 * unavailable placeholders until real companion snapshots are connected. */
monitor_pages_t *monitor_pages_create(lv_obj_t *first, monitor_frame_variant_t variant,
                                      const monitor_activity_data_t *activity);
bool monitor_pages_switch(monitor_pages_t *pages, bool next);
/* Switching starts paused; resume after the panel has entered HPM. */
void monitor_pages_resume(monitor_pages_t *pages);
int32_t monitor_pages_position(const monitor_pages_t *pages);
bool monitor_pages_active(const monitor_pages_t *pages);
/* Compose a transition into the existing RLCD buffer; false when idle. */
bool monitor_pages_compose(monitor_pages_t *pages, uint8_t *packed, monitor_damage_t *dirty);
bool monitor_pages_is_second(const monitor_pages_t *pages);
void monitor_pages_settle(monitor_pages_t *pages);
/* Only for scene lookup; the controller is owned by its marker object. */
monitor_pages_t *monitor_pages_from_first(lv_obj_t *first);

#ifdef __cplusplus
}
#endif
