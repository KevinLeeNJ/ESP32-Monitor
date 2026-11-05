#include "monitor_mqtt_wire.h"
#include <limits.h>
#include <stdlib.h>
#include <string.h>
#include <sys/select.h>
#include <sys/socket.h>
#include <time.h>
#include <unistd.h>

#ifndef MSG_NOSIGNAL
#define MSG_NOSIGNAL 0
#endif

/* A packet that has started may be delayed across several Wi-Fi power-save
 * wakeups. The caller's timeout only bounds the wait for the next packet;
 * once the fixed header arrives, the rest gets a wider budget. */
#define MONITOR_MQTT_PACKET_TIMEOUT_MS 10000u

static uint64_t monotonic_ms(void)
{
    struct timespec t;
    if (clock_gettime(CLOCK_MONOTONIC, &t)) return 0;
    return (uint64_t)t.tv_sec * 1000u + (uint64_t)t.tv_nsec / 1000000u;
}

static int read_exact(int fd, uint8_t *data, size_t len, uint64_t deadline, bool idle)
{
    size_t used = 0;
    while (used < len) {
        uint64_t now = monotonic_ms();
        if (now >= deadline) return used || !idle ? -1 : 0;
        uint64_t remaining = deadline - now;
        struct timeval tv = {(long)(remaining / 1000), (long)((remaining % 1000) * 1000)};
        fd_set set; FD_ZERO(&set); FD_SET(fd, &set);
        int ready = select(fd + 1, &set, NULL, NULL, &tv);
        if (ready <= 0) return ready == 0 && !used && idle ? 0 : -1;
        ssize_t n = recv(fd, data + used, len - used, 0);
        if (n <= 0) return -1;
        used += (size_t)n;
    }
    return 1;
}

int monitor_mqtt_read(int fd, monitor_mqtt_packet_t *packet, uint8_t *buffer, size_t capacity, unsigned timeout_ms)
{
    if (fd < 0 || !packet || !buffer || !timeout_ms) return -1;
    uint64_t deadline = monotonic_ms() + timeout_ms;
    uint8_t header, b;
    int status = read_exact(fd, &header, 1, deadline, true);
    if (status != 1) return status;
    const uint64_t packet_deadline = monotonic_ms() + MONITOR_MQTT_PACKET_TIMEOUT_MS;
    size_t len = 0, mult = 1;
    for (unsigned i = 0; i < 4; i++) {
        if (read_exact(fd, &b, 1, packet_deadline, false) != 1) return -1;
        len += (b & 0x7f) * mult;
        if (!(b & 0x80)) {
            if ((i && b == 0) || len > capacity || len > MONITOR_MQTT_MAX_PACKET) return -1;
            if (len && read_exact(fd, buffer, len, packet_deadline, false) != 1) return -1;
            packet->header = header; packet->body = buffer; packet->length = len;
            return 1;
        }
        mult *= 128;
    }
    return -1;
}

static bool write_all(int fd, const void *data, size_t len)
{
    const uint8_t *p = data;
    while (len) {
        ssize_t n = send(fd, p, len, MSG_NOSIGNAL);
        if (n <= 0) return false;
        p += n; len -= (size_t)n;
    }
    return true;
}

static bool send_frame(int fd, uint8_t header, const uint8_t *data, size_t len)
{
    if (len > MONITOR_MQTT_MAX_PACKET) return false;
    uint8_t prefix[5] = {header}; size_t n = 1, rest = len;
    do { uint8_t b = rest % 128; rest /= 128; prefix[n++] = b | (rest ? 128 : 0); } while (rest);
    return write_all(fd, prefix, n) && (!len || write_all(fd, data, len));
}

static bool put_field(uint8_t *buf, size_t cap, size_t *n, const void *value, size_t len)
{
    if (len > UINT16_MAX || *n + len + 2 > cap) return false;
    buf[(*n)++] = (uint8_t)(len >> 8); buf[(*n)++] = (uint8_t)len;
    memcpy(buf + *n, value, len); *n += len; return true;
}

