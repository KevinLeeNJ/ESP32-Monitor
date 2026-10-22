#include "monitor_devices.h"
#include "monitor_clock.h"
#include "monitor_display_refresh.h"
#include "esp_crt_bundle.h"
#include "esp_http_client.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "freertos/semphr.h"
#include <sys/socket.h>
#include "nvs.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>

#define PERIOD_US (30LL * 1000000)
#define RETAIN_US (90LL * 1000000)
#define REQUEST_US (8LL * 1000000)
static const char *TAG = "monitor_devices";
static TaskHandle_t s_task;
static esp_timer_handle_t s_expiry_timer;
static portMUX_TYPE s_mux = portMUX_INITIALIZER_UNLOCKED;
typedef struct { char url[193]; char token[129]; } devices_config_t;
static devices_config_t s_config;
static uint32_t s_generation;
static bool s_wifi, s_have_snapshot;
static int64_t s_last_success;
static monitor_devices_snapshot_t s_snapshot;

/* Caller holds s_mux. Compare visible content only; successful identical
 * responses renew the lease without waking the display task. */
static bool publish(monitor_devices_snapshot_t *next)
{
    next->version = s_snapshot.version;
    if (memcmp(next, &s_snapshot, sizeof(*next)) == 0) return false;
    next->version++;
    s_snapshot = *next;
    return true;
}
static void notify_display(bool changed)
{
    if (changed) monitor_display_refresh_request_reason(MONITOR_REFRESH_DEVICES);
}
void monitor_devices_get(monitor_devices_snapshot_t *out)
{
    if (!out) return;
    portENTER_CRITICAL(&s_mux);
    *out = s_snapshot;
    portEXIT_CRITICAL(&s_mux);
}
void monitor_devices_request_refresh(void)
{
    if (s_task) xTaskNotifyGive(s_task);
}
void monitor_devices_config_changed(void)
{
    devices_config_t config = {0};
    nvs_handle_t nvs;
    if (nvs_open("monitor_loc", NVS_READONLY, &nvs) == ESP_OK) {
        size_t size = sizeof(config.url);
        if (nvs_get_str(nvs, "devices_api_url", config.url, &size) != ESP_OK) config.url[0] = 0;
        size = sizeof(config.token);
        if (nvs_get_str(nvs, "devices_token", config.token, &size) != ESP_OK) config.token[0] = 0;
        nvs_close(nvs);
    }
    if (!monitor_devices_normalize_url(config.url) || !monitor_devices_valid_token(config.token)) {
        config.url[0] = 0;
        config.token[0] = 0;
    }
    bool changed = false;
    portENTER_CRITICAL(&s_mux);
    if (memcmp(&s_config, &config, sizeof(config)) != 0) {
        s_config = config;
        ++s_generation;
        s_have_snapshot = false;
        monitor_devices_snapshot_t next = {.status = config.url[0] ? MONITOR_DEVICES_WAITING : MONITOR_DEVICES_NOT_CONFIGURED};
        changed = publish(&next);
    }
    portEXIT_CRITICAL(&s_mux);
    notify_display(changed);
    monitor_devices_request_refresh();
}
void monitor_devices_set_wifi_available(bool available)
{
    bool changed = false;
    portENTER_CRITICAL(&s_mux);
    if (s_wifi != available) {
        s_wifi = available;
        ++s_generation;
        if (!available && s_have_snapshot) {
            monitor_devices_snapshot_t next = s_snapshot;
            next.status = MONITOR_DEVICES_STALE;
            changed = publish(&next);
        }
    }
    portEXIT_CRITICAL(&s_mux);
    notify_display(changed);
    monitor_devices_request_refresh();
}

/* Expiration must wake the display even while a network request is blocked. */
static void expire_snapshot(void *arg)
{
    (void)arg;
    bool changed = false;
    portENTER_CRITICAL(&s_mux);
    if (s_have_snapshot && esp_timer_get_time() - s_last_success >= RETAIN_US) {
        monitor_devices_snapshot_t next = {.status = MONITOR_DEVICES_UNAVAILABLE};
        changed = publish(&next);
        s_have_snapshot = false;
    }
    portEXIT_CRITICAL(&s_mux);
    notify_display(changed);
}

typedef struct {
    int64_t deadline;
    SemaphoreHandle_t lock;
    int socket;
    bool rejected;
    bool json_type;
} response_guard_t;
/* Shutdown interrupts a slow header/body read without touching the HTTP/TLS
 * object's ownership. DISCONNECTED clears the descriptor before it is closed,
 * and stop_blocking joins this callback before cleanup frees the context. */
