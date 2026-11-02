#include "monitor_serial_wire.h"
#include "monitor_pairing_code.h"
#include <stdio.h>
#include <string.h>

static uint16_t le16(const uint8_t *p) { return (uint16_t)(p[0] | (p[1] << 8)); }
static uint32_t le32(const uint8_t *p)
{
    return (uint32_t)p[0] | ((uint32_t)p[1] << 8) | ((uint32_t)p[2] << 16) | ((uint32_t)p[3] << 24);
}
static void put16(uint8_t *p, uint16_t v) { p[0] = (uint8_t)v; p[1] = (uint8_t)(v >> 8); }
static void put32(uint8_t *p, uint32_t v)
{
    p[0] = (uint8_t)v; p[1] = (uint8_t)(v >> 8); p[2] = (uint8_t)(v >> 16); p[3] = (uint8_t)(v >> 24);
}

static uint32_t crc32_ieee(const uint8_t *data, size_t length)
{
    uint32_t crc = 0xffffffffu;
    for (size_t i = 0; i < length; i++) {
        crc ^= data[i];
        for (unsigned bit = 0; bit < 8; bit++) crc = (crc >> 1) ^ (0xedb88320u & (0u - (crc & 1u)));
    }
    return ~crc;
}

static bool type_valid(uint8_t type)
{
    switch (type) {
    case MONITOR_SERIAL_HELLO:
    case MONITOR_SERIAL_AUTH:
    case MONITOR_SERIAL_PAIR:
    case MONITOR_SERIAL_CONFIG:
    case MONITOR_SERIAL_CONFIG_ACK:
    case MONITOR_SERIAL_SNAPSHOT:
    case MONITOR_SERIAL_HEARTBEAT:
    case MONITOR_SERIAL_ACK:
    case MONITOR_SERIAL_GOODBYE:
    case MONITOR_SERIAL_ERROR:
        return true;
    default:
        return false;
    }
}

static bool equal(const uint8_t *a, const uint8_t *b, size_t length)
{
    uint8_t difference = 0;
    for (size_t i = 0; i < length; i++) difference |= (uint8_t)(a[i] ^ b[i]);
    return difference == 0;
}

void monitor_serial_decoder_init(monitor_serial_decoder_t *decoder)
{
    if (!decoder) return;
    memset(decoder, 0, sizeof *decoder);
}

static void discard_front(monitor_serial_decoder_t *decoder, size_t count)
{
    if (count > decoder->length) count = decoder->length;
    memmove(decoder->data, decoder->data + count, decoder->length - count);
    decoder->length -= count;
    decoder->candidate = false;
    decoder->candidate_ms = 0;
}

