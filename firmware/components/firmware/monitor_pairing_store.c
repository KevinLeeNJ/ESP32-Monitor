#include "monitor_pairing_store.h"
#include "monitor_discovery_auth.h"
#include "monitor_pairing_crypto.h"
#include "esp_random.h"
#include "nvs.h"
#include <stdio.h>
#include <string.h>

#define NAMESPACE "companion"
#define PAIR_KEY "pair_v1"
#define STATE_VERSION 2U
#define MAX_REVOCATIONS 4

/* Approve (serial/MQTT worker tasks) and revoke (button task) run
 * concurrently on different tasks. NVS serializes single calls, but the
 * read-modify-write sequences here are not atomic without this store-wide
 * lock; a recursive mutex also covers identity() called inside them. */
static void *s_store_lock;

#ifdef ESP_PLATFORM
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

static portMUX_TYPE s_store_lock_mux = portMUX_INITIALIZER_UNLOCKED;

static void *pairing_store_lock(void)
{
    portENTER_CRITICAL(&s_store_lock_mux);
    SemaphoreHandle_t lock = s_store_lock;
    portEXIT_CRITICAL(&s_store_lock_mux);
    if (lock) return lock;
    SemaphoreHandle_t created = xSemaphoreCreateRecursiveMutex();
    portENTER_CRITICAL(&s_store_lock_mux);
    if (!s_store_lock && created) s_store_lock = created;
    lock = s_store_lock;
    portEXIT_CRITICAL(&s_store_lock_mux);
    if (created && created != lock) vSemaphoreDelete(created);
    return lock;
}

static void pairing_store_take(void *lock) { xSemaphoreTakeRecursive(lock, portMAX_DELAY); }
static void pairing_store_give(void *lock) { xSemaphoreGiveRecursive(lock); }
#else
#include <pthread.h>
#include <stdlib.h>

static pthread_once_t s_store_once = PTHREAD_ONCE_INIT;

static void pairing_store_lock_init(void)
{
    pthread_mutexattr_t attr;
    pthread_mutexattr_init(&attr);
    pthread_mutexattr_settype(&attr, PTHREAD_MUTEX_RECURSIVE);
    pthread_mutex_t *mutex = malloc(sizeof *mutex);
    if (mutex && pthread_mutex_init(mutex, &attr) == 0) s_store_lock = mutex;
    else free(mutex);
    pthread_mutexattr_destroy(&attr);
}

static void *pairing_store_lock(void)
{
    pthread_once(&s_store_once, pairing_store_lock_init);
    return s_store_lock;
}

static void pairing_store_take(void *lock) { pthread_mutex_lock(lock); }
static void pairing_store_give(void *lock) { pthread_mutex_unlock(lock); }
#endif

typedef struct {
    char host_id[37];
    char pairing_id[37];
    uint8_t discovery_key[32];
} stored_revocation_t;

typedef struct {
    uint32_t version;
    uint32_t has_pair;
    monitor_pairing_record_t pair;
    uint32_t revocation_count;
    stored_revocation_t revocations[MAX_REVOCATIONS];
} stored_state_t;

/* Version 1 held only an active pairing. */
typedef struct {
    uint32_t version;
    monitor_pairing_record_t pair;
} stored_pair_v1_t;

static bool uuid_valid(const char *s)
{
    if (strnlen(s, 37) != 36) return false;
    for (unsigned i = 0; i < 36; i++) {
        if (i == 8 || i == 13 || i == 18 || i == 23) {
            if (s[i] != '-') return false;
        } else if (!((s[i] >= '0' && s[i] <= '9') || (s[i] >= 'a' && s[i] <= 'f'))) return false;
    }
    return true;
}

