#pragma once

#include <stdbool.h>
#include <stddef.h>
#include "esp_err.h"

#ifdef __cplusplus
extern "C" {
#endif

/* Initialize the system clock and schedule NTP syncs. Call after netif/event loop
 * initialization; notify monitor_clock_on_wifi_connected() whenever STA gets IP. */
esp_err_t monitor_clock_start(void);
void monitor_clock_on_wifi_connected(void);
bool monitor_clock_get_text(char *time_text, size_t time_size,
                            char *date_text, size_t date_size);

#ifdef __cplusplus
}
#endif
