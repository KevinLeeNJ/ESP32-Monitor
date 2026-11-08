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
 * HTTP producer. Resuming continues the next wall-clock 30 s slot (RTC second
 * :00 or :30) instead of polling immediately; a Wi-Fi recovery cannot pull
 * that first poll forward past the slot. */
void monitor_devices_set_companion_active(bool active);
/* Adopt the Client's last page-one cards when a companion session ends, so
 * page one keeps them while the direct producer waits for its next slot. */
void monitor_devices_adopt_snapshot(const monitor_devices_snapshot_t *snapshot);
/* Milliseconds until the next wall-clock 30 s slot when the current slot was
 * already serviced, otherwise 0 (poll now). Lets a companion session keep the
 * standalone polling cadence. */
uint32_t monitor_devices_refresh_delay_ms(void);
void monitor_devices_get(monitor_devices_snapshot_t *out);
#ifdef __cplusplus
}
#endif
