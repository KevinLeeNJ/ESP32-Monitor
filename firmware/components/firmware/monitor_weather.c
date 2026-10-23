#include "monitor_weather.h"
#include "monitor_display_refresh.h"
#include "monitor_wifi_manager.h"
#include "monitor_clock.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cJSON.h"
#include "esp_check.h"
#include "esp_crt_bundle.h"
#include "esp_http_client.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "nvs.h"
#include "miniz.h"

#define TAG "monitor_weather"
#define WEATHER_EVT_UPDATE BIT0
#define WEATHER_EVT_WIFI_CHANGED BIT1
#define HTTP_TIMEOUT_MS 10000
#define RESPONSE_MAX 8192

static TaskHandle_t s_task;
static volatile bool s_wifi_available;
static portMUX_TYPE s_data_mux = portMUX_INITIALIZER_UNLOCKED;
static monitor_weather_data_t s_data = {
    .condition = MONITOR_WEATHER_CLEAR, .aqi = -1,
    .latitude = 0, .longitude = 0,
    .location_valid = false, .weather_valid = false, .aqi_valid = false,
};

typedef struct { char *buffer; size_t capacity; size_t length; } response_context_t;

static esp_err_t http_collect_event(esp_http_client_event_t *event)
{
    response_context_t *ctx = (response_context_t *)event->user_data;
    if (ctx != NULL && event->event_id == HTTP_EVENT_ON_DATA && event->data_len > 0) {
        size_t copy = (size_t)event->data_len;
        if (copy > ctx->capacity - 1 - ctx->length) return ESP_FAIL;
        memcpy(ctx->buffer + ctx->length, event->data, copy);
        ctx->length += copy;
        ctx->buffer[ctx->length] = '\0';
    }
    return ESP_OK;
}

static bool http_get(const char *url, char *out, size_t out_size, const char *api_key)
{
    response_context_t response = {.buffer = out, .capacity = out_size, .length = 0};
    if (out_size == 0) return false;
    out[0] = '\0';
    esp_http_client_config_t config = {
        .url = url,
        .timeout_ms = HTTP_TIMEOUT_MS,
        .disable_auto_redirect = false,
        .event_handler = http_collect_event,
        .user_data = &response,
        .crt_bundle_attach = esp_crt_bundle_attach,
    };
    esp_http_client_handle_t client = esp_http_client_init(&config);
    if (client == NULL) return false;
    esp_err_t perform_err = esp_http_client_perform(client);
    int status = esp_http_client_get_status_code(client);
    // QWeather currently returns gzip bodies. The ESP-IDF HTTP client
    // exposes the wire body to the event callback, so decode the gzip
    // deflate payload before handing it to cJSON.
    if (perform_err == ESP_OK && response.length >= 18 &&
        (uint8_t)out[0] == 0x1f && (uint8_t)out[1] == 0x8b &&
        (uint8_t)out[2] == 0x08) {
        char *decoded = (char *)malloc(out_size);
        size_t decoded_len = 0;
        if (decoded != NULL) {
            // Standard gzip header (10 bytes) and trailer (8 bytes).
            decoded_len = tinfl_decompress_mem_to_mem(
                decoded, out_size - 1, out + 10, response.length - 18,
                TINFL_FLAG_USING_NON_WRAPPING_OUTPUT_BUF);
            if (decoded_len != TINFL_DECOMPRESS_MEM_TO_MEM_FAILED) {
                response.length = decoded_len;
                memcpy(out, decoded, decoded_len);
                out[decoded_len] = '\0';
            } else {
                ESP_LOGW(TAG, "gzip decode failed: compressed_len=%u",
                         (unsigned)response.length);
                response.length = 0;
            }
            free(decoded);
        } else {
            ESP_LOGW(TAG, "gzip decode buffer allocation failed");
            response.length = 0;
        }
    }
    bool ok = perform_err == ESP_OK && status == 200 && response.length > 0;
    if (!ok) {
        ESP_LOGW(TAG, "HTTP GET failed: err=%s status=%d response_len=%u",
                 esp_err_to_name(perform_err), status, (unsigned)response.length);
    }
    esp_http_client_cleanup(client);
    return ok;
}

static bool read_configured_location(float *lat, float *lon)
{
    nvs_handle_t nvs;
    if (nvs_open("monitor_loc", NVS_READONLY, &nvs) != ESP_OK) return false;
    char lat_text[24] = {0}, lon_text[24] = {0};
    size_t lat_size = sizeof(lat_text), lon_size = sizeof(lon_text);
    esp_err_t a = nvs_get_str(nvs, "latitude", lat_text, &lat_size);
    esp_err_t b = nvs_get_str(nvs, "longitude", lon_text, &lon_size);
    nvs_close(nvs);
    if (a != ESP_OK || b != ESP_OK || lat_text[0] == '\0' || lon_text[0] == '\0') return false;
    *lat = strtof(lat_text, NULL);
    *lon = strtof(lon_text, NULL);
    return *lat >= -90.0f && *lat <= 90.0f && *lon >= -180.0f && *lon <= 180.0f;
}

