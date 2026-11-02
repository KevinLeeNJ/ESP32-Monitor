#pragma once
#include <stdbool.h>
#include <stdint.h>
#include "esp_err.h"

#ifdef __cplusplus
extern "C" {
#endif

typedef struct {
    uint32_t version;
    bool visible;
    char code[7];
} monitor_pairing_view_t;

typedef enum {
    MONITOR_PAIRING_ACTION_NONE = 0,
    MONITOR_PAIRING_ACTION_CONFIRM,
    MONITOR_PAIRING_ACTION_CANCEL,
} monitor_pairing_action_t;

/* Called once a matching, fresh discovery offer has been validated. */
esp_err_t monitor_pairing_service_offer(const char *ipv4, uint16_t port, const char *host_id);
/* Serial pairing runs inside the serial task; claim the popup and expose the
 * button decision through a polled action instead of a pairing task. */
esp_err_t monitor_pairing_service_serial_begin(const char *code);
void monitor_pairing_service_serial_end(void);
monitor_pairing_action_t monitor_pairing_service_serial_action(void);
void monitor_pairing_service_view(monitor_pairing_view_t *out);
/* Returns true when an active pairing consumed this button action. */
bool monitor_pairing_service_action(bool confirm);

#ifdef __cplusplus
}
#endif
