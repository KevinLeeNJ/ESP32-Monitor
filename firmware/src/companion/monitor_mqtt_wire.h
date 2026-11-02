#pragma once
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#define MONITOR_MQTT_MAX_PACKET 17000

typedef struct {
    uint8_t header;
    uint8_t *body;
    size_t length;
} monitor_mqtt_packet_t;

/* Return 1 for a complete frame, 0 on idle timeout, -1 on malformed or I/O error. */
int monitor_mqtt_read(int fd, monitor_mqtt_packet_t *packet, uint8_t *buffer, size_t capacity, unsigned timeout_ms);
bool monitor_mqtt_connect_packet(int fd, const char *device, const char *session, const char *token,
                                  const char *will_topic, const uint8_t *will, size_t will_length);
bool monitor_mqtt_subscribe(int fd, uint16_t id, const char *topic);
bool monitor_mqtt_publish(int fd, const char *topic, const uint8_t *payload, size_t length,
                          unsigned qos, uint16_t id);
bool monitor_mqtt_puback(int fd, uint16_t id);
bool monitor_mqtt_ping(int fd);
bool monitor_mqtt_disconnect(int fd);
/* Extract a non-retained QoS 0/1 PUBLISH, rejecting malformed packet IDs. */
bool monitor_mqtt_parse_publish(const monitor_mqtt_packet_t *packet, char *topic, size_t topic_capacity,
                                 const uint8_t **payload, size_t *length, uint16_t *packet_id);
