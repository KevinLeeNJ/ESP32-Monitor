#pragma once
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include "companion.pb.h"

bool monitor_pairing_encode(const esp32monitor_companion_PairRequest *request,
                            uint8_t *out, size_t capacity, size_t *written);
bool monitor_pairing_response(const uint8_t *data, size_t len,
                              esp32monitor_companion_PairResponse *out);
