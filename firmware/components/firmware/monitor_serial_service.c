#include "monitor_serial_service.h"
#include "monitor_serial_wire.h"
#include "monitor_serial_proto.h"
#include "monitor_lan_session.h"
#include "monitor_pairing_store.h"
#include "monitor_pairing_code.h"
#include "monitor_pairing_crypto.h"
#include "monitor_pairing_service.h"
#include "monitor_companion_config.h"
#include "monitor_companion_mqtt.h"
#include "monitor_display_refresh.h"
#include "monitor_wifi_manager.h"
#include "esp_heap_caps.h"
#include "esp_log.h"
#include "esp_random.h"
#include "esp_rom_sys.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "driver/usb_serial_jtag.h"
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#define TAG "companion_serial"
#define SERIAL_TIMEOUT_MS 3000u
#define SERIAL_PAIR_OFFER_MS 5000u
#define SERIAL_PAIR_RESULT_MS 5000u
#define HEARTBEAT_MS 1000u
#define SERIAL_POLL_MS 20u
/* With no USB host the listener has nothing to time out, so the read wait
 * only paces host-attach detection. One-second chunks keep tickless idle in
 * Light Sleep instead of waking on the 20 ms companion cadence; received
 * bytes still wake the read immediately. */
#define SERIAL_DETACHED_POLL_MS 1000u
#define SERIAL_TASK_STACK 16384u
#define COMPANION_DEVICE_NAME "ESP32 Monitor"

typedef enum {
    SERIAL_IDLE,
    SERIAL_HELLO_SENT,
    SERIAL_PAIRING,
    SERIAL_AUTH_SENT,
    SERIAL_CONFIG_WAIT,
    SERIAL_RUNNING,
} serial_state_t;

typedef struct {
    monitor_serial_decoder_t *decoder;
    serial_state_t state;
    bool authenticated;
    bool frame_authenticated;
    bool pairing_ui;
    char device_id[37];
    char host_id[37];
    char pairing_id[37];
    char session_id[37];
    char host_nonce[44];
    char device_nonce[44];
    monitor_pairing_record_t pair;
    monitor_pairing_session_t pair_session;
    monitor_serial_keys_t keys;
    uint32_t tx_seq;
    uint32_t config_seq;
    uint64_t last_rx_ms;
    uint64_t last_heartbeat_ms;
    monitor_lan_session_t *lan;
    bool committed;
    esp32monitor_companion_Snapshot *pending_snapshot;
    esp32monitor_companion_Heartbeat *pending_heartbeat;
    esp32monitor_companion_Config config;
    bool heap_monitor;
    uint32_t heap_start_free;
    uint32_t heap_start_largest;
    uint32_t heap_min_largest;
    uint32_t heap_suspend_free;
    uint32_t heap_suspend_largest;
    uint32_t heap_resume_free;
    uint32_t heap_resume_largest;
} serial_ctx_t;

