#include "monitor_serial_proto.h"
#include "monitor_proto_strict.h"
#include "pb_decode.h"
#include "pb_encode.h"
#include <string.h>

typedef struct { const uint8_t *data; size_t length; } reader_t;
typedef struct { uint8_t *data; size_t capacity; size_t length; bool ok; } writer_t;

static bool read_varint(reader_t *r, uint64_t *value)
{
    uint64_t result = 0;
    for (unsigned shift = 0; shift < 64; shift += 7) {
        if (!r->length || shift == 63) return false;
        uint8_t byte = *r->data++;
        r->length--;
        result |= (uint64_t)(byte & 0x7f) << shift;
        if (!(byte & 0x80)) { *value = result; return true; }
    }
    return false;
}

static bool read_field(reader_t *r, uint32_t *tag, uint8_t *wire, const uint8_t **value, size_t *length)
{
    uint64_t key;
    if (!read_varint(r, &key) || key == 0 || (key >> 3) > UINT32_MAX) return false;
    *tag = (uint32_t)(key >> 3); *wire = (uint8_t)(key & 7);
    switch (*wire) {
    case 0: {
        const uint8_t *start = r->data;
        uint64_t ignored;
        if (!read_varint(r, &ignored)) return false;
        *value = start; *length = (size_t)(r->data - start);
        return true;
    }
    case 1:
        if (r->length < 8) return false;
        *value = r->data; *length = 8; r->data += 8; r->length -= 8;
        return true;
    case 2: {
        uint64_t n;
        if (!read_varint(r, &n) || n > r->length) return false;
        *value = r->data; *length = (size_t)n; r->data += n; r->length -= (size_t)n;
        return true;
    }
    case 5:
        if (r->length < 4) return false;
        *value = r->data; *length = 4; r->data += 4; r->length -= 4;
        return true;
    default:
        return false;
    }
}

static bool field_seen(uint64_t *seen, uint32_t tag)
{
    if (tag == 0 || tag >= 64 || (*seen & (1ull << tag))) return false;
    *seen |= 1ull << tag;
    return true;
}

static bool zigzag(const uint8_t *data, size_t length, int32_t *out)
{
    reader_t r = {data, length};
    uint64_t v;
    if (!read_varint(&r, &v) || r.length || v > UINT32_MAX) return false;
    *out = (int32_t)((v >> 1) ^ (uint32_t)-(int32_t)(v & 1));
    return true;
}

static bool copy_text(char *out, size_t capacity, const uint8_t *data, size_t length)
{
    if (length >= capacity) return false;
    for (size_t i = 0; i < length; i++)
        if (data[i] < 0x20 || data[i] == 0x7f) return false;
    memcpy(out, data, length); out[length] = 0;
    return true;
}

static bool copy_bytes(uint8_t *out, size_t capacity, size_t *stored, const uint8_t *data, size_t length)
{
    if (length > capacity) return false;
    memcpy(out, data, length); *stored = length;
    return true;
}

static void put_byte(writer_t *w, uint8_t byte)
{
    if (!w->ok || w->length >= w->capacity) { w->ok = false; return; }
    w->data[w->length++] = byte;
}

static void put_varint(writer_t *w, uint64_t value)
{
    do {
        uint8_t byte = (uint8_t)(value & 0x7f);
        value >>= 7;
        if (value) byte |= 0x80;
        put_byte(w, byte);
    } while (value);
}

static void put_tag(writer_t *w, uint32_t tag, uint8_t wire)
{
    put_varint(w, ((uint64_t)tag << 3) | wire);
}

static void put_sint(writer_t *w, uint32_t tag, int32_t value)
{
    put_tag(w, tag, 0);
    put_varint(w, ((uint32_t)value << 1) ^ (uint32_t)(value >> 31));
}

static void put_bool(writer_t *w, uint32_t tag, bool value)
{
    put_tag(w, tag, 0);
    put_varint(w, value ? 1 : 0);
}

