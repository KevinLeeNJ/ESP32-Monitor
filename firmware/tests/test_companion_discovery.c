#include "monitor_discovery.h"
#include "monitor_discovery_auth.h"
#include "monitor_pairing_wire.h"
#include "monitor_pairing_code.h"
#include "monitor_pairing_session.h"
#include "sha256.h"
#include "pb_encode.h"
#include <assert.h>
#include <string.h>
#include <stdio.h>

static bool test_hash(const uint8_t *data, size_t len, uint8_t digest[32])
{
    char hex[65];
    monitor_sha256(data, (uint32_t)len, hex);
    for (unsigned i = 0; i < 32; i++) {
        unsigned value;
        if (sscanf(hex + i * 2, "%2x", &value) != 1) return false;
        digest[i] = (uint8_t)value;
    }
    return true;
}

static bool test_hmac(const uint8_t key[32], const uint8_t *data, size_t len, uint8_t digest[32])
{
    if (len > 1024) return false;
    uint8_t buffer[64 + 1024], inner[32];
    memset(buffer, 0x36, 64);
    for (unsigned i = 0; i < 32; ++i) buffer[i] ^= key[i];
    memcpy(buffer + 64, data, len);
    if (!test_hash(buffer, 64 + len, inner)) return false;
    memset(buffer, 0x5c, 64);
    for (unsigned i = 0; i < 32; ++i) buffer[i] ^= key[i];
    memcpy(buffer + 64, inner, 32);
    return test_hash(buffer, 96, digest);
}

