"""Serial handshake protobuf encoding checked against Go wire.Marshal vectors.

The envelope and Hello/Auth/EncryptedConfig fields use nanopb callbacks and are
encoded by monitor_serial_proto.c instead. The expected bytes below were
produced by client/internal/transport/wire.Marshal (protobuf Envelope), so a
byte match proves the firmware output decodes on the Client.
"""
import subprocess
import sys
import tempfile
from pathlib import Path
import host_port

root = Path(__file__).resolve().parents[2]
source = root / 'firmware'

host = '11111111-1111-4111-8111-111111111111'
device = '22222222-2222-4222-8222-222222222222'
pair = '33333333-3333-4333-8333-333333333333'
session = '44444444-4444-4444-8444-444444444444'
request = '55555555-5555-4555-8555-555555555555'
host_nonce = 'AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE'
device_nonce = 'AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI'

hello_vector = (
    '0802ca01fb0108021208726573706f6e73651a2431313131313131312d313131312d343131312d383131312d313131313131313131313131'
    '2204686f73742a2b41514542415145424151454241514542415145424151454241514542415145424151454241514542415145320102'
    '3a2432323232323232322d323232322d343232322d383232322d323232323232323232323232420d4553503332204d6f6e69746f724a'
    '05312e302e30522b416749434167494341674943416749434167494341674943416749434167494341674943416749434167495a243333'
    '3333333333332d333333332d343333332d383333332d33333333333333333333333360e0ff01'
)
auth_vector = (
    '0802d201910208021208726573706f6e73651a2431313131313131312d313131312d343131312d383131312d313131313131313131313131'
    '222432323232323232322d323232322d343232322d383232322d3232323232323232323232322a2433333333333333332d333333332d343333'
    '332d383333332d333333333333333333333333322434343434343434342d343434342d343434342d383434342d343434343434343434343434'
    '3a2b41514542415145424151454241514542415145424151454241514542415145424151454241514542415145422b416749434167494341'
    '674943416749434167494341674943416749434167494341674943416749434167494a1170726f6f662d70726f6f662d70726f6f66'
)
encrypted_vector = (
    '0802da0180010802122434343434343434342d343434342d343434342d383434342d3434343434343434343434341a243535353535353535'
    '2d353535352d343535352d383535352d353535353535353535353535220b6165732d3235362d67636d2a0c00000000000000000000000432'
    '036162633a1000000000000000000000000000000000'
)

