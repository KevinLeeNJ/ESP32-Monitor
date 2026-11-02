#include "monitor_discovery.h"
#include "pb_decode.h"
#include <stdlib.h>
#include "pb_encode.h"
#include <string.h>

static bool varint(const uint8_t **p, const uint8_t *end, uint64_t *v)
{
    *v = 0;
    for (unsigned i = 0; i < 10 && *p < end; ++i) {
        uint8_t b = *(*p)++;
        if (i == 9 && b > 1) return false;
        *v |= (uint64_t)(b & 127) << (7 * i);
        if (!(b & 128)) return true;
    }
    return false;
}

/* Discovery has only singular scalar/string fields. Unknown fields are
 * rejected just like the Client's strict wire validation. */
static bool fields(const uint8_t *p, size_t n, bool envelope)
{
    const uint8_t *end = p + n;
    uint32_t seen = 0;
    while (p < end) {
        uint64_t key, v;
        if (!varint(&p, end, &key) || key > UINT32_MAX) return false;
        unsigned tag = (unsigned)(key >> 3), wt = key & 7;
        if (envelope ? (tag != 1 && tag != 11) : (tag < 1 || tag > 15)) return false;
        if (seen & (1u << tag)) return false;
        seen |= 1u << tag;
        bool scalar = envelope ? tag == 1 : (tag == 1 || tag == 9 || tag == 10 || tag == 14);
        if (wt != (scalar ? 0u : 2u) || !varint(&p, end, &v)) return false;
        if (scalar) {
            if ((envelope && v != 2) || (!envelope && tag == 14 && v > 1) || v > UINT32_MAX) return false;
        } else {
            if (v > (uint64_t)(end - p)) return false;
            if (envelope) {
                if (!fields(p, (size_t)v, false)) return false;
            } else if (memchr(p, 0, (size_t)v)) return false;
            p += (size_t)v;
        }
    }
    const uint32_t required = envelope ? ((1u << 1) | (1u << 11)) :
        ((1u << 1) | (1u << 2) | (1u << 3) | (1u << 4) | (1u << 7));
    return (seen & required) == required;
}

bool monitor_discovery_decode(const uint8_t *data, size_t len, esp32monitor_companion_Discovery *out)
{
    if (!data || !out || !len || len > 1024 || !fields(data, len, true)) return false;
    esp32monitor_companion_Envelope *msg = calloc(1, sizeof(*msg));
    if (!msg) return false;
    pb_istream_t stream = pb_istream_from_buffer(data, len);
    bool valid = pb_decode(&stream, esp32monitor_companion_Envelope_fields, msg) &&
        msg->which_payload == esp32monitor_companion_Envelope_discovery_tag &&
        msg->payload.discovery.protocol_version == 1;
    if (valid) *out = msg->payload.discovery;
    free(msg);
    return valid;
}

static bool uuid(const char *s)
{
    if (!s || strlen(s) != 36) return false;
    for (unsigned i = 0; i < 36; i++) {
        if (i == 8 || i == 13 || i == 18 || i == 23) {
            if (s[i] != '-') return false;
        } else if (!((s[i] >= '0' && s[i] <= '9') || (s[i] >= 'a' && s[i] <= 'f'))) return false;
    }
    return true;
}

static int base64url(char c)
{
    if (c >= 'A' && c <= 'Z') return c - 'A';
    if (c >= 'a' && c <= 'z') return c - 'a' + 26;
    if (c >= '0' && c <= '9') return c - '0' + 52;
    if (c == '-') return 62;
    if (c == '_') return 63;
    return -1;
}

static bool valid_nonce(const char *s)
{
    if (!s || strlen(s) != 43) return false;
    for (unsigned i = 0; i < 43; i++) if (base64url(s[i]) < 0) return false;
    return (base64url(s[42]) & 3) == 0;
}

static bool printable_ascii(const char *s)
{
    if (!s || strlen(s) > 64) return false;
    for (; *s; s++) if ((unsigned char)*s < 32 || (unsigned char)*s > 126) return false;
    return true;
}

bool monitor_discovery_announce(const esp32monitor_companion_Discovery *request,
                                const char *device_id, const char *device_name,
                                const char *firmware_version, const char *nonce,
                                uint8_t *out, size_t capacity, size_t *written)
{
    /* A formerly paired host probes with pairing_id+mac. The device has no
     * record to verify them, so it answers with a fresh unauthenticated
     * announcement instead of reflecting those fields. This is how the Client
     * learns that its stored pairing was revoked while offline. */
    if (!request || !out || !written || request->protocol_version != 1 ||
        strcmp(request->type, "monitor_discover") || !uuid(request->request_id) ||
        !uuid(request->host_id) || !valid_nonce(request->nonce) ||
        request->has_device_id || request->has_pairing_id != request->has_mac ||
        request->has_response_nonce || request->has_host_port || request->has_mqtt_port ||
        request->has_session_id || request->has_device_name || request->has_firmware_version ||
        request->has_pairing_open || !uuid(device_id) || !valid_nonce(nonce) ||
        !printable_ascii(device_name) || !printable_ascii(firmware_version)) return false;
    esp32monitor_companion_Envelope *env = calloc(1, sizeof(*env));
    if (!env) return false;
    env->wire_version = 2;
    env->which_payload = esp32monitor_companion_Envelope_discovery_tag;
    esp32monitor_companion_Discovery *d = &env->payload.discovery;
    d->protocol_version = 1;
    strcpy(d->type, "monitor_announce");
    strcpy(d->request_id, request->request_id);
    strcpy(d->host_id, request->host_id);
    strcpy(d->nonce, request->nonce);
    d->has_device_id = d->has_response_nonce = d->has_device_name = true;
    d->has_firmware_version = d->has_pairing_open = d->pairing_open = true;
    strcpy(d->device_id, device_id);
    strcpy(d->response_nonce, nonce);
    strcpy(d->device_name, device_name);
    strcpy(d->firmware_version, firmware_version);
    pb_ostream_t stream = pb_ostream_from_buffer(out, capacity);
    bool valid = pb_encode(&stream, esp32monitor_companion_Envelope_fields, env);
    if (valid) *written = stream.bytes_written;
    free(env);
    return valid;
}