bool monitor_mqtt_connect_packet(int fd, const char *device, const char *session, const char *token,
                                  const char *will_topic, const uint8_t *will, size_t will_length)
{
    if (!device || !session || !token || !will_topic || !will || strlen(device) != 36 || strlen(session) != 36 || strlen(token) != 43) return false;
    uint8_t body[1400] = {0,4,'M','Q','T','T',4,0xce,0,60};
    size_t n = 10; char id[42], username[74];
    memcpy(id, "emon-", 5); memcpy(id+5, device, 37);
    memcpy(username, device, 36); username[36] = ':'; memcpy(username+37, session, 37);
    return put_field(body, sizeof body, &n, id, strlen(id)) &&
           put_field(body, sizeof body, &n, will_topic, strlen(will_topic)) &&
           put_field(body, sizeof body, &n, will, will_length) &&
           put_field(body, sizeof body, &n, username, strlen(username)) &&
           put_field(body, sizeof body, &n, token, strlen(token)) && send_frame(fd, 0x10, body, n);
}

bool monitor_mqtt_subscribe(int fd, uint16_t id, const char *topic)
{
    if (!id || !topic) return false;
    uint8_t body[512] = {(uint8_t)(id >> 8), (uint8_t)id}; size_t n = 2;
    if (!put_field(body, sizeof body, &n, topic, strlen(topic)) || n == sizeof body) return false;
    body[n++] = 1; return send_frame(fd, 0x82, body, n);
}

bool monitor_mqtt_publish(int fd, const char *topic, const uint8_t *payload, size_t length, unsigned qos, uint16_t id)
{
    if (!topic || (!payload && length) || qos > 1 || (qos && !id)) return false;
    size_t topic_len = strlen(topic), size = 2 + topic_len + (qos ? 2 : 0) + length;
    if (topic_len > 256 || size > MONITOR_MQTT_MAX_PACKET) return false;
    uint8_t *body = malloc(size); if (!body) return false;
    size_t n = 0; bool ok = put_field(body, size, &n, topic, topic_len);
    if (ok && qos) { body[n++] = (uint8_t)(id >> 8); body[n++] = (uint8_t)id; }
    if (ok && length) memcpy(body + n, payload, length);
    if (ok) ok = send_frame(fd, qos ? 0x32 : 0x30, body, size);
    free(body); return ok;
}
bool monitor_mqtt_puback(int fd, uint16_t id)
{ uint8_t b[2] = {(uint8_t)(id >> 8), (uint8_t)id}; return id && send_frame(fd, 0x40, b, 2); }
bool monitor_mqtt_ping(int fd) { return send_frame(fd, 0xc0, NULL, 0); }
bool monitor_mqtt_disconnect(int fd) { return send_frame(fd, 0xe0, NULL, 0); }

bool monitor_mqtt_parse_publish(const monitor_mqtt_packet_t *packet, char *topic, size_t capacity,
                                 const uint8_t **payload, size_t *length, uint16_t *packet_id)
{
    if (!packet || !topic || !payload || !length || !packet_id || (packet->header >> 4) != 3 ||
        (packet->header & 1) || ((packet->header >> 1) & 3) > 1 || packet->length < 2) return false;
    const uint8_t *b = packet->body; size_t n = ((size_t)b[0] << 8) | b[1];
    unsigned qos = (packet->header >> 1) & 3; size_t prefix = 2 + n + (qos ? 2 : 0);
    if (!n || n >= capacity || prefix > packet->length || memchr(b + 2, 0, n)) return false;
    memcpy(topic, b + 2, n); topic[n] = 0;
    *packet_id = qos ? (uint16_t)((b[2+n] << 8) | b[3+n]) : 0;
    if (qos && !*packet_id) return false;
    *payload = b + prefix; *length = packet->length - prefix; return true;
}
