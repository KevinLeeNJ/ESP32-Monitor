#include "monitor_mqtt_wire.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>

int main(void)
{
    int fds[2]; assert(!socketpair(AF_UNIX,SOCK_STREAM,0,fds));
    uint8_t buffer[17000]; monitor_mqtt_packet_t packet;
    const char *device="22222222-2222-4222-8222-222222222222", *session="44444444-4444-4444-8444-444444444444";
    const char *token="AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA";
    uint8_t will[]={1,2,3};
    assert(monitor_mqtt_connect_packet(fds[0],device,session,token,"will",will,sizeof will));
    assert(monitor_mqtt_read(fds[1],&packet,buffer,sizeof buffer,1000)==1 && packet.header==0x10);
    assert(packet.length>10 && !memcmp(packet.body,"\0\4MQTT\4\316\0\74",10));
    assert(monitor_mqtt_subscribe(fds[0],8,"emon/down/#"));
    assert(monitor_mqtt_read(fds[1],&packet,buffer,sizeof buffer,1000)==1 && packet.header==0x82);
    assert(packet.body[0]==0 && packet.body[1]==8 && packet.body[packet.length-1]==1);
    const uint8_t payload[]={0,1,2,3};
    assert(monitor_mqtt_publish(fds[0],"emon/down/snapshot",payload,sizeof payload,1,9));
    assert(monitor_mqtt_read(fds[1],&packet,buffer,sizeof buffer,1000)==1);
    char topic[64]; const uint8_t *result;size_t n;uint16_t id;
    assert(monitor_mqtt_parse_publish(&packet,topic,sizeof topic,&result,&n,&id));
    assert(!strcmp(topic,"emon/down/snapshot") && id==9 && n==sizeof payload && !memcmp(result,payload,n));
    packet.header|=1;assert(!monitor_mqtt_parse_publish(&packet,topic,sizeof topic,&result,&n,&id));
    assert(monitor_mqtt_puback(fds[0],9));assert(monitor_mqtt_read(fds[1],&packet,buffer,sizeof buffer,1000)==1 && packet.header==0x40 && packet.length==2);
    assert(monitor_mqtt_ping(fds[0]));assert(monitor_mqtt_read(fds[1],&packet,buffer,sizeof buffer,1000)==1 && packet.header==0xc0 && packet.length==0);
    assert(monitor_mqtt_read(fds[1],&packet,buffer,sizeof buffer,5)==0);
    uint8_t malformed[]={0x30,0x80,0x80,0x80,0x80};assert(write(fds[0],malformed,sizeof malformed)==sizeof malformed);
    assert(monitor_mqtt_read(fds[1],&packet,buffer,sizeof buffer,1000)==-1);
    close(fds[0]);close(fds[1]);puts("MQTT framing tests passed");return 0;
}