static uint32_t internal_free(void)
{
    return (uint32_t)heap_caps_get_free_size(MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
}

static uint32_t internal_largest(void)
{
    return (uint32_t)heap_caps_get_largest_free_block(MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
}

static void heap_sample(serial_ctx_t *ctx)
{
    if (!ctx || !ctx->authenticated) return;
    uint32_t largest = internal_largest();
    if (!ctx->heap_min_largest || largest < ctx->heap_min_largest) ctx->heap_min_largest = largest;
}

static portMUX_TYPE s_lock = portMUX_INITIALIZER_UNLOCKED;
static bool s_started, s_stop, s_revoke, s_active, s_committed, s_driver;
static SemaphoreHandle_t s_tx_lock;

static uint64_t monotonic_ms(void) { return (uint64_t)esp_timer_get_time() / 1000u; }

static void fill_uuid(char out[37])
{
    uint8_t b[16]; esp_fill_random(b, sizeof b);
    b[6] = (b[6] & 15) | 64; b[8] = (b[8] & 63) | 128;
    snprintf(out, 37, "%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
             b[0],b[1],b[2],b[3],b[4],b[5],b[6],b[7],b[8],b[9],b[10],b[11],b[12],b[13],b[14],b[15]);
}

static void fill_nonce(char out[44])
{
    static const char alphabet[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
    uint8_t raw[32]; esp_fill_random(raw, sizeof raw);
    size_t at = 0; unsigned accumulator = 0, bits = 0;
    for (unsigned i = 0; i < sizeof raw; i++) {
        accumulator = (accumulator << 8) | raw[i];
        bits += 8;
        while (bits >= 6) { out[at++] = alphabet[(accumulator >> (bits - 6)) & 63]; bits -= 6; }
    }
    if (bits) out[at++] = alphabet[(accumulator << (6 - bits)) & 63];
    out[at] = 0;
    memset(raw, 0, sizeof raw);
}

static bool equal_constant(const char *a, const char *b)
{
    size_t length = strlen(a);
    uint8_t difference = (uint8_t)(length ^ strlen(b));
    for (size_t i = 0; i < length; i++) difference |= (uint8_t)(a[i] ^ b[i]);
    return difference == 0;
}

static bool tx_bytes(const uint8_t *data, size_t length)
{
    if (!s_tx_lock || !data || !length) return false;
    xSemaphoreTake(s_tx_lock, portMAX_DELAY);
    size_t at = 0;
    /* A burst of human logs can fill the USB TX ring just before a frame.
     * Keep retrying partial/timeout writes under one bounded deadline so a
     * large config frame is not reported sent when the host saw a gap. */
    int64_t deadline = esp_timer_get_time() + 3000000;
    while (at < length) {
        int written = usb_serial_jtag_write_bytes(data + at, length - at, pdMS_TO_TICKS(1000));
        if (written > 0) {
            at += (size_t)written;
            continue;
        }
        if (esp_timer_get_time() >= deadline) break;
        vTaskDelay(pdMS_TO_TICKS(10));
    }
    xSemaphoreGive(s_tx_lock);
    return at == length;
}

/* One diagnostic line may need to reach USB before a Goodbye frame while the
 * session still owns the port. Normal logs stay on UART0 during a session. */
static void serial_usb_line(const char *line)
{
    if (!s_tx_lock || !line) return;
    size_t length = strlen(line);
    xSemaphoreTake(s_tx_lock, portMAX_DELAY);
    if (s_driver) (void)usb_serial_jtag_write_bytes(line, length, pdMS_TO_TICKS(200));
    esp_rom_printf("%s", line);
    xSemaphoreGive(s_tx_lock);
}

static bool send_frame(serial_ctx_t *ctx, uint8_t type, const uint8_t *payload, size_t length, bool authenticated)
{
    if (!ctx || ctx->tx_seq == UINT32_MAX) return false;
    uint32_t seq = ctx->tx_seq + 1;
    /* Size the frame exactly and keep it in internal RAM: the 16 KB maximum
     * in PSRAM made TX sensitive to external-memory cache behaviour. */
    size_t capacity = MONITOR_SERIAL_HEADER + length + (authenticated ? 32u : 0u) + 4u;
    uint8_t *frame = heap_caps_malloc(capacity, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (!frame) frame = malloc(capacity);
    if (!frame) return false;
    size_t written = 0;
    bool ok = monitor_serial_encode(type, seq, payload, length,
                                    authenticated ? ctx->keys.frame : NULL, 2,
                                    frame, capacity, &written) &&
              tx_bytes(frame, written);
    free(frame);
    if (ok) ctx->tx_seq = seq;
    return ok;
}

static bool send_envelope(serial_ctx_t *ctx, uint8_t type, const esp32monitor_companion_Envelope *envelope,
                          bool authenticated)
{
    uint8_t *buffer = heap_caps_malloc_prefer(8192, 2,
        MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    if (!buffer) return false;
    size_t length = 0;
    bool ok = monitor_serial_envelope_encode(envelope, buffer, 8192, &length) &&
              send_frame(ctx, type, buffer, length, authenticated);
    free(buffer);
    return ok;
}

static bool send_ack(serial_ctx_t *ctx, uint32_t ack_seq)
{
    esp32monitor_companion_Envelope envelope;
    memset(&envelope, 0, sizeof envelope);
    envelope.wire_version = 2;
    envelope.which_payload = esp32monitor_companion_Envelope_ack_tag;
    envelope.payload.ack.protocol_version = 1;
    strcpy(envelope.payload.ack.session_id, ctx->session_id);
    envelope.payload.ack.ack_seq = ack_seq;
    strcpy(envelope.payload.ack.status, "accepted");
    return send_envelope(ctx, MONITOR_SERIAL_ACK, &envelope, true);
}

static void pairing_end(serial_ctx_t *ctx)
{
    if (ctx->pairing_ui) {
        monitor_pairing_service_serial_end();
        ctx->pairing_ui = false;
    }
    memset(&ctx->pair_session, 0, sizeof ctx->pair_session);
}

static void pair_fail(serial_ctx_t *ctx, const char *code)
{
    esp32monitor_companion_Envelope envelope;
    memset(&envelope, 0, sizeof envelope);
    envelope.wire_version = 2;
    envelope.which_payload = esp32monitor_companion_Envelope_error_tag;
    envelope.payload.error.protocol_version = 1;
    ESP_LOGW(TAG, "serial pairing aborted: %s", code);
    (void)send_envelope(ctx, MONITOR_SERIAL_ERROR, &envelope, false);
}

static bool send_pair_request(serial_ctx_t *ctx, const char *step)
{
    esp32monitor_companion_Envelope envelope;
    memset(&envelope, 0, sizeof envelope);
    envelope.wire_version = 2;
    envelope.which_payload = esp32monitor_companion_Envelope_pair_request_tag;
    esp32monitor_companion_PairRequest *request = &envelope.payload.pair_request;
    request->protocol_version = 1;
    strcpy(request->step, step);
    fill_uuid(request->request_id);
    if (!strcmp(step, "begin")) {
        request->has_device_id = request->has_device_name = request->has_device_nonce = true;
        strcpy(request->device_id, ctx->device_id);
        strcpy(request->device_name, COMPANION_DEVICE_NAME);
        strcpy(request->device_nonce, ctx->device_nonce);
    } else {
        if (!ctx->pair_session.initial.pairing_id[0]) return false;
        request->has_pairing_id = request->has_pairing_ticket = true;
        strcpy(request->pairing_id, ctx->pair_session.initial.pairing_id);
        strcpy(request->pairing_ticket, ctx->pair_session.initial.pairing_ticket);
        request->has_device_confirmed = true;
        request->device_confirmed = !strcmp(step, "confirm");
    }
    return send_envelope(ctx, MONITOR_SERIAL_PAIR, &envelope, false);
}

static void end_session(serial_ctx_t *ctx, const char *reason, bool goodbye, bool revoke)
{
    bool was_authenticated = ctx->authenticated;
    if (was_authenticated) {
        /* Log before the Goodbye frame: the host closes its reader as soon as
         * it sees Goodbye, so a post-close summary would never reach USB. */
        uint32_t session_min_free = 0;
        if (ctx->heap_monitor) {
            session_min_free = (uint32_t)heap_caps_get_minimum_free_size(
                MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
            (void)heap_caps_monitor_local_minimum_free_size_stop();
            ctx->heap_monitor = false;
        }
        char line[320];
        int length = snprintf(line, sizeof line,
            "I (%lu) companion_serial: serial heap close: start free=%u largest=%u "
            "session_min_free=%u session_min_largest=%u suspend free=%u largest=%u "
            "now free=%u largest=%u psram_free=%u\n",
            (unsigned long)esp_log_timestamp(),
            (unsigned)ctx->heap_start_free, (unsigned)ctx->heap_start_largest,
            (unsigned)session_min_free, (unsigned)ctx->heap_min_largest,
            (unsigned)ctx->heap_suspend_free, (unsigned)ctx->heap_suspend_largest,
            (unsigned)internal_free(), (unsigned)internal_largest(),
            (unsigned)heap_caps_get_free_size(MALLOC_CAP_SPIRAM));
        if (length > 0) serial_usb_line(line);
    }
    pairing_end(ctx);
    if (ctx->lan && goodbye && ctx->authenticated)
        (void)monitor_lan_goodbye(ctx->lan, reason, time(NULL));
    if (ctx->lan) monitor_lan_destroy(ctx->lan);
    ctx->lan = NULL;
    if (ctx->committed) monitor_companion_sink_connection(false, true);
    free(ctx->pending_snapshot); ctx->pending_snapshot = NULL;
    free(ctx->pending_heartbeat); ctx->pending_heartbeat = NULL;
    if (revoke) {
        esp_err_t error = monitor_pairing_store_revoke();
        if (error == ESP_OK) ESP_LOGI(TAG, "pairing record revoked by host");
        else ESP_LOGE(TAG, "pairing record revoke failed: %s", esp_err_to_name(error));
    }
    memset(&ctx->keys, 0, sizeof ctx->keys);
    memset(&ctx->pair, 0, sizeof ctx->pair);
    memset(&ctx->config, 0, sizeof ctx->config);
    if (ctx->decoder) monitor_serial_decoder_init(ctx->decoder);
    ctx->authenticated = false;
    ctx->state = SERIAL_IDLE;
    ctx->session_id[0] = ctx->host_nonce[0] = ctx->device_nonce[0] = 0;
    portENTER_CRITICAL(&s_lock); s_active = false; s_committed = false; s_revoke = false; portEXIT_CRITICAL(&s_lock);
    monitor_display_refresh_request_reason(MONITOR_REFRESH_RECOVER);
    /* The serial link no longer owns the monitor: bring Wi-Fi back and let
     * clock/weather/Devices producers and LAN discovery resume. */
    if (ctx->committed) (void)monitor_wifi_resume();
    ctx->committed = false;
    ctx->heap_resume_free = internal_free();
    ctx->heap_resume_largest = internal_largest();
    if (was_authenticated) {
        ESP_LOGI(TAG, "serial heap after close: free=%u largest=%u",
                 (unsigned)ctx->heap_resume_free, (unsigned)ctx->heap_resume_largest);
    }
    ESP_LOGW(TAG, "session ended: %s", reason);
}

static bool send_heartbeat(serial_ctx_t *ctx, uint64_t now)
{
    esp32monitor_companion_Envelope envelope;
    memset(&envelope, 0, sizeof envelope);
    envelope.wire_version = 2;
    envelope.which_payload = esp32monitor_companion_Envelope_heartbeat_tag;
    esp32monitor_companion_Heartbeat *heartbeat = &envelope.payload.heartbeat;
    heartbeat->protocol_version = 1;
    strcpy(heartbeat->host_id, ctx->host_id);
    strcpy(heartbeat->device_id, ctx->device_id);
    strcpy(heartbeat->session_id, ctx->session_id);
    heartbeat->seq = ctx->tx_seq + 1;
    heartbeat->sent_at = (int64_t)time(NULL);
    heartbeat->has_last_snapshot_seq = true;
    heartbeat->last_snapshot_seq = monitor_lan_snapshot_seq(ctx->lan);
    heartbeat->has_config_epoch = true;
    strcpy(heartbeat->config_epoch, ctx->config.config_epoch);
    heartbeat->has_config_revision = true;
    heartbeat->config_revision = ctx->config.config_revision;
    if (!send_envelope(ctx, MONITOR_SERIAL_HEARTBEAT, &envelope, true)) return false;
    ctx->last_heartbeat_ms = now;
    return true;
}

static bool send_config(serial_ctx_t *ctx)
{
    uint8_t aad[128], nonce[12];
    size_t aad_length = 0;
    if (!monitor_serial_config_aad(ctx->session_id, ctx->config.request_id, aad, sizeof aad, &aad_length))
        return false;
    uint32_t seq = ctx->tx_seq + 1;
    if (!monitor_serial_config_nonce(seq, nonce)) return false;

    /* CPU-only encoding buffers prefer PSRAM; USB frames stay internal in send_frame(). */
    enum { PLAIN_CAP = 2048, SEALED_CAP = PLAIN_CAP + 16, INNER_CAP = 2560, WRAPPED_CAP = 2688 };
    uint8_t *buffers = heap_caps_malloc_prefer(WRAPPED_CAP, 2,
        MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    uint8_t *plain = heap_caps_malloc_prefer(PLAIN_CAP, 2,
        MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    uint8_t *sealed = heap_caps_malloc_prefer(SEALED_CAP, 2,
        MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    uint8_t *inner = heap_caps_malloc_prefer(INNER_CAP, 2,
        MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
    heap_sample(ctx);
    if (!buffers || !plain || !sealed || !inner) {
        free(buffers); free(plain); free(sealed); free(inner);
        return false;
    }
    esp32monitor_companion_Envelope envelope;
    memset(&envelope, 0, sizeof envelope);
    envelope.wire_version = 2;
    envelope.which_payload = esp32monitor_companion_Envelope_config_tag;
    envelope.payload.config = ctx->config;
    size_t plain_length = 0;
    bool ok = monitor_serial_envelope_encode(&envelope, plain, PLAIN_CAP, &plain_length) && plain_length <= 4080;
    if (ok) {
        size_t sealed_length = 0;
        ok = monitor_serial_aead_encrypt(ctx->keys.config, nonce, aad, aad_length, plain, plain_length,
                                         sealed, SEALED_CAP, &sealed_length) &&
             sealed_length == plain_length + 16;
        if (ok) {
            monitor_serial_encrypted_t encrypted;
            memset(&encrypted, 0, sizeof encrypted);
            encrypted.protocol_version = 1;
            strcpy(encrypted.session_id, ctx->session_id);
            strcpy(encrypted.request_id, ctx->config.request_id);
            strcpy(encrypted.algorithm, "aes-256-gcm");
            encrypted.nonce_length = 12; memcpy(encrypted.nonce, nonce, 12);
            encrypted.ciphertext_length = sealed_length - 16;
            memcpy(encrypted.ciphertext, sealed, sealed_length - 16);
            encrypted.tag_length = 16;
            memcpy(encrypted.tag, sealed + sealed_length - 16, 16);
            size_t inner_length = 0, wrapped_length = 0;
            ok = monitor_serial_encrypted_encode(&encrypted, inner, INNER_CAP, &inner_length) &&
                 monitor_serial_envelope_wrap(MONITOR_SERIAL_FIELD_ENCRYPTED_CONFIG, inner, inner_length,
                                              buffers, WRAPPED_CAP, &wrapped_length);
            memset(&encrypted, 0, sizeof encrypted);
            if (ok) {
                ctx->config_seq = seq;
                ok = send_frame(ctx, MONITOR_SERIAL_CONFIG, buffers, wrapped_length, true);
            }
        }
    }
    memset(plain, 0, PLAIN_CAP);
    memset(sealed, 0, SEALED_CAP);
    memset(inner, 0, INNER_CAP);
    memset(buffers, 0, WRAPPED_CAP);
    free(buffers); free(plain); free(sealed); free(inner);
    return ok;
}

static bool serial_send(void *user, const char *suffix, const uint8_t *payload, size_t length, unsigned qos)
{
    (void)qos;
    serial_ctx_t *ctx = user;
    if (!suffix || !payload || !length) return false;
    if (!strcmp(suffix, "ack")) return send_frame(ctx, MONITOR_SERIAL_ACK, payload, length, true);
    if (!strcmp(suffix, "goodbye")) return send_frame(ctx, MONITOR_SERIAL_GOODBYE, payload, length, true);
    if (!strcmp(suffix, "resync")) {
        ESP_LOGW(TAG, "resync unsupported on serial");
        return true;
    }
    return false;
}

static bool serial_stage_apply(void *user, const esp32monitor_companion_Snapshot *snapshot,
                               const esp32monitor_companion_Heartbeat *heartbeat)
{
    serial_ctx_t *ctx = user;
    if (ctx->committed) return monitor_companion_sink_apply(user, snapshot, heartbeat);
    if (snapshot) {
        if (!ctx->pending_snapshot) ctx->pending_snapshot = heap_caps_malloc_prefer(sizeof *snapshot, 2,
            MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
        if (!ctx->pending_snapshot) return false;
        *ctx->pending_snapshot = *snapshot;
    }
    if (heartbeat) {
        if (!ctx->pending_heartbeat) ctx->pending_heartbeat = heap_caps_malloc_prefer(sizeof *heartbeat, 2,
            MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT, MALLOC_CAP_INTERNAL | MALLOC_CAP_8BIT);
        if (!ctx->pending_heartbeat) return false;
        *ctx->pending_heartbeat = *heartbeat;
    }
    return true;
}

static bool start_application_session(serial_ctx_t *ctx, uint64_t now)
{
    memset(&ctx->config, 0, sizeof ctx->config);
    ctx->config.protocol_version = 1;
    strcpy(ctx->config.device_id, ctx->device_id);
    strcpy(ctx->config.session_id, ctx->session_id);
    fill_uuid(ctx->config.request_id);
    fill_uuid(ctx->config.config_epoch);
    ctx->config.config_revision = 1;
    if (monitor_companion_config_read(&ctx->config) != ESP_OK) {
        ESP_LOGE(TAG, "config read failed");
        return false;
    }
    ctx->lan = monitor_lan_create_serial(ctx->host_id, ctx->device_id, ctx->pairing_id, ctx->session_id,
                                         &ctx->config, serial_send, serial_stage_apply, ctx, now);
    if (!ctx->lan) {
        ESP_LOGE(TAG, "serial session create failed");
        return false;
    }
    if (!send_config(ctx)) {
        ESP_LOGE(TAG, "config send failed");
        return false;
    }
    heap_sample(ctx);
    ctx->state = SERIAL_CONFIG_WAIT;
    /* Config sealing takes time; stamp the watchdog with the clock after those
     * steps so the session is not born already expired. */
    ctx->last_rx_ms = monotonic_ms();
    ESP_LOGI(TAG, "authenticated serial session; encrypted config sent");
    return true;
}

static void handle_hello(serial_ctx_t *ctx, const uint8_t *payload, size_t length, uint64_t now)
{
    if (ctx->authenticated || ctx->state == SERIAL_CONFIG_WAIT || ctx->state == SERIAL_RUNNING) return;
    /* A probe (HELLO without AUTH) must not lock the port: a fresh HELLO from
     * the same or a new host restarts the unauthenticated handshake. */
    if (ctx->state != SERIAL_IDLE) {
        pairing_end(ctx);
        memset(&ctx->keys, 0, sizeof ctx->keys);
        memset(&ctx->pair, 0, sizeof ctx->pair);
        memset(&ctx->config, 0, sizeof ctx->config);
        ctx->session_id[0] = ctx->host_nonce[0] = ctx->device_nonce[0] = 0;
        ctx->authenticated = false;
        ctx->state = SERIAL_IDLE;
    }
    uint32_t field; const uint8_t *inner; size_t inner_length;
    monitor_serial_hello_t hello;
    if (!monitor_serial_envelope_payload(payload, length, &field, &inner, &inner_length) ||
        field != MONITOR_SERIAL_FIELD_HELLO || !monitor_serial_hello_decode(inner, inner_length, &hello) ||
        hello.protocol_version != 1 || strcmp(hello.step, "request") || !hello.supports_v1 ||
        !monitor_companion_valid_uuid(hello.host_id)) return;
    uint8_t raw[32];
    if (!monitor_companion_decode_nonce(hello.host_nonce, raw)) return;

    monitor_pairing_record_t pair;
    bool paired = monitor_pairing_store_load(&pair) == ESP_OK;
    if (paired && strcmp(pair.host_id, hello.host_id)) {
        ESP_LOGW(TAG, "hello ignored: host mismatch");
        memset(&pair, 0, sizeof pair);
        return;
    }
    if (monitor_pairing_identity(ctx->device_id) != ESP_OK) {
        memset(&pair, 0, sizeof pair);
        return;
    }
    fill_nonce(ctx->device_nonce);
    strcpy(ctx->host_id, hello.host_id);
    strcpy(ctx->host_nonce, hello.host_nonce);
    ctx->pairing_id[0] = 0;
    if (paired) strcpy(ctx->pairing_id, pair.pairing_id);
    ctx->pair = pair;
    memset(&pair, 0, sizeof pair);

    monitor_serial_hello_out_t response;
    memset(&response, 0, sizeof response);
    response.protocol_version = 1;
    response.host_id = ctx->host_id;
    response.host_name = hello.host_name;
    response.host_nonce = ctx->host_nonce;
    response.device_id = ctx->device_id;
    response.device_name = COMPANION_DEVICE_NAME;
    response.firmware_version = "1.0.0";
    response.device_nonce = ctx->device_nonce;
    response.pairing_id = ctx->pairing_id[0] ? ctx->pairing_id : NULL;
    response.max_payload_bytes = 16368;
    uint8_t inner_out[1024], wrapped[1100];
    size_t inner_size = 0, wrapped_size = 0;
    if (!monitor_serial_hello_encode(&response, inner_out, sizeof inner_out, &inner_size) ||
        !monitor_serial_envelope_wrap(MONITOR_SERIAL_FIELD_HELLO, inner_out, inner_size,
                                      wrapped, sizeof wrapped, &wrapped_size) ||
        !send_frame(ctx, MONITOR_SERIAL_HELLO, wrapped, wrapped_size, false)) {
        ESP_LOGE(TAG, "hello response failed");
        return;
    }
    ctx->state = SERIAL_HELLO_SENT;
    ctx->last_rx_ms = now;
    ESP_LOGI(TAG, "hello from host; paired=%d", paired ? 1 : 0);
    if (!paired) {
        /* A probe reads only the HELLO response. Pairing starts without a
         * popup and waits briefly; the code appears only after the host's
         * pending response, so probing cannot lock the device. */
        if (!send_pair_request(ctx, "begin")) {
            ESP_LOGW(TAG, "pairing request failed");
            return;
        }
        ctx->state = SERIAL_PAIRING;
        ctx->last_rx_ms = monotonic_ms();
    }
}

static void handle_auth(serial_ctx_t *ctx, uint32_t seq, const uint8_t *payload, size_t length, uint64_t now)
{
    uint32_t field; const uint8_t *inner; size_t inner_length;
    monitor_serial_auth_t auth;
    if (!monitor_serial_envelope_payload(payload, length, &field, &inner, &inner_length) ||
        field != MONITOR_SERIAL_FIELD_AUTH || !monitor_serial_auth_decode(inner, inner_length, &auth) ||
        auth.protocol_version != 1) return;

    if (ctx->state == SERIAL_HELLO_SENT && !strcmp(auth.step, "challenge")) {
        if (strcmp(auth.host_id, ctx->host_id) || strcmp(auth.device_id, ctx->device_id) ||
            strcmp(auth.host_nonce, ctx->host_nonce) || strcmp(auth.device_nonce, ctx->device_nonce) ||
            !ctx->pairing_id[0] || strcmp(auth.pairing_id, ctx->pairing_id) ||
            !monitor_companion_valid_uuid(auth.session_id)) {
            ESP_LOGW(TAG, "auth challenge rejected");
            return;
        }
        if (!monitor_serial_derive_keys(auth.host_id, auth.device_id, auth.pairing_id, auth.session_id,
                                        auth.host_nonce, auth.device_nonce, ctx->pair.serial_key, &ctx->keys)) {
            ESP_LOGW(TAG, "auth key derivation failed");
            return;
        }
        strcpy(ctx->session_id, auth.session_id);
        monitor_serial_auth_out_t response;
        memset(&response, 0, sizeof response);
        response.step = "response";
        response.host_id = auth.host_id;
        response.device_id = auth.device_id;
        response.pairing_id = auth.pairing_id;
        response.session_id = auth.session_id;
        response.host_nonce = auth.host_nonce;
        response.device_nonce = auth.device_nonce;
        response.proof = ctx->keys.device_proof;
        uint8_t inner_out[1024], wrapped[1100];
        size_t inner_size = 0, wrapped_size = 0;
        if (!monitor_serial_auth_encode(&response, inner_out, sizeof inner_out, &inner_size) ||
            !monitor_serial_envelope_wrap(MONITOR_SERIAL_FIELD_AUTH, inner_out, inner_size,
                                          wrapped, sizeof wrapped, &wrapped_size) ||
            !send_frame(ctx, MONITOR_SERIAL_AUTH, wrapped, wrapped_size, false)) {
            ESP_LOGE(TAG, "auth response failed");
            end_session(ctx, "auth", false, false);
            return;
        }
        ctx->state = SERIAL_AUTH_SENT;
        ctx->last_rx_ms = monotonic_ms();
        return;
    }

    if (ctx->state == SERIAL_AUTH_SENT && !strcmp(auth.step, "result")) {
        if (strcmp(auth.host_id, ctx->host_id) || strcmp(auth.device_id, ctx->device_id) ||
            strcmp(auth.pairing_id, ctx->pairing_id) || strcmp(auth.session_id, ctx->session_id) ||
            strcmp(auth.host_nonce, ctx->host_nonce) || strcmp(auth.device_nonce, ctx->device_nonce) ||
            !auth.has_proof || !auth.has_accepted || !auth.accepted ||
            !equal_constant(auth.proof, ctx->keys.host_proof)) {
            ESP_LOGW(TAG, "host proof rejected");
            end_session(ctx, "unauthorized", false, false);
            return;
        }
        ctx->authenticated = true;
        ctx->heap_start_free = internal_free();
        ctx->heap_start_largest = internal_largest();
        ctx->heap_min_largest = ctx->heap_start_largest;
        ctx->heap_monitor = heap_caps_monitor_local_minimum_free_size_start() == ESP_OK;
        /* Once the host is authenticated the port carries only frames; logs
         * stay on UART0 so console bursts cannot split a config or snapshot. */
        portENTER_CRITICAL(&s_lock); s_active = true; portEXIT_CRITICAL(&s_lock);
        monitor_display_refresh_request_reason(MONITOR_REFRESH_RECOVER);
        /* Boot logs may still sit in the USB TX ring. Drain them before the
         * authenticated frame burst so the host sees an unbroken config. */
        (void)usb_serial_jtag_wait_tx_done(pdMS_TO_TICKS(2000));
        if (!send_ack(ctx, seq)) {
            end_session(ctx, "ack", false, false);
            return;
        }
        if (!start_application_session(ctx, now)) end_session(ctx, "config", false, false);
    }
}

static void handle_pair(serial_ctx_t *ctx, const uint8_t *payload, size_t length, uint64_t now)
{
    if (ctx->state != SERIAL_PAIRING) return;
    esp32monitor_companion_Envelope envelope;
    if (!monitor_serial_envelope_decode(&envelope, payload, length) ||
        envelope.which_payload != esp32monitor_companion_Envelope_pair_response_tag) {
        pairing_end(ctx);
        pair_fail(ctx, "invalid_message");
        ctx->state = SERIAL_IDLE;
        return;
    }
    esp32monitor_companion_PairResponse *response = &envelope.payload.pair_response;
    if (ctx->pair_session.state == MONITOR_PAIR_IDLE) {
        if (!monitor_pairing_begin(&ctx->pair_session, response, ctx->device_nonce, ctx->device_id,
                                   ctx->host_id, now, monitor_pairing_sha256) ||
            monitor_pairing_service_serial_begin(ctx->pair_session.code) != ESP_OK) {
            pairing_end(ctx);
            pair_fail(ctx, "pairing_rejected");
            ctx->state = SERIAL_IDLE;
            return;
        }
        ctx->pairing_ui = true;
        ctx->last_rx_ms = now;
        ESP_LOGI(TAG, "serial pairing awaiting physical confirmation");
        return;
    }
    if (!monitor_pairing_apply(&ctx->pair_session, response, now)) {
        pairing_end(ctx);
        pair_fail(ctx, "pairing_rejected");
        ctx->state = SERIAL_IDLE;
        return;
    }
    ctx->last_rx_ms = now;
    if (ctx->pair_session.state == MONITOR_PAIR_REJECTED) {
        pairing_end(ctx);
        ctx->state = SERIAL_IDLE;
        ESP_LOGW(TAG, "serial pairing rejected by host");
        return;
    }
    if (ctx->pair_session.state != MONITOR_PAIR_APPROVED) return;
    monitor_pairing_record_t record;
    esp_err_t stored = monitor_pairing_store_approved(&ctx->pair_session);
    if (stored == ESP_OK) stored = monitor_pairing_store_load(&record);
    if (stored != ESP_OK) {
        ESP_LOGE(TAG, "serial pairing storage failed: %s", esp_err_to_name(stored));
        pairing_end(ctx);
        pair_fail(ctx, "storage_failed");
        ctx->state = SERIAL_IDLE;
        return;
    }
    ctx->pair = record;
    strcpy(ctx->pairing_id, record.pairing_id);
    memset(&record, 0, sizeof record);
    pairing_end(ctx);
    ctx->state = SERIAL_HELLO_SENT;
    ctx->last_rx_ms = now;
    ESP_LOGI(TAG, "serial pairing approved");
}

static void handle_config_ack(serial_ctx_t *ctx, const uint8_t *payload, size_t length, uint64_t now)
{
    esp32monitor_companion_Envelope envelope;
    if (!monitor_serial_envelope_decode(&envelope, payload, length) ||
        envelope.which_payload != esp32monitor_companion_Envelope_config_ack_tag) {
        end_session(ctx, "config_ack", true, false);
        return;
    }
    esp32monitor_companion_ConfigAck *ack = &envelope.payload.config_ack;
    bool valid = ack->protocol_version == 1 && !strcmp(ack->device_id, ctx->device_id) &&
                 !strcmp(ack->session_id, ctx->session_id) && ack->has_request_id &&
                 !strcmp(ack->request_id, ctx->config.request_id) && ack->has_config_epoch &&
                 !strcmp(ack->config_epoch, ctx->config.config_epoch) && ack->has_config_revision &&
                 ack->config_revision == ctx->config.config_revision && !strcmp(ack->state, "applied") &&
                 ack->weather_configured == ctx->config.weather.enabled &&
                 ack->devices_configured == ctx->config.devices.enabled;
    if (!valid || ctx->state != SERIAL_CONFIG_WAIT || !monitor_lan_config_accepted(ctx->lan, now)) {
        ESP_LOGW(TAG, "config ack rejected");
        end_session(ctx, "config_ack", true, false);
        return;
    }
    ctx->state = SERIAL_RUNNING;
    ctx->last_rx_ms = now;
    if (!send_heartbeat(ctx, now)) {
        end_session(ctx, "heartbeat", true, false);
        return;
    }
    ESP_LOGI(TAG, "serial session proving");
}

static void handle_data(serial_ctx_t *ctx, const char *suffix, const uint8_t *payload, size_t length, uint64_t now)
{
    if (ctx->state != SERIAL_RUNNING || !ctx->lan ||
        !monitor_lan_receive(ctx->lan, suffix, payload, length, now, time(NULL))) {
        ESP_LOGW(TAG, "rejected %s len=%u state=%d", suffix, (unsigned)length, (int)ctx->state);
        end_session(ctx, "protocol", true, false);
        return;
    }
    ctx->last_rx_ms = now;
    if (!ctx->committed && monitor_lan_state(ctx->lan) == MONITOR_LAN_CONNECTED) {
        portENTER_CRITICAL(&s_lock); s_committed = true; portEXIT_CRITICAL(&s_lock);
        if (monitor_companion_mqtt_running()) {
            monitor_companion_mqtt_stop();
            if (!monitor_companion_mqtt_wait_stopped(2000)) {
                end_session(ctx, "lan_stop", true, false);
                return;
            }
        }
        ctx->committed = true;
        (void)monitor_companion_sink_apply(ctx, ctx->pending_snapshot, ctx->pending_heartbeat);
        free(ctx->pending_snapshot); ctx->pending_snapshot = NULL;
        free(ctx->pending_heartbeat); ctx->pending_heartbeat = NULL;
        ctx->heap_suspend_free = internal_free();
        ctx->heap_suspend_largest = internal_largest();
        if (monitor_wifi_suspend() != ESP_OK) ESP_LOGW(TAG, "Wi-Fi suspend failed");
        ESP_LOGI(TAG, "serial session running");
    }
    if (ctx->committed && monitor_lan_state(ctx->lan) == MONITOR_LAN_CONNECTED)
        monitor_companion_sink_connection(true, false);
    heap_sample(ctx);
}

static void handle_ack(serial_ctx_t *ctx, const uint8_t *payload, size_t length, uint64_t now)
{
    esp32monitor_companion_Envelope envelope;
    if (!monitor_serial_envelope_decode(&envelope, payload, length) ||
        envelope.which_payload != esp32monitor_companion_Envelope_ack_tag) return;
    esp32monitor_companion_Ack *ack = &envelope.payload.ack;
    if (ack->protocol_version != 1 || strcmp(ack->session_id, ctx->session_id) ||
        (strcmp(ack->status, "accepted") && strcmp(ack->status, "duplicate"))) return;
    if (ack->ack_seq == ctx->config_seq) ctx->last_rx_ms = now;
}

static void handle_goodbye(serial_ctx_t *ctx, const uint8_t *payload, size_t length)
{
    esp32monitor_companion_Envelope envelope;
    if (!monitor_serial_envelope_decode(&envelope, payload, length) ||
        envelope.which_payload != esp32monitor_companion_Envelope_goodbye_tag) {
        end_session(ctx, "goodbye", false, false);
        return;
    }
    esp32monitor_companion_Goodbye *goodbye = &envelope.payload.goodbye;
    if (goodbye->protocol_version == 1 && !strncmp(goodbye->session_id, ctx->session_id, sizeof goodbye->session_id)) {
        bool revoked = !strcmp(goodbye->reason, "host_revoked");
        end_session(ctx, revoked ? "revoked" : "goodbye", false, revoked);
    } else {
        end_session(ctx, "goodbye", false, false);
    }
}

static void on_frame(void *user, const monitor_serial_frame_t *frame)
{
    serial_ctx_t *ctx = user;
    if (!ctx || !frame) return;
    bool secure = ctx->frame_authenticated;
    uint64_t now = monotonic_ms();
    if (frame->type == MONITOR_SERIAL_HELLO) {
        if (!secure) handle_hello(ctx, frame->payload, frame->length, now);
        return;
    }
    if (frame->type == MONITOR_SERIAL_AUTH) {
        if (!secure) handle_auth(ctx, frame->seq, frame->payload, frame->length, now);
        return;
    }
    if (frame->type == MONITOR_SERIAL_PAIR) {
        if (!secure) handle_pair(ctx, frame->payload, frame->length, now);
        return;
    }
    if (!ctx->authenticated || !secure) {
        ESP_LOGW(TAG, "rejected unauthenticated frame type=%u", frame->type);
        return;
    }
    switch (frame->type) {
    case MONITOR_SERIAL_CONFIG_ACK: handle_config_ack(ctx, frame->payload, frame->length, now); break;
    case MONITOR_SERIAL_SNAPSHOT: handle_data(ctx, "snapshot", frame->payload, frame->length, now); break;
    case MONITOR_SERIAL_HEARTBEAT: handle_data(ctx, "heartbeat", frame->payload, frame->length, now); break;
    case MONITOR_SERIAL_ACK: handle_ack(ctx, frame->payload, frame->length, now); break;
    case MONITOR_SERIAL_GOODBYE: handle_goodbye(ctx, frame->payload, frame->length); break;
    case MONITOR_SERIAL_ERROR:
        ESP_LOGW(TAG, "peer error frame len=%u", (unsigned)frame->length);
        end_session(ctx, "error", true, false);
        break;
    default:
        break;
    }
}

static void tick(serial_ctx_t *ctx, uint64_t now)
{
    bool revoke;
    portENTER_CRITICAL(&s_lock); revoke = s_revoke; portEXIT_CRITICAL(&s_lock);
    if (revoke) {
        if (ctx->authenticated) end_session(ctx, "revoked", true, false);
        else { portENTER_CRITICAL(&s_lock); s_revoke = false; portEXIT_CRITICAL(&s_lock); }
        return;
    }
    if (ctx->state == SERIAL_IDLE) return;
    /* Frame callbacks stamp last_rx_ms with their own (slightly newer) clock
     * reading than this loop's `now`; a plain subtraction would underflow. */
    bool expired = (ctx->authenticated || ctx->state == SERIAL_HELLO_SENT || ctx->state == SERIAL_AUTH_SENT) &&
                   now >= ctx->last_rx_ms && now - ctx->last_rx_ms >= SERIAL_TIMEOUT_MS;
    if (expired) {
        if (ctx->authenticated) { end_session(ctx, "timeout", true, false); return; }
        memset(&ctx->keys, 0, sizeof ctx->keys);
        memset(&ctx->pair, 0, sizeof ctx->pair);
        memset(&ctx->config, 0, sizeof ctx->config);
        ctx->session_id[0] = ctx->host_nonce[0] = ctx->device_nonce[0] = 0;
        ctx->state = SERIAL_IDLE;
        monitor_serial_decoder_init(ctx->decoder);
        ESP_LOGW(TAG, "handshake timed out");
        return;
    }
    if (ctx->state == SERIAL_PAIRING) {
        monitor_pairing_action_t action = monitor_pairing_service_serial_action();
        if (action == MONITOR_PAIRING_ACTION_CANCEL) {
            pairing_end(ctx);
            pair_fail(ctx, "pairing_cancelled");
            ctx->state = SERIAL_IDLE;
            return;
        }
        if (action == MONITOR_PAIRING_ACTION_CONFIRM && ctx->pair_session.state == MONITOR_PAIR_WAITING) {
            if (!monitor_pairing_confirm(&ctx->pair_session, now) || !send_pair_request(ctx, "confirm")) {
                pairing_end(ctx);
                pair_fail(ctx, "pairing_failed");
                ctx->state = SERIAL_IDLE;
                return;
            }
            ctx->last_rx_ms = monotonic_ms();
        }
        monitor_pairing_tick(&ctx->pair_session, now);
        if (ctx->pair_session.state == MONITOR_PAIR_EXPIRED) {
            pairing_end(ctx);
            pair_fail(ctx, "pairing_expired");
            ctx->state = SERIAL_IDLE;
            return;
        }
        if (ctx->pair_session.state == MONITOR_PAIR_IDLE &&
            now >= ctx->last_rx_ms && now - ctx->last_rx_ms >= SERIAL_PAIR_OFFER_MS) {
            pairing_end(ctx);
            ctx->state = SERIAL_IDLE;
            ESP_LOGW(TAG, "pairing offer timed out");
        } else if (ctx->pair_session.state == MONITOR_PAIR_CONFIRMED &&
                   now >= ctx->last_rx_ms && now - ctx->last_rx_ms >= SERIAL_PAIR_RESULT_MS) {
            pairing_end(ctx);
            pair_fail(ctx, "pairing_expired");
            ctx->state = SERIAL_IDLE;
        }
        return;
    }
    if (ctx->state != SERIAL_RUNNING) return;
    if (now >= ctx->last_heartbeat_ms && now - ctx->last_heartbeat_ms >= HEARTBEAT_MS && !send_heartbeat(ctx, now)) {
        end_session(ctx, "heartbeat", true, false);
        return;
    }
    if (ctx->committed && ctx->lan && monitor_lan_state(ctx->lan) == MONITOR_LAN_CONNECTED)
        monitor_companion_sink_connection(true, false);
    if (ctx->lan && monitor_lan_state(ctx->lan) == MONITOR_LAN_CLOSED)
        end_session(ctx, "closed", false, false);
}

static int serial_log_vprintf(const char *format, va_list args)
{
    char buffer[256];
    va_list copy;
    va_copy(copy, args);
    int length = vsnprintf(buffer, sizeof buffer, format, copy);
    va_end(copy);
    if (length < 0) return length;
    size_t used = (size_t)length < sizeof buffer ? (size_t)length : sizeof buffer - 1;
    portENTER_CRITICAL(&s_lock); bool active = s_active; portEXIT_CRITICAL(&s_lock);
    if (s_tx_lock) xSemaphoreTake(s_tx_lock, portMAX_DELAY);
    if (s_driver && !active) {
        /* The driver write alone covers USB monitoring. esp_rom_printf is
         * deliberately not called here: its ROM formatter rejects precision
         * specs, and on ESP32-S3 every ROM console write also reaches the
         * USB Serial/JTAG secondary console (ESP_ROM_CONSOLE_OUTPUT_SECONDARY),
         * which would duplicate every line on USB. */
        (void)usb_serial_jtag_write_bytes(buffer, used, pdMS_TO_TICKS(50));
    } else {
        /* No driver path (install failed or a session owns the port): the ROM
         * console keeps UART0 alive, at the cost of the secondary USJ copy. */
        esp_rom_printf("%s", buffer);
    }
    if (s_tx_lock) xSemaphoreGive(s_tx_lock);
    return length;
}

static void serial_task(void *argument)
{
    (void)argument;
    serial_ctx_t *ctx = calloc(1, sizeof *ctx);
    if (ctx) ctx->decoder = calloc(1, sizeof *ctx->decoder);
    if (!ctx || !ctx->decoder) {
        ESP_LOGE(TAG, "serial session allocation failed");
        free(ctx);
        vTaskDelete(NULL);
        return;
    }
    ESP_LOGI(TAG, "USB Serial/JTAG listener ready");
    for (;;) {
        portENTER_CRITICAL(&s_lock); bool stop = s_stop; portEXIT_CRITICAL(&s_lock);
        if (stop) break;
        uint8_t buffer[512];
        TickType_t wait = pdMS_TO_TICKS(SERIAL_POLL_MS);
#if CONFIG_USJ_NO_AUTO_LS_ON_CONNECTION || CONFIG_ESP_CONSOLE_USB_SERIAL_JTAG_ENABLED
        if (!usb_serial_jtag_is_connected()) wait = pdMS_TO_TICKS(SERIAL_DETACHED_POLL_MS);
#endif
        int received = usb_serial_jtag_read_bytes(buffer, sizeof buffer, wait);
        uint64_t now = monotonic_ms();
        ctx->frame_authenticated = ctx->authenticated;
        const uint8_t *key = ctx->authenticated ? ctx->keys.frame : NULL;
        if (received > 0)
            (void)monitor_serial_feed(ctx->decoder, buffer, (size_t)received, now, key, 1, on_frame, ctx);
        (void)monitor_serial_tick(ctx->decoder, now, key, 1, on_frame, ctx);
        tick(ctx, now);
    }
    if (ctx->authenticated) end_session(ctx, "stop", true, false);
    memset(ctx->decoder, 0, sizeof *ctx->decoder);
    free(ctx->decoder);
    memset(ctx, 0, sizeof *ctx);
    free(ctx);
    vTaskDelete(NULL);
}

esp_err_t monitor_serial_start(void)
{
    portENTER_CRITICAL(&s_lock);
    bool started = s_started;
    portEXIT_CRITICAL(&s_lock);
    if (started) return ESP_OK;
    if (!s_tx_lock) s_tx_lock = xSemaphoreCreateMutex();
    if (!s_tx_lock) return ESP_ERR_NO_MEM;
    usb_serial_jtag_driver_config_t config = USB_SERIAL_JTAG_DRIVER_CONFIG_DEFAULT();
    config.rx_buffer_size = 4096;
    config.tx_buffer_size = 4096;
    esp_err_t error = usb_serial_jtag_driver_install(&config);
    if (error != ESP_OK) {
        ESP_LOGE(TAG, "USB Serial/JTAG driver install failed: %s", esp_err_to_name(error));
        return error;
    }
    s_driver = true;
    esp_log_set_vprintf(serial_log_vprintf);
    if (xTaskCreate(serial_task, "companion_serial", SERIAL_TASK_STACK, NULL, 3, NULL) != pdPASS) {
        ESP_LOGE(TAG, "serial task create failed");
        return ESP_ERR_NO_MEM;
    }
    portENTER_CRITICAL(&s_lock);
    s_started = true;
    s_stop = false;
    portEXIT_CRITICAL(&s_lock);
    return ESP_OK;
}

void monitor_serial_revoke(void)
{
    portENTER_CRITICAL(&s_lock); s_revoke = true; portEXIT_CRITICAL(&s_lock);
}

void monitor_serial_stop(void)
{
    portENTER_CRITICAL(&s_lock); s_stop = true; portEXIT_CRITICAL(&s_lock);
}

bool monitor_serial_active(void)
{
    portENTER_CRITICAL(&s_lock); bool active = s_committed; portEXIT_CRITICAL(&s_lock);
    return active;
}
