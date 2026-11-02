#pragma once
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#define MONITOR_SERIAL_HEADER 12u
#define MONITOR_SERIAL_MAX_PAYLOAD 16336u
#define MONITOR_SERIAL_MAX_UNAUTH 2048u
#define MONITOR_SERIAL_MAX_FRAME 16384u

typedef enum {
    MONITOR_SERIAL_HELLO = 0x01,
    MONITOR_SERIAL_AUTH = 0x02,
    MONITOR_SERIAL_PAIR = 0x03,
    MONITOR_SERIAL_CONFIG = 0x10,
    MONITOR_SERIAL_CONFIG_ACK = 0x11,
    MONITOR_SERIAL_SNAPSHOT = 0x20,
    MONITOR_SERIAL_HEARTBEAT = 0x21,
    MONITOR_SERIAL_ACK = 0x22,
    MONITOR_SERIAL_GOODBYE = 0x7e,
    MONITOR_SERIAL_ERROR = 0x7f,
} monitor_serial_type_t;

typedef struct {
    uint8_t type;
    uint32_t seq;
    uint8_t header[MONITOR_SERIAL_HEADER];
    const uint8_t *payload;
    size_t length;
} monitor_serial_frame_t;

/* The payload is valid only for the duration of the callback. */
typedef void (*monitor_serial_frame_fn)(void *user, const monitor_serial_frame_t *frame);

/* The buffer is 16 KiB; allocate the decoder on the heap or statically. */
typedef struct {
    uint8_t data[MONITOR_SERIAL_MAX_FRAME];
    size_t length;
    uint64_t candidate_ms;
    bool candidate;
} monitor_serial_decoder_t;

void monitor_serial_decoder_init(monitor_serial_decoder_t *decoder);
size_t monitor_serial_feed(monitor_serial_decoder_t *decoder, const uint8_t *data, size_t length,
                           uint64_t now_ms, const uint8_t key[32], uint8_t direction,
                           monitor_serial_frame_fn callback, void *user);
size_t monitor_serial_tick(monitor_serial_decoder_t *decoder, uint64_t now_ms,
                           const uint8_t key[32], uint8_t direction,
                           monitor_serial_frame_fn callback, void *user);

/* key == NULL builds an unauthenticated handshake frame. direction: 1 host->monitor, 2 monitor->host. */
bool monitor_serial_encode(uint8_t type, uint32_t seq, const uint8_t *payload, size_t length,
                           const uint8_t key[32], uint8_t direction,
                           uint8_t *out, size_t capacity, size_t *written);

typedef struct {
    uint8_t frame[32];
    uint8_t config[32];
    char device_proof[65];
    char host_proof[65];
} monitor_serial_keys_t;

bool monitor_serial_derive_keys(const char *host, const char *device, const char *pairing,
                                const char *session, const char *host_nonce,
                                const char *device_nonce, const char *serial_key,
                                monitor_serial_keys_t *out);

/* Crypto backend: production uses PSA; host tests may substitute. */
bool monitor_serial_sha256(const uint8_t *data, size_t length, uint8_t out[32]);
bool monitor_serial_hmac(const uint8_t key[32], const uint8_t *data, size_t length, uint8_t out[32]);
bool monitor_serial_hmac_frame(const uint8_t key[32], uint8_t direction,
                               const uint8_t header[MONITOR_SERIAL_HEADER],
                               const uint8_t *payload, size_t length, uint8_t out[32]);

/* AES-256-GCM for EncryptedConfig. Output/input is ciphertext || 16-byte tag. */
bool monitor_serial_aead_encrypt(const uint8_t key[32], const uint8_t nonce[12],
                                 const uint8_t *aad, size_t aad_length,
                                 const uint8_t *plain, size_t length,
                                 uint8_t *out, size_t capacity, size_t *written);
bool monitor_serial_aead_decrypt(const uint8_t key[32], const uint8_t nonce[12],
                                 const uint8_t *aad, size_t aad_length,
                                 const uint8_t *sealed, size_t length,
                                 uint8_t *out, size_t capacity, size_t *written);
bool monitor_serial_config_nonce(uint32_t seq, uint8_t nonce[12]);
/* lp("ESP32MON-CONFIG-V1") || lp(session) || lp(request), little-endian lengths. */
bool monitor_serial_config_aad(const char *session, const char *request,
                               uint8_t *out, size_t capacity, size_t *length);