static bool locate_by_public_ip(float *lat, float *lon)
{
    char response[RESPONSE_MAX];
    if (!http_get("http://ipwho.is/", response, sizeof(response), NULL)) return false;
    cJSON *json = cJSON_Parse(response);
    if (json == NULL) return false;
    cJSON *success = cJSON_GetObjectItem(json, "success");
    cJSON *lat_item = cJSON_GetObjectItem(json, "latitude");
    cJSON *lon_item = cJSON_GetObjectItem(json, "longitude");
    bool ok = (success == NULL || cJSON_IsTrue(success)) && cJSON_IsNumber(lat_item) && cJSON_IsNumber(lon_item);
    if (ok) { *lat = (float)lat_item->valuedouble; *lon = (float)lon_item->valuedouble; }
    cJSON_Delete(json);
    return ok;
}

static monitor_weather_condition_t condition_from_qweather(int code)
{
    if (code == 100) return MONITOR_WEATHER_CLEAR;
    if (code == 101 || code == 102) return MONITOR_WEATHER_FEW_CLOUDS;
    if (code == 103) return MONITOR_WEATHER_SCATTERED_CLOUDS;
    if (code == 104) return MONITOR_WEATHER_OVERCAST;
    if (code >= 300 && code <= 304) return MONITOR_WEATHER_THUNDERSTORM;
    if (code == 305 || code == 309 || code == 399) return MONITOR_WEATHER_LIGHT_RAIN;
    if (code == 306 || code == 314) return MONITOR_WEATHER_MODERATE_RAIN;
    if (code >= 307 && code <= 312) return MONITOR_WEATHER_HEAVY_RAIN;
    if (code == 313 || code == 404 || code == 405 || code == 406 || code == 407) return MONITOR_WEATHER_SLEET;
    if (code == 400 || code == 408 || code == 499) return MONITOR_WEATHER_LIGHT_SNOW;
    if (code == 401 || code == 409) return MONITOR_WEATHER_MODERATE_SNOW;
    if (code == 402 || code == 403 || code == 410) return MONITOR_WEATHER_HEAVY_SNOW;
    if (code == 500 || code == 501) return MONITOR_WEATHER_FOG;
    if (code == 502) return MONITOR_WEATHER_HAZE;
    if (code == 503 || code == 504 || code == 507 || code == 508) return MONITOR_WEATHER_DUST_STORM;
    return MONITOR_WEATHER_SCATTERED_CLOUDS;
}

static bool read_qweather_config(char *host, size_t host_size, char *key, size_t key_size)
{
    if (host_size == 0 || key_size == 0) return false;
    host[0] = '\0';
    key[0] = '\0';
    nvs_handle_t nvs;
    if (nvs_open("monitor_loc", NVS_READONLY, &nvs) != ESP_OK) return false;
    size_t host_len = host_size, key_len = key_size;
    esp_err_t host_err = nvs_get_str(nvs, "qweather_host", host, &host_len);
    esp_err_t key_err = nvs_get_str(nvs, "qweather_key", key, &key_len);
    nvs_close(nvs);
    return host_err == ESP_OK && key_err == ESP_OK && host[0] != '\0' && key[0] != '\0';
}

