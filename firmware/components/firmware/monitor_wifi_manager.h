#pragma once

#include <stdbool.h>
#include "esp_err.h"
#include "monitor_ui.h"

#ifdef __cplusplus
extern "C" {
#endif

esp_err_t monitor_wifi_start(void);
monitor_wifi_state_t monitor_wifi_get_state(void);
bool monitor_wifi_is_provisioning(void);

#ifdef __cplusplus
}
#endif
