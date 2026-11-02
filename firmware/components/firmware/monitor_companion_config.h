#pragma once
#include "companion.pb.h"
#include "esp_err.h"
/* Reads existing user configuration without modifying it. */
esp_err_t monitor_companion_config_read(esp32monitor_companion_Config *config);
