#include "monitor_weather.h"
#include "monitor_display_refresh.h"
#include "monitor_wifi_manager.h"
#include "monitor_clock.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <math.h>
#include <limits.h>

#include "cJSON.h"
#include "esp_check.h"
#include "esp_crt_bundle.h"
#include "esp_http_client.h"
#include "esp_log.h"
#include "esp_task_wdt.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "nvs.h"
#include "zlib.h"

#define TAG "monitor_weather"
#define WEATHER_EVT_UPDATE BIT0
#define WEATHER_EVT_WIFI_CHANGED BIT1
#define HTTP_TIMEOUT_MS 4000
#define RESPONSE_MAX 8192

static TaskHandle_t s_task;
static volatile bool s_wifi_available;
static portMUX_TYPE s_data_mux = portMUX_INITIALIZER_UNLOCKED;
static monitor_weather_data_t s_data = {
    .condition = MONITOR_WEATHER_CLEAR, .aqi = -1,
    .latitude = 0, .longitude = 0,
    .location_valid = false, .weather_valid = false, .aqi_valid = false,
};

static void feed_task_watchdog(void)
{
    ESP_ERROR_CHECK(esp_task_wdt_reset());
}

typedef struct {
    char *buffer;
    size_t capacity;
    size_t length;
    z_stream inflater;
    bool gzip;
    bool initialized;
    bool finished;
    bool failed;
} response_context_t;

static void response_reset(response_context_t *ctx)
{
    if (ctx->initialized) inflateEnd(&ctx->inflater);
    char *buffer = ctx->buffer;
    size_t capacity = ctx->capacity;
    *ctx = (response_context_t){.buffer = buffer, .capacity = capacity};
    buffer[0] = '\0';
}

static bool response_collect(response_context_t *ctx, const void *data, size_t length)
{
    if (ctx->failed || ctx->finished) return false;
    if (!ctx->gzip) {
        if (length > ctx->capacity - 1 - ctx->length) return false;
        memcpy(ctx->buffer + ctx->length, data, length);
        ctx->length += length;
        ctx->buffer[ctx->length] = '\0';
        return true;
    }
    if (!ctx->initialized) {
        /* zlib owns the gzip header, optional fields, CRC32 and ISIZE checks.
         * Its state/window live on the heap, never on the weather task stack. */
        if (inflateInit2(&ctx->inflater, MAX_WBITS + 16) != Z_OK) return false;
        ctx->initialized = true;
    }
    z_stream *stream = &ctx->inflater;
    stream->next_in = (Bytef *)data;
    stream->avail_in = (uInt)length;
    while (stream->avail_in > 0) {
        feed_task_watchdog();
        stream->next_out = (Bytef *)ctx->buffer + ctx->length;
        /* Include the NUL slot so a full JSON buffer can still consume a
         * trailer in a later callback. Any output into that slot is rejected. */
        stream->avail_out = (uInt)(ctx->capacity - ctx->length);
        uInt input_before = stream->avail_in;
        uInt output_before = stream->avail_out;
        int result = inflate(stream, Z_NO_FLUSH);
        ctx->length += output_before - stream->avail_out;
        if (ctx->length >= ctx->capacity) return false;
        ctx->buffer[ctx->length] = '\0';
        if (result == Z_STREAM_END) {
            ctx->finished = true;
            /* This endpoint returns one JSON document, not a gzip archive. */
            return stream->avail_in == 0;
        }
        if (result != Z_OK ||
            (input_before == stream->avail_in && output_before == stream->avail_out)) return false;
    }
    /* All input was consumed synchronously; never retain the SDK buffer. */
    stream->next_in = Z_NULL;
    return true;
}

static esp_err_t http_collect_event(esp_http_client_event_t *event)
{
    feed_task_watchdog();
    response_context_t *ctx = (response_context_t *)event->user_data;
    if (ctx == NULL) return ESP_OK;
    if (event->event_id == HTTP_EVENT_HEADERS_SENT) {
        /* A redirect/authentication retry starts a new response. */
        response_reset(ctx);
    } else if (event->event_id == HTTP_EVENT_ON_HEADER &&
               event->header_key != NULL && event->header_value != NULL &&
               strcasecmp(event->header_key, "Content-Encoding") == 0) {
        if (strcasecmp(event->header_value, "gzip") == 0) ctx->gzip = true;
        else if (strcasecmp(event->header_value, "identity") != 0) ctx->failed = true;
    } else if (event->event_id == HTTP_EVENT_ON_DATA && event->data_len > 0) {
        if (!response_collect(ctx, event->data, (size_t)event->data_len)) ctx->failed = true;
    }
    /* Some SDK dispatch sites ignore callback errors. Keep failure sticky and
     * check it after perform(), so a partial response never reaches cJSON. */
    return ctx->failed ? ESP_FAIL : ESP_OK;
}

static bool http_get(const char *url, char *out, size_t out_size, const char *api_key)
{
    if (out_size == 0 || out_size > UINT_MAX) return false;
    response_context_t response = {.buffer = out, .capacity = out_size};
    out[0] = '\0';
    esp_http_client_config_t config = {
        .url = url,
        .timeout_ms = HTTP_TIMEOUT_MS,
        .buffer_size = 1024,
        .disable_auto_redirect = false,
        .event_handler = http_collect_event,
        .user_data = &response,
        .crt_bundle_attach = esp_crt_bundle_attach,
    };
    esp_http_client_handle_t client = esp_http_client_init(&config);
    if (client == NULL) return false;
    esp_err_t perform_err = esp_http_client_set_header(client, "Accept-Encoding", "gzip, identity");
    if (perform_err == ESP_OK) perform_err = esp_http_client_perform(client);
    int status = esp_http_client_get_status_code(client);
    bool ok = perform_err == ESP_OK && status == 200 && !response.failed &&
              response.length > 0 && (!response.gzip || response.finished) &&
              esp_http_client_is_complete_data_received(client);
    if (!ok) {
        ESP_LOGW(TAG, "HTTP GET failed: err=%s status=%d response_len=%u decode_failed=%d gzip_complete=%d",
                 esp_err_to_name(perform_err), status, (unsigned)response.length,
                 response.failed, !response.gzip || response.finished);
        out[0] = '\0';
    }
    esp_http_client_cleanup(client);
    if (response.initialized) inflateEnd(&response.inflater);
    return ok;
}

