#pragma once

#include <stdbool.h>
#include "esp_err.h"
#include "sdkconfig.h"

#ifdef __cplusplus
extern "C" {
#endif

#if CONFIG_MONITOR_PM_PROFILE
esp_err_t monitor_pm_profile_init(void);
/* KEY alone held for 3 seconds. Runs in task context, never in the ISR. */
void monitor_pm_profile_key_hold(void);
bool monitor_pm_profile_active(void);
#else
static inline esp_err_t monitor_pm_profile_init(void) { return ESP_OK; }
static inline void monitor_pm_profile_key_hold(void) {}
static inline bool monitor_pm_profile_active(void) { return false; }
#endif

#ifdef __cplusplus
}
#endif