/* Board tests reuse the same assertions under the Unity runner. */
#ifdef ESP_PLATFORM
void monitor_test_companion_discovery(void)
#else
int main(void)
#endif
{
    esp32monitor_companion_Envelope env = esp32monitor_companion_Envelope_init_zero;
    env.wire_version = 2;
    env.which_payload = esp32monitor_companion_Envelope_discovery_tag;
    esp32monitor_companion_Discovery *d = &env.payload.discovery;
    d->protocol_version = 1;
    strcpy(d->type, "monitor_discover");
    strcpy(d->request_id, "request");
    strcpy(d->host_id, "host");
    strcpy(d->nonce, "nonce");
    uint8_t bytes[1024];
    pb_ostream_t stream = pb_ostream_from_buffer(bytes, sizeof bytes);
    assert(pb_encode(&stream, esp32monitor_companion_Envelope_fields, &env));
    /* Independently generated with Python protobuf from companion.proto. */
    static const uint8_t vector[] = {
        0x08,0x02,0x5a,0x2a,0x08,0x02,0x12,0x10,0x6d,0x6f,0x6e,0x69,0x74,0x6f,0x72,0x5f,
        0x64,0x69,0x73,0x63,0x6f,0x76,0x65,0x72,0x1a,0x07,0x72,0x65,0x71,0x75,0x65,0x73,
        0x74,0x22,0x04,0x68,0x6f,0x73,0x74,0x3a,0x05,0x6e,0x6f,0x6e,0x63,0x65
    };
    assert(stream.bytes_written == sizeof vector && !memcmp(bytes, vector, sizeof vector));
    esp32monitor_companion_Discovery decoded = esp32monitor_companion_Discovery_init_zero;
    assert(monitor_discovery_decode(bytes, stream.bytes_written, &decoded));
    assert(!strcmp(decoded.type, "monitor_discover"));
    assert(!strcmp(decoded.host_id, "host"));
    assert(!monitor_discovery_decode(bytes, stream.bytes_written - 1, &decoded));
    size_t n = stream.bytes_written;
    bytes[n++] = 8; bytes[n++] = 2; /* duplicate wire_version */
    assert(!monitor_discovery_decode(bytes, n, &decoded));
    assert(!strcmp(decoded.host_id, "host")); /* failure is atomic */
    bytes[1] = 1;
    assert(!monitor_discovery_decode(bytes, stream.bytes_written, &decoded));
    esp32monitor_companion_Discovery authenticated = esp32monitor_companion_Discovery_init_zero;
    authenticated.protocol_version = 1;
    strcpy(authenticated.type, "monitor_discover");
    strcpy(authenticated.request_id, "44444444-4444-4444-8444-444444444444");
    strcpy(authenticated.host_id, "11111111-1111-4111-8111-111111111111");
    authenticated.has_device_id = authenticated.has_pairing_id = true;
    strcpy(authenticated.device_id, "22222222-2222-4222-8222-222222222222");
    strcpy(authenticated.pairing_id, "33333333-3333-4333-8333-333333333333");
    strcpy(authenticated.nonce, "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE");
    char mac[65];
    const char *key = "AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM";
    assert(monitor_discovery_mac(&authenticated, key, test_hash, test_hmac, mac));
    assert(!strcmp(mac, "267bd0e53aa9f7a6c0e357caaeaa1bd8b82528a80627c0d286f1f9457d6e4bc5"));
    authenticated.has_mac = true; strcpy(authenticated.mac, mac);
    assert(monitor_discovery_verify(&authenticated, key, test_hash, test_hmac));
    size_t auth_size = 0;
    assert(monitor_discovery_announce_paired(&authenticated, authenticated.device_id, "ESP32 Monitor", "1.0.0",
            "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", key, test_hash, test_hmac,
            bytes, sizeof bytes, &auth_size));
    esp32monitor_companion_Discovery auth_reply = esp32monitor_companion_Discovery_init_zero;
    assert(monitor_discovery_decode(bytes, auth_size, &auth_reply));
    assert(auth_reply.has_pairing_open && !auth_reply.pairing_open && auth_reply.has_pairing_id);
    assert(monitor_discovery_verify(&auth_reply, key, test_hash, test_hmac));
    /* The derived key is what a revocation tombstone retains; it must produce
     * the same MAC and authenticate a pairing_open=true revocation reply. */
    uint8_t derived[32];
    assert(monitor_discovery_key(key, authenticated.host_id, authenticated.device_id,
                                 authenticated.pairing_id, test_hash, test_hmac, derived));
    char keyed_mac[65];
    assert(monitor_discovery_mac_keyed(&authenticated, derived, test_hmac, keyed_mac));
    assert(!strcmp(keyed_mac, mac));
    assert(monitor_discovery_verify_keyed(&authenticated, derived, test_hmac));
    size_t revoked_size = 0;
    assert(monitor_discovery_announce_revoked(&authenticated, authenticated.device_id, "ESP32 Monitor", "1.0.0",
            "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", derived, test_hmac,
            bytes, sizeof bytes, &revoked_size));
    esp32monitor_companion_Discovery revoked_reply = esp32monitor_companion_Discovery_init_zero;
    assert(monitor_discovery_decode(bytes, revoked_size, &revoked_reply));
    assert(revoked_reply.has_pairing_open && revoked_reply.pairing_open &&
           revoked_reply.has_pairing_id &&
           !strcmp(revoked_reply.pairing_id, authenticated.pairing_id));
    assert(monitor_discovery_verify_keyed(&revoked_reply, derived, test_hmac));
    assert(monitor_discovery_verify(&revoked_reply, key, test_hash, test_hmac));
    derived[0] ^= 1;
    assert(!monitor_discovery_verify_keyed(&revoked_reply, derived, test_hmac));
    assert(!monitor_discovery_announce_revoked(&authenticated, authenticated.device_id, "ESP32 Monitor", "1.0.0",
            "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", derived, test_hmac,
            bytes, sizeof bytes, &revoked_size));
    strcpy(authenticated.type, "monitor_connect_offer");
    assert(!monitor_discovery_verify(&authenticated, key, test_hash, test_hmac));
    strcpy(decoded.request_id, "01234567-89ab-cdef-0123-456789abcdef");
    strcpy(decoded.host_id, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee");
    strcpy(decoded.nonce, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA");
    size_t written = 0;
    assert(monitor_discovery_announce(&decoded, "12345678-1234-1234-1234-123456789abc",
                                     "Monitor", "1.0", decoded.nonce, bytes, sizeof bytes, &written));
    esp32monitor_companion_Discovery reply = esp32monitor_companion_Discovery_init_zero;
    assert(monitor_discovery_decode(bytes, written, &reply));
    assert(!strcmp(reply.type, "monitor_announce") && reply.has_pairing_open && reply.pairing_open);
    assert(reply.has_device_id && !strcmp(reply.request_id, decoded.request_id));
    /* A formerly paired probe is answered with a fresh unauthenticated reply. */
    decoded.has_mac = true;
    decoded.has_pairing_id = true;
    strcpy(decoded.pairing_id, "33333333-3333-4333-8333-333333333333");
    assert(monitor_discovery_announce(&decoded, "12345678-1234-1234-1234-123456789abc",
                                     "Monitor", "1.0", decoded.nonce, bytes, sizeof bytes, &written));
    assert(monitor_discovery_decode(bytes, written, &reply));
    assert(!reply.has_mac && !reply.has_pairing_id && reply.has_pairing_open && reply.pairing_open);
    assert(!strcmp(reply.request_id, decoded.request_id) && !strcmp(reply.host_id, decoded.host_id));
    /* Mixed or response-only fields remain invalid. */
    decoded.has_pairing_id = false;
    assert(!monitor_discovery_announce(&decoded, "12345678-1234-1234-1234-123456789abc",
                                      "Monitor", "1.0", decoded.nonce, bytes, sizeof bytes, &written));
    decoded.has_pairing_id = true;
    static const uint8_t response_vector[] = {
        0x08,0x02,0x6a,0x2f,0x08,0x02,0x12,0x04,0x70,0x61,0x69,0x72,0x1a,0x07,0x70,0x65,
        0x6e,0x64,0x69,0x6e,0x67,0x22,0x04,0x68,0x6f,0x73,0x74,0x2a,0x03,0x4d,0x61,0x63,
        0x32,0x05,0x6e,0x6f,0x6e,0x63,0x65,0x3a,0x06,0x74,0x69,0x63,0x6b,0x65,0x74,0x40,
        0x80,0xd3,0x0e
    };
    esp32monitor_companion_PairResponse response = esp32monitor_companion_PairResponse_init_zero;
    assert(monitor_pairing_response(response_vector, sizeof response_vector, &response));
    assert(!strcmp(response.state, "pending") && response.expires_in_ms == 120000);
    assert(!monitor_pairing_response(response_vector, sizeof response_vector - 1, &response));
    assert(!strcmp(response.state, "pending"));
    memcpy(bytes, response_vector, sizeof response_vector);
    bytes[sizeof response_vector] = 8; bytes[sizeof response_vector + 1] = 2;
    assert(!monitor_pairing_response(bytes, sizeof response_vector + 2, &response));
    esp32monitor_companion_PairRequest req = esp32monitor_companion_PairRequest_init_zero;
    req.protocol_version = 1;
    strcpy(req.step, "begin"); strcpy(req.request_id, "request");
    req.has_device_id = req.has_device_name = req.has_device_nonce = true;
    strcpy(req.device_id, "device"); strcpy(req.device_name, "Monitor"); strcpy(req.device_nonce, "nonce");
    static const uint8_t request_vector[] = {
        0x08,0x02,0x62,0x2a,0x08,0x02,0x12,0x05,0x62,0x65,0x67,0x69,0x6e,0x1a,0x07,0x72,
        0x65,0x71,0x75,0x65,0x73,0x74,0x22,0x06,0x64,0x65,0x76,0x69,0x63,0x65,0x2a,0x07,
        0x4d,0x6f,0x6e,0x69,0x74,0x6f,0x72,0x32,0x05,0x6e,0x6f,0x6e,0x63,0x65
    };
    assert(monitor_pairing_encode(&req, bytes, sizeof bytes, &written));
    assert(written == sizeof request_vector && !memcmp(bytes, request_vector, written));
    char code[7] = "xxxxxx";
    const char *zero_nonce = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA";
    assert(monitor_pairing_code(zero_nonce, zero_nonce, "01234567-89ab-cdef-0123-456789abcdef",
                                "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "12345678-1234-1234-1234-123456789abc", test_hash, code));
    assert(!strcmp(code, "746133")); /* Python hashlib, same transcript as Client */
    assert(!monitor_pairing_code("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB", zero_nonce,
                                 "01234567-89ab-cdef-0123-456789abcdef", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
                                 "12345678-1234-1234-1234-123456789abc", test_hash, code));
    assert(!strcmp(code, "746133"));
    strcpy(response.host_id, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee");
    strcpy(response.pairing_id, "12345678-1234-1234-1234-123456789abc");
    strcpy(response.host_nonce, zero_nonce); strcpy(response.pairing_ticket, zero_nonce);
    assert(monitor_pairing_validate_response(&response, response.host_id));
    response.has_device_token = true;
    assert(!monitor_pairing_validate_response(&response, response.host_id));
    strcpy(response.state, "approved"); response.has_serial_key = true;
    strcpy(response.device_token, zero_nonce); strcpy(response.serial_key, zero_nonce);
    assert(monitor_pairing_validate_response(&response, response.host_id));
    assert(!monitor_pairing_validate_response(&response, "00000000-0000-0000-0000-000000000000"));
    strcpy(response.state, "pending"); response.has_device_token = response.has_serial_key = false;
    memset(response.device_token, 0, sizeof response.device_token);
    memset(response.serial_key, 0, sizeof response.serial_key);
    monitor_pairing_session_t session = {0};
    assert(monitor_pairing_begin(&session, &response, zero_nonce, "01234567-89ab-cdef-0123-456789abcdef",
                                 response.host_id, 1000, test_hash));
    assert(session.state == MONITOR_PAIR_WAITING && !strcmp(session.code, "746133"));
    response.pairing_ticket[0] = 'B';
    assert(!monitor_pairing_apply(&session, &response, 1050));
    response.pairing_ticket[0] = 'A';
    strcpy(response.state, "approved"); response.has_device_token = response.has_serial_key = true;
    strcpy(response.device_token, zero_nonce); strcpy(response.serial_key, zero_nonce);
    assert(!monitor_pairing_apply(&session, &response, 1100));
    assert(monitor_pairing_confirm(&session, 1100));
    assert(monitor_pairing_apply(&session, &response, 1200));
    assert(session.state == MONITOR_PAIR_APPROVED && !session.code[0]);
    assert(session.approved.has_serial_key);
    memset(&session, 0, sizeof session);
    strcpy(response.state, "pending"); response.has_device_token = response.has_serial_key = false;
    assert(monitor_pairing_begin(&session, &response, zero_nonce, "01234567-89ab-cdef-0123-456789abcdef",
                                 response.host_id, 1000, test_hash));
    monitor_pairing_cancel(&session);
    assert(session.state == MONITOR_PAIR_CANCELLED && !session.code[0] && !session.initial.pairing_ticket[0]);
    memset(&session, 0, sizeof session);
    assert(monitor_pairing_begin(&session, &response, zero_nonce, "01234567-89ab-cdef-0123-456789abcdef",
                                 response.host_id, 1000, test_hash));
    assert(!monitor_pairing_confirm(&session, 121000));
    assert(session.state == MONITOR_PAIR_EXPIRED && !session.initial.pairing_ticket[0]);
    memset(&session, 0, sizeof session);
    assert(monitor_pairing_begin(&session, &response, zero_nonce, "01234567-89ab-cdef-0123-456789abcdef",
                                 response.host_id, 1000, test_hash));
    strcpy(response.state, "rejected");
    assert(monitor_pairing_apply(&session, &response, 1100));
    assert(session.state == MONITOR_PAIR_REJECTED && !session.code[0]);
    decoded.has_mac = false;
    decoded.nonce[42] = 'B'; /* nonzero base64url padding bits */
    assert(!monitor_discovery_announce(&decoded, "12345678-1234-1234-1234-123456789abc",
                                      "Monitor", "1.0", decoded.nonce, bytes, sizeof bytes, &written));
#ifndef ESP_PLATFORM
    return 0;
#endif
}
