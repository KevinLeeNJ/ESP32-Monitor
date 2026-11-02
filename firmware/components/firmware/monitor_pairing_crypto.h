#pragma once
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

/* SHA-256 via the ESP-IDF PSA crypto provider. */
bool monitor_pairing_sha256(const uint8_t *data, size_t len, uint8_t digest[32]);
bool monitor_pairing_hmac(const uint8_t key[32], const uint8_t *data, size_t len, uint8_t digest[32]);
