#include "display_port.h"

#include <stdlib.h>
#include <string.h>

#include "driver/gpio.h"
#include "driver/spi_master.h"
#include "esp_check.h"
#include "esp_lcd_io_spi.h"
#include "esp_lcd_panel_io.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#define RLCD_SPI_HOST SPI3_HOST
#define RLCD_SPI_CLOCK_HZ (10 * 1000 * 1000)
#define RLCD_PACKED_BYTES (RLCD_WIDTH * RLCD_HEIGHT / 8U)

struct rlcd_port {
    esp_lcd_panel_io_handle_t io;
    rlcd_pins_t pins;
    uint8_t *packed;
};

static esp_err_t send_cmd(rlcd_port_t *port, uint8_t cmd)
{
    return esp_lcd_panel_io_tx_param(port->io, cmd, NULL, 0);
}

static esp_err_t send_data(rlcd_port_t *port, const uint8_t *data, size_t len)
{
    return esp_lcd_panel_io_tx_param(port->io, -1, data, len);
}

static esp_err_t send_data1(rlcd_port_t *port, uint8_t value)
{
    return send_data(port, &value, 1);
}

static esp_err_t set_window(rlcd_port_t *port)
{
    ESP_RETURN_ON_ERROR(send_cmd(port, 0x2a), "rlcd", "column command failed");
    ESP_RETURN_ON_ERROR(send_data1(port, 0x12), "rlcd", "column start failed");
    ESP_RETURN_ON_ERROR(send_data1(port, 0x2a), "rlcd", "column end failed");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0x2b), "rlcd", "page command failed");
    ESP_RETURN_ON_ERROR(send_data1(port, 0x00), "rlcd", "page start failed");
    ESP_RETURN_ON_ERROR(send_data1(port, 0xc7), "rlcd", "page end failed");
    return ESP_OK;
}

static esp_err_t panel_init_sequence(rlcd_port_t *port)
{
    /* Register sequence from Waveshare's Examples/09_LVGL_V9_Test. */
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xd6), "rlcd", "nvm load");
    ESP_RETURN_ON_ERROR(send_data(port, (const uint8_t[]){0x17, 0x02}, 2), "rlcd", "nvm args");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xd1), "rlcd", "booster");
    ESP_RETURN_ON_ERROR(send_data1(port, 0x01), "rlcd", "booster arg");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xc0), "rlcd", "gate voltage");
    ESP_RETURN_ON_ERROR(send_data(port, (const uint8_t[]){0x11, 0x04}, 2), "rlcd", "gate args");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xc1), "rlcd", "vshp");
    ESP_RETURN_ON_ERROR(send_data(port, (const uint8_t[]){0x69, 0x69, 0x69, 0x69}, 4), "rlcd", "vshp args");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xc2), "rlcd", "vslp");
    ESP_RETURN_ON_ERROR(send_data(port, (const uint8_t[]){0x19, 0x19, 0x19, 0x19}, 4), "rlcd", "vslp args");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xc4), "rlcd", "vshn");
    ESP_RETURN_ON_ERROR(send_data(port, (const uint8_t[]){0x4b, 0x4b, 0x4b, 0x4b}, 4), "rlcd", "vshn args");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xc5), "rlcd", "vsln");
    ESP_RETURN_ON_ERROR(send_data(port, (const uint8_t[]){0x19, 0x19, 0x19, 0x19}, 4), "rlcd", "vsln args");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xd8), "rlcd", "vcom");
    ESP_RETURN_ON_ERROR(send_data(port, (const uint8_t[]){0x80, 0xe9}, 2), "rlcd", "vcom args");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xb2), "rlcd", "frame rate");
    ESP_RETURN_ON_ERROR(send_data1(port, 0x02), "rlcd", "frame rate arg");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xb3), "rlcd", "waveform b3");
    ESP_RETURN_ON_ERROR(send_data(port, (const uint8_t[]){0xe5,0xf6,0x05,0x46,0x77,0x77,0x77,0x77,0x76,0x45}, 10),
                        "rlcd", "waveform b3 args");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xb4), "rlcd", "waveform b4");
    ESP_RETURN_ON_ERROR(send_data(port, (const uint8_t[]){0x05,0x46,0x77,0x77,0x77,0x77,0x76,0x45}, 8),
                        "rlcd", "waveform b4 args");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0x62), "rlcd", "gate");
    ESP_RETURN_ON_ERROR(send_data(port, (const uint8_t[]){0x32,0x03,0x1f}, 3), "rlcd", "gate args");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xb7), "rlcd", "b7");
    ESP_RETURN_ON_ERROR(send_data1(port, 0x13), "rlcd", "b7 arg");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xb0), "rlcd", "b0");
    ESP_RETURN_ON_ERROR(send_data1(port, 0x64), "rlcd", "b0 arg");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0x11), "rlcd", "sleep out");
    vTaskDelay(pdMS_TO_TICKS(200));
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xc9), "rlcd", "c9");
    ESP_RETURN_ON_ERROR(send_data1(port, 0x00), "rlcd", "c9 arg");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0x36), "rlcd", "madctl");
    ESP_RETURN_ON_ERROR(send_data1(port, 0x48), "rlcd", "madctl arg");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0x3a), "rlcd", "pixel format");
    ESP_RETURN_ON_ERROR(send_data1(port, 0x11), "rlcd", "pixel format arg");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xb9), "rlcd", "b9");
    ESP_RETURN_ON_ERROR(send_data1(port, 0x20), "rlcd", "b9 arg");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xb8), "rlcd", "b8");
    ESP_RETURN_ON_ERROR(send_data1(port, 0x29), "rlcd", "b8 arg");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0x21), "rlcd", "inversion on");
    ESP_RETURN_ON_ERROR(set_window(port), "rlcd", "window");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0x35), "rlcd", "te");
    ESP_RETURN_ON_ERROR(send_data1(port, 0x00), "rlcd", "te arg");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xd0), "rlcd", "d0");
    ESP_RETURN_ON_ERROR(send_data1(port, 0xff), "rlcd", "d0 arg");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0x38), "rlcd", "idle off");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0x29), "rlcd", "display on");
    return ESP_OK;
}

