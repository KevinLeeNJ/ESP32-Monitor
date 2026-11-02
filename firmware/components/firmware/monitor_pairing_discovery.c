#include "monitor_pairing_discovery.h"
#include "monitor_companion_mqtt.h"
#include "monitor_pairing_service.h"
#include "monitor_weather.h"
#include "monitor_devices.h"
#include "monitor_pairing_store.h"
#include "monitor_discovery.h"
#include "monitor_discovery_auth.h"
#include "monitor_pairing_crypto.h"
#include "esp_random.h"
#include "esp_timer.h"
#include "esp_log.h"
#include "nvs.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include <arpa/inet.h>
#include <sys/socket.h>
#include <unistd.h>
#include <string.h>
#include <stdio.h>

#define DISCOVERY_PORT 18444
#define CONTEXTS 4
#define REVOCATIONS 4
#define OFFER_TTL_US (10LL * 1000000)
typedef struct {
    bool used;
    struct sockaddr_in peer;
    int64_t created;
    esp32monitor_companion_Discovery request;
    char response_nonce[44];
} pending_t;

static void nonce(char out[44])
{
    static const char abc[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
    uint8_t raw[32]; esp_fill_random(raw, sizeof raw);
    uint32_t v = 0; unsigned bits = 0, n = 0;
    for (unsigned i = 0; i < sizeof raw; i++) {
        v = (v << 8) | raw[i]; bits += 8;
        while (bits >= 6) { bits -= 6; out[n++] = abc[(v >> bits) & 63]; }
    }
    if (bits) out[n++] = abc[(v << (6 - bits)) & 63];
    out[n] = 0;
}

static bool match_offer(const pending_t *c, const esp32monitor_companion_Discovery *d,
                        const struct sockaddr_in *peer, const char *device)
{
    return c->used && esp_timer_get_time() - c->created < OFFER_TTL_US &&
           c->peer.sin_addr.s_addr == peer->sin_addr.s_addr && c->peer.sin_port == peer->sin_port &&
           !strcmp(d->type, "monitor_connect_offer") && !strcmp(d->request_id, c->request.request_id) &&
           !strcmp(d->host_id, c->request.host_id) && !strcmp(d->nonce, c->request.nonce) &&
           d->has_device_id && !strcmp(d->device_id, device) && d->has_response_nonce &&
           !strcmp(d->response_nonce, c->response_nonce) && d->has_host_port &&
           d->host_port > 0 && d->host_port <= 65535 && !d->has_pairing_id && !d->has_mac &&
           !d->has_mqtt_port && !d->has_session_id && d->has_pairing_open && d->pairing_open &&
           d->has_device_name && !strcmp(d->device_name, "ESP32 Monitor") &&
           d->has_firmware_version && !strcmp(d->firmware_version, "1.0.0");
}

static bool match_paired_offer(const pending_t *c, const esp32monitor_companion_Discovery *d,
                               const struct sockaddr_in *peer, const char *device,
                               const monitor_pairing_record_t *record)
{
    return c->used && esp_timer_get_time() - c->created < OFFER_TTL_US &&
           c->peer.sin_addr.s_addr == peer->sin_addr.s_addr && c->peer.sin_port == peer->sin_port &&
           !strcmp(d->type, "monitor_connect_offer") && !strcmp(d->request_id, c->request.request_id) &&
           !strcmp(d->host_id, record->host_id) && !strcmp(d->host_id, c->request.host_id) &&
           !strcmp(d->nonce, c->request.nonce) && d->has_device_id && !strcmp(d->device_id, device) &&
           d->has_pairing_id && !strcmp(d->pairing_id, record->pairing_id) && d->has_response_nonce &&
           !strcmp(d->response_nonce, c->response_nonce) && !d->has_host_port &&
           d->has_mqtt_port && d->mqtt_port > 0 && d->mqtt_port <= 65535 && d->has_session_id &&
           monitor_companion_valid_uuid(d->session_id) && d->has_pairing_open && !d->pairing_open &&
           d->has_device_name && !strcmp(d->device_name, "ESP32 Monitor") &&
           d->has_firmware_version && !strcmp(d->firmware_version, "1.0.0") &&
           monitor_discovery_verify(d, record->serial_key, monitor_pairing_sha256, monitor_pairing_hmac);
}

static void discovery_task(void *arg)
{
    (void)arg;
    char device[37];
    if (monitor_pairing_identity(device) != ESP_OK) goto exit;
    monitor_pairing_record_t existing;
    esp_err_t state = monitor_pairing_store_load(&existing);
    memset(&existing, 0, sizeof existing);
    monitor_pairing_revocation_t revocations[REVOCATIONS];
    size_t revocation_count = monitor_pairing_store_revocations(revocations, REVOCATIONS);
    memset(revocations, 0, sizeof revocations);
    ESP_LOGI("pair_discovery", "pairing record: %s, revocations=%u", state == ESP_OK ? "present" :
             state == ESP_ERR_NVS_NOT_FOUND ? "absent" : "unavailable", (unsigned)revocation_count);
    int fd = socket(AF_INET, SOCK_DGRAM, IPPROTO_IP);
    if (fd < 0) goto exit;
    struct sockaddr_in local = {.sin_family = AF_INET, .sin_port = htons(DISCOVERY_PORT), .sin_addr.s_addr = htonl(INADDR_ANY)};
    if (bind(fd, (struct sockaddr *)&local, sizeof local) < 0) { close(fd); goto exit; }
    pending_t contexts[CONTEXTS] = {0};
    unsigned next = 0;
    for (;;) {
        uint8_t input[1025], output[1024];
        struct sockaddr_in peer; socklen_t peer_len = sizeof peer;
        ssize_t n = recvfrom(fd, input, sizeof input, 0, (struct sockaddr *)&peer, &peer_len);
        if (n <= 0 || n > 1024) continue;
        monitor_pairing_record_t record;
        esp_err_t stored = monitor_pairing_store_load(&record);
        if (stored != ESP_OK && stored != ESP_ERR_NVS_NOT_FOUND) continue;
        esp32monitor_companion_Discovery d = esp32monitor_companion_Discovery_init_zero;
        if (!monitor_discovery_decode(input, (size_t)n, &d)) continue;
        if (!strcmp(d.type, "monitor_discover")) {
            char response_nonce[44]; nonce(response_nonce); size_t out_size = 0;
            bool valid = false, revoked = false;
            if (stored == ESP_OK) {
                if (!strcmp(d.host_id, record.host_id) && d.has_pairing_id &&
                    !strcmp(d.pairing_id, record.pairing_id)) {
                    valid = monitor_discovery_announce_paired(&d, device, "ESP32 Monitor", "1.0.0", response_nonce,
                                record.serial_key, monitor_pairing_sha256, monitor_pairing_hmac, output, sizeof output, &out_size);
                }
            }
            if (!valid) {
                monitor_pairing_revocation_t revoked_records[REVOCATIONS];
                size_t count = monitor_pairing_store_revocations(revoked_records, REVOCATIONS);
                for (size_t i = 0; i < count; i++) {
                    if (!d.has_device_id || strcmp(d.device_id, device) || !d.has_pairing_id ||
                        strcmp(d.host_id, revoked_records[i].host_id) ||
                        strcmp(d.pairing_id, revoked_records[i].pairing_id)) continue;
                    if (monitor_discovery_announce_revoked(&d, device, "ESP32 Monitor", "1.0.0", response_nonce,
                            revoked_records[i].discovery_key, monitor_pairing_hmac,
                            output, sizeof output, &out_size)) {
                        valid = true;
                        revoked = true;
                        break;
                    }
                }
                memset(revoked_records, 0, sizeof revoked_records);
            }
            if (!valid && stored == ESP_ERR_NVS_NOT_FOUND) {
                /* A paired former host is handled by the revocation tombstone;
                 * this path is only for a genuinely unpaired monitor. */
                if (d.has_pairing_id || d.has_mac || d.has_device_id) {
                    continue;
                }
                valid = monitor_discovery_announce(&d, device, "ESP32 Monitor", "1.0.0", response_nonce,
                                                   output, sizeof output, &out_size);
            }
            if (!valid) continue;
            if (sendto(fd, output, out_size, 0, (struct sockaddr *)&peer, peer_len) < 0) continue;
            if (revoked) continue;
            pending_t *c = &contexts[next++ % CONTEXTS];
            *c = (pending_t){.used = true, .peer = peer, .created = esp_timer_get_time(), .request = d};
            memcpy(c->response_nonce, response_nonce, sizeof c->response_nonce);
        } else if (!strcmp(d.type, "monitor_connect_offer")) {
            for (unsigned i = 0; i < CONTEXTS; i++) {
                if (stored == ESP_OK) {
                    if (!match_paired_offer(&contexts[i], &d, &peer, device, &record)) continue;
                    contexts[i].used = false;
                    char address[INET_ADDRSTRLEN];
                    if (inet_ntop(AF_INET, &peer.sin_addr, address, sizeof address)) {
                        /* The companion session owns page-one data. Pause the
                         * direct producers first: their in-flight TLS request
                         * can otherwise hold the internal RAM needed by the
                         * MQTT worker task right after Wi-Fi resume. */
                        monitor_weather_set_companion_active(true);
                        monitor_devices_set_companion_active(true);
                        esp_err_t started = monitor_companion_mqtt_start(
                            address, (uint16_t)d.mqtt_port, record.host_id, device,
                            record.pairing_id, d.session_id, record.device_token);
                        if (started == ESP_ERR_NO_MEM) {
                            /* Give the producer that was already allocating a
                             * moment to finish/fail, then retry once. */
                            vTaskDelay(pdMS_TO_TICKS(2000));
                            started = monitor_companion_mqtt_start(
                                address, (uint16_t)d.mqtt_port, record.host_id, device,
                                record.pairing_id, d.session_id, record.device_token);
                        }
                        if (started == ESP_OK) {
                            ESP_LOGI("pair_discovery", "authenticated LAN session starting");
                        } else {
                            monitor_weather_set_companion_active(false);
                            monitor_devices_set_companion_active(false);
                            if (started != ESP_ERR_INVALID_STATE)
                                ESP_LOGW("pair_discovery", "LAN session unavailable: %s", esp_err_to_name(started));
                        }
                    }
                    break;
                }
                if (!match_offer(&contexts[i], &d, &peer, device)) continue;
                contexts[i].used = false;
                char address[INET_ADDRSTRLEN];
                if (inet_ntop(AF_INET, &peer.sin_addr, address, sizeof address)) {
                    esp_err_t result = monitor_pairing_service_offer(address, (uint16_t)d.host_port, d.host_id);
                    if (result != ESP_OK) ESP_LOGW("pair_discovery", "pairing offer failed: %s", esp_err_to_name(result));
                }
                break;
            }
        }
    }
exit:
    ESP_LOGE("pair_discovery", "discovery unavailable");
    vTaskDelete(NULL);
}

esp_err_t monitor_pairing_discovery_start(void)
{
    return xTaskCreate(discovery_task, "pair_discovery", 16384, NULL, 3, NULL) == pdPASS ? ESP_OK : ESP_ERR_NO_MEM;
}
