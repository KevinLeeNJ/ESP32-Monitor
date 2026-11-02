#include "monitor_pairing_wire.h"
#include "pb_decode.h"
#include "pb_encode.h"
#include <stdlib.h>
#include <string.h>

static bool read_varint(const uint8_t **p, const uint8_t *end, uint64_t *value)
{
    *value = 0;
    for (unsigned i = 0; i < 10 && *p < end; i++) {
        uint8_t b = *(*p)++;
        if (i == 9 && b > 1) return false;
        *value |= (uint64_t)(b & 127) << (7 * i);
        if (!(b & 128)) return true;
    }
    return false;
}

static bool check_fields(const uint8_t *p, size_t n, bool outer)
{
    const uint8_t *end = p + n;
    uint32_t seen = 0;
    while (p < end) {
        uint64_t key, v;
        if (!read_varint(&p, end, &key) || key > UINT32_MAX) return false;
        unsigned tag = (unsigned)(key >> 3), type = key & 7;
        if (outer ? (tag != 1 && tag != 13) : (tag < 1 || tag > 10)) return false;
        if (seen & (1u << tag)) return false;
        seen |= 1u << tag;
        bool scalar = outer ? tag == 1 : (tag == 1 || tag == 8);
        if (type != (scalar ? 0u : 2u) || !read_varint(&p, end, &v)) return false;
        if (scalar) {
            if (outer && v != 2) return false;
            if (!outer && tag == 1 && v > UINT32_MAX) return false;
        } else {
            if (v > (uint64_t)(end - p)) return false;
            if (outer ? !check_fields(p, (size_t)v, false) : memchr(p, 0, (size_t)v) != NULL) return false;
            p += (size_t)v;
        }
    }
    uint32_t required = outer ? ((1u << 1) | (1u << 13)) : 0x1feu;
    return (seen & required) == required;
}

bool monitor_pairing_encode(const esp32monitor_companion_PairRequest *request,
                            uint8_t *out, size_t capacity, size_t *written)
{
    if (!request || !out || !written || request->protocol_version != 1) return false;
    esp32monitor_companion_Envelope *env = calloc(1, sizeof(*env));
    if (!env) return false;
    env->wire_version = 2;
    env->which_payload = esp32monitor_companion_Envelope_pair_request_tag;
    env->payload.pair_request = *request;
    pb_ostream_t stream = pb_ostream_from_buffer(out, capacity);
    bool valid = pb_encode(&stream, esp32monitor_companion_Envelope_fields, env);
    if (valid) *written = stream.bytes_written;
    free(env);
    return valid;
}

bool monitor_pairing_response(const uint8_t *data, size_t len,
                              esp32monitor_companion_PairResponse *out)
{
    if (!data || !out || !len || len > 4096 || !check_fields(data, len, true)) return false;
    esp32monitor_companion_Envelope *env = calloc(1, sizeof(*env));
    if (!env) return false;
    pb_istream_t stream = pb_istream_from_buffer(data, len);
    bool valid = pb_decode(&stream, esp32monitor_companion_Envelope_fields, env) &&
        env->which_payload == esp32monitor_companion_Envelope_pair_response_tag &&
        env->payload.pair_response.protocol_version == 1;
    if (valid) *out = env->payload.pair_response;
    memset(env, 0, sizeof(*env));
    free(env);
    return valid;
}
