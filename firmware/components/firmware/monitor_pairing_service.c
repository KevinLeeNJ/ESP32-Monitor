#include "monitor_pairing_service.h"
#include "monitor_pairing_store.h"
#include "monitor_pairing_crypto.h"
#include "monitor_pairing_http.h"
#include "monitor_display_refresh.h"
#include "monitor_companion_mqtt.h"
#include "esp_random.h"
#include "esp_timer.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define PAIR_CONFIRM 1u
#define PAIR_CANCEL 2u

typedef struct { char ip[16]; uint16_t port; char host[37]; } offer_t;
static portMUX_TYPE s_mux = portMUX_INITIALIZER_UNLOCKED;
static TaskHandle_t s_task;
static bool s_busy;
static bool s_serial;
static monitor_pairing_action_t s_serial_action;
static monitor_pairing_view_t s_view;

static void publish(const char *code)
{
    portENTER_CRITICAL(&s_mux);
    s_view.visible = code != NULL;
    if (code) memcpy(s_view.code, code, sizeof s_view.code);
    else memset(s_view.code, 0, sizeof s_view.code);
    s_view.version++;
    portEXIT_CRITICAL(&s_mux);
    monitor_display_refresh_request_reason(MONITOR_REFRESH_RECOVER);
}

void monitor_pairing_service_view(monitor_pairing_view_t *out)
{
    if (!out) return;
    portENTER_CRITICAL(&s_mux);
    *out = s_view;
    portEXIT_CRITICAL(&s_mux);
}

bool monitor_pairing_service_action(bool confirm)
{
    portENTER_CRITICAL(&s_mux);
    TaskHandle_t task = s_task;
    bool serial = s_serial;
    bool active = s_view.visible && (task || serial);
    if (serial) s_serial_action = confirm ? MONITOR_PAIRING_ACTION_CONFIRM : MONITOR_PAIRING_ACTION_CANCEL;
    else if (active) xTaskNotify(task, confirm ? PAIR_CONFIRM : PAIR_CANCEL, eSetBits);
    portEXIT_CRITICAL(&s_mux);
    return active;
}

esp_err_t monitor_pairing_service_serial_begin(const char *code)
{
    if (!code || strlen(code) != 6) return ESP_ERR_INVALID_ARG;
    portENTER_CRITICAL(&s_mux);
    bool busy = s_busy || s_serial;
    if (!busy) { s_serial = true; s_serial_action = MONITOR_PAIRING_ACTION_NONE; }
    portEXIT_CRITICAL(&s_mux);
    if (busy) return ESP_ERR_INVALID_STATE;
    publish(code);
    return ESP_OK;
}

void monitor_pairing_service_serial_end(void)
{
    portENTER_CRITICAL(&s_mux);
    bool owned = s_serial;
    s_serial = false;
    s_serial_action = MONITOR_PAIRING_ACTION_NONE;
    portEXIT_CRITICAL(&s_mux);
    if (owned) publish(NULL);
}

monitor_pairing_action_t monitor_pairing_service_serial_action(void)
{
    portENTER_CRITICAL(&s_mux);
    monitor_pairing_action_t action = s_serial_action;
    s_serial_action = MONITOR_PAIRING_ACTION_NONE;
    portEXIT_CRITICAL(&s_mux);
    return action;
}

static void make_uuid(char out[37])
{
    uint8_t b[16]; esp_fill_random(b, sizeof b);
    b[6] = (b[6] & 15) | 64; b[8] = (b[8] & 63) | 128;
    snprintf(out, 37, "%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
             b[0],b[1],b[2],b[3],b[4],b[5],b[6],b[7],b[8],b[9],b[10],b[11],b[12],b[13],b[14],b[15]);
}

