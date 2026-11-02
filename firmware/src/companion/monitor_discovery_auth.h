#pragma once
#include "monitor_discovery.h"
#include "monitor_pairing_code.h"

typedef bool (*monitor_companion_hmac_t)(const uint8_t key[32], const uint8_t *data,
                                         size_t length, uint8_t out[32]);
/* Derive the discovery-only key so a revocation tombstone need not retain the
 * pairing's serial key. */
bool monitor_discovery_key(const char *serial_key, const char *host_id, const char *device_id,
                           const char *pairing_id, monitor_pairing_hash_t hash,
                           monitor_companion_hmac_t hmac, uint8_t out[32]);
bool monitor_discovery_mac(const esp32monitor_companion_Discovery *message, const char *serial_key,
                           monitor_pairing_hash_t hash, monitor_companion_hmac_t hmac, char out[65]);
bool monitor_discovery_mac_keyed(const esp32monitor_companion_Discovery *message,
                                 const uint8_t key[32], monitor_companion_hmac_t hmac, char out[65]);
bool monitor_discovery_verify(const esp32monitor_companion_Discovery *message, const char *serial_key,
                              monitor_pairing_hash_t hash, monitor_companion_hmac_t hmac);
bool monitor_discovery_verify_keyed(const esp32monitor_companion_Discovery *message,
                                    const uint8_t key[32], monitor_companion_hmac_t hmac);
bool monitor_discovery_announce_paired(const esp32monitor_companion_Discovery *request,
                                       const char *device_id, const char *device_name,
                                       const char *firmware_version, const char *nonce,
                                       const char *serial_key, monitor_pairing_hash_t hash,
                                       monitor_companion_hmac_t hmac, uint8_t *out,
                                       size_t capacity, size_t *written);
/* Answer a paired probe after the pairing was revoked. The reply keeps
 * pairing_open=true but is still authenticated by the old discovery key. */
bool monitor_discovery_announce_revoked(const esp32monitor_companion_Discovery *request,
                                        const char *device_id, const char *device_name,
                                        const char *firmware_version, const char *nonce,
                                        const uint8_t key[32], monitor_companion_hmac_t hmac,
                                        uint8_t *out, size_t capacity, size_t *written);
