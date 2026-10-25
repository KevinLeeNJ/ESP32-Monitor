#include "monitor_devices.h"
#include "monitor_clock.h"
#include "monitor_display_refresh.h"
#include "esp_crt_bundle.h"
#include "esp_http_client.h"
#include "esp_log.h"
#ifdef ESP_PLATFORM
#include "esp_task_wdt.h"
#endif
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
#define REQUEST_US (4LL * 1000000)
#define INITIAL_RETRY_US (2LL * 1000000)
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
static void notify_display(bool changed, bool immediate)
{
    if (changed) monitor_display_refresh_request_reason(
        MONITOR_REFRESH_DEVICES | (immediate ? MONITOR_REFRESH_RECOVER : 0));
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
    notify_display(changed, true);
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
    notify_display(changed, true);
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
    notify_display(changed, true);
}

typedef struct {
    int64_t deadline;
    int64_t hard_deadline;
    esp_timer_handle_t timer;
    SemaphoreHandle_t lock;
    int socket;
    bool rejected;
    bool timed_out;
    const char *phase;
    bool json_type;
    unsigned connections;
} response_guard_t;

/* Owned exclusively by devices_task. The event context must outlive every
 * request while the HTTP handle retains its idle TCP/TLS connection. */
typedef struct {
    esp_http_client_handle_t client;
    response_guard_t guard;
} devices_transport_t;

static void feed_task_watchdog(void)
{
#ifdef ESP_PLATFORM
    ESP_ERROR_CHECK(esp_task_wdt_reset());
#endif
}

/* Shutdown interrupts a slow header/body read without touching the HTTP/TLS
 * object's ownership. DISCONNECTED clears the descriptor before it is closed,
 * and stop_blocking joins this callback before cleanup frees the context. */
