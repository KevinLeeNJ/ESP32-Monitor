"""Exercise EMON framing and serial key derivation against fixed vectors.

The crypto primitives are stubbed deterministically; the vectors for keys and
proofs come from the independent Go/Python implementation in
client/internal/transport/protocol_test.go. This checks transcript, HKDF input
wiring, HMAC input framing, and the decoder, not the PSA primitive itself.
"""
import struct
import subprocess
import sys
import tempfile
import zlib
from pathlib import Path

root = Path(__file__).resolve().parents[2]
source = root / 'firmware'

payload = b'hello-envelope'
header = b'EMON' + bytes([2, 0x01]) + struct.pack('<H', len(payload)) + struct.pack('<I', 7)
crc = zlib.crc32(header[4:] + payload) & 0xffffffff
expected_frame = (header + payload + struct.pack('<I', crc)).hex()

host = '11111111-1111-4111-8111-111111111111'
device = '22222222-2222-4222-8222-222222222222'
pair = '33333333-3333-4333-8333-333333333333'
session = '44444444-4444-4444-8444-444444444444'


def lp(text):
    raw = text.encode()
    return struct.pack('<H', len(raw)) + raw


transcript = (lp('ESP32MON-SERIAL-V1') + lp(host) + lp(device) + lp(pair) + lp(session)
              + bytes([1]) * 32 + bytes([2]) * 32)

