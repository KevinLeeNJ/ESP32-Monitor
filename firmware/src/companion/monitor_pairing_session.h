#pragma once
#include "monitor_pairing_code.h"

typedef enum {
    MONITOR_PAIR_IDLE, MONITOR_PAIR_WAITING, MONITOR_PAIR_CONFIRMED,
    MONITOR_PAIR_APPROVED, MONITOR_PAIR_CANCELLED, MONITOR_PAIR_EXPIRED,
    MONITOR_PAIR_REJECTED
} monitor_pairing_state_t;

typedef struct {
    monitor_pairing_state_t state;
    uint64_t deadline_ms;
    char code[7];
    esp32monitor_companion_PairResponse initial;
    esp32monitor_companion_PairResponse approved;
} monitor_pairing_session_t;

/* Caller serializes events and uses a monotonic clock. */
bool monitor_pairing_begin(monitor_pairing_session_t *session,
                           const esp32monitor_companion_PairResponse *response,
                           const char *device_nonce, const char *device_id,
                           const char *expected_host, uint64_t now_ms,
                           monitor_pairing_hash_t hash);
bool monitor_pairing_confirm(monitor_pairing_session_t *session, uint64_t now_ms);
bool monitor_pairing_apply(monitor_pairing_session_t *session,
                           const esp32monitor_companion_PairResponse *response, uint64_t now_ms);
void monitor_pairing_cancel(monitor_pairing_session_t *session);
void monitor_pairing_tick(monitor_pairing_session_t *session, uint64_t now_ms);
