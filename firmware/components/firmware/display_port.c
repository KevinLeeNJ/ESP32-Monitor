#include "display_port.h"

#include <stdlib.h>
#include <string.h>

#include "driver/gpio.h"
#include "driver/spi_master.h"
#include "esp_check.h"
#include "esp_log.h"
#include "esp_attr.h"
#include "esp_pm.h"
#include "esp_lcd_io_spi.h"
#include "esp_lcd_panel_io.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "freertos/semphr.h"

#define RLCD_SPI_HOST SPI3_HOST
#define RLCD_SPI_CLOCK_HZ (10 * 1000 * 1000)
#define RLCD_PACKED_BYTES (RLCD_WIDTH * RLCD_HEIGHT / 8U)

struct rlcd_port {
    esp_lcd_panel_io_handle_t io;
    rlcd_pins_t pins;
    uint8_t *packed;
    uint8_t *staging;
    SemaphoreHandle_t tx_done;
    SemaphoreHandle_t frame_ready;
    bool te_enabled;
    bool animation_mode;
    bool mode_known;
    esp_pm_lock_handle_t awake_lock;
    bool awake_lock_held;
    bool tx_pending;
    bool recover_full;
};

static esp_err_t retain_panel_pins(const rlcd_pins_t *pins)
{
    /* The default sleep configuration isolates GPIOs. RST must remain high
     * after initialization, and CS/DC must not float during static display. */
    const int gpio[] = {pins->rst, pins->cs, pins->dc, pins->mosi, pins->sclk, pins->te};
    for (size_t i = 0; i < sizeof(gpio) / sizeof(gpio[0]); ++i) {
        if (gpio[i] >= 0) ESP_RETURN_ON_ERROR(gpio_sleep_sel_dis(gpio[i]), "rlcd", "retain panel GPIO");
    }
    return ESP_OK;
}

static esp_err_t set_panel_awake(rlcd_port_t *port, bool awake)
{
    if (port->awake_lock_held == awake) return ESP_OK;
    esp_err_t err = awake ? esp_pm_lock_acquire(port->awake_lock) : esp_pm_lock_release(port->awake_lock);
    if (err == ESP_OK) port->awake_lock_held = awake;
    return err;
}

esp_lcd_panel_io_handle_t rlcd_port_panel_io(rlcd_port_t *port)
{
    return port ? port->io : NULL;
}

static void IRAM_ATTR frame_ready_isr(void *ctx)
{
    rlcd_port_t *port = ctx;
    BaseType_t wake = pdFALSE;
    xSemaphoreGiveFromISR(port->frame_ready, &wake);
    if (wake) portYIELD_FROM_ISR();
}

static void set_frame_sync(rlcd_port_t *port, bool enabled)
{
    if (!port->frame_ready) return;
    gpio_intr_disable(port->pins.te);
    port->te_enabled = enabled;
    xSemaphoreTake(port->frame_ready, 0);
    if (enabled) gpio_intr_enable(port->pins.te);
}

bool rlcd_port_frame_sync_active(const rlcd_port_t *port)
{
    return port && port->animation_mode && port->te_enabled;
}

static void wait_panel_frame(rlcd_port_t *port)
{
    if (!rlcd_port_frame_sync_active(port)) return;
    /* Ignore an edge from before this frame was ready; start the write at the
     * next vertical scan boundary. Only one semaphore token can accumulate. */
    xSemaphoreTake(port->frame_ready, 0);
    if (xSemaphoreTake(port->frame_ready, pdMS_TO_TICKS(100))) return;
    set_frame_sync(port, false);
    ESP_LOGW("rlcd", "TE timeout; using nominal animation cadence until next HPM");
}

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

static esp_err_t set_window(rlcd_port_t *port, uint8_t xs, uint8_t xe, uint8_t ys, uint8_t ye)
{
    ESP_RETURN_ON_ERROR(send_cmd(port, 0x2a), "rlcd", "column command failed");
    ESP_RETURN_ON_ERROR(send_data1(port, xs), "rlcd", "column start failed");
    ESP_RETURN_ON_ERROR(send_data1(port, xe), "rlcd", "column end failed");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0x2b), "rlcd", "page command failed");
    ESP_RETURN_ON_ERROR(send_data1(port, ys), "rlcd", "page start failed");
    ESP_RETURN_ON_ERROR(send_data1(port, ye), "rlcd", "page end failed");
    return ESP_OK;
}