static void hardware_reset(rlcd_port_t *port)
{
    gpio_set_level(port->pins.rst, 1);
    vTaskDelay(pdMS_TO_TICKS(50));
    gpio_set_level(port->pins.rst, 0);
    vTaskDelay(pdMS_TO_TICKS(20));
    gpio_set_level(port->pins.rst, 1);
    vTaskDelay(pdMS_TO_TICKS(50));
}

esp_err_t rlcd_port_init(rlcd_port_t **out_port, const rlcd_pins_t *pins)
{
    ESP_RETURN_ON_FALSE(out_port != NULL && pins != NULL, ESP_ERR_INVALID_ARG, "rlcd", "invalid arg");
    rlcd_port_t *port = calloc(1, sizeof(*port));
    ESP_RETURN_ON_FALSE(port != NULL, ESP_ERR_NO_MEM, "rlcd", "no memory");
    port->pins = *pins;

    gpio_config_t rst_cfg = {
        .pin_bit_mask = 1ULL << pins->rst,
        .mode = GPIO_MODE_OUTPUT,
        .pull_up_en = GPIO_PULLUP_ENABLE,
        .intr_type = GPIO_INTR_DISABLE,
    };
    ESP_RETURN_ON_ERROR(gpio_config(&rst_cfg), "rlcd", "reset gpio config");
    gpio_set_level(pins->rst, 1);

    spi_bus_config_t bus_cfg = {
        .mosi_io_num = pins->mosi,
        .miso_io_num = -1,
        .sclk_io_num = pins->sclk,
        .quadwp_io_num = -1,
        .quadhd_io_num = -1,
        .max_transfer_sz = RLCD_PACKED_BYTES,
    };
    ESP_RETURN_ON_ERROR(spi_bus_initialize(RLCD_SPI_HOST, &bus_cfg, SPI_DMA_CH_AUTO), "rlcd", "spi bus");

    esp_lcd_panel_io_spi_config_t io_cfg = {
        .dc_gpio_num = pins->dc,
        .cs_gpio_num = pins->cs,
        .pclk_hz = RLCD_SPI_CLOCK_HZ,
        .lcd_cmd_bits = 8,
        .lcd_param_bits = 8,
        .spi_mode = 0,
        .trans_queue_depth = 10,
        .flags.sio_mode = 1,
    };
    ESP_RETURN_ON_ERROR(esp_lcd_new_panel_io_spi((esp_lcd_spi_bus_handle_t)RLCD_SPI_HOST, &io_cfg, &port->io),
                        "rlcd", "panel io");

    port->packed = heap_caps_malloc(RLCD_PACKED_BYTES, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if(port->packed == NULL) {
        port->packed = heap_caps_malloc(RLCD_PACKED_BYTES, MALLOC_CAP_DMA | MALLOC_CAP_8BIT);
    }
    ESP_RETURN_ON_FALSE(port->packed != NULL, ESP_ERR_NO_MEM, "rlcd", "frame buffer");
    memset(port->packed, 0xff, RLCD_PACKED_BYTES);

    hardware_reset(port);
    ESP_RETURN_ON_ERROR(panel_init_sequence(port), "rlcd", "init sequence");
    ESP_RETURN_ON_ERROR(set_window(port), "rlcd", "window before first frame");
    ESP_RETURN_ON_ERROR(esp_lcd_panel_io_tx_color(port->io, 0x2c, port->packed, RLCD_PACKED_BYTES),
                        "rlcd", "initial flush");
    *out_port = port;
    return ESP_OK;
}

esp_err_t rlcd_port_draw(rlcd_port_t *port, const uint8_t *packed, uint32_t len)
{
    ESP_RETURN_ON_FALSE(port != NULL && packed != NULL, ESP_ERR_INVALID_ARG, "rlcd", "invalid frame");
    ESP_RETURN_ON_FALSE(len == RLCD_PACKED_BYTES, ESP_ERR_INVALID_SIZE, "rlcd", "wrong frame size");
    memcpy(port->packed, packed, len);
    ESP_RETURN_ON_ERROR(set_window(port), "rlcd", "set window");
    return esp_lcd_panel_io_tx_color(port->io, 0x2c, port->packed, len);
}

esp_err_t rlcd_port_deinit(rlcd_port_t *port)
{
    if(port == NULL) return ESP_OK;
    if(port->io) esp_lcd_panel_io_del(port->io);
    spi_bus_free(RLCD_SPI_HOST);
    free(port->packed);
    free(port);
    return ESP_OK;
}