static void put_text(writer_t *w, uint32_t tag, const char *text)
{
    if (!text) return;
    size_t length = strlen(text);
    put_tag(w, tag, 2);
    put_varint(w, length);
    for (size_t i = 0; i < length; i++) put_byte(w, (uint8_t)text[i]);
}

static void put_bytes(writer_t *w, uint32_t tag, const uint8_t *data, size_t length)
{
    put_tag(w, tag, 2);
    put_varint(w, length);
    for (size_t i = 0; i < length; i++) put_byte(w, data[i]);
}

bool monitor_serial_envelope_payload(const uint8_t *data, size_t length, uint32_t *field,
                                     const uint8_t **payload, size_t *payload_length)
{
    if (!data || !field || !payload || !payload_length || !length || length > 16384) return false;
    reader_t r = {data, length};
    bool has_version = false, has_payload = false;
    while (r.length) {
        uint32_t tag; uint8_t wire; const uint8_t *value; size_t value_length;
        if (!read_field(&r, &tag, &wire, &value, &value_length)) return false;
        if (tag == 1) {
            if (has_version || wire != 0 || value_length != 1 || value[0] != 2) return false;
            has_version = true;
        } else if (tag >= 10 && tag <= 28) {
            if (has_payload || wire != 2) return false;
            has_payload = true; *field = tag; *payload = value; *payload_length = value_length;
        }
    }
    return has_version && has_payload;
}

bool monitor_serial_hello_decode(const uint8_t *data, size_t length, monitor_serial_hello_t *out)
{
    if (!data || !out || !length) return false;
    memset(out, 0, sizeof *out);
    reader_t r = {data, length};
    uint64_t seen = 0;
    while (r.length) {
        uint32_t tag; uint8_t wire; const uint8_t *value; size_t value_length;
        if (!read_field(&r, &tag, &wire, &value, &value_length) || !field_seen(&seen, tag)) return false;
        switch (tag) {
        case 1:
            if (wire != 0 || !zigzag(value, value_length, &out->protocol_version)) return false;
            break;
        case 2:
            if (wire != 2 || !copy_text(out->step, sizeof out->step, value, value_length)) return false;
            break;
        case 3:
            if (wire != 2 || !copy_text(out->host_id, sizeof out->host_id, value, value_length)) return false;
            break;
        case 4:
            if (wire != 2 || !copy_text(out->host_name, sizeof out->host_name, value, value_length)) return false;
            break;
        case 5:
            if (wire != 2 || !copy_text(out->host_nonce, sizeof out->host_nonce, value, value_length)) return false;
            break;
        case 6:
            if (wire == 0) {
                int32_t version;
                if (!zigzag(value, value_length, &version)) return false;
                if (version == 1) out->supports_v1 = true;
            } else if (wire == 2) {
                reader_t packed = {value, value_length};
                while (packed.length) {
                    const uint8_t *start = packed.data;
                    uint64_t ignored;
                    if (!read_varint(&packed, &ignored)) return false;
                    int32_t version;
                    if (!zigzag(start, (size_t)(packed.data - start), &version)) return false;
                    if (version == 1) out->supports_v1 = true;
                }
            } else return false;
            break;
        case 7:
            if (wire != 2 || !copy_text(out->device_id, sizeof out->device_id, value, value_length)) return false;
            break;
        case 8:
            if (wire != 2 || !copy_text(out->device_name, sizeof out->device_name, value, value_length)) return false;
            break;
        case 9:
            if (wire != 2 || !copy_text(out->firmware_version, sizeof out->firmware_version, value, value_length)) return false;
            break;
        case 10:
            if (wire != 2 || !copy_text(out->device_nonce, sizeof out->device_nonce, value, value_length)) return false;
            break;
        case 11:
            if (wire != 2 || !copy_text(out->pairing_id, sizeof out->pairing_id, value, value_length)) return false;
            out->has_pairing_id = true;
            break;
        case 12:
            if (wire != 0 || !zigzag(value, value_length, &out->max_payload_bytes)) return false;
            out->has_max_payload_bytes = true;
            break;
        default:
            break;
        }
    }
    return (seen & 0x7e) == 0x7e;
}

