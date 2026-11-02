#include "monitor_pairing_code.h"
#include <stdio.h>
#include <string.h>

static int digit(char c)
{
    if (c >= 'A' && c <= 'Z') return c - 'A';
    if (c >= 'a' && c <= 'z') return c - 'a' + 26;
    if (c >= '0' && c <= '9') return c - '0' + 52;
    if (c == '-') return 62;
    if (c == '_') return 63;
    return -1;
}

bool monitor_companion_decode_nonce(const char *s, uint8_t out[32])
{
    if (!s || !out || strlen(s) != 43) return false;
    uint32_t bits = 0;
    unsigned count = 0, written = 0;
    for (unsigned i = 0; i < 43; i++) {
        int x = digit(s[i]);
        if (x < 0) return false;
        bits = (bits << 6) | (unsigned)x;
        count += 6;
        if (count >= 8) {
            count -= 8;
            out[written++] = (uint8_t)(bits >> count);
        }
    }
    return written == 32 && (bits & 3u) == 0;
}

bool monitor_companion_valid_uuid(const char *s)
{
    if (!s || strlen(s) != 36) return false;
    for (unsigned i = 0; i < 36; i++) {
        if (i == 8 || i == 13 || i == 18 || i == 23) {
            if (s[i] != '-') return false;
        } else if (!((s[i] >= '0' && s[i] <= '9') || (s[i] >= 'a' && s[i] <= 'f'))) return false;
    }
    return true;
}

bool monitor_pairing_code(const char *device_nonce, const char *host_nonce,
                          const char *device_id, const char *host_id, const char *pairing_id,
                          monitor_pairing_hash_t hash, char code[7])
{
    static const char domain[] = "ESP32MON-PAIR-V1";
    uint8_t input[sizeof(domain) - 1 + 64 + 108], digest[32];
    if (!hash || !code || !monitor_companion_valid_uuid(device_id) || !monitor_companion_valid_uuid(host_id) || !monitor_companion_valid_uuid(pairing_id)) return false;
    memcpy(input, domain, sizeof(domain) - 1);
    size_t pos = sizeof(domain) - 1;
    if (!monitor_companion_decode_nonce(device_nonce, input + pos) || !monitor_companion_decode_nonce(host_nonce, input + pos + 32)) return false;
    pos += 64;
    memcpy(input + pos, device_id, 36); pos += 36;
    memcpy(input + pos, host_id, 36); pos += 36;
    memcpy(input + pos, pairing_id, 36); pos += 36;
    if (!hash(input, pos, digest)) return false;
    uint32_t value = ((uint32_t)digest[0] << 24) | ((uint32_t)digest[1] << 16) |
                     ((uint32_t)digest[2] << 8) | digest[3];
    snprintf(code, 7, "%06u", (unsigned)(value % 1000000));
    return true;
}

bool monitor_pairing_validate_response(const esp32monitor_companion_PairResponse *response,
                                       const char *expected_host)
{
    uint8_t scratch[32];
    if (!response || response->protocol_version != 1 || !monitor_companion_valid_uuid(expected_host) ||
        !monitor_companion_valid_uuid(response->host_id) || strcmp(response->host_id, expected_host) ||
        !monitor_companion_valid_uuid(response->pairing_id) || !response->host_name[0] ||
        response->expires_in_ms <= 0 || response->expires_in_ms > 120000 ||
        !monitor_companion_decode_nonce(response->host_nonce, scratch) || !monitor_companion_decode_nonce(response->pairing_ticket, scratch)) return false;
    if (!strcmp(response->state, "approved")) {
        return response->has_device_token && response->has_serial_key &&
               monitor_companion_decode_nonce(response->device_token, scratch) && monitor_companion_decode_nonce(response->serial_key, scratch);
    }
    return (!strcmp(response->state, "pending") || !strcmp(response->state, "rejected")) &&
           !response->has_device_token && !response->has_serial_key;
}
