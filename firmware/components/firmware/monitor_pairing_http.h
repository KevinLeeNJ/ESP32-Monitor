#pragma once
#include <stdint.h>
#include "esp_err.h"
#include "monitor_pairing_wire.h"

/* address must be the IPv4 source of the selected discovery offer. */
esp_err_t monitor_pairing_http_exchange(const char *address, uint16_t port,
                                        const esp32monitor_companion_PairRequest *request,
                                        esp32monitor_companion_PairResponse *response);