static size_t parse(monitor_serial_decoder_t *decoder, uint64_t now_ms,
                    const uint8_t key[32], uint8_t direction,
                    monitor_serial_frame_fn callback, void *user)
{
    size_t delivered = 0;
    for (;;) {
        if (decoder->length < 4) {
            decoder->candidate = false;
            decoder->candidate_ms = 0;
            return delivered;
        }
        size_t at = 0;
        while (at + 4 <= decoder->length && memcmp(decoder->data + at, "EMON", 4) != 0) at++;
        if (at + 4 > decoder->length) {
            size_t keep = decoder->length < 3 ? decoder->length : 3;
            if (keep < decoder->length) discard_front(decoder, decoder->length - keep);
            decoder->candidate = false;
            decoder->candidate_ms = 0;
            return delivered;
        }
        if (at) discard_front(decoder, at);
        if (!decoder->candidate) {
            decoder->candidate = true;
            decoder->candidate_ms = now_ms;
        }
        if (decoder->length < MONITOR_SERIAL_HEADER) return delivered;
        uint16_t payload_len = le16(decoder->data + 6);
        if (decoder->data[4] != 2 || !type_valid(decoder->data[5]) || payload_len < 2 ||
            payload_len > 16368 || le32(decoder->data + 8) == 0) {
            discard_front(decoder, 1);
            continue;
        }
        size_t total = 16u + payload_len;
        if (decoder->length < total) {
            if (now_ms - decoder->candidate_ms > 2000) {
                discard_front(decoder, 1);
                continue;
            }
            return delivered;
        }
        if (crc32_ieee(decoder->data + 4, (size_t)payload_len + 8) != le32(decoder->data + 12 + payload_len)) {
            discard_front(decoder, 1);
            continue;
        }
        size_t data_len = payload_len;
        if (key) {
            if (payload_len < 34) {
                discard_front(decoder, 1);
                continue;
            }
            data_len = (size_t)payload_len - 32;
            uint8_t mac[32];
            if (!monitor_serial_hmac_frame(key, direction, decoder->data, decoder->data + MONITOR_SERIAL_HEADER,
                                           data_len, mac) ||
                !equal(mac, decoder->data + MONITOR_SERIAL_HEADER + data_len, 32)) {
                discard_front(decoder, 1);
                continue;
            }
        }
        monitor_serial_frame_t frame;
        frame.type = decoder->data[5];
        frame.seq = le32(decoder->data + 8);
        memcpy(frame.header, decoder->data, MONITOR_SERIAL_HEADER);
        frame.payload = decoder->data + MONITOR_SERIAL_HEADER;
        frame.length = data_len;
        if (callback) callback(user, &frame);
        delivered++;
        discard_front(decoder, total);
    }
}

size_t monitor_serial_feed(monitor_serial_decoder_t *decoder, const uint8_t *data, size_t length,
                           uint64_t now_ms, const uint8_t key[32], uint8_t direction,
                           monitor_serial_frame_fn callback, void *user)
{
    if (!decoder || (!data && length) || (key && direction != 1 && direction != 2)) return 0;
    size_t delivered = parse(decoder, now_ms, key, direction, callback, user);
    while (length) {
        if (decoder->length == MONITOR_SERIAL_MAX_FRAME) discard_front(decoder, 1);
        size_t room = MONITOR_SERIAL_MAX_FRAME - decoder->length;
        size_t take = length < room ? length : room;
        memcpy(decoder->data + decoder->length, data, take);
        decoder->length += take;
        data += take;
        length -= take;
        delivered += parse(decoder, now_ms, key, direction, callback, user);
    }
    return delivered;
}

size_t monitor_serial_tick(monitor_serial_decoder_t *decoder, uint64_t now_ms,
                           const uint8_t key[32], uint8_t direction,
                           monitor_serial_frame_fn callback, void *user)
{
    if (!decoder || (key && direction != 1 && direction != 2)) return 0;
    return parse(decoder, now_ms, key, direction, callback, user);
}

bool monitor_serial_encode(uint8_t type, uint32_t seq, const uint8_t *payload, size_t length,
                           const uint8_t key[32], uint8_t direction,
                           uint8_t *out, size_t capacity, size_t *written)
{
    if (!out || !written || !type_valid(type) || !seq || (!payload && length)) return false;
    if (length > (key ? MONITOR_SERIAL_MAX_PAYLOAD : MONITOR_SERIAL_MAX_UNAUTH)) return false;
    if (key && direction != 1 && direction != 2) return false;
    size_t payload_field = length + (key ? 32u : 0u);
    size_t total = 16u + payload_field;
    if (payload_field > 16368 || total > MONITOR_SERIAL_MAX_FRAME || capacity < total) return false;
    memcpy(out, "EMON", 4);
    out[4] = 2;
    out[5] = type;
    put16(out + 6, (uint16_t)payload_field);
    put32(out + 8, seq);
    if (length) memcpy(out + MONITOR_SERIAL_HEADER, payload, length);
    if (key) {
        if (!monitor_serial_hmac_frame(key, direction, out, out + MONITOR_SERIAL_HEADER, length,
                                       out + MONITOR_SERIAL_HEADER + length)) return false;
    }
    put32(out + MONITOR_SERIAL_HEADER + payload_field,
          crc32_ieee(out + 4, MONITOR_SERIAL_HEADER - 4 + payload_field));
    *written = total;
    return true;
}

