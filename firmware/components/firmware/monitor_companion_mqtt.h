#pragma once
#include "companion.pb.h"
#include "esp_err.h"
#include <stdbool.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct {
    uint32_t generation;
    bool connected;
    bool has_snapshot;
    bool has_heartbeat;
    uint64_t snapshot_received_ms;
    uint64_t heartbeat_received_ms;
    esp32monitor_companion_Snapshot snapshot;
    esp32monitor_companion_Heartbeat heartbeat;
} monitor_companion_mqtt_snapshot_t;

/* Reserve the worker stack before network services fragment internal RAM. */
esp_err_t monitor_companion_mqtt_init(void);
/* Shared internal-RAM worker: one session or pairing transaction at a time. */
esp_err_t monitor_companion_worker_submit(void (*work)(void *), void *arg);
/* Invoke only for a fresh, authenticated discovery offer. Runs asynchronously. */
esp_err_t monitor_companion_mqtt_start(const char *ipv4, uint16_t port, const char *host_id,
                                       const char *device_id, const char *pairing_id,
                                       const char *session_id, const char *device_token);
void monitor_companion_mqtt_stop(void);
/* Wait until the session has released its buffers; the worker stack is retained. */
bool monitor_companion_mqtt_wait_stopped(uint32_t timeout_ms);
/* Best effort: publish up/goodbye(pairing_revoked) on the active session, then stop. */
void monitor_companion_mqtt_revoke(void);
/* The caller supplies storage; this is a consistent detached copy. */
bool monitor_companion_mqtt_get(monitor_companion_mqtt_snapshot_t *out);
/* Cheap change marker; inspect the full copy only when it advances. */
uint32_t monitor_companion_mqtt_generation(void);
/* Transport-neutral snapshot sink shared by LAN and Serial sessions. */
bool monitor_companion_sink_apply(void *user, const esp32monitor_companion_Snapshot *snapshot,
                                  const esp32monitor_companion_Heartbeat *heartbeat);
void monitor_companion_sink_connection(bool connected, bool clear_snapshot);
void monitor_companion_sink_init(void);
bool monitor_companion_mqtt_running(void);

#ifdef __cplusplus
}
#endif
