#pragma once
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include "companion.pb.h"

/* SHA-256 implementation supplied by platform crypto. Returns false on error. */
typedef bool (*monitor_pairing_hash_t)(const uint8_t *data, size_t len, uint8_t digest[32]);
bool monitor_companion_decode_nonce(const char *text, uint8_t out[32]);
bool monitor_companion_valid_uuid(const char *text);
/* Inputs are base64url unpadded 32-byte nonces and canonical UUIDs. */
bool monitor_pairing_code(const char *device_nonce, const char *host_nonce,
                          const char *device_id, const char *host_id, const char *pairing_id,
                          monitor_pairing_hash_t hash, char code[7]);
/* Validate response identity, lifetime, nonces, state and credential presence. */
bool monitor_pairing_validate_response(const esp32monitor_companion_PairResponse *response,
                                       const char *expected_host);
