#pragma once
#include "esp_err.h"
#include "monitor_devices_data.h"
#ifdef __cplusplus
extern "C" {
#endif
esp_err_t monitor_devices_start(void);
void monitor_devices_request_refresh(void);
void monitor_devices_config_changed(void);
void monitor_devices_set_wifi_available(bool available);
void monitor_devices_get(monitor_devices_snapshot_t *out);
#ifdef __cplusplus
}
#endif
