#pragma once
#include "companion.pb.h"
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

/* Envelope field numbers that use callback-allocated nanopb messages. */
#define MONITOR_SERIAL_FIELD_HELLO 25u
#define MONITOR_SERIAL_FIELD_AUTH 26u
#define MONITOR_SERIAL_FIELD_ENCRYPTED_CONFIG 27u

typedef struct {
    int32_t protocol_version;
    char step[16];
    char host_id[37];
    char host_name[65];
    char host_nonce[44];
    char device_id[37];
    char device_name[65];
    char firmware_version[65];
    char device_nonce[44];
    char pairing_id[37];
    bool has_pairing_id;
    int32_t max_payload_bytes;
    bool has_max_payload_bytes;
    bool supports_v1;
} monitor_serial_hello_t;

typedef struct {
    int32_t protocol_version;
    char step[16];
    char host_id[37];
    char device_id[37];
    char pairing_id[37];
    char session_id[37];
    char host_nonce[44];
    char device_nonce[44];
    char proof[65];
    bool has_proof;
    bool accepted;
    bool has_accepted;
} monitor_serial_auth_t;

typedef struct {
    int32_t protocol_version;
    char session_id[37];
    char request_id[37];
    char algorithm[16];
    uint8_t nonce[16];
    size_t nonce_length;
    uint8_t tag[24];
    size_t tag_length;
    uint8_t ciphertext[4096];
    size_t ciphertext_length;
} monitor_serial_encrypted_t;

typedef struct {
    const char *step;
    const char *host_id;
    const char *device_id;
    const char *pairing_id;
    const char *session_id;
    const char *host_nonce;
    const char *device_nonce;
    const char *proof;
    bool has_accepted;
    bool accepted;
} monitor_serial_auth_out_t;

typedef struct {
    int32_t protocol_version;
    const char *host_id;
    const char *host_name;
    const char *host_nonce;
    const char *device_id;
    const char *device_name;
    const char *firmware_version;
    const char *device_nonce;
    const char *pairing_id;
    int32_t max_payload_bytes;
} monitor_serial_hello_out_t;

/* Returns the single payload field span of a wire_version 2 Envelope. Unknown
 * fields are skipped; duplicate or missing payload fields are rejected. */
bool monitor_serial_envelope_payload(const uint8_t *data, size_t length, uint32_t *field,
                                     const uint8_t **payload, size_t *payload_length);
bool monitor_serial_hello_decode(const uint8_t *data, size_t length, monitor_serial_hello_t *out);
bool monitor_serial_auth_decode(const uint8_t *data, size_t length, monitor_serial_auth_t *out);
bool monitor_serial_encrypted_decode(const uint8_t *data, size_t length, monitor_serial_encrypted_t *out);

/* Writes the inner message; wrap with monitor_serial_envelope_wrap. */
bool monitor_serial_hello_encode(const monitor_serial_hello_out_t *in, uint8_t *out, size_t capacity, size_t *length);
bool monitor_serial_auth_encode(const monitor_serial_auth_out_t *in, uint8_t *out, size_t capacity, size_t *length);
bool monitor_serial_encrypted_encode(const monitor_serial_encrypted_t *in, uint8_t *out, size_t capacity, size_t *length);
bool monitor_serial_envelope_wrap(uint32_t field, const uint8_t *inner, size_t length,
                                  uint8_t *out, size_t capacity, size_t *written);

/* Static nanopb messages (Config, Snapshot, Heartbeat, Ack, Goodbye, ...). */
bool monitor_serial_envelope_encode(const esp32monitor_companion_Envelope *envelope,
                                    uint8_t *out, size_t capacity, size_t *written);
bool monitor_serial_envelope_decode(esp32monitor_companion_Envelope *out, const uint8_t *data, size_t length);
