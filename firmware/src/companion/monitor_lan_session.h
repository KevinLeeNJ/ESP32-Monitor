#pragma once
#include "companion.pb.h"
#include <stdbool.h>
#include <stdint.h>

typedef enum { MONITOR_LAN_OPEN, MONITOR_LAN_CONFIG, MONITOR_LAN_SNAPSHOT,
               MONITOR_LAN_PROVING, MONITOR_LAN_CONNECTED, MONITOR_LAN_CLOSED } monitor_lan_state_t;
typedef bool (*monitor_lan_send_t)(void *user, const char *suffix, const uint8_t *payload, size_t length, unsigned qos);
/* Apply must atomically copy the snapshot/heartbeat before returning true. */
typedef bool (*monitor_lan_apply_t)(void *user, const esp32monitor_companion_Snapshot *snapshot,
                                    const esp32monitor_companion_Heartbeat *heartbeat);

typedef struct monitor_lan_session monitor_lan_session_t;
monitor_lan_session_t *monitor_lan_create(const char *host, const char *device, const char *pairing,
                                         const char *session, const char *open_request,
                                         const esp32monitor_companion_Config *config,
                                         monitor_lan_send_t send, monitor_lan_apply_t apply, void *user,
                                         uint64_t now_ms);
/* Serial sessions start after the encrypted Config was sent and skip Open/Presence. */
monitor_lan_session_t *monitor_lan_create_serial(const char *host, const char *device, const char *pairing,
                                                const char *session, const esp32monitor_companion_Config *config,
                                                monitor_lan_send_t send, monitor_lan_apply_t apply, void *user,
                                                uint64_t now_ms);
bool monitor_lan_config_accepted(monitor_lan_session_t *s, uint64_t now_ms);
uint32_t monitor_lan_snapshot_seq(const monitor_lan_session_t *s);
void monitor_lan_destroy(monitor_lan_session_t *s);
bool monitor_lan_start(monitor_lan_session_t *s, int64_t unix_seconds);
bool monitor_lan_update_config(monitor_lan_session_t *s, const esp32monitor_companion_Config *config,
                               uint64_t now_ms, int64_t unix_seconds);
bool monitor_lan_receive(monitor_lan_session_t *s, const char *suffix,
                         const uint8_t *data, size_t length, uint64_t now_ms, int64_t unix_seconds);
/* Best effort: publishes up/goodbye and closes the session. */
bool monitor_lan_goodbye(monitor_lan_session_t *s, const char *reason, int64_t unix_seconds);
/* Reason supplied by the peer in a valid down/goodbye, or NULL. */
const char *monitor_lan_close_reason(const monitor_lan_session_t *s);
bool monitor_lan_alive(const monitor_lan_session_t *s, uint64_t now_ms);
monitor_lan_state_t monitor_lan_state(const monitor_lan_session_t *s);
bool monitor_lan_presence(const char *host, const char *device, const char *session,
                          bool online, uint8_t *out, size_t capacity, size_t *length);
