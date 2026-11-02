#pragma once
#include "esp_err.h"
#include "monitor_pairing_session.h"
#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct {
    char host_id[37];
    char pairing_id[37];
    char device_token[44];
    char serial_key[44];
} monitor_pairing_record_t;

/* A revoked pairing keeps only its discovery key, so the former host can be
 * told (authenticated) that the credential no longer exists. */
typedef struct {
    char host_id[37];
    char pairing_id[37];
    uint8_t discovery_key[32];
} monitor_pairing_revocation_t;

/* Initialize after NVS; identity and credentials use a dedicated namespace. */
esp_err_t monitor_pairing_identity(char out[37]);
esp_err_t monitor_pairing_store_load(monitor_pairing_record_t *out);
esp_err_t monitor_pairing_store_approved(const monitor_pairing_session_t *session);
esp_err_t monitor_pairing_store_revoke(void);
size_t monitor_pairing_store_revocations(monitor_pairing_revocation_t *out, size_t capacity);

#ifdef __cplusplus
}
#endif
