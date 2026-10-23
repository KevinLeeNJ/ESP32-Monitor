#pragma once

#include <stdint.h>
#include <stdbool.h>

#include "esp_err.h"
#include "esp_lcd_panel_io.h"

#ifdef __cplusplus
extern "C" {
#endif

#define RLCD_WIDTH  400
#define RLCD_HEIGHT 300
/* ST7305 HPM settling delay retained after mode switch. */
#define RLCD_HPM_PREPARE_MS 300U

typedef struct {
    int mosi;
    int sclk;
    int dc;
    int cs;
    int rst;
    int te;
} rlcd_pins_t;

typedef struct rlcd_port rlcd_port_t;

/* Caller installs the shared IRAM-safe GPIO ISR service before initialization. */
esp_err_t rlcd_port_init(rlcd_port_t **out_port, const rlcd_pins_t *pins);
/* Borrowed IO handle; display_port retains DMA callback ownership. */
esp_lcd_panel_io_handle_t rlcd_port_panel_io(rlcd_port_t *port);
esp_err_t rlcd_port_draw(rlcd_port_t *port, const uint8_t *packed, uint32_t len);
esp_err_t rlcd_port_draw_diff(rlcd_port_t *port, const uint8_t *packed, uint32_t len);
/* Call on the display task, between completed draws. HPM=32 Hz, LPM=1 Hz.
 * Blocks through the HPM/LPM mode settling sequence. Panel voltage values are
 * configured once during initialization, following the U8g2 300x400 profile.
 * Idempotent: no command traffic when the requested mode is already active. */
esp_err_t rlcd_port_set_animation_mode(rlcd_port_t *port, bool active);
/* True while HPM writes are paced by the panel TE signal. On a TE timeout,
 * writes continue and callers resume the nominal frame timer until next HPM. */
bool rlcd_port_frame_sync_active(const rlcd_port_t *port);
esp_err_t rlcd_port_deinit(rlcd_port_t *port);

#ifdef __cplusplus
}
#endif
