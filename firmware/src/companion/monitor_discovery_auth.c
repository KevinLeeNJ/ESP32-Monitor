#include "monitor_discovery_auth.h"
#include "pb_encode.h"
#include <stdlib.h>
#include <stdio.h>
#include <string.h>

static bool append(uint8_t *buf, size_t cap, size_t *used, const char *value)
{
    size_t len = strlen(value);
    if (len > UINT16_MAX || *used + 2 + len > cap) return false;
    buf[(*used)++] = (uint8_t)len;
    buf[(*used)++] = (uint8_t)(len >> 8);
    memcpy(buf + *used, value, len); *used += len;
    return true;
}

bool monitor_discovery_key(const char *serial_key, const char *host_id, const char *device_id,
                           const char *pairing_id, monitor_pairing_hash_t hash,
                           monitor_companion_hmac_t hmac, uint8_t out[32])
{
    static const char domain[] = "ESP32MON-DISCOVERY-V1";
    uint8_t raw[32], salt[32], prk[32], buffer[1024];
    if (!serial_key || !host_id || !device_id || !pairing_id || !hash || !hmac || !out ||
        !monitor_companion_decode_nonce(serial_key, raw)) return false;
    size_t n = 0;
    if (!append(buffer, sizeof buffer, &n, host_id) ||
        !append(buffer, sizeof buffer, &n, device_id) ||
        !append(buffer, sizeof buffer, &n, pairing_id) ||
        !hash(buffer, n, salt) || !hmac(salt, raw, sizeof raw, prk)) return false;
    memcpy(buffer, domain, sizeof domain - 1); buffer[sizeof domain - 1] = 1;
    return hmac(prk, buffer, sizeof domain, out);
}

bool monitor_discovery_mac_keyed(const esp32monitor_companion_Discovery *m, const uint8_t key[32],
                                 monitor_companion_hmac_t hmac, char out[65])
{
    if (!m || !key || !hmac || !out) return false;
    static const char domain[] = "ESP32MON-DISCOVERY-V1";
    uint8_t digest[32], buffer[1024];
    char protocol[16], host_port[16], mqtt_port[16];
    snprintf(protocol, sizeof protocol, "%ld", (long)m->protocol_version);
    if (m->has_host_port) snprintf(host_port, sizeof host_port, "%ld", (long)m->host_port); else host_port[0] = 0;
    if (m->has_mqtt_port) snprintf(mqtt_port, sizeof mqtt_port, "%ld", (long)m->mqtt_port); else mqtt_port[0] = 0;
    const char *values[] = {domain, m->type, protocol, m->request_id, m->host_id,
        m->has_device_id ? m->device_id : "", m->has_pairing_id ? m->pairing_id : "", m->nonce,
        m->has_response_nonce ? m->response_nonce : "", host_port, mqtt_port,
        m->has_session_id ? m->session_id : "", m->has_device_name ? m->device_name : "",
        m->has_firmware_version ? m->firmware_version : "", m->has_pairing_open ? (m->pairing_open ? "1" : "0") : ""};
    size_t n = 0;
    for (size_t i = 0; i < sizeof values / sizeof values[0]; i++)
        if (!append(buffer, sizeof buffer, &n, values[i])) return false;
    if (!hmac(key, buffer, n, digest)) return false;
    static const char hex[] = "0123456789abcdef";
    for (unsigned i = 0; i < 32; i++) { out[i*2] = hex[digest[i] >> 4]; out[i*2+1] = hex[digest[i] & 15]; }
    out[64] = 0;
    return true;
}

bool monitor_discovery_mac(const esp32monitor_companion_Discovery *m, const char *serial_key,
                           monitor_pairing_hash_t hash, monitor_companion_hmac_t hmac, char out[65])
{
    uint8_t key[32];
    if (!m || !monitor_discovery_key(serial_key, m->host_id, m->has_device_id ? m->device_id : "",
                                     m->has_pairing_id ? m->pairing_id : "", hash, hmac, key)) return false;
    return monitor_discovery_mac_keyed(m, key, hmac, out);
}

