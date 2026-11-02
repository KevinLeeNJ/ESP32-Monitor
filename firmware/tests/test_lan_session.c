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
static const char *open_id="66666666-6666-4666-8666-666666666666";
static const char *cfg_id="77777777-7777-4777-8777-777777777777";
static unsigned sent_open, sent_cfg, sent_presence, sent_ack, sent_resync, sent_goodbye, snapshots, heartbeats;
static bool send_cb(void *ctx,const char *topic,const uint8_t *payload,size_t length,unsigned qos)
{
    (void)ctx;
    esp32monitor_companion_Envelope *env=calloc(1,sizeof *env); assert(env);
    assert(monitor_proto_strict(payload,length,esp32monitor_companion_Envelope_fields));
    pb_istream_t input=pb_istream_from_buffer(payload,length);
    assert(pb_decode(&input,esp32monitor_companion_Envelope_fields,env));
    if (!strcmp(topic,"presence")) { assert(qos==1 && env->which_payload==esp32monitor_companion_Envelope_presence_tag); sent_presence++; }
    else {
        assert(env->which_payload==esp32monitor_companion_Envelope_control_tag);
        esp32monitor_companion_Control *c=&env->payload.control;
        assert(!strcmp(c->host_id,host) && !strcmp(c->session_id,sid));
        esp32monitor_companion_Envelope *inner=calloc(1,sizeof *inner); assert(inner);
        pb_istream_t data=pb_istream_from_buffer(c->data.bytes,c->data.size);
        assert(pb_decode(&data,esp32monitor_companion_Envelope_fields,inner));
        if (!strcmp(topic,"open")) { assert(qos==1 && inner->which_payload==esp32monitor_companion_Envelope_open_tag); sent_open++; }
        else if (!strcmp(topic,"proxy-config")) { assert(qos==1 && inner->which_payload==esp32monitor_companion_Envelope_config_tag); sent_cfg++; }
        else if (!strcmp(topic,"ack")) { assert(qos==0 && inner->which_payload==esp32monitor_companion_Envelope_ack_tag); sent_ack++; }
        else if (!strcmp(topic,"resync")) { assert(qos==1 && inner->which_payload==esp32monitor_companion_Envelope_resync_tag); assert(!strcmp(inner->payload.resync.reason,"missing_snapshot")); sent_resync++; }
        else if (!strcmp(topic,"goodbye")) { assert(qos==1 && inner->which_payload==esp32monitor_companion_Envelope_goodbye_tag); assert(!strcmp(inner->payload.goodbye.reason,"pairing_revoked")); sent_goodbye++; }
        else assert(false);
        free(inner);
    }
    free(env);return true;
}
static bool apply_cb(void *ctx,const esp32monitor_companion_Snapshot *x,const esp32monitor_companion_Heartbeat *h)
{ (void)ctx; if(x){assert(x->seq==3);snapshots++;} if(h){assert(h->seq==4);heartbeats++;} return true; }
static size_t encode(esp32monitor_companion_Envelope *e,uint8_t *data,size_t cap)
{ pb_ostream_t out=pb_ostream_from_buffer(data,cap);assert(pb_encode(&out,esp32monitor_companion_Envelope_fields,e));return out.bytes_written; }
static void base(esp32monitor_companion_Envelope *e,unsigned tag)
{ memset(e,0,sizeof *e); e->wire_version=2;e->which_payload=tag; }
static size_t wrap(esp32monitor_companion_Envelope *outer,esp32monitor_companion_Envelope *inner,uint32_t seq,const char *request,uint8_t *out)
{
    uint8_t nested[4096]; size_t n=encode(inner,nested,sizeof nested);
    base(outer,esp32monitor_companion_Envelope_control_tag); esp32monitor_companion_Control *c=&outer->payload.control;
    c->protocol_version=1;strcpy(c->host_id,host);strcpy(c->device_id,device);strcpy(c->session_id,sid);c->seq=seq;c->sent_at=123;
    if(request){c->has_request_id=true;strcpy(c->request_id,request);} c->data.size=n;memcpy(c->data.bytes,nested,n);
    return encode(outer,out,16384);
}
int main(void)
{
    esp32monitor_companion_Config cfg=esp32monitor_companion_Config_init_zero;
    cfg.protocol_version=1;strcpy(cfg.request_id,cfg_id);strcpy(cfg.device_id,device);strcpy(cfg.session_id,sid);
    strcpy(cfg.config_epoch,epoch);cfg.config_revision=1;strcpy(cfg.weather.location_mode,"ip");cfg.time.enabled=true;cfg.time.utc_offset_minutes=480;
    monitor_lan_session_t *s=monitor_lan_create(host,device,pair,sid,open_id,&cfg,send_cb,apply_cb,NULL,1000);assert(s);
    assert(monitor_lan_start(s,100));assert(sent_presence==1 && sent_open==1);
    esp32monitor_companion_Envelope *outer=calloc(1,sizeof *outer), *inner=calloc(1,sizeof *inner); uint8_t *bytes=malloc(16384);assert(outer&&inner&&bytes);
    base(inner,esp32monitor_companion_Envelope_session_tag);esp32monitor_companion_Session *v=&inner->payload.session;
    v->protocol_version=1;strcpy(v->host_id,host);strcpy(v->device_id,device);strcpy(v->pairing_id,pair);strcpy(v->session_id,sid);strcpy(v->transport,"lan");v->heartbeat_interval_ms=30000;v->disconnect_timeout_ms=90000;v->config_required=true;
    size_t len=wrap(outer,inner,1,open_id,bytes);assert(monitor_lan_receive(s,"session",bytes,len,1100,100));assert(sent_cfg==1);
    base(inner,esp32monitor_companion_Envelope_config_ack_tag);esp32monitor_companion_ConfigAck *a=&inner->payload.config_ack;
    a->protocol_version=1;a->has_request_id=true;strcpy(a->request_id,cfg_id);strcpy(a->device_id,device);strcpy(a->session_id,sid);a->has_config_epoch=true;strcpy(a->config_epoch,epoch);a->has_config_revision=true;a->config_revision=1;strcpy(a->state,"applied");
    len=wrap(outer,inner,2,cfg_id,bytes);assert(monitor_lan_receive(s,"config-ack",bytes,len,1200,100));
    base(outer,esp32monitor_companion_Envelope_snapshot_tag);esp32monitor_companion_Snapshot *x=&outer->payload.snapshot;
    x->protocol_version=1;strcpy(x->host_id,host);strcpy(x->device_id,device);strcpy(x->session_id,sid);x->seq=3;x->sent_at=100;strcpy(x->config_epoch,epoch);x->config_revision=1;
    strcpy(x->sections.github.state,"disabled");strcpy(x->sections.codex.state,"disabled");strcpy(x->sections.music.state,"disabled");strcpy(x->sections.proxy.state,"disabled");
    strcpy(x->session_id,"99999999-9999-4999-8999-999999999999");
    len=encode(outer,bytes,16384);assert(!monitor_lan_receive(s,"snapshot",bytes,len,1900,100));assert(snapshots==0 && sent_ack==0);
    strcpy(x->session_id,sid);
    len=encode(outer,bytes,16384);assert(monitor_lan_receive(s,"snapshot",bytes,len,2000,100));assert(snapshots==1 && sent_ack==1);
    assert(!monitor_lan_receive(s,"snapshot",bytes,len,2100,100));assert(snapshots==1 && sent_ack==1);
    base(outer,esp32monitor_companion_Envelope_heartbeat_tag);esp32monitor_companion_Heartbeat *h=&outer->payload.heartbeat;
    h->protocol_version=1;strcpy(h->host_id,host);strcpy(h->device_id,device);strcpy(h->session_id,sid);h->seq=4;h->sent_at=101;
    h->has_last_snapshot_seq=true;h->last_snapshot_seq=3;h->has_config_epoch=true;strcpy(h->config_epoch,epoch);h->has_config_revision=true;h->config_revision=1;
    len=encode(outer,bytes,16384);assert(monitor_lan_receive(s,"heartbeat",bytes,len,3000,101));assert(heartbeats==1 && sent_ack==2 && monitor_lan_state(s)==MONITOR_LAN_CONNECTED);
    assert(monitor_lan_alive(s,92999));assert(!monitor_lan_alive(s,93000));
    base(outer,esp32monitor_companion_Envelope_heartbeat_tag);h=&outer->payload.heartbeat;
    h->protocol_version=1;strcpy(h->host_id,host);strcpy(h->device_id,device);strcpy(h->session_id,sid);h->seq=5;h->sent_at=102;
    h->has_last_snapshot_seq=true;h->last_snapshot_seq=4;
    h->has_config_epoch=true;strcpy(h->config_epoch,epoch);h->has_config_revision=true;h->config_revision=1;
    len=encode(outer,bytes,16384);assert(monitor_lan_receive(s,"heartbeat",bytes,len,3500,102));
    assert(sent_resync==1 && sent_ack==3 && heartbeats==1 && monitor_lan_state(s)==MONITOR_LAN_CONNECTED);
    h->seq=6;len=encode(outer,bytes,16384);assert(monitor_lan_receive(s,"heartbeat",bytes,len,4000,102));
    assert(sent_resync==1 && sent_ack==4);
    bytes[len++]=0x08;bytes[len++]=0x02;assert(!monitor_lan_receive(s,"heartbeat",bytes,len,4100,102));
    cfg.config_revision=2;strcpy(cfg.request_id,"88888888-8888-4888-8888-888888888888");
    assert(monitor_lan_update_config(s,&cfg,4100,102));assert(sent_cfg==2 && monitor_lan_state(s)==MONITOR_LAN_CONFIG);
    base(outer,esp32monitor_companion_Envelope_snapshot_tag);x=&outer->payload.snapshot;
    x->protocol_version=1;strcpy(x->host_id,host);strcpy(x->device_id,device);strcpy(x->session_id,sid);x->seq=8;strcpy(x->config_epoch,epoch);x->config_revision=1;
    strcpy(x->sections.github.state,"disabled");strcpy(x->sections.codex.state,"disabled");strcpy(x->sections.music.state,"disabled");strcpy(x->sections.proxy.state,"disabled");
    len=encode(outer,bytes,16384);assert(monitor_lan_receive(s,"snapshot",bytes,len,4200,102));assert(snapshots==1);
    assert(monitor_lan_goodbye(s,"pairing_revoked",103));assert(sent_goodbye==1 && monitor_lan_state(s)==MONITOR_LAN_CLOSED);
    assert(monitor_lan_close_reason(s)==NULL);
    assert(!monitor_lan_goodbye(s,"pairing_revoked",104));
    monitor_lan_session_t *s2=monitor_lan_create(host,device,pair,sid,open_id,&cfg,send_cb,apply_cb,NULL,5000);assert(s2);
    assert(monitor_lan_start(s2,200));
    base(inner,esp32monitor_companion_Envelope_goodbye_tag);esp32monitor_companion_Goodbye *g=&inner->payload.goodbye;
    g->protocol_version=1;strcpy(g->session_id,sid);strcpy(g->reason,"host_revoked");
    len=wrap(outer,inner,1,NULL,bytes);assert(monitor_lan_receive(s2,"goodbye",bytes,len,5100,201));
    assert(monitor_lan_state(s2)==MONITOR_LAN_CLOSED && !strcmp(monitor_lan_close_reason(s2),"host_revoked"));
    monitor_lan_destroy(s2);
    monitor_lan_destroy(s);free(outer);free(inner);free(bytes);
    puts("LAN session handshake, snapshot, heartbeat, replay, timeout and goodbye passed");return 0;
}
