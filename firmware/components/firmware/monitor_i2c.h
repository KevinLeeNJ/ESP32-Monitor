#pragma once

#include "esp_err.h"
#include "driver/i2c_master.h"

#ifdef __cplusplus
extern "C" {
#endif

esp_err_t monitor_i2c_init(void);
i2c_master_bus_handle_t monitor_i2c_get_bus(void);

#ifdef __cplusplus
}
#endif