test = r'''
#include "monitor_serial_wire.h"
#include "monitor_pairing_code.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>

static uint8_t captured_t[512];
static size_t captured_len;
static uint8_t salt[32], prk[32];
static uint8_t expected_frame[32], expected_config[32], expected_device[32], expected_host[32];
static uint8_t frame_key[32];
static unsigned frame_direction;

static void unhex(const char *text, uint8_t *out, size_t length)
{
    for (size_t i = 0; i < length; i++) {
        unsigned value = 0;
        assert(sscanf(text + i * 2, "%2x", &value) == 1);
        out[i] = (uint8_t)value;
    }
}
static void tohex(const uint8_t *data, size_t length, char *out)
{
    static const char digits[] = "0123456789abcdef";
    for (size_t i = 0; i < length; i++) {
        out[i * 2] = digits[data[i] >> 4];
        out[i * 2 + 1] = digits[data[i] & 15];
    }
    out[length * 2] = 0;
}
static int b64(char c)
{
    if (c >= 'A' && c <= 'Z') return c - 'A';
    if (c >= 'a' && c <= 'z') return c - 'a' + 26;
    if (c >= '0' && c <= '9') return c - '0' + 52;
    if (c == '-') return 62;
    if (c == '_') return 63;
    return -1;
}
bool monitor_companion_decode_nonce(const char *text, uint8_t out[32])
{
    if (!text || strlen(text) != 43) return false;
    unsigned bits = 0;
    uint32_t value = 0;
    size_t written = 0;
    for (size_t i = 0; i < 43; i++) {
        int digit = b64(text[i]);
        if (digit < 0) return false;
        value = (value << 6) | (unsigned)digit;
        bits += 6;
        if (bits >= 8) {
            bits -= 8;
            if (written >= 32) return false;
            out[written++] = (uint8_t)(value >> bits);
        }
    }
    return written == 32;
}
bool monitor_serial_sha256(const uint8_t *data, size_t length, uint8_t out[32])
{
    if (!data || length > sizeof captured_t) return false;
    memcpy(captured_t, data, length);
    captured_len = length;
    memset(out, 0x5a, 32);
    memcpy(salt, out, 32);
    return true;
}
bool monitor_serial_hmac(const uint8_t key[32], const uint8_t *data, size_t length, uint8_t out[32])
{
    if (!memcmp(key, salt, 32) && length == 32) {
        memcpy(out, prk, 32);
        return true;
    }
    if (!memcmp(key, prk, 32)) {
        if (length == 6 && !memcmp(data, "frame\x01", 6)) {
            memcpy(out, expected_frame, 32);
            return true;
        }
        if (length == 13 && !memcmp(data, "proxy-config\x01", 13)) {
            memcpy(out, expected_config, 32);
            return true;
        }
        return false;
    }
    if (length == 6 + captured_len && !memcmp(data, "device", 6) &&
        !memcmp(data + 6, captured_t, captured_len)) {
        memcpy(out, expected_device, 32);
        return true;
    }
    if (length == 4 + captured_len && !memcmp(data, "host", 4) &&
        !memcmp(data + 4, captured_t, captured_len)) {
        memcpy(out, expected_host, 32);
        return true;
    }
    return false;
}
bool monitor_serial_hmac_frame(const uint8_t key[32], uint8_t direction,
                               const uint8_t header[MONITOR_SERIAL_HEADER],
                               const uint8_t *payload, size_t length, uint8_t out[32])
{
    uint32_t hash = 2166136261u;
    for (unsigned i = 0; i < 32; i++) hash = (hash ^ key[i]) * 16777619u;
    hash = (hash ^ direction) * 16777619u;
    for (unsigned i = 0; i < MONITOR_SERIAL_HEADER; i++) hash = (hash ^ header[i]) * 16777619u;
    for (size_t i = 0; i < length; i++) hash = (hash ^ payload[i]) * 16777619u;
    for (unsigned i = 0; i < 32; i++) out[i] = (uint8_t)(hash >> (8 * (i % 4))) ^ (uint8_t)(i * 17);
    frame_direction = direction;
    return true;
}

static int delivered;
static uint8_t last_payload[256];
static size_t last_length;
static uint8_t last_type;
static uint32_t last_seq;
static void on_frame(void *user, const monitor_serial_frame_t *frame)
{
    (void)user;
    delivered++;
    last_type = frame->type;
    last_seq = frame->seq;
    last_length = frame->length;
    assert(last_length <= sizeof last_payload);
    memcpy(last_payload, frame->payload, last_length);
}

static monitor_serial_decoder_t decoder;
int main(void)
{
    unhex("@FRAME@", expected_frame, 32);
    unhex("@CONFIG@", expected_config, 32);
    unhex("@DEVICE@", expected_device, 32);
    unhex("@HOST@", expected_host, 32);
    memset(prk, 0x6b, 32);

    const char *host = "@HOST_ID@";
    const char *device = "@DEVICE_ID@";
    const char *pair = "@PAIR_ID@";
    const char *session = "@SESSION_ID@";
    const char *hn = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE";
    const char *dn = "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI";
    const char *sk = "AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM";
    monitor_serial_keys_t keys;
    assert(monitor_serial_derive_keys(host, device, pair, session, hn, dn, sk, &keys));
    char text[65];
    tohex(keys.frame, 32, text);
    assert(!strcmp(text, "@FRAME@"));
    tohex(keys.config, 32, text);
    assert(!strcmp(text, "@CONFIG@"));
    assert(!strcmp(keys.device_proof, "@DEVICE@"));
    assert(!strcmp(keys.host_proof, "@HOST@"));
    char longhex[1024];
    tohex(captured_t, captured_len, longhex);
    assert(!strcmp(longhex, "@TRANSCRIPT@"));
    assert(captured_len == @TRANSCRIPT_LEN@);
    memcpy(frame_key, keys.frame, 32);

    uint8_t body[] = "hello-envelope";
    const size_t body_len = sizeof body - 1;
    uint8_t frame[16384];
    size_t length = 0;
    assert(monitor_serial_encode(MONITOR_SERIAL_HELLO, 7, body, body_len, NULL, 0, frame, sizeof frame, &length));
    assert(length == 16 + body_len);
    tohex(frame, length, text);
    assert(!strcmp(text, "@UNAUTH_FRAME@"));
    monitor_serial_decoder_init(&decoder);
    delivered = 0;
    assert(monitor_serial_feed(&decoder, (const uint8_t *)"ROM logs\r\n", 10, 1000, NULL, 0, on_frame, NULL) == 0);
    for (size_t i = 0; i < length; i++)
        monitor_serial_feed(&decoder, frame + i, 1, 1001, NULL, 0, on_frame, NULL);
    assert(delivered == 1 && last_type == MONITOR_SERIAL_HELLO && last_seq == 7);
    assert(last_length == body_len && !memcmp(last_payload, body, body_len));

    assert(monitor_serial_encode(MONITOR_SERIAL_HEARTBEAT, 42, body, body_len, frame_key, 2, frame, sizeof frame, &length));
    monitor_serial_decoder_init(&decoder);
    delivered = 0;
    monitor_serial_feed(&decoder, frame, length, 2000, frame_key, 2, on_frame, NULL);
    assert(delivered == 1 && last_type == MONITOR_SERIAL_HEARTBEAT && last_seq == 42 && frame_direction == 2);
    monitor_serial_decoder_init(&decoder);
    delivered = 0;
    monitor_serial_feed(&decoder, frame, length, 2000, frame_key, 1, on_frame, NULL);
    assert(delivered == 0);
    uint8_t corrupt[16384];
    memcpy(corrupt, frame, length);
    corrupt[12] ^= 1;
    monitor_serial_decoder_init(&decoder);
    delivered = 0;
    monitor_serial_feed(&decoder, corrupt, length, 2100, frame_key, 2, on_frame, NULL);
    assert(delivered == 0);
    monitor_serial_decoder_init(&decoder);
    delivered = 0;
    monitor_serial_feed(&decoder, corrupt, length, 2100, frame_key, 2, on_frame, NULL);
    monitor_serial_feed(&decoder, frame, length, 2100, frame_key, 2, on_frame, NULL);
    assert(delivered == 1 && last_type == MONITOR_SERIAL_HEARTBEAT);

    uint8_t suffix[64];
    size_t suffix_len = 0;
    assert(monitor_serial_encode(MONITOR_SERIAL_HELLO, 8, body, body_len, NULL, 0, suffix, sizeof suffix, &suffix_len));
    uint8_t fake[12] = {'E', 'M', 'O', 'N', 2, MONITOR_SERIAL_HEARTBEAT, 0xf0, 0x3f, 1, 0, 0, 0};
    monitor_serial_decoder_init(&decoder);
    delivered = 0;
    monitor_serial_feed(&decoder, fake, sizeof fake, 3000, NULL, 0, on_frame, NULL);
    monitor_serial_feed(&decoder, suffix, suffix_len, 3000, NULL, 0, on_frame, NULL);
    assert(delivered == 0);
    assert(monitor_serial_tick(&decoder, 5101, NULL, 0, on_frame, NULL) == 1);
    assert(delivered == 1 && last_type == MONITOR_SERIAL_HELLO && last_seq == 8);

    uint8_t big[16336];
    memset(big, 0x41, sizeof big);
    assert(!monitor_serial_encode(MONITOR_SERIAL_HELLO, 1, big, 2049, NULL, 0, frame, sizeof frame, &length));
    assert(monitor_serial_encode(MONITOR_SERIAL_HELLO, 1, big, 2048, NULL, 0, frame, sizeof frame, &length));
    assert(!monitor_serial_encode(MONITOR_SERIAL_HELLO, 1, big, 16337, frame_key, 2, frame, sizeof frame, &length));
    assert(monitor_serial_encode(MONITOR_SERIAL_HELLO, 1, big, 16336, frame_key, 2, frame, sizeof frame, &length));
    assert(!monitor_serial_encode(MONITOR_SERIAL_HELLO, 0, body, sizeof body, NULL, 0, frame, sizeof frame, &length));
    assert(!monitor_serial_encode(0x55, 1, body, sizeof body, NULL, 0, frame, sizeof frame, &length));
    assert(!monitor_serial_encode(MONITOR_SERIAL_HELLO, 1, body, sizeof body, frame_key, 0, frame, sizeof frame, &length));
    puts("Serial EMON framing, fragment/noise, CRC/HMAC tamper, limits and key vectors: PASS");
    return 0;
}
'''