static void update_weather(void)
{
    float lat, lon;
    bool location_ok = read_configured_location(&lat, &lon);
    if (!location_ok) {
        portENTER_CRITICAL(&s_data_mux);
        location_ok = s_data.location_valid;
        if (location_ok) { lat = s_data.latitude; lon = s_data.longitude; }
        portEXIT_CRITICAL(&s_data_mux);
    }
    if (!location_ok) location_ok = locate_by_public_ip(&lat, &lon);
    if (!location_ok) { ESP_LOGW(TAG, "location unavailable"); return; }

    char host[192];
    char key[128];
    if (!read_qweather_config(host, sizeof(host), key, sizeof(key))) {
        ESP_LOGW(TAG, "QWeather API host/key is not configured; set both in provisioning Advanced settings");
        return;
    }

    char weather_url[384];
    char aqi_url[384];
    snprintf(weather_url, sizeof(weather_url),
             "https://%s/v7/weather/now?location=%.5f,%.5f&key=%s", host, lon, lat, key);
    snprintf(aqi_url, sizeof(aqi_url),
             "https://%s/airquality/v1/current/%.5f/%.5f?key=%s&lang=zh", host, lat, lon, key);

    char *response = (char *)malloc(RESPONSE_MAX);
    if (response == NULL) {
        ESP_LOGE(TAG, "weather response allocation failed (%d bytes), free heap=%u",
                 RESPONSE_MAX, (unsigned)esp_get_free_heap_size());
        return;
    }
    bool weather_ok = false, aqi_ok = false;
    bool display_changed = false;
    bool initial_data;
    portENTER_CRITICAL(&s_data_mux);
    initial_data = !s_data.weather_valid && !s_data.aqi_valid;
    portEXIT_CRITICAL(&s_data_mux);
    if (http_get(weather_url, response, RESPONSE_MAX, key)) {
        cJSON *json = cJSON_Parse(response);
        cJSON *api_code = json ? cJSON_GetObjectItem(json, "code") : NULL;
        cJSON *now = json ? cJSON_GetObjectItem(json, "now") : NULL;
        cJSON *code = now ? cJSON_GetObjectItem(now, "icon") : NULL;
        if (cJSON_IsString(api_code) && strcmp(api_code->valuestring, "200") != 0) {
            ESP_LOGW(TAG, "QWeather weather API returned code=%s", api_code->valuestring);
        }
        if (cJSON_IsString(code)) {
            int condition_code = atoi(code->valuestring);
            portENTER_CRITICAL(&s_data_mux);
            monitor_weather_condition_t condition = condition_from_qweather(condition_code);
            display_changed |= !s_data.weather_valid || s_data.condition != condition;
            s_data.condition = condition;
            s_data.weather_valid = true;
            portEXIT_CRITICAL(&s_data_mux);
            weather_ok = true;
        }
        if (json == NULL) {
            ESP_LOGW(TAG, "QWeather weather response invalid");
        }
        if (json) cJSON_Delete(json);
    }
    if (http_get(aqi_url, response, RESPONSE_MAX, key)) {
        cJSON *json = cJSON_Parse(response);
        cJSON *indexes = json ? cJSON_GetObjectItem(json, "indexes") : NULL;
        cJSON *index = cJSON_IsArray(indexes) ? cJSON_GetArrayItem(indexes, 0) : NULL;
        cJSON *aqi_display = index ? cJSON_GetObjectItem(index, "aqiDisplay") : NULL;
        cJSON *aqi = index ? cJSON_GetObjectItem(index, "aqi") : NULL;
        int value = cJSON_IsString(aqi_display) ? atoi(aqi_display->valuestring) :
                    (cJSON_IsNumber(aqi) ? aqi->valueint : (cJSON_IsString(aqi) ? atoi(aqi->valuestring) : -1));
        if (value >= 0) {
            portENTER_CRITICAL(&s_data_mux);
            display_changed |= !s_data.aqi_valid || s_data.aqi != value;
            s_data.aqi = value;
            s_data.aqi_valid = true;
            portEXIT_CRITICAL(&s_data_mux);
            aqi_ok = true;
        }
        if (json == NULL) {
            ESP_LOGW(TAG, "QWeather air response invalid");
        }
        if (json) cJSON_Delete(json);
    }
    free(response);
    portENTER_CRITICAL(&s_data_mux); s_data.latitude = lat; s_data.longitude = lon; s_data.location_valid = true; portEXIT_CRITICAL(&s_data_mux);
    if (display_changed) monitor_display_refresh_request_reason(
        initial_data ? MONITOR_REFRESH_RECOVER : MONITOR_REFRESH_WEATHER);
    ESP_LOGI(TAG, "update: weather=%s aqi=%s", weather_ok ? "ok" : "failed", aqi_ok ? "ok" : "failed");
}

static void weather_task(void *arg)
{
    (void)arg;
    for (;;) {
        uint32_t notified = 0;
        xTaskNotifyWait(0, UINT32_MAX, &notified, portMAX_DELAY);
        if (s_wifi_available && monitor_clock_is_valid() && (notified & WEATHER_EVT_UPDATE)) update_weather();
    }
}

esp_err_t monitor_weather_start(void)
{
    if (xTaskCreate(weather_task, "monitor_weather", 16384, NULL, 3, &s_task) != pdPASS) return ESP_ERR_NO_MEM;
    return ESP_OK;
}

void monitor_weather_request_refresh(void)
{
    if (s_task != NULL) xTaskNotify(s_task, WEATHER_EVT_UPDATE, eSetBits);
}

void monitor_weather_on_wifi_connected(void)
{
    s_wifi_available = true;
    monitor_weather_request_refresh();
}

void monitor_weather_on_wifi_disconnected(void)
{
    s_wifi_available = false;
    if (s_task != NULL) xTaskNotify(s_task, WEATHER_EVT_WIFI_CHANGED, eSetBits);
}

void monitor_weather_on_time_valid(void)
{
    monitor_weather_request_refresh();
}

void monitor_weather_get(monitor_weather_data_t *out)
{
    if (!out) return;
    portENTER_CRITICAL(&s_data_mux); *out = s_data; portEXIT_CRITICAL(&s_data_mux);
}
