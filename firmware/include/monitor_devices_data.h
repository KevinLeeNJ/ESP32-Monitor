#pragma once
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#ifdef __cplusplus
extern "C" {
#endif
#define MONITOR_DEVICES_MAX 4
#define MONITOR_DEVICES_BODY_MAX 4096
#define MONITOR_DEVICES_URL_MAX 192
#define MONITOR_DEVICES_TOKEN_MAX 128

typedef struct {
    char name[5];
    char value[9];
    int percent;
    bool percent_valid;
} monitor_device_metric_t;
typedef struct {
    char name[9];
    char uptime[9];
    uint8_t metric_count;
    monitor_device_metric_t metrics[3];
} monitor_device_t;
typedef enum {
    MONITOR_DEVICES_NOT_CONFIGURED = 0,
    MONITOR_DEVICES_WAITING,
    MONITOR_DEVICES_READY,
    MONITOR_DEVICES_STALE,
    MONITOR_DEVICES_UNAVAILABLE,
    MONITOR_DEVICES_AUTH_ERROR,
} monitor_devices_status_t;
typedef struct {
    monitor_device_t devices[MONITOR_DEVICES_MAX];
    uint8_t count;
    monitor_devices_status_t status;
    uint32_t version;
} monitor_devices_snapshot_t;
/* In-place normalization removes trailing slashes, preserving explicit ports. */
bool monitor_devices_normalize_url(char *url);
bool monitor_devices_valid_token(const char *token);
/* Invalid responses leave out unchanged. Input has an extra NUL at body[len]. */
bool monitor_devices_parse(const char *body, size_t len, monitor_devices_snapshot_t *out);
#ifdef __cplusplus
}
#endif
