#include "monitor_i2c.h"

#include "driver/gpio.h"

static i2c_master_bus_handle_t s_bus;

esp_err_t monitor_i2c_init(void)
{
    if (s_bus != NULL) return ESP_OK;
    const i2c_master_bus_config_t config = {
        .i2c_port = I2C_NUM_0,
        .sda_io_num = GPIO_NUM_13,
        .scl_io_num = GPIO_NUM_14,
        .clk_source = I2C_CLK_SRC_DEFAULT,
        .glitch_ignore_cnt = 7,
        .flags.enable_internal_pullup = false,
    };
    return i2c_new_master_bus(&config, &s_bus);
}

i2c_master_bus_handle_t monitor_i2c_get_bus(void) { return s_bus; }