static void request_deadline(void *arg)
{
    response_guard_t *guard = arg;
    xSemaphoreTake(guard->lock, portMAX_DELAY);
    int64_t left = guard->deadline - esp_timer_get_time();
    /* ON_CONNECTED grants the response its own budget. Re-arm this one-shot
     * only if that event moved the deadline; never poll an idle connection. */
    if (left > 0 && !guard->rejected &&
        esp_timer_start_once(guard->timer, left) == ESP_OK) {
        xSemaphoreGive(guard->lock);
        return;
    }
    guard->timed_out = true;
    guard->rejected = true;
    if (guard->socket >= 0) shutdown(guard->socket, SHUT_RDWR);
    xSemaphoreGive(guard->lock);
}
static esp_err_t guard_event(esp_http_client_event_t *event)
{
    feed_task_watchdog();
    response_guard_t *guard = event->user_data;
    xSemaphoreTake(guard->lock, portMAX_DELAY);
    if (event->event_id == HTTP_EVENT_ON_CONNECTED) {
        guard->socket = esp_http_client_get_socket(event->client);
        ++guard->connections;
        int64_t now = esp_timer_get_time();
        if (now >= guard->deadline) guard->rejected = guard->timed_out = true;
        if (!guard->rejected) {
            /* TLS setup must not consume the response budget. Both phases
             * stay below the active task's five-second watchdog interval. */
            int64_t end = now + REQUEST_US;
            guard->deadline = end < guard->hard_deadline ? end : guard->hard_deadline;
            esp_http_client_set_timeout_ms(event->client, (int)((guard->deadline - now + 999) / 1000));
        }
    }
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
    if (esp_timer_get_time() >= guard->deadline) guard->rejected = guard->timed_out = true;
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
static void transport_close_client(devices_transport_t *transport)
{
    if (transport->client) esp_http_client_cleanup(transport->client);
    transport->client = NULL;
    if (transport->guard.lock) xSemaphoreTake(transport->guard.lock, portMAX_DELAY);
    transport->guard.socket = -1;
    if (transport->guard.lock) xSemaphoreGive(transport->guard.lock);
}

/* Only call with no request deadline callback running. */
static void transport_reset(devices_transport_t *transport)
{
    transport_close_client(transport);
    if (transport->guard.lock) vSemaphoreDelete(transport->guard.lock);
    memset(transport, 0, sizeof(*transport));
    transport->guard.socket = -1;
}

static bool fetch_attempt(devices_transport_t *transport, const devices_config_t *settings,
                          char *body, monitor_devices_snapshot_t *out, int *status, bool *retryable)
{
    response_guard_t *guard = &transport->guard;
    *status = 0;
    *retryable = false;
    if (!transport->client) {
        char url[MONITOR_DEVICES_URL_MAX + sizeof("/v1/devices/summary")];
        snprintf(url, sizeof(url), "%s/v1/devices/summary", settings->url);
        esp_http_client_config_t config = {
            .url = url, .timeout_ms = 4000, .disable_auto_redirect = true,
            .crt_bundle_attach = esp_crt_bundle_attach,
            .event_handler = guard_event, .user_data = guard,
            /* Normal 30s polls keep the socket busy without extra probes. */
            .keep_alive_enable = true, .keep_alive_idle = 45,
            .keep_alive_interval = 10, .keep_alive_count = 3,
        };
        transport->client = esp_http_client_init(&config);
        if (!transport->client) return false;
    }
    esp_http_client_handle_t client = transport->client;
    char auth[sizeof("Bearer ") + MONITOR_DEVICES_TOKEN_MAX];
    snprintf(auth, sizeof(auth), "Bearer %s", settings->token);
    esp_err_t headers = esp_http_client_set_header(client, "Authorization", auth);
    memset(auth, 0, sizeof(auth));
    if (headers != ESP_OK ||
        esp_http_client_set_header(client, "Accept", "application/json") != ESP_OK ||
        esp_http_client_set_header(client, "Accept-Encoding", "identity") != ESP_OK ||
        esp_http_client_set_header(client, "Connection", "keep-alive") != ESP_OK ||
        !remaining(client, guard)) return false;
    guard->phase = "connect/send";
    if (esp_http_client_open(client, 0) != ESP_OK) {
        *retryable = true;
        return false;
    }
    if (!remaining(client, guard)) return false;
    guard->phase = "headers";
    int64_t header_length = esp_http_client_fetch_headers(client);
    if (header_length < 0) {
        *retryable = true;
        return false;
    }
    *status = esp_http_client_get_status_code(client);
    if (!remaining(client, guard) || *status != 200 || !guard->json_type) return false;
    size_t len = 0;
    guard->phase = "body";
    for (;;) {
        if (!remaining(client, guard)) return false;
        /* Read through message completion (including chunk trailers) before
         * retaining the connection for another request. */
        int n = esp_http_client_read(client, body + len, MONITOR_DEVICES_BODY_MAX + 1 - len);
        if (n < 0) { *retryable = true; return false; }
        len += n;
        if (len > MONITOR_DEVICES_BODY_MAX) return false;
        if (!n) {
            if (!esp_http_client_is_complete_data_received(client)) {
                *retryable = true;
                return false;
            }
            break;
        }
    }
    if (!remaining(client, guard)) return false;
    body[len] = 0;
    guard->phase = "parse";
    return monitor_devices_parse(body, len, out);
}

static bool fetch(devices_transport_t *transport, const devices_config_t *settings,
                  monitor_devices_snapshot_t *out, int *status, bool *reused)
{
    response_guard_t *guard = &transport->guard;
    *reused = false;
    if (!guard->lock) {
        guard->lock = xSemaphoreCreateMutex();
        guard->socket = -1;
    }
    if (!guard->lock) return false;
    xSemaphoreTake(guard->lock, portMAX_DELAY);
    guard->deadline = esp_timer_get_time() + REQUEST_US;
    guard->hard_deadline = guard->deadline + REQUEST_US;
    guard->rejected = false;
    guard->timed_out = false;
    guard->phase = "prepare";
    guard->json_type = false;
    unsigned connections_before = guard->connections;
    xSemaphoreGive(guard->lock);
    const bool had_client = transport->client != NULL;
    esp_timer_handle_t deadline_timer = NULL;
    const esp_timer_create_args_t args = {.callback = request_deadline, .arg = guard, .name = "devices_deadline"};
    char *body = malloc(MONITOR_DEVICES_BODY_MAX + 1);
    bool ok = false;
    if (!body || esp_timer_create(&args, &deadline_timer) != ESP_OK) goto done;
    guard->timer = deadline_timer;
    if (esp_timer_start_once(deadline_timer, REQUEST_US) != ESP_OK) goto done;
    for (unsigned attempt = 0; attempt < 2; ++attempt) {
        feed_task_watchdog();
        bool retryable = false;
        ok = fetch_attempt(transport, settings, body, out, status, &retryable);
        if (ok || !had_client || attempt || !retryable) break;
        /* A proxy may have silently expired the idle connection. Retry this
         * idempotent GET once within the existing transaction's bounds. */
        if (!remaining(transport->client, guard)) break;
        transport_close_client(transport);
        xSemaphoreTake(guard->lock, portMAX_DELAY);
        guard->json_type = false;
        xSemaphoreGive(guard->lock);
    }
done:
    if (deadline_timer) {
        esp_timer_stop_blocking(deadline_timer, portMAX_DELAY);
        esp_timer_delete(deadline_timer);
    }
    guard->timer = NULL;
    if (guard->rejected || esp_timer_get_time() >= guard->deadline) ok = false;
    *reused = ok && had_client && guard->connections == connections_before;
    if (!ok || !esp_http_client_is_persistent_connection(transport->client)) transport_close_client(transport);
    free(body);
    return ok;
}
static void devices_task(void *arg)
{
    (void)arg;
    ESP_ERROR_CHECK(esp_task_wdt_add(NULL));
    int64_t next_poll = 0;
    devices_transport_t transport = {.guard.socket = -1};
    uint32_t transport_generation = UINT32_MAX;
    bool initial_retry_used = false;
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
        notify_display(changed, true);
        if (generation != transport_generation || !wifi || !configured) {
            transport_reset(&transport);
            transport_generation = generation;
            initial_retry_used = false;
        }
        bool clock_ready = strncmp(config.url, "https://", 8) != 0 || monitor_clock_is_valid();
        if (configured && wifi && clock_ready && now >= next_poll) {
            monitor_devices_snapshot_t candidate = {0};
            int status = 0;
            /* The clock task owns :00/:30 polling while wall time is valid.
             * Keep a relative fallback only for HTTP before time is known. */
            next_poll = monitor_clock_is_valid() ? INT64_MAX : now + PERIOD_US;
            bool reused = false;
            int64_t request_started = esp_timer_get_time();
            bool ok = fetch(&transport, &config, &candidate, &status, &reused);
            feed_task_watchdog();
            now = esp_timer_get_time();
            /* Merge refreshes received during this request. A newer config or
             * connection generation starts a new request immediately below. */
            ulTaskNotifyTake(pdTRUE, 0);
            bool arm_expiry = false;
            bool immediate = false;
            portENTER_CRITICAL(&s_mux);
            if (generation == s_generation && s_wifi) {
                /* One delayed retry per configuration/connection generation,
                 * only before first data and only for transport failures.
                 * Normal :00/:30 polling and auth/server errors stay unchanged. */
                if (!ok && status == 0 && !s_have_snapshot && !initial_retry_used) {
                    initial_retry_used = true;
                    next_poll = now + INITIAL_RETRY_US;
                }
                immediate = !s_have_snapshot;
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
                immediate |= candidate.status != s_snapshot.status;
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
            notify_display(changed, immediate);
            /* No URL, token or response body in logs. */
            ESP_LOGI(TAG, "summary %s, HTTP %d, connection %s, elapsed=%u ms, phase=%s, deadline=%d",
                     ok ? "received" : "failed", status,
                     ok ? (reused ? "reused" : "new") : "discarded",
                     (unsigned)((now - request_started) / 1000), transport.guard.phase,
                     transport.guard.timed_out);
            continue;
        }
        int64_t wake = expiry;
        if (configured && wifi) {
            /* Time becoming valid wakes us through the clock task as well. */
            int64_t poll = clock_ready ? next_poll : INT64_MAX;
            if (poll < wake) wake = poll;
        }
        TickType_t wait = wake == INT64_MAX ? portMAX_DELAY :
                          pdMS_TO_TICKS((wake > now ? wake - now : 1000) / 1000 + 1);
        /* Poll/lease deadlines and producer notifications own the idle wait;
         * keep the task watchdog subscribed throughout active HTTP work. */
        ESP_ERROR_CHECK(esp_task_wdt_delete(NULL));
        uint32_t notified = ulTaskNotifyTake(pdTRUE, wait);
        ESP_ERROR_CHECK(esp_task_wdt_add(NULL));
        if (notified) next_poll = 0;
        feed_task_watchdog();
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