static bool color_done_cb(esp_lcd_panel_io_handle_t io, esp_lcd_panel_io_event_data_t *edata, void *ctx)
{
    (void)io; (void)edata;
    rlcd_port_t *port = (rlcd_port_t *)ctx;
    BaseType_t wake = pdFALSE;
    xSemaphoreGiveFromISR(port->tx_done, &wake);
    return wake == pdTRUE;
}

static esp_err_t wait_color_done(rlcd_port_t *port)
{
    return xSemaphoreTake(port->tx_done, pdMS_TO_TICKS(1000)) == pdTRUE ? ESP_OK : ESP_ERR_TIMEOUT;
}

static esp_err_t set_frame_rates(rlcd_port_t *port)
{
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xd8), "rlcd", "vcom");
    /* ST7305 OSCSET OSCSW=111: select the 32 Hz HPM oscillator. */
    ESP_RETURN_ON_ERROR(send_data(port, (const uint8_t[]){0xa6, 0xe9}, 2), "rlcd", "oscillator args");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xb2), "rlcd", "frame rate");
    /* FRCTRL HFRA=1: 32 Hz HPM; LFRA=2: 1 Hz LPM.
     * https://files.waveshare.com/wiki/common/ST_7305_V0_2.pdf pp.76,92 */
    ESP_RETURN_ON_ERROR(send_data1(port, 0x12), "rlcd", "frame rate arg");
    return ESP_OK;
}

static esp_err_t panel_init_sequence(rlcd_port_t *port)
{
    /* Panel voltages, NVM and waveforms follow the u8g2 reference supplied by
     * the project owner for flicker-free HPM/LPM switching. Keep these values
     * together; do not replace them with generic ST7305 datasheet defaults. */
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xd6), "rlcd", "nvm load");
    ESP_RETURN_ON_ERROR(send_data(port, (const uint8_t[]){0x13, 0x02}, 2), "rlcd", "nvm args");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xd1), "rlcd", "booster");
    ESP_RETURN_ON_ERROR(send_data1(port, 0x01), "rlcd", "booster arg");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xc0), "rlcd", "gate voltage");
    ESP_RETURN_ON_ERROR(send_data(port, (const uint8_t[]){0x12, 0x0a}, 2), "rlcd", "gate args");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xc1), "rlcd", "vshp");
    ESP_RETURN_ON_ERROR(send_data(port, (const uint8_t[]){0x3c, 0x3e, 0x3c, 0x3c}, 4), "rlcd", "vshp args");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xc2), "rlcd", "vslp");
    ESP_RETURN_ON_ERROR(send_data(port, (const uint8_t[]){0x23, 0x21, 0x23, 0x23}, 4), "rlcd", "vslp args");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xc4), "rlcd", "vshn");
    ESP_RETURN_ON_ERROR(send_data(port, (const uint8_t[]){0x5a, 0x5c, 0x5a, 0x5a}, 4), "rlcd", "vshn args");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xc5), "rlcd", "vsln");
    ESP_RETURN_ON_ERROR(send_data(port, (const uint8_t[]){0x37, 0x35, 0x37, 0x37}, 4), "rlcd", "vsln args");
    ESP_RETURN_ON_ERROR(set_frame_rates(port), "rlcd", "frame rate setup");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xb3), "rlcd", "waveform b3");
    ESP_RETURN_ON_ERROR(send_data(port, (const uint8_t[]){0xe5,0xf6,0x17,0x77,0x77,0x77,0x77,0x77,0x77,0x71}, 10),
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
    ESP_RETURN_ON_ERROR(set_window(port, 0x12, 0x2a, 0x00, 0xc7), "rlcd", "window");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0x35), "rlcd", "te");
    ESP_RETURN_ON_ERROR(send_data1(port, 0x00), "rlcd", "te arg");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0xd0), "rlcd", "d0");
    ESP_RETURN_ON_ERROR(send_data1(port, 0xff), "rlcd", "d0 arg");
    ESP_RETURN_ON_ERROR(rlcd_port_set_animation_mode(port, false), "rlcd", "initial low power mode (1 Hz)");
    ESP_RETURN_ON_ERROR(send_cmd(port, 0x29), "rlcd", "display on");
    port->mode_known = true;
    port->animation_mode = false;
    return ESP_OK;
}