bool monitor_serial_auth_decode(const uint8_t *data, size_t length, monitor_serial_auth_t *out)
{
    if (!data || !out || !length) return false;
    memset(out, 0, sizeof *out);
    reader_t r = {data, length};
    uint64_t seen = 0;
    while (r.length) {
        uint32_t tag; uint8_t wire; const uint8_t *value; size_t value_length;
        if (!read_field(&r, &tag, &wire, &value, &value_length) || !field_seen(&seen, tag)) return false;
        switch (tag) {
        case 1:
            if (wire != 0 || !zigzag(value, value_length, &out->protocol_version)) return false;
            break;
        case 2:
            if (wire != 2 || !copy_text(out->step, sizeof out->step, value, value_length)) return false;
            break;
        case 3:
            if (wire != 2 || !copy_text(out->host_id, sizeof out->host_id, value, value_length)) return false;
            break;
        case 4:
            if (wire != 2 || !copy_text(out->device_id, sizeof out->device_id, value, value_length)) return false;
            break;
        case 5:
            if (wire != 2 || !copy_text(out->pairing_id, sizeof out->pairing_id, value, value_length)) return false;
            break;
        case 6:
            if (wire != 2 || !copy_text(out->session_id, sizeof out->session_id, value, value_length)) return false;
            break;
        case 7:
            if (wire != 2 || !copy_text(out->host_nonce, sizeof out->host_nonce, value, value_length)) return false;
            break;
        case 8:
            if (wire != 2 || !copy_text(out->device_nonce, sizeof out->device_nonce, value, value_length)) return false;
            break;
        case 9:
            if (wire != 2 || !copy_text(out->proof, sizeof out->proof, value, value_length)) return false;
            out->has_proof = true;
            break;
        case 10: {
            if (wire != 0 || value_length != 1 || value[0] > 1) return false;
            out->accepted = value[0] != 0; out->has_accepted = true;
            break;
        }
        default:
            break;
        }
    }
    return (seen & 0x1fe) == 0x1fe;
}

bool monitor_serial_encrypted_decode(const uint8_t *data, size_t length, monitor_serial_encrypted_t *out)
{
    if (!data || !out || !length) return false;
    memset(out, 0, sizeof *out);
    reader_t r = {data, length};
    uint64_t seen = 0;
    while (r.length) {
        uint32_t tag; uint8_t wire; const uint8_t *value; size_t value_length;
        if (!read_field(&r, &tag, &wire, &value, &value_length) || !field_seen(&seen, tag)) return false;
        switch (tag) {
        case 1:
            if (wire != 0 || !zigzag(value, value_length, &out->protocol_version)) return false;
            break;
        case 2:
            if (wire != 2 || !copy_text(out->session_id, sizeof out->session_id, value, value_length)) return false;
            break;
        case 3:
            if (wire != 2 || !copy_text(out->request_id, sizeof out->request_id, value, value_length)) return false;
            break;
        case 4:
            if (wire != 2 || !copy_text(out->algorithm, sizeof out->algorithm, value, value_length)) return false;
            break;
        case 5:
            if (wire != 2 || !copy_bytes(out->nonce, sizeof out->nonce, &out->nonce_length, value, value_length)) return false;
            break;
        case 6:
            if (wire != 2 || !copy_bytes(out->ciphertext, sizeof out->ciphertext, &out->ciphertext_length, value, value_length)) return false;
            break;
        case 7:
            if (wire != 2 || !copy_bytes(out->tag, sizeof out->tag, &out->tag_length, value, value_length)) return false;
            break;
        default:
            break;
        }
    }
    return (seen & 0xfe) == 0xfe;
}