static bool token_valid(const char token[44])
{
    if (strnlen(token, 44) != 43) return false;
    for (unsigned i = 0; i < 43; i++) {
        char c = token[i];
        if (!((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
              (c >= '0' && c <= '9') || c == '-' || c == '_')) return false;
    }
    char last = token[42];
    return last == 'A' || last == 'E' || last == 'I' || last == 'M' || last == 'Q' || last == 'U' ||
           last == 'Y' || last == 'c' || last == 'g' || last == 'k' || last == 'o' || last == 's' ||
           last == 'w' || last == '0' || last == '4' || last == '8';
}

static bool record_valid(const monitor_pairing_record_t *r)
{
    return uuid_valid(r->host_id) && uuid_valid(r->pairing_id) &&
           token_valid(r->device_token) && token_valid(r->serial_key);
}

static bool state_valid(const stored_state_t *s)
{
    if (s->version != STATE_VERSION || s->has_pair > 1 || s->revocation_count > MAX_REVOCATIONS)
        return false;
    if (s->has_pair && !record_valid(&s->pair)) return false;
    for (unsigned i = 0; i < s->revocation_count; i++)
        if (!uuid_valid(s->revocations[i].host_id) || !uuid_valid(s->revocations[i].pairing_id))
            return false;
    return true;
}

static esp_err_t read_state(nvs_handle_t nvs, stored_state_t *state)
{
    memset(state, 0, sizeof *state);
    size_t len = sizeof *state;
    esp_err_t e = nvs_get_blob(nvs, PAIR_KEY, state, &len);
    if (e != ESP_OK) return e;
    if (len == sizeof(stored_pair_v1_t)) {
        stored_pair_v1_t old;
        memcpy(&old, state, sizeof old);
        memset(state, 0, sizeof *state);
        if (old.version != 1 || !record_valid(&old.pair)) return ESP_ERR_INVALID_STATE;
        state->version = STATE_VERSION;
        state->has_pair = 1;
        state->pair = old.pair;
        return ESP_OK;
    }
    return len == sizeof *state && state_valid(state) ? ESP_OK : ESP_ERR_INVALID_STATE;
}

/* Keep only the derived discovery key. The former host can verify the
 * revocation with the serial key it still holds, while the monitor no longer
 * retains any session credential. */
static void add_revocation(stored_state_t *s, const monitor_pairing_record_t *r, const char *device)
{
    if (!device || !r->pairing_id[0]) return;
    unsigned index = s->revocation_count;
    for (unsigned i = 0; i < s->revocation_count; i++) {
        if (!strcmp(s->revocations[i].pairing_id, r->pairing_id)) { index = i; break; }
    }
    if (index == s->revocation_count) {
        if (s->revocation_count == MAX_REVOCATIONS) {
            memmove(&s->revocations[0], &s->revocations[1], sizeof(s->revocations[0]) * (MAX_REVOCATIONS - 1));
            index = MAX_REVOCATIONS - 1;
        } else {
            s->revocation_count++;
        }
    }
    stored_revocation_t *slot = &s->revocations[index];
    memset(slot, 0, sizeof *slot);
    memcpy(slot->host_id, r->host_id, sizeof slot->host_id);
    memcpy(slot->pairing_id, r->pairing_id, sizeof slot->pairing_id);
    (void)monitor_discovery_key(r->serial_key, r->host_id, device, r->pairing_id,
                                monitor_pairing_sha256, monitor_pairing_hmac, slot->discovery_key);
}

esp_err_t monitor_pairing_identity(char out[37])
{
    if (!out) return ESP_ERR_INVALID_ARG;
    void *lock = pairing_store_lock();
    if (lock) pairing_store_take(lock);
    nvs_handle_t nvs;
    esp_err_t e = nvs_open(NAMESPACE, NVS_READWRITE, &nvs);
    if (e != ESP_OK) {
        if (lock) pairing_store_give(lock);
        return e;
    }
    char id[37] = {0}; size_t len = sizeof id;
    e = nvs_get_str(nvs, "device_id", id, &len);
    if (e == ESP_ERR_NVS_NOT_FOUND) {
        uint8_t b[16]; esp_fill_random(b, sizeof b);
        b[6] = (b[6] & 0x0f) | 0x40; b[8] = (b[8] & 0x3f) | 0x80;
        snprintf(id, sizeof id, "%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
                 b[0],b[1],b[2],b[3],b[4],b[5],b[6],b[7],b[8],b[9],b[10],b[11],b[12],b[13],b[14],b[15]);
        e = nvs_set_str(nvs, "device_id", id);
        if (e == ESP_OK) e = nvs_commit(nvs);
    }
    if (e == ESP_OK && !uuid_valid(id)) e = ESP_ERR_INVALID_STATE;
    if (e == ESP_OK) memcpy(out, id, sizeof id);
    nvs_close(nvs);
    if (lock) pairing_store_give(lock);
    return e;
}

esp_err_t monitor_pairing_store_load(monitor_pairing_record_t *out)
{
    if (!out) return ESP_ERR_INVALID_ARG;
    void *lock = pairing_store_lock();
    if (lock) pairing_store_take(lock);
    nvs_handle_t nvs;
    esp_err_t e = nvs_open(NAMESPACE, NVS_READONLY, &nvs);
    if (e == ESP_OK) {
        stored_state_t state;
        e = read_state(nvs, &state);
        nvs_close(nvs);
        if (e == ESP_OK && !state.has_pair) e = ESP_ERR_NVS_NOT_FOUND;
        if (e == ESP_OK) *out = state.pair;
        memset(&state, 0, sizeof state);
    }
    if (lock) pairing_store_give(lock);
    return e;
}

size_t monitor_pairing_store_revocations(monitor_pairing_revocation_t *out, size_t capacity)
{
    if (!out || !capacity) return 0;
    void *lock = pairing_store_lock();
    if (lock) pairing_store_take(lock);
    nvs_handle_t nvs;
    size_t count = 0;
    if (nvs_open(NAMESPACE, NVS_READONLY, &nvs) == ESP_OK) {
        stored_state_t state;
        esp_err_t e = read_state(nvs, &state);
        nvs_close(nvs);
        if (e == ESP_OK) {
            count = state.revocation_count < capacity ? state.revocation_count : capacity;
            for (size_t i = 0; i < count; i++) {
                memcpy(out[i].host_id, state.revocations[i].host_id, sizeof out[i].host_id);
                memcpy(out[i].pairing_id, state.revocations[i].pairing_id, sizeof out[i].pairing_id);
                memcpy(out[i].discovery_key, state.revocations[i].discovery_key, sizeof out[i].discovery_key);
            }
        }
        memset(&state, 0, sizeof state);
    }
    if (lock) pairing_store_give(lock);
    return count;
}

esp_err_t monitor_pairing_store_approved(const monitor_pairing_session_t *session)
{
    if (!session || session->state != MONITOR_PAIR_APPROVED) return ESP_ERR_INVALID_ARG;
    const esp32monitor_companion_PairResponse *r = &session->approved;
    if (!monitor_pairing_validate_response(r, r->host_id)) return ESP_ERR_INVALID_ARG;
    void *lock = pairing_store_lock();
    if (lock) pairing_store_take(lock);
    char device[37] = {0};
    bool have_device = monitor_pairing_identity(device) == ESP_OK;
    nvs_handle_t nvs;
    esp_err_t e = nvs_open(NAMESPACE, NVS_READWRITE, &nvs);
    if (e != ESP_OK) {
        if (lock) pairing_store_give(lock);
        return e;
    }
    stored_state_t state;
    e = read_state(nvs, &state);
    if (e != ESP_OK) {
        /* A missing or damaged blob must not block a fresh pairing; the old
         * revocation history is lost, not the ability to pair. */
        memset(&state, 0, sizeof state);
        state.version = STATE_VERSION;
    }
    if (state.has_pair && strcmp(state.pair.pairing_id, r->pairing_id)) {
        add_revocation(&state, &state.pair, have_device ? device : NULL);
    }
    state.version = STATE_VERSION;
    state.has_pair = 1;
    memcpy(state.pair.host_id, r->host_id, sizeof state.pair.host_id);
    memcpy(state.pair.pairing_id, r->pairing_id, sizeof state.pair.pairing_id);
    memcpy(state.pair.device_token, r->device_token, sizeof state.pair.device_token);
    memcpy(state.pair.serial_key, r->serial_key, sizeof state.pair.serial_key);
    e = nvs_set_blob(nvs, PAIR_KEY, &state, sizeof state);
    if (e == ESP_OK) e = nvs_commit(nvs);
    nvs_close(nvs);
    memset(&state, 0, sizeof state);
    if (lock) pairing_store_give(lock);
    return e;
}

esp_err_t monitor_pairing_store_revoke(void)
{
    void *lock = pairing_store_lock();
    if (lock) pairing_store_take(lock);
    char device[37] = {0};
    bool have_device = monitor_pairing_identity(device) == ESP_OK;
    nvs_handle_t nvs;
    esp_err_t e = nvs_open(NAMESPACE, NVS_READWRITE, &nvs);
    if (e != ESP_OK) {
        if (lock) pairing_store_give(lock);
        return e;
    }
    stored_state_t state;
    e = read_state(nvs, &state);
    if (e == ESP_ERR_NVS_NOT_FOUND) {
        nvs_close(nvs);
        if (lock) pairing_store_give(lock);
        return ESP_OK;
    }
    if (e != ESP_OK) {
        /* Damaged state: erase it so BOOT revoke still succeeds. */
        e = nvs_erase_key(nvs, PAIR_KEY);
        if (e == ESP_ERR_NVS_NOT_FOUND) e = ESP_OK;
        if (e == ESP_OK) e = nvs_commit(nvs);
        nvs_close(nvs);
        if (lock) pairing_store_give(lock);
        return e;
    }
    if (state.has_pair) {
        add_revocation(&state, &state.pair, have_device ? device : NULL);
        memset(&state.pair, 0, sizeof state.pair);
        state.has_pair = 0;
        state.version = STATE_VERSION;
        e = nvs_set_blob(nvs, PAIR_KEY, &state, sizeof state);
        if (e == ESP_OK) e = nvs_commit(nvs);
    } else {
        e = ESP_OK;
    }
    nvs_close(nvs);
    memset(&state, 0, sizeof state);
    if (lock) pairing_store_give(lock);
    return e;
}