esp_err_t rlcd_port_set_animation_mode(rlcd_port_t *port, bool active)
{
    ESP_RETURN_ON_FALSE(port != NULL, ESP_ERR_INVALID_ARG, "rlcd", "invalid port");
    if (port->mode_known && port->animation_mode == active) return ESP_OK;
    /* Protect commands and animation/TE waits. The panel settles independently
     * with TE disabled, so its full settling delays may use automatic sleep. */
    ESP_RETURN_ON_ERROR(set_panel_awake(port, true), "rlcd", "panel wake lock");
    set_frame_sync(port, false);
    const bool unknown = !port->mode_known;
    port->mode_known = false;
    /* ST7305 v0.2 section 7.11, p53. Recover an unknown state via HPM first. */
    if (active || unknown) {
        ESP_RETURN_ON_ERROR(send_cmd(port, 0x38), "rlcd", "HPM switch");
        ESP_RETURN_ON_ERROR(set_panel_awake(port, false), "rlcd", "HPM settle unlock");
        vTaskDelay(pdMS_TO_TICKS(300));
        /* Reacquire before enabling TE, or issuing the recovery LPM command. */
        ESP_RETURN_ON_ERROR(set_panel_awake(port, true), "rlcd", "HPM settle relock");
    }
    if (!active) {
        ESP_RETURN_ON_ERROR(send_cmd(port, 0x39), "rlcd", "LPM switch");
        ESP_RETURN_ON_ERROR(set_panel_awake(port, false), "rlcd", "LPM settle unlock");
        vTaskDelay(pdMS_TO_TICKS(100));
    }
    port->animation_mode = active;
    port->mode_known = true;
    set_frame_sync(port, active);
    ESP_LOGI("rlcd", "panel mode: %s", active ? "HPM 32 Hz (animation)" : "LPM 1 Hz (static)");
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
    ESP_RETURN_ON_ERROR(esp_pm_lock_create(ESP_PM_NO_LIGHT_SLEEP, 0, "rlcd_hpm", &port->awake_lock),
                        "rlcd", "panel wake lock creation");
    ESP_RETURN_ON_ERROR(retain_panel_pins(pins), "rlcd", "panel sleep GPIO configuration");

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

    port->tx_done = xSemaphoreCreateBinary();
    ESP_RETURN_ON_FALSE(port->tx_done != NULL, ESP_ERR_NO_MEM, "rlcd", "completion semaphore");
    if (pins->te >= 0) {
        port->frame_ready = xSemaphoreCreateBinary();
        ESP_RETURN_ON_FALSE(port->frame_ready != NULL, ESP_ERR_NO_MEM, "rlcd", "TE semaphore");
        gpio_config_t te_cfg = {.pin_bit_mask=1ULL << pins->te, .mode=GPIO_MODE_INPUT,
                                .intr_type=GPIO_INTR_POSEDGE};
        ESP_RETURN_ON_ERROR(gpio_config(&te_cfg), "rlcd", "TE gpio");
        gpio_intr_disable(pins->te);
        /* app_main installs the shared IRAM-safe GPIO ISR service once. */
        ESP_RETURN_ON_ERROR(gpio_isr_handler_add(pins->te, frame_ready_isr, port), "rlcd", "TE ISR handler");
    }

    esp_lcd_panel_io_spi_config_t io_cfg = {
        .dc_gpio_num = pins->dc,
        .cs_gpio_num = pins->cs,
        .pclk_hz = RLCD_SPI_CLOCK_HZ,
        .lcd_cmd_bits = 8,
        .lcd_param_bits = 8,
        .spi_mode = 0,
        .trans_queue_depth = 2,
        .on_color_trans_done = color_done_cb,
        .user_ctx = port,
        .flags.sio_mode = 1,
    };
    ESP_RETURN_ON_ERROR(esp_lcd_new_panel_io_spi((esp_lcd_spi_bus_handle_t)RLCD_SPI_HOST, &io_cfg, &port->io),
                        "rlcd", "panel io");

    port->packed = heap_caps_malloc(RLCD_PACKED_BYTES, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if(port->packed == NULL) {
        port->packed = heap_caps_malloc(RLCD_PACKED_BYTES, MALLOC_CAP_DMA | MALLOC_CAP_8BIT);
    }
    ESP_RETURN_ON_FALSE(port->packed != NULL, ESP_ERR_NO_MEM, "rlcd", "frame buffer");
    port->staging = heap_caps_malloc(RLCD_PACKED_BYTES, MALLOC_CAP_DMA | MALLOC_CAP_8BIT);
    ESP_RETURN_ON_FALSE(port->staging != NULL, ESP_ERR_NO_MEM, "rlcd", "staging buffer");
    memset(port->packed, 0xff, RLCD_PACKED_BYTES);

    hardware_reset(port);
    ESP_RETURN_ON_ERROR(panel_init_sequence(port), "rlcd", "init sequence");
    ESP_RETURN_ON_ERROR(set_window(port, 0x12, 0x2a, 0x00, 0xc7), "rlcd", "window before first frame");
    memcpy(port->staging, port->packed, RLCD_PACKED_BYTES);
    ESP_RETURN_ON_ERROR(esp_lcd_panel_io_tx_color(port->io, 0x2c, port->staging, RLCD_PACKED_BYTES),
                        "rlcd", "initial flush");
    ESP_RETURN_ON_ERROR(wait_color_done(port), "rlcd", "initial transfer timeout");
    *out_port = port;
    return ESP_OK;
}

/* A timed-out DMA still owns staging. Drain it before touching the buffer or
 * issuing another window; a late callback can never acknowledge a newer DMA. */
static esp_err_t submit_windows(rlcd_port_t *port, const uint8_t *packed,
                                const monitor_damage_plan_t *plan)
{
    if (port->tx_pending) {
        esp_err_t err = wait_color_done(port);
        if (err != ESP_OK) return err;
        port->tx_pending = false;
    }
    wait_panel_frame(port); /* Exactly once, including unchanged animation frames. */
    for (unsigned n = 0; n < plan->count; ++n) {
        monitor_window_t w = plan->windows[n];
        unsigned row_bytes = (w.t2 - w.t1 + 1U) * 3U, out = 0;
        for (unsigned p = w.p1; p <= w.p2; ++p) {
            memcpy(port->staging + out, packed + p * 75U + w.t1 * 3U, row_bytes);
            out += row_bytes;
        }
        esp_err_t err = set_window(port, (uint8_t)(0x2a - w.t2), (uint8_t)(0x2a - w.t1), w.p1, w.p2);
        if (err != ESP_OK) return err;
        err = esp_lcd_panel_io_tx_color(port->io, 0x2c, port->staging, out);
        if (err != ESP_OK) return err;
        port->tx_pending = true;
        err = wait_color_done(port);
        if (err != ESP_OK) return err;
        port->tx_pending = false;
        out = 0;
        for (unsigned p = w.p1; p <= w.p2; ++p) {
            memcpy(port->packed + p * 75U + w.t1 * 3U, port->staging + out, row_bytes);
            out += row_bytes;
        }
    }
    return ESP_OK;
}

esp_err_t rlcd_port_draw_dirty(rlcd_port_t *port, const uint8_t *packed, uint32_t len,
                               const monitor_damage_t *dirty, bool force_full)
{
    ESP_RETURN_ON_FALSE(port && packed && dirty, ESP_ERR_INVALID_ARG, "rlcd", "invalid frame");
    ESP_RETURN_ON_FALSE(len == RLCD_PACKED_BYTES, ESP_ERR_INVALID_SIZE, "rlcd", "wrong frame size");
    monitor_damage_plan_t plan;
    if (force_full || port->recover_full) {
        memset(&plan, 0, sizeof(plan));
        plan.count = 1;
        plan.bytes = RLCD_PACKED_BYTES;
        plan.windows[0] = (monitor_window_t){0,199,0,24};
    } else {
        monitor_damage_plan(dirty, packed, port->packed, &plan);
    }
    esp_err_t err = submit_windows(port, packed, &plan);
    port->recover_full = err != ESP_OK;
    return err;
}

esp_err_t rlcd_port_draw(rlcd_port_t *port, const uint8_t *packed, uint32_t len)
{
    const monitor_damage_t dirty = {0};
    return rlcd_port_draw_dirty(port, packed, len, &dirty, true);
}

esp_err_t rlcd_port_deinit(rlcd_port_t *port)
{
    if(port == NULL) return ESP_OK;
    if(port->frame_ready) {
        set_frame_sync(port, false);
        gpio_isr_handler_remove(port->pins.te);
        vSemaphoreDelete(port->frame_ready);
    }
    if(port->io) esp_lcd_panel_io_del(port->io);
    if(port->tx_done) vSemaphoreDelete(port->tx_done);
    if(port->awake_lock_held) esp_pm_lock_release(port->awake_lock);
    if(port->awake_lock) esp_pm_lock_delete(port->awake_lock);
    if(port->staging) heap_caps_free(port->staging);
    spi_bus_free(RLCD_SPI_HOST);
    heap_caps_free(port->packed);
    free(port);
    return ESP_OK;
}
