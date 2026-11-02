#pragma once
#include "esp_err.h"
#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

/* Starts the USB Serial/JTAG listener. It answers HELLO and only changes
 * transport state after an authenticated AUTH using a stored pairing. */
esp_err_t monitor_serial_start(void);
/* Best effort: send up goodbye(pairing_revoked) before the caller revokes NVS. */
void monitor_serial_revoke(void);
void monitor_serial_stop(void);
bool monitor_serial_active(void);

#ifdef __cplusplus
}
#endif
