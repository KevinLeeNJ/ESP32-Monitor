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
/* While an authenticated companion session owns page one, pause the direct
 * HTTP producer. Resuming requests an immediate refresh. */
void monitor_devices_set_companion_active(bool active);
/* Milliseconds until the next wall-clock 30 s slot when the current slot was
 * already serviced, otherwise 0 (poll now). Lets a companion session keep the
 * standalone polling cadence. */
uint32_t monitor_devices_refresh_delay_ms(void);
void monitor_devices_get(monitor_devices_snapshot_t *out);
#ifdef __cplusplus
}
#endif