static bool uuid_valid(const char *text)
{
    if (!text || strlen(text) != 36) return false;
    for (unsigned i = 0; i < 36; i++) {
        char c = text[i];
        if (i == 8 || i == 13 || i == 18 || i == 23) {
            if (c != '-') return false;
        } else if (!((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'))) {
            return false;
        }
    }
    return true;
}

static bool append_lp(uint8_t *buffer, size_t capacity, size_t *at, const char *text)
{
    size_t length = strlen(text);
    if (length > 65535 || *at + 2 + length > capacity) return false;
    put16(buffer + *at, (uint16_t)length);
    memcpy(buffer + *at + 2, text, length);
    *at += 2 + length;
    return true;
}

static void hex(const uint8_t digest[32], char out[65])
{
    static const char digits[] = "0123456789abcdef";
    for (unsigned i = 0; i < 32; i++) {
        out[i * 2] = digits[digest[i] >> 4];
        out[i * 2 + 1] = digits[digest[i] & 15];
    }
    out[64] = 0;
}

bool monitor_serial_derive_keys(const char *host, const char *device, const char *pairing,
                                const char *session, const char *host_nonce,
                                const char *device_nonce, const char *serial_key,
                                monitor_serial_keys_t *out)
{
    if (!out) return false;
    memset(out, 0, sizeof *out);
    if (!uuid_valid(host) || !uuid_valid(device) || !uuid_valid(pairing) || !uuid_valid(session)) return false;
    uint8_t host_raw[32], device_raw[32], key_raw[32];
    if (!monitor_companion_decode_nonce(host_nonce, host_raw) ||
        !monitor_companion_decode_nonce(device_nonce, device_raw) ||
        !monitor_companion_decode_nonce(serial_key, key_raw)) return false;
    uint8_t transcript[512];
    size_t at = 0;
    bool ok = append_lp(transcript, sizeof transcript, &at, "ESP32MON-SERIAL-V1") &&
              append_lp(transcript, sizeof transcript, &at, host) &&
              append_lp(transcript, sizeof transcript, &at, device) &&
              append_lp(transcript, sizeof transcript, &at, pairing) &&
              append_lp(transcript, sizeof transcript, &at, session);
    if (!ok || at + 64 > sizeof transcript) goto fail;
    memcpy(transcript + at, host_raw, 32);
    memcpy(transcript + at + 32, device_raw, 32);
    at += 64;

    uint8_t salt[32], prk[32], digest[32], proof_input[512], info[16];
    if (!monitor_serial_sha256(transcript, at, salt)) goto fail;
    for (unsigned side = 0; side < 2; side++) {
        const char *label = side ? "host" : "device";
        size_t label_len = strlen(label);
        if (label_len + at > sizeof proof_input) goto fail;
        memcpy(proof_input, label, label_len);
        memcpy(proof_input + label_len, transcript, at);
        if (!monitor_serial_hmac(key_raw, proof_input, label_len + at, digest)) goto fail;
        hex(digest, side ? out->host_proof : out->device_proof);
    }
    if (!monitor_serial_hmac(salt, key_raw, 32, prk)) goto fail;
    memcpy(info, "frame", 5);
    info[5] = 1;
    if (!monitor_serial_hmac(prk, info, 6, out->frame)) goto fail;
    memcpy(info, "proxy-config", 12);
    info[12] = 1;
    if (!monitor_serial_hmac(prk, info, 13, out->config)) goto fail;
    memset(salt, 0, sizeof salt);
    memset(prk, 0, sizeof prk);
    memset(key_raw, 0, sizeof key_raw);
    return true;
fail:
    memset(out, 0, sizeof *out);
    memset(key_raw, 0, sizeof key_raw);
    return false;
}