test = r'''
#include "monitor_serial_proto.h"
#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static void unhex(const char *text, uint8_t *out, size_t length)
{
    for (size_t i = 0; i < length; i++) {
        unsigned value = 0;
        assert(sscanf(text + i * 2, "%2x", &value) == 1);
        out[i] = (uint8_t)value;
    }
}
static bool same(const uint8_t *data, size_t length, const char *hex)
{
    size_t expected = strlen(hex) / 2;
    if (length != expected) { printf("length %zu != %zu\n", length, expected); return false; }
    uint8_t *bytes = malloc(expected);
    unhex(hex, bytes, expected);
    bool equal = !memcmp(data, bytes, expected);
    free(bytes);
    return equal;
}

int main(void)
{
    uint8_t inner[1024], wrapped[1100];
    size_t inner_length = 0, wrapped_length = 0;

    monitor_serial_hello_out_t hello = {
        .protocol_version = 1, .host_id = "@HOST@", .host_name = "host", .host_nonce = "@HN@",
        .device_id = "@DEVICE@", .device_name = "ESP32 Monitor", .firmware_version = "1.0.0",
        .device_nonce = "@DN@", .pairing_id = "@PAIR@", .max_payload_bytes = 16368,
    };
    assert(monitor_serial_hello_encode(&hello, inner, sizeof inner, &inner_length));
    assert(monitor_serial_envelope_wrap(MONITOR_SERIAL_FIELD_HELLO, inner, inner_length, wrapped, sizeof wrapped, &wrapped_length));
    assert(same(wrapped, wrapped_length, "@HELLO@"));

    monitor_serial_hello_t decoded;
    uint32_t field;
    const uint8_t *payload;
    size_t payload_length;
    assert(monitor_serial_envelope_payload(wrapped, wrapped_length, &field, &payload, &payload_length));
    assert(field == MONITOR_SERIAL_FIELD_HELLO);
    assert(monitor_serial_hello_decode(payload, payload_length, &decoded));
    assert(decoded.protocol_version == 1 && !strcmp(decoded.step, "response") && decoded.supports_v1);
    assert(!strcmp(decoded.host_id, "@HOST@") && !strcmp(decoded.host_nonce, "@HN@"));
    assert(!strcmp(decoded.device_id, "@DEVICE@") && !strcmp(decoded.device_nonce, "@DN@"));
    assert(decoded.has_pairing_id && !strcmp(decoded.pairing_id, "@PAIR@"));
    assert(decoded.has_max_payload_bytes && decoded.max_payload_bytes == 16368);

    monitor_serial_auth_out_t auth = {
        .step = "response", .host_id = "@HOST@", .device_id = "@DEVICE@", .pairing_id = "@PAIR@",
        .session_id = "@SESSION@", .host_nonce = "@HN@", .device_nonce = "@DN@",
        .proof = "proof-proof-proof",
    };
    assert(monitor_serial_auth_encode(&auth, inner, sizeof inner, &inner_length));
    assert(monitor_serial_envelope_wrap(MONITOR_SERIAL_FIELD_AUTH, inner, inner_length, wrapped, sizeof wrapped, &wrapped_length));
    assert(same(wrapped, wrapped_length, "@AUTH@"));

    monitor_serial_auth_t auth_decoded;
    assert(monitor_serial_envelope_payload(wrapped, wrapped_length, &field, &payload, &payload_length));
    assert(field == MONITOR_SERIAL_FIELD_AUTH);
    assert(monitor_serial_auth_decode(payload, payload_length, &auth_decoded));
    assert(auth_decoded.protocol_version == 1 && !strcmp(auth_decoded.step, "response"));
    assert(auth_decoded.has_proof && !strcmp(auth_decoded.proof, "proof-proof-proof"));
    assert(!auth_decoded.has_accepted);

    monitor_serial_encrypted_t encrypted;
    memset(&encrypted, 0, sizeof encrypted);
    encrypted.protocol_version = 1;
    strcpy(encrypted.session_id, "@SESSION@");
    strcpy(encrypted.request_id, "@REQUEST@");
    strcpy(encrypted.algorithm, "aes-256-gcm");
    encrypted.nonce_length = 12;
    encrypted.nonce[11] = 4;
    encrypted.ciphertext_length = 3;
    memcpy(encrypted.ciphertext, "abc", 3);
    encrypted.tag_length = 16;
    assert(monitor_serial_encrypted_encode(&encrypted, inner, sizeof inner, &inner_length));
    assert(monitor_serial_envelope_wrap(MONITOR_SERIAL_FIELD_ENCRYPTED_CONFIG, inner, inner_length, wrapped, sizeof wrapped, &wrapped_length));
    assert(same(wrapped, wrapped_length, "@ENCRYPTED@"));

    monitor_serial_encrypted_t encrypted_decoded;
    assert(monitor_serial_envelope_payload(wrapped, wrapped_length, &field, &payload, &payload_length));
    assert(field == MONITOR_SERIAL_FIELD_ENCRYPTED_CONFIG);
    assert(monitor_serial_encrypted_decode(payload, payload_length, &encrypted_decoded));
    assert(encrypted_decoded.nonce_length == 12 && encrypted_decoded.ciphertext_length == 3 &&
           encrypted_decoded.tag_length == 16 && !memcmp(encrypted_decoded.ciphertext, "abc", 3));

    /* A wire_version other than 2, two payload fields, and truncation are invalid. */
    uint8_t bad[1100];
    memcpy(bad, wrapped, wrapped_length);
    bad[1] = 1;
    assert(!monitor_serial_envelope_payload(bad, wrapped_length, &field, &payload, &payload_length));
    memcpy(bad, wrapped, wrapped_length);
    bad[0] = 0xaa; bad[1] = 0x01; /* another payload field, wrong length is caught too */
    assert(!monitor_serial_envelope_payload(bad, wrapped_length, &field, &payload, &payload_length));
    assert(!monitor_serial_envelope_payload(wrapped, wrapped_length - 1, &field, &payload, &payload_length));
    assert(!monitor_serial_auth_decode(payload, 1, &auth_decoded));

    /* Duplicate required fields must be rejected before they can change meaning. */
    uint8_t duplicate[128] = {0x08, 0x02, 0x08, 0x02};
    monitor_serial_auth_t duplicate_auth;
    assert(!monitor_serial_auth_decode(duplicate, 4, &duplicate_auth));

    puts("Serial handshake protobuf vectors (Go wire.Marshal), envelope and boundary checks: PASS");
    return 0;
}
'''

test = (test.replace('@HOST@', host)
            .replace('@DEVICE@', device)
            .replace('@PAIR@', pair)
            .replace('@SESSION@', session)
            .replace('@REQUEST@', request)
            .replace('@HN@', host_nonce)
            .replace('@DN@', device_nonce)
            .replace('@HELLO@', hello_vector)
            .replace('@AUTH@', auth_vector)
            .replace('@ENCRYPTED@', encrypted_vector))

with tempfile.TemporaryDirectory() as tmp:
    path = Path(tmp) / 'test.c'
    path.write_text(test)
    binary = host_port.binary(tmp)
    subprocess.run([
        host_port.cc(sys.argv[1] if len(sys.argv) > 1 else None), '-std=c11', '-Wall', '-Wextra', '-Werror',
        '-I' + str(source / 'src/companion'), '-I' + str(source / 'third_party/nanopb'),
        str(path),
        str(source / 'src/companion/monitor_serial_proto.c'),
        str(source / 'src/companion/monitor_proto_strict.c'),
        str(source / 'src/companion/companion.pb.c'),
        str(source / 'third_party/nanopb/pb_common.c'),
        str(source / 'third_party/nanopb/pb_decode.c'),
        str(source / 'third_party/nanopb/pb_encode.c'),
        '-o', str(binary)], check=True)
    host_port.run(binary)