bool monitor_serial_hello_encode(const monitor_serial_hello_out_t *in, uint8_t *out, size_t capacity, size_t *length)
{
    if (!in || !out || !length || !in->host_id || !in->host_nonce || !in->device_id || !in->device_nonce) return false;
    writer_t w = {out, capacity, 0, true};
    put_sint(&w, 1, in->protocol_version);
    put_text(&w, 2, "response");
    put_text(&w, 3, in->host_id);
    put_text(&w, 4, in->host_name);
    put_text(&w, 5, in->host_nonce);
    put_tag(&w, 6, 2);
    put_varint(&w, 1);
    put_varint(&w, 2); /* sint32 1 */
    put_text(&w, 7, in->device_id);
    put_text(&w, 8, in->device_name);
    put_text(&w, 9, in->firmware_version);
    put_text(&w, 10, in->device_nonce);
    put_text(&w, 11, in->pairing_id);
    if (in->max_payload_bytes) put_sint(&w, 12, in->max_payload_bytes);
    if (!w.ok) return false;
    *length = w.length; return true;
}

bool monitor_serial_auth_encode(const monitor_serial_auth_out_t *in, uint8_t *out, size_t capacity, size_t *length)
{
    if (!in || !out || !length || !in->step || !in->host_id || !in->device_id || !in->pairing_id ||
        !in->session_id || !in->host_nonce || !in->device_nonce) return false;
    writer_t w = {out, capacity, 0, true};
    put_sint(&w, 1, 1);
    put_text(&w, 2, in->step);
    put_text(&w, 3, in->host_id);
    put_text(&w, 4, in->device_id);
    put_text(&w, 5, in->pairing_id);
    put_text(&w, 6, in->session_id);
    put_text(&w, 7, in->host_nonce);
    put_text(&w, 8, in->device_nonce);
    put_text(&w, 9, in->proof);
    if (in->has_accepted) put_bool(&w, 10, in->accepted);
    if (!w.ok) return false;
    *length = w.length; return true;
}

bool monitor_serial_encrypted_encode(const monitor_serial_encrypted_t *in, uint8_t *out, size_t capacity, size_t *length)
{
    if (!in || !out || !length) return false;
    writer_t w = {out, capacity, 0, true};
    put_sint(&w, 1, in->protocol_version);
    put_text(&w, 2, in->session_id);
    put_text(&w, 3, in->request_id);
    put_text(&w, 4, in->algorithm);
    put_bytes(&w, 5, in->nonce, in->nonce_length);
    put_bytes(&w, 6, in->ciphertext, in->ciphertext_length);
    put_bytes(&w, 7, in->tag, in->tag_length);
    if (!w.ok) return false;
    *length = w.length; return true;
}

bool monitor_serial_envelope_wrap(uint32_t field, const uint8_t *inner, size_t length,
                                  uint8_t *out, size_t capacity, size_t *written)
{
    if (!inner || !length || !out || !written) return false;
    writer_t w = {out, capacity, 0, true};
    put_tag(&w, 1, 0);
    put_varint(&w, 2);
    put_bytes(&w, field, inner, length);
    if (!w.ok) return false;
    *written = w.length; return true;
}

bool monitor_serial_envelope_encode(const esp32monitor_companion_Envelope *envelope,
                                    uint8_t *out, size_t capacity, size_t *written)
{
    if (!envelope || !out || !written) return false;
    pb_ostream_t stream = pb_ostream_from_buffer(out, capacity);
    if (!pb_encode(&stream, esp32monitor_companion_Envelope_fields, envelope)) return false;
    *written = stream.bytes_written; return true;
}

bool monitor_serial_envelope_decode(esp32monitor_companion_Envelope *out, const uint8_t *data, size_t length)
{
    if (!out || !data || !length || length > 16384 ||
        !monitor_proto_strict(data, length, esp32monitor_companion_Envelope_fields)) return false;
    memset(out, 0, sizeof *out);
    pb_istream_t stream = pb_istream_from_buffer(data, length);
    return pb_decode(&stream, esp32monitor_companion_Envelope_fields, out) && out->wire_version == 2;
}