test = (test.replace('@HOST_ID@', host)
            .replace('@DEVICE_ID@', device)
            .replace('@PAIR_ID@', pair)
            .replace('@SESSION_ID@', session)
            .replace('@UNAUTH_FRAME@', expected_frame)
            .replace('@TRANSCRIPT_LEN@', str(len(transcript)))
            .replace('@TRANSCRIPT@', transcript.hex())
            .replace('@CONFIG@', '2cd902bee8d60808ff7396a204e60d0c7ff39a6bdf9553dd90e2c30cf0583035')
            .replace('@DEVICE@', 'e20742d25a921bcca4d1888d3d4164420bf29964008218aaf39a76db125db6aa')
            .replace('@HOST@', '5ac16db3d5eb1f11b4cbab2d0b7fa4fb10e96f5139041e7063bb55f0b3440d08')
            .replace('@FRAME@', '5585de967095e0ac73af32f48bd236c7910a054eb17911ae159ba79fa2de7e05'))

with tempfile.TemporaryDirectory() as tmp:
    path = Path(tmp) / 'test.c'
    path.write_text(test)
    binary = Path(tmp) / 'test'
    subprocess.run([sys.argv[1], '-std=c11', '-Wall', '-Wextra', '-Werror',
                    '-I' + str(source / 'src/companion'), '-I' + str(source / 'third_party/nanopb'),
                    str(path), str(source / 'src/companion/monitor_serial_wire.c'),
                    '-o', str(binary)], check=True)
    subprocess.run([str(binary)], check=True)