static bool parse_coordinate(const char *text, float min, float max, float *out)
{
    char *end;
    float value = strtof(text, &end);
    if (end == text || *end || !isfinite(value) || value < min || value > max) return false;
    *out = value;
    return true;
}

static bool json_nonnegative_int(const cJSON *item, int *out)
{
    if (cJSON_IsNumber(item)) {
        double n = item->valuedouble;
        if (!isfinite(n) || n < 0 || n > INT_MAX || floor(n) != n) return false;
        *out = (int)n;
        return true;
    }
    if (!cJSON_IsString(item) || !item->valuestring[0]) return false;
    int value = 0;
    for (const char *p = item->valuestring; *p; ++p) {
        if (*p < '0' || *p > '9' || value > (INT_MAX - (*p - '0')) / 10) return false;
        value = value * 10 + (*p - '0');
    }
    *out = value;
    return true;
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
    float latitude, longitude;
    if (!parse_coordinate(lat_text, -90, 90, &latitude) ||
        !parse_coordinate(lon_text, -180, 180, &longitude)) return false;
    *lat = latitude;
    *lon = longitude;
    return true;
}

static bool locate_by_public_ip(float *lat, float *lon)
{
    /* PERF may inline this function into update_weather and keep its stack
     * frame alive during the later TLS requests. Do not reserve 8 KB there. */
    char *response = malloc(RESPONSE_MAX);
    if (response == NULL) return false;
    if (!http_get("http://ipwho.is/", response, RESPONSE_MAX, NULL)) {
        free(response);
        return false;
    }
    cJSON *json = cJSON_Parse(response);
    free(response);
    if (json == NULL) return false;
    cJSON *success = cJSON_GetObjectItem(json, "success");
    cJSON *lat_item = cJSON_GetObjectItem(json, "latitude");
    cJSON *lon_item = cJSON_GetObjectItem(json, "longitude");
    bool ok = (success == NULL || cJSON_IsTrue(success)) && cJSON_IsNumber(lat_item) && cJSON_IsNumber(lon_item) &&
              isfinite(lat_item->valuedouble) && isfinite(lon_item->valuedouble) &&
              lat_item->valuedouble >= -90 && lat_item->valuedouble <= 90 &&
              lon_item->valuedouble >= -180 && lon_item->valuedouble <= 180;
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
    feed_task_watchdog();
    char host[192];
    char key[128];
    if (!read_qweather_config(host, sizeof(host), key, sizeof(key))) {
        ESP_LOGW(TAG, "QWeather API host/key is not configured; set both in provisioning Advanced settings");
        return;
    }

    float lat, lon;
    bool location_ok = read_configured_location(&lat, &lon);
    if (!location_ok) {
        portENTER_CRITICAL(&s_data_mux);
        location_ok = s_data.location_valid;
        if (location_ok) { lat = s_data.latitude; lon = s_data.longitude; }
        portEXIT_CRITICAL(&s_data_mux);
    }
    if (!location_ok) location_ok = locate_by_public_ip(&lat, &lon);
    feed_task_watchdog();
    if (!location_ok) { ESP_LOGW(TAG, "location unavailable"); return; }

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
        int condition_code;
        if (cJSON_IsString(api_code) && strcmp(api_code->valuestring, "200") == 0 &&
            cJSON_IsString(code) && json_nonnegative_int(code, &condition_code) &&
            condition_code >= 100 && condition_code <= 999) {
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
    feed_task_watchdog();
    if (http_get(aqi_url, response, RESPONSE_MAX, key)) {
        cJSON *json = cJSON_Parse(response);
        cJSON *indexes = json ? cJSON_GetObjectItem(json, "indexes") : NULL;
        cJSON *index = cJSON_IsArray(indexes) ? cJSON_GetArrayItem(indexes, 0) : NULL;
        cJSON *aqi_display = index ? cJSON_GetObjectItem(index, "aqiDisplay") : NULL;
        cJSON *aqi = index ? cJSON_GetObjectItem(index, "aqi") : NULL;
        int value;
        if (json_nonnegative_int(aqi_display, &value) || json_nonnegative_int(aqi, &value)) {
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
    ESP_ERROR_CHECK(esp_task_wdt_add(NULL));
    for (;;) {
        uint32_t notified = 0;
        /* Wi-Fi, time validity and the clock scheduler all notify this task.
         * Do not wake once a second solely to service an idle watchdog. */
        ESP_ERROR_CHECK(esp_task_wdt_delete(NULL));
        xTaskNotifyWait(0, UINT32_MAX, &notified, portMAX_DELAY);
        ESP_ERROR_CHECK(esp_task_wdt_add(NULL));
        feed_task_watchdog();
        if (s_wifi_available && monitor_clock_is_valid() && (notified & WEATHER_EVT_UPDATE)) update_weather();
    }
}

esp_err_t monitor_weather_start(void)
{
    if (s_task != NULL) return ESP_OK;
    if (xTaskCreate(weather_task, "monitor_weather", 8192, NULL, 3, &s_task) != pdPASS) return ESP_ERR_NO_MEM;
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