static void make_nonce(char out[44])
{
    static const char alphabet[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
    uint8_t bytes[32]; esp_fill_random(bytes, sizeof bytes);
    uint32_t value = 0; unsigned bits = 0, at = 0;
    for (unsigned i = 0; i < sizeof bytes; i++) {
        value = (value << 8) | bytes[i]; bits += 8;
        while (bits >= 6) { bits -= 6; out[at++] = alphabet[(value >> bits) & 63]; }
    }
    if (bits) out[at++] = alphabet[(value << (6 - bits)) & 63];
    out[at] = 0;
}

static uint64_t now_ms(void) { return (uint64_t)esp_timer_get_time() / 1000; }

static void pairing_task(void *arg)
{
    portENTER_CRITICAL(&s_mux);
    s_task = xTaskGetCurrentTaskHandle();
    portEXIT_CRITICAL(&s_mux);
    offer_t offer = *(offer_t *)arg;
    free(arg);
    monitor_pairing_session_t session = {0};
    esp32monitor_companion_PairRequest request = esp32monitor_companion_PairRequest_init_zero;
    esp32monitor_companion_PairResponse response = esp32monitor_companion_PairResponse_init_zero;
    char device[37], nonce[44];
    if (monitor_pairing_identity(device) != ESP_OK) goto done;
    make_nonce(nonce);
    request.protocol_version = 1;
    strcpy(request.step, "begin"); make_uuid(request.request_id);
    request.has_device_id = request.has_device_name = request.has_device_nonce = true;
    strcpy(request.device_id, device); strcpy(request.device_name, "ESP32 Monitor"); strcpy(request.device_nonce, nonce);
    esp_err_t exchange = monitor_pairing_http_exchange(offer.ip, offer.port, &request, &response);
    if (exchange != ESP_OK) { ESP_LOGW("monitor_pairing", "begin exchange failed: %s", esp_err_to_name(exchange)); goto done; }
    if (!monitor_pairing_begin(&session, &response, nonce, device, offer.host, now_ms(), monitor_pairing_sha256)) {
        ESP_LOGW("monitor_pairing", "begin response rejected"); goto done;
    }
    publish(session.code);
    for (;;) {
        uint32_t events = 0;
        xTaskNotifyWait(0, UINT32_MAX, &events, pdMS_TO_TICKS(1000));
        if (events & PAIR_CANCEL) { monitor_pairing_cancel(&session); break; }
        monitor_pairing_tick(&session, now_ms());
        if (session.state == MONITOR_PAIR_EXPIRED) break;
        if ((events & PAIR_CONFIRM) && session.state == MONITOR_PAIR_WAITING) monitor_pairing_confirm(&session, now_ms());
        request = (esp32monitor_companion_PairRequest)esp32monitor_companion_PairRequest_init_zero;
        request.protocol_version = 1; make_uuid(request.request_id);
        bool confirmed = session.state == MONITOR_PAIR_CONFIRMED;
        strcpy(request.step, confirmed ? "confirm" : "status");
        request.has_pairing_id = request.has_pairing_ticket = true;
        strcpy(request.pairing_id, session.initial.pairing_id);
        strcpy(request.pairing_ticket, session.initial.pairing_ticket);
        request.has_device_confirmed = confirmed; request.device_confirmed = confirmed;
        if (monitor_pairing_http_exchange(offer.ip, offer.port, &request, &response) != ESP_OK) break;
        /* A cancellation received while HTTP was running must win over approval. */
        events = 0; xTaskNotifyWait(0, UINT32_MAX, &events, 0);
        if (events & PAIR_CANCEL) { monitor_pairing_cancel(&session); break; }
        if ((events & PAIR_CONFIRM) && session.state == MONITOR_PAIR_WAITING) monitor_pairing_confirm(&session, now_ms());
        if (!monitor_pairing_apply(&session, &response, now_ms())) break;
        if (session.state == MONITOR_PAIR_APPROVED) {
            esp_err_t saved = monitor_pairing_store_approved(&session);
            if (saved != ESP_OK) ESP_LOGE("monitor_pairing", "credential storage failed: %s", esp_err_to_name(saved));
            break;
        }
        if (session.state == MONITOR_PAIR_REJECTED) break;
    }
done:
    publish(NULL);
    memset(&session, 0, sizeof session);
    memset(&request, 0, sizeof request);
    memset(&response, 0, sizeof response);
    memset(nonce, 0, sizeof nonce);
    portENTER_CRITICAL(&s_mux);
    s_task = NULL; s_busy = false;
    portEXIT_CRITICAL(&s_mux);
}

esp_err_t monitor_pairing_service_offer(const char *ipv4, uint16_t port, const char *host_id)
{
    if (!ipv4 || !port || !host_id || strlen(ipv4) >= 16 || strlen(host_id) != 36) return ESP_ERR_INVALID_ARG;
    portENTER_CRITICAL(&s_mux);
    bool busy = s_busy || s_serial;
    if (!busy) s_busy = true;
    portEXIT_CRITICAL(&s_mux);
    if (busy) return ESP_ERR_INVALID_STATE;
    offer_t *offer = calloc(1, sizeof *offer);
    if (!offer) goto fail;
    strcpy(offer->ip, ipv4); strcpy(offer->host, host_id); offer->port = port;
    esp_err_t scheduled = monitor_companion_worker_submit(pairing_task, offer);
    if (scheduled != ESP_OK) {
        free(offer);
        portENTER_CRITICAL(&s_mux); s_busy = false; portEXIT_CRITICAL(&s_mux);
        return scheduled;
    }
    return ESP_OK;
fail:
    portENTER_CRITICAL(&s_mux); s_busy = false; portEXIT_CRITICAL(&s_mux);
    return ESP_ERR_NO_MEM;
}
