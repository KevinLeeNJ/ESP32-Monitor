#include "monitor_companion_config.h"
#include "monitor_devices_data.h"
#include "nvs.h"
#include <errno.h>
#include <math.h>
#include <stdlib.h>
#include <string.h>

static bool coordinate(const char *text, double min, double max, double *out)
{
    char *end; errno = 0;
    if (!text[0]) return false;
    double v = strtod(text, &end);
    if (errno || *end || !isfinite(v) || v < min || v > max) return false;
    *out = v; return true;
}
static void read_string(nvs_handle_t nvs, const char *key, char *out, size_t size)
{
    if (nvs_get_str(nvs,key,out,&size) != ESP_OK) out[0] = 0;
}

esp_err_t monitor_companion_config_read(esp32monitor_companion_Config *config)
{
    if (!config) return ESP_ERR_INVALID_ARG;
    /* Session identity, epoch and revision belong to the caller. */
    memset(&config->weather, 0, sizeof config->weather);
    memset(&config->devices, 0, sizeof config->devices);
    config->time.enabled = true;
    config->time.utc_offset_minutes = 480; /* monitor_clock currently sets CST-8 */
    strcpy(config->weather.location_mode,"ip");
    nvs_handle_t nvs;
    esp_err_t error=nvs_open("monitor_loc",NVS_READONLY,&nvs);
    if (error == ESP_ERR_NVS_NOT_FOUND) return ESP_OK;
    if (error != ESP_OK) return error;
    char lat[24]={0},lon[24]={0};
    read_string(nvs,"latitude",lat,sizeof lat); read_string(nvs,"longitude",lon,sizeof lon);
    read_string(nvs,"qweather_host",config->weather.qweather_host,sizeof config->weather.qweather_host);
    read_string(nvs,"qweather_key",config->weather.qweather_key,sizeof config->weather.qweather_key);
    read_string(nvs,"devices_api_url",config->devices.devices_api_url,sizeof config->devices.devices_api_url);
    read_string(nvs,"devices_token",config->devices.devices_token,sizeof config->devices.devices_token);
    nvs_close(nvs);
    config->weather.enabled = config->weather.qweather_host[0] && config->weather.qweather_key[0];
    if (config->weather.enabled) {
        config->weather.has_qweather_host=config->weather.has_qweather_key=true;
        double latitude,longitude;
        if (coordinate(lat,-90,90,&latitude) && coordinate(lon,-180,180,&longitude)) {
            strcpy(config->weather.location_mode,"coordinates");
            config->weather.has_latitude=config->weather.has_longitude=true;
            config->weather.latitude=latitude; config->weather.longitude=longitude;
        }
    } else {
        memset(config->weather.qweather_host,0,sizeof config->weather.qweather_host);
        memset(config->weather.qweather_key,0,sizeof config->weather.qweather_key);
    }
    config->devices.enabled = monitor_devices_normalize_url(config->devices.devices_api_url) &&
                              config->devices.devices_api_url[0] && monitor_devices_valid_token(config->devices.devices_token);
    if (config->devices.enabled) config->devices.has_devices_api_url=config->devices.has_devices_token=true;
    else { memset(config->devices.devices_api_url,0,sizeof config->devices.devices_api_url); memset(config->devices.devices_token,0,sizeof config->devices.devices_token); }
    return ESP_OK;
}