bool monitor_discovery_verify_keyed(const esp32monitor_companion_Discovery *message,
                                    const uint8_t key[32], monitor_companion_hmac_t hmac)
{
    char expected[65];
    if (!message || !message->has_mac || strlen(message->mac) != 64 ||
        !monitor_discovery_mac_keyed(message, key, hmac, expected)) return false;
    unsigned diff = 0;
    for (unsigned i = 0; i < 64; i++) diff |= (unsigned char)(expected[i] ^ message->mac[i]);
    return diff == 0;
}

bool monitor_discovery_verify(const esp32monitor_companion_Discovery *message, const char *serial_key,
                              monitor_pairing_hash_t hash, monitor_companion_hmac_t hmac)
{
    uint8_t key[32];
    if (!message || !monitor_discovery_key(serial_key, message->host_id,
                                           message->has_device_id ? message->device_id : "",
                                           message->has_pairing_id ? message->pairing_id : "",
                                           hash, hmac, key)) return false;
    return monitor_discovery_verify_keyed(message, key, hmac);
}

static bool announce_with_pairing(const esp32monitor_companion_Discovery *request,
                                  const char *device_id, const char *device_name,
                                  const char *firmware_version, const char *nonce,
                                  const uint8_t key[32], monitor_companion_hmac_t hmac,
                                  bool pairing_open, uint8_t *out, size_t capacity, size_t *written)
{
    if (!request || !request->has_device_id || !request->has_pairing_id || !request->has_mac ||
        !device_id || strcmp(request->device_id, device_id) ||
        !monitor_discovery_verify_keyed(request, key, hmac)) return false;
    esp32monitor_companion_Discovery plain = *request;
    plain.has_device_id = plain.has_pairing_id = plain.has_mac = false;
    uint8_t tmp[1024]; size_t size = 0;
    if (!monitor_discovery_announce(&plain, device_id, device_name, firmware_version, nonce, tmp, sizeof tmp, &size)) return false;
    esp32monitor_companion_Discovery reply = esp32monitor_companion_Discovery_init_zero;
    if (!monitor_discovery_decode(tmp, size, &reply)) return false;
    reply.has_pairing_id = true;
    strcpy(reply.pairing_id, request->pairing_id);
    reply.pairing_open = pairing_open;
    reply.has_mac = true;
    if (!monitor_discovery_mac_keyed(&reply, key, hmac, reply.mac)) return false;
    esp32monitor_companion_Envelope *env = calloc(1, sizeof(*env));
    if (!env) return false;
    env->wire_version = 2;
    env->which_payload = esp32monitor_companion_Envelope_discovery_tag;
    env->payload.discovery = reply;
    pb_ostream_t stream = pb_ostream_from_buffer(out, capacity);
    bool valid = pb_encode(&stream, esp32monitor_companion_Envelope_fields, env);
    if (valid) *written = stream.bytes_written;
    free(env);
    return valid;
}

bool monitor_discovery_announce_paired(const esp32monitor_companion_Discovery *request,
                                       const char *device_id, const char *device_name,
                                       const char *firmware_version, const char *nonce,
                                       const char *serial_key, monitor_pairing_hash_t hash,
                                       monitor_companion_hmac_t hmac, uint8_t *out,
                                       size_t capacity, size_t *written)
{
    uint8_t key[32];
    if (!request || !monitor_discovery_key(serial_key, request->host_id,
                                           request->has_device_id ? request->device_id : "",
                                           request->has_pairing_id ? request->pairing_id : "",
                                           hash, hmac, key)) return false;
    return announce_with_pairing(request, device_id, device_name, firmware_version, nonce,
                                 key, hmac, false, out, capacity, written);
}

bool monitor_discovery_announce_revoked(const esp32monitor_companion_Discovery *request,
                                        const char *device_id, const char *device_name,
                                        const char *firmware_version, const char *nonce,
                                        const uint8_t key[32], monitor_companion_hmac_t hmac,
                                        uint8_t *out, size_t capacity, size_t *written)
{
    return announce_with_pairing(request, device_id, device_name, firmware_version, nonce,
                                 key, hmac, true, out, capacity, written);
}