static void request_deadline(void *arg)
{
    response_guard_t *guard = arg;
    xSemaphoreTake(guard->lock, portMAX_DELAY);
    guard->rejected = true;
    if (guard->socket >= 0) shutdown(guard->socket, SHUT_RDWR);
    xSemaphoreGive(guard->lock);
}
static esp_err_t guard_event(esp_http_client_event_t *event)
{
    response_guard_t *guard = event->user_data;
    xSemaphoreTake(guard->lock, portMAX_DELAY);
    if (event->event_id == HTTP_EVENT_ON_CONNECTED) guard->socket = esp_http_client_get_socket(event->client);
    if (event->event_id == HTTP_EVENT_DISCONNECTED) guard->socket = -1;
    if (event->event_id == HTTP_EVENT_ON_HEADER) {
        if (!strcasecmp(event->header_key, "Content-Encoding") && strcasecmp(event->header_value, "identity"))
            guard->rejected = true;
        if (!strcasecmp(event->header_key, "Content-Length") && strtoull(event->header_value, NULL, 10) > MONITOR_DEVICES_BODY_MAX)
            guard->rejected = true;
        if (!strcasecmp(event->header_key, "Content-Type")) {
            guard->json_type = !strncasecmp(event->header_value, "application/json", 16) &&
                (event->header_value[16] == 0 || event->header_value[16] == ';');
            if (!guard->json_type) guard->rejected = true;
        }
    }
    if (esp_timer_get_time() >= guard->deadline) guard->rejected = true;
    if (guard->rejected && guard->socket >= 0) shutdown(guard->socket, SHUT_RDWR);
    xSemaphoreGive(guard->lock);
    return ESP_OK;
}
static bool remaining(esp_http_client_handle_t client, response_guard_t *guard)
{
    int64_t us = guard->deadline - esp_timer_get_time();
    xSemaphoreTake(guard->lock, portMAX_DELAY);
    bool rejected = guard->rejected;
    xSemaphoreGive(guard->lock);
    if (rejected || us <= 0) return false;
    esp_http_client_set_timeout_ms(client, (int)((us + 999) / 1000));
    return true;
}
static bool fetch(const devices_config_t *settings, monitor_devices_snapshot_t *out, int *status)
{
    char url[MONITOR_DEVICES_URL_MAX + sizeof("/v1/devices/summary")];
    char auth[sizeof("Bearer ") + MONITOR_DEVICES_TOKEN_MAX];
    snprintf(url, sizeof(url), "%s/v1/devices/summary", settings->url);
    snprintf(auth, sizeof(auth), "Bearer %s", settings->token);
    response_guard_t guard = {
        .deadline = esp_timer_get_time() + REQUEST_US,
        .lock = xSemaphoreCreateMutex(), .socket = -1,
    };
    if (!guard.lock) return false;
    esp_timer_handle_t deadline_timer = NULL;
    const esp_timer_create_args_t deadline_args = {.callback = request_deadline, .arg = &guard, .name = "devices_deadline"};
    esp_http_client_config_t config = {
        .url = url, .timeout_ms = 8000, .disable_auto_redirect = true,
        .crt_bundle_attach = esp_crt_bundle_attach,
        .event_handler = guard_event, .user_data = &guard,
    };
    esp_http_client_handle_t client = esp_http_client_init(&config);
    char *body = malloc(MONITOR_DEVICES_BODY_MAX + 1);
    bool ok = false;
    if (!client || !body || esp_timer_create(&deadline_args, &deadline_timer) != ESP_OK) goto done;
    if (esp_timer_start_once(deadline_timer, REQUEST_US) != ESP_OK) goto done;
    if (esp_http_client_set_header(client, "Authorization", auth) != ESP_OK ||
        esp_http_client_set_header(client, "Accept", "application/json") != ESP_OK ||
        esp_http_client_set_header(client, "Accept-Encoding", "identity") != ESP_OK ||
        esp_http_client_open(client, 0) != ESP_OK || !remaining(client, &guard)) goto done;
    int64_t header_length = esp_http_client_fetch_headers(client);
    *status = esp_http_client_get_status_code(client);
    if (header_length < 0 || !remaining(client, &guard)) goto done;
    if (*status != 200 || !guard.json_type) goto done;
    size_t len = 0;
    for (;;) {
        if (!remaining(client, &guard)) goto done;
        /* The extra byte detects overflow even when Content-Length is absent. */
        int n = esp_http_client_read(client, body + len, MONITOR_DEVICES_BODY_MAX + 1 - len);
        if (n < 0) goto done;
        len += n;
        if (len > MONITOR_DEVICES_BODY_MAX) goto done;
        if (!n) {
            if (!esp_http_client_is_complete_data_received(client)) goto done;
            break;
        }
    }
    if (!remaining(client, &guard)) goto done;
    body[len] = 0;
    ok = monitor_devices_parse(body, len, out);
done:
    if (deadline_timer) {
        esp_timer_stop_blocking(deadline_timer, portMAX_DELAY);
        esp_timer_delete(deadline_timer);
    }
    if (client) esp_http_client_cleanup(client);
    vSemaphoreDelete(guard.lock);
    free(body);
    memset(auth, 0, sizeof(auth));
    return ok;
}
static void devices_task(void *arg)
{
    (void)arg;
    int64_t next_poll = 0;
    for (;;) {
        bool changed = false;
        devices_config_t config;
        uint32_t generation;
        bool wifi, configured;
        int64_t now = esp_timer_get_time(), expiry = INT64_MAX;
        portENTER_CRITICAL(&s_mux);
        config = s_config;
        generation = s_generation;
        wifi = s_wifi;
        configured = config.url[0] && config.token[0];
        if (s_have_snapshot) {
            expiry = s_last_success + RETAIN_US;
            if (now >= expiry) {
                monitor_devices_snapshot_t next = {.status = MONITOR_DEVICES_UNAVAILABLE};
                changed = publish(&next);
                s_have_snapshot = false;
                expiry = INT64_MAX;
            }
        }
        portEXIT_CRITICAL(&s_mux);
        notify_display(changed);
        bool clock_ready = strncmp(config.url, "https://", 8) != 0 || monitor_clock_is_valid();
        if (configured && wifi && clock_ready && now >= next_poll) {
            monitor_devices_snapshot_t candidate = {0};
            int status = 0;
            next_poll = now + PERIOD_US;
            bool ok = fetch(&config, &candidate, &status);
            now = esp_timer_get_time();
            /* Merge refreshes received during this request. A newer config or
             * connection generation starts a new request immediately below. */
            ulTaskNotifyTake(pdTRUE, 0);
            bool arm_expiry = false;
            portENTER_CRITICAL(&s_mux);
            if (generation == s_generation && s_wifi) {
                if (ok) {
                    s_have_snapshot = true;
                    s_last_success = now;
                    arm_expiry = true;
                } else {
                    candidate = s_snapshot;
                    if (s_have_snapshot && now - s_last_success >= RETAIN_US) {
                        memset(&candidate, 0, sizeof(candidate));
                        s_have_snapshot = false;
                    }
                    candidate.status = status == 401 || status == 403 ? MONITOR_DEVICES_AUTH_ERROR :
                        (s_have_snapshot ? MONITOR_DEVICES_STALE : MONITOR_DEVICES_UNAVAILABLE);
                }
                changed = publish(&candidate);
            } else {
                changed = false;
                next_poll = 0;
            }
            portEXIT_CRITICAL(&s_mux);
            if (arm_expiry) {
                esp_timer_stop(s_expiry_timer);
                esp_timer_start_once(s_expiry_timer, RETAIN_US);
            }
            notify_display(changed);
            /* No URL, token or response body in logs. */
            ESP_LOGI(TAG, "summary %s, HTTP %d", ok ? "received" : "failed", status);
            continue;
        }
        int64_t wake = expiry;
        if (configured && wifi) {
            int64_t poll = clock_ready ? next_poll : now + 1000000;
            if (poll < wake) wake = poll;
        }
        TickType_t wait = wake == INT64_MAX ? portMAX_DELAY : pdMS_TO_TICKS((wake > now ? wake - now : 1000) / 1000 + 1);
        if (ulTaskNotifyTake(pdTRUE, wait)) next_poll = 0;
    }
}
esp_err_t monitor_devices_start(void)
{
    if (s_task) return ESP_OK;
    const esp_timer_create_args_t timer_args = {.callback = expire_snapshot, .name = "devices_expiry"};
    esp_err_t err = esp_timer_create(&timer_args, &s_expiry_timer);
    if (err != ESP_OK) return err;
    monitor_devices_config_changed();
    if (xTaskCreate(devices_task, "monitor_devices", 12288, NULL, 3, &s_task) != pdPASS) {
        esp_timer_delete(s_expiry_timer);
        s_expiry_timer = NULL;
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}
