#include "monitor_lan_session.h"
#include "monitor_proto_strict.h"
#include "pb_encode.h"
#include "pb_decode.h"
#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static const char *host="11111111-1111-4111-8111-111111111111";
static const char *device="22222222-2222-4222-8222-222222222222";
static const char *pair="33333333-3333-4333-8333-333333333333";
static const char *sid="44444444-4444-4444-8444-444444444444";
static const char *epoch="55555555-5555-4555-8555-555555555555";
static const char *request="66666666-6666-4666-8666-666666666666";
static unsigned acks, goodbyes, snapshots, heartbeats, expected_ack=3;

static bool send_cb(void *ctx,const char *topic,const uint8_t *payload,size_t length,unsigned qos)
{
    (void)ctx;(void)qos;
    esp32monitor_companion_Envelope *env=calloc(1,sizeof *env);assert(env);
    assert(monitor_proto_strict(payload,length,esp32monitor_companion_Envelope_fields));
    pb_istream_t input=pb_istream_from_buffer(payload,length);
    assert(pb_decode(&input,esp32monitor_companion_Envelope_fields,env));
    /* Serial carries the inner Envelope; it must never be wrapped in Control. */
    assert(env->which_payload!=esp32monitor_companion_Envelope_control_tag);
    if(!strcmp(topic,"ack")){
        assert(env->which_payload==esp32monitor_companion_Envelope_ack_tag);
        assert(env->payload.ack.protocol_version==1 && !strcmp(env->payload.ack.session_id,sid));
        assert(env->payload.ack.ack_seq==expected_ack++);
        acks++;
    }else if(!strcmp(topic,"goodbye")){
        assert(env->which_payload==esp32monitor_companion_Envelope_goodbye_tag);
        assert(!strcmp(env->payload.goodbye.session_id,sid));
        goodbyes++;
    }else if(!strcmp(topic,"resync")){
        assert(env->which_payload==esp32monitor_companion_Envelope_resync_tag);
    }else assert(false);
    free(env);return true;
}
static bool apply_cb(void *ctx,const esp32monitor_companion_Snapshot *x,const esp32monitor_companion_Heartbeat *h)
{ (void)ctx; if(x){assert(x->seq==3);snapshots++;} if(h){assert(h->seq==4);heartbeats++;} return true; }
static size_t encode(esp32monitor_companion_Envelope *e,uint8_t *data,size_t cap)
{ pb_ostream_t out=pb_ostream_from_buffer(data,cap);assert(pb_encode(&out,esp32monitor_companion_Envelope_fields,e));return out.bytes_written; }
static void base(esp32monitor_companion_Envelope *e,unsigned tag)
{ memset(e,0,sizeof *e);e->wire_version=2;e->which_payload=tag; }
static void snapshot(esp32monitor_companion_Envelope *e)
{
    base(e,esp32monitor_companion_Envelope_snapshot_tag);esp32monitor_companion_Snapshot *x=&e->payload.snapshot;
    x->protocol_version=1;strcpy(x->host_id,host);strcpy(x->device_id,device);strcpy(x->session_id,sid);x->seq=3;x->sent_at=100;
    strcpy(x->config_epoch,epoch);x->config_revision=1;
    strcpy(x->sections.github.state,"disabled");strcpy(x->sections.codex.state,"disabled");
    strcpy(x->sections.music.state,"disabled");strcpy(x->sections.proxy.state,"disabled");
}
static void heartbeat(esp32monitor_companion_Envelope *e,uint32_t seq,uint32_t last)
{
    base(e,esp32monitor_companion_Envelope_heartbeat_tag);esp32monitor_companion_Heartbeat *h=&e->payload.heartbeat;
    h->protocol_version=1;strcpy(h->host_id,host);strcpy(h->device_id,device);strcpy(h->session_id,sid);h->seq=seq;h->sent_at=101;
    h->has_last_snapshot_seq=true;h->last_snapshot_seq=last;
    h->has_config_epoch=true;strcpy(h->config_epoch,epoch);h->has_config_revision=true;h->config_revision=1;
}

int main(void)
{
    esp32monitor_companion_Config cfg=esp32monitor_companion_Config_init_zero;
    cfg.protocol_version=1;strcpy(cfg.request_id,request);strcpy(cfg.device_id,device);strcpy(cfg.session_id,sid);
    strcpy(cfg.config_epoch,epoch);cfg.config_revision=1;strcpy(cfg.weather.location_mode,"ip");
    cfg.time.enabled=true;cfg.time.utc_offset_minutes=480;

    monitor_lan_session_t *s=monitor_lan_create_serial(host,device,pair,sid,&cfg,send_cb,apply_cb,NULL,1000);
    assert(s);assert(monitor_lan_state(s)==MONITOR_LAN_CONFIG);

    esp32monitor_companion_Envelope envelope;uint8_t bytes[16384];
    snapshot(&envelope);size_t len=encode(&envelope,bytes,sizeof bytes);
    /* Snapshots are not accepted before the encrypted Config is acknowledged. */
    assert(!monitor_lan_receive(s,"snapshot",bytes,len,1100,100));
    assert(snapshots==0 && acks==0);

    assert(monitor_lan_config_accepted(s,1100));
    assert(monitor_lan_state(s)==MONITOR_LAN_SNAPSHOT);
    assert(!monitor_lan_config_accepted(s,1100));

    assert(monitor_lan_receive(s,"snapshot",bytes,len,1200,100));
    assert(snapshots==1 && acks==1 && monitor_lan_state(s)==MONITOR_LAN_PROVING);
    assert(monitor_lan_snapshot_seq(s)==3);
    assert(!monitor_lan_receive(s,"snapshot",bytes,len,1300,100));

    heartbeat(&envelope,4,3);len=encode(&envelope,bytes,sizeof bytes);
    assert(monitor_lan_receive(s,"heartbeat",bytes,len,2300,101));
    assert(heartbeats==1 && acks==2 && monitor_lan_state(s)==MONITOR_LAN_CONNECTED);

    /* A serial mismatch cannot emit a Control resync; the transport ignores it. */
    heartbeat(&envelope,5,4);len=encode(&envelope,bytes,sizeof bytes);
    assert(monitor_lan_receive(s,"heartbeat",bytes,len,2400,102));
    assert(acks==3 && heartbeats==1);

    assert(monitor_lan_goodbye(s,"pairing_revoked",103));
    assert(goodbyes==1 && monitor_lan_state(s)==MONITOR_LAN_CLOSED);
    monitor_lan_destroy(s);
    puts("Serial session config/ack/snapshot/heartbeat/goodbye passed");return 0;
}
