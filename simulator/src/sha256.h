#pragma once

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/* Portable SHA-256 used by simulator tests and artifact metadata. */
void monitor_sha256(const uint8_t *data, uint32_t len, char out_hex[65]);

#ifdef __cplusplus
}
#endif
