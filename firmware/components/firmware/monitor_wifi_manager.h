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
/* Serial companion sessions own the monitor: the radio, AP, DNS and web
 * server are stopped until monitor_wifi_resume() restores the saved mode. */
esp_err_t monitor_wifi_suspend(void);
esp_err_t monitor_wifi_resume(void);

#ifdef __cplusplus
}
#endif
