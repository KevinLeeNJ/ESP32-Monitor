#pragma once
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include "companion.pb.h"

/* Reject malformed or duplicate fields before decoding into bounded storage. */
bool monitor_discovery_decode(const uint8_t *data, size_t len,
                              esp32monitor_companion_Discovery *out);

/* Encode the unauthenticated announcement for a first-time pairing window. */
bool monitor_discovery_announce(const esp32monitor_companion_Discovery *request,
                                const char *device_id, const char *device_name,
                                const char *firmware_version, const char *nonce,
                                uint8_t *out, size_t capacity, size_t *written);
