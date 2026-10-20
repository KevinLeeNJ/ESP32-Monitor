#pragma once

#include <stdint.h>

#include "esp_err.h"

#ifdef __cplusplus
extern "C" {
#endif

#define RLCD_WIDTH  400
#define RLCD_HEIGHT 300

typedef struct {
    int mosi;
    int sclk;
    int dc;
    int cs;
    int rst;
    int te;
} rlcd_pins_t;

typedef struct rlcd_port rlcd_port_t;

esp_err_t rlcd_port_init(rlcd_port_t **out_port, const rlcd_pins_t *pins);
esp_err_t rlcd_port_draw(rlcd_port_t *port, const uint8_t *packed, uint32_t len);
esp_err_t rlcd_port_draw_diff(rlcd_port_t *port, const uint8_t *packed, uint32_t len);
esp_err_t rlcd_port_deinit(rlcd_port_t *port);

#ifdef __cplusplus
}
#endif
