#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include "esp_err.h"

#ifdef __cplusplus
extern "C" {
#endif

/* Initialize the system clock and schedule NTP syncs. Call after netif/event loop
 * initialization; notify monitor_clock_on_wifi_connected() whenever STA gets IP. */
esp_err_t monitor_clock_start(void);
void monitor_clock_on_wifi_connected(void);
void monitor_clock_on_wifi_disconnected(void);
/* Use a Client-supplied UTC reading only while the RTC is untrusted and Wi-Fi
 * is unavailable; a stored Client time re-trusts the RTC. */
void monitor_clock_offer_client_time(int64_t utc_epoch_s);
bool monitor_clock_is_valid(void);
bool monitor_clock_get_text(char *time_text, size_t time_size,
                            char *date_text, size_t date_size);

#ifdef __cplusplus
}
#endif
