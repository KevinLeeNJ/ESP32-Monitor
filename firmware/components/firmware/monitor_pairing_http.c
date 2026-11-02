#include "monitor_pairing_http.h"
#include "esp_http_client.h"
#include <arpa/inet.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define RESPONSE_MAX 4096
#define REQUEST_MAX 512

typedef struct {
    uint8_t *data;
    size_t size;
    bool overflow;
} response_buffer_t;

static esp_err_t receive(esp_http_client_event_t *event)
{
    response_buffer_t *buffer = event->user_data;
    if (event->event_id == HTTP_EVENT_ON_DATA && event->data_len > 0) {
        size_t n = (size_t)event->data_len;
        if (buffer->overflow || n > RESPONSE_MAX - buffer->size) {
            buffer->overflow = true;
            return ESP_FAIL;
        }
        memcpy(buffer->data + buffer->size, event->data, n);
        buffer->size += n;
    }
    return ESP_OK;
}

esp_err_t monitor_pairing_http_exchange(const char *address, uint16_t port,
                                        const esp32monitor_companion_PairRequest *request,
                                        esp32monitor_companion_PairResponse *response)
{
    struct in_addr ip;
    if (!address || !port || !request || !response || inet_pton(AF_INET, address, &ip) != 1)
        return ESP_ERR_INVALID_ARG;
    uint8_t request_data[REQUEST_MAX]; size_t request_size = 0;
    if (!monitor_pairing_encode(request, request_data, sizeof request_data, &request_size))
        return ESP_ERR_INVALID_ARG;
    response_buffer_t buffer = {.data = malloc(RESPONSE_MAX)};
    if (!buffer.data) return ESP_ERR_NO_MEM;
    char url[80];
    snprintf(url, sizeof url, "http://%s:%u/companion/v1/pair", address, (unsigned)port);
    esp_http_client_config_t config = {.url = url, .timeout_ms = 4000,
        .disable_auto_redirect = true, .event_handler = receive, .user_data = &buffer};
    esp_http_client_handle_t client = esp_http_client_init(&config);
    if (!client) { free(buffer.data); return ESP_ERR_NO_MEM; }
    esp_err_t err = esp_http_client_set_method(client, HTTP_METHOD_POST);
    if (err == ESP_OK) err = esp_http_client_set_header(client, "Content-Type", "application/x-protobuf");
    if (err == ESP_OK) err = esp_http_client_set_header(client, "Accept", "application/x-protobuf");
    if (err == ESP_OK) err = esp_http_client_set_post_field(client, (const char *)request_data, (int)request_size);
    if (err == ESP_OK) err = esp_http_client_perform(client);
    int status = esp_http_client_get_status_code(client);
    if (err == ESP_OK && (buffer.overflow || (status != 200 && status != 201 && status != 202))) err = ESP_FAIL;
    if (err == ESP_OK && !monitor_pairing_response(buffer.data, buffer.size, response)) err = ESP_FAIL;
    esp_http_client_cleanup(client);
    memset(buffer.data, 0, RESPONSE_MAX);
    free(buffer.data);
    memset(request_data, 0, sizeof request_data);
    return err;
}
