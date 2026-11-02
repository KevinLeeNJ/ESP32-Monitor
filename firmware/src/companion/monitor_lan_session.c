#include "monitor_lan_session.h"
#include "monitor_proto_strict.h"
#include "pb_decode.h"
#include "pb_encode.h"
#include <stdlib.h>
#include <string.h>
#include <math.h>
#include <stdio.h>

#ifdef ESP_PLATFORM
#include "esp_heap_caps.h"
#endif

/* Envelopes are ~6 KB each and outlive nothing but a session. Keeping several
 * of them in internal RAM starves the Wi-Fi/MQTT task stacks, so prefer PSRAM. */
static void *large_calloc(size_t size)
{
#ifdef ESP_PLATFORM
    void *p = heap_caps_calloc(1, size, MALLOC_CAP_SPIRAM | MALLOC_CAP_8BIT);
    if (p) return p;
#endif
    return calloc(1, size);
}

struct monitor_lan_session {
    char host[37], device[37], pairing[37], session[37], open_request[37];
    esp32monitor_companion_Config config;
    monitor_lan_send_t send;
    monitor_lan_apply_t apply;
    void *user;
    monitor_lan_state_t state;
    char close_reason[32];
    uint32_t output_seq, input_seq, snapshot_seq;
    uint64_t started_ms, last_proof_ms, first_snapshot_ms, last_resync_ms;
    esp32monitor_companion_Envelope *outer, *inner;
    uint8_t data[8192];
    bool serial;
};

static bool copy_id(char out[37], const char *in)
{
    if (!in || strlen(in) != 36) return false;
    for (unsigned i = 0; i < 36; i++) {
        if (i == 8 || i == 13 || i == 18 || i == 23) { if (in[i] != '-') return false; }
        else if (!((in[i] >= '0' && in[i] <= '9') || (in[i] >= 'a' && in[i] <= 'f'))) return false;
    }
    memcpy(out, in, 37); return true;
}

static bool encode(const esp32monitor_companion_Envelope *env, uint8_t *out, size_t capacity, size_t *n)
{
    pb_ostream_t stream = pb_ostream_from_buffer(out, capacity);
    if (!pb_encode(&stream, esp32monitor_companion_Envelope_fields, env)) return false;
    *n = stream.bytes_written; return true;
}
static bool decode(esp32monitor_companion_Envelope *out, const uint8_t *data, size_t length)
{
    if (!length || length > 16384 || !monitor_proto_strict(data, length, esp32monitor_companion_Envelope_fields)) return false;
    memset(out, 0, sizeof *out);
    pb_istream_t stream = pb_istream_from_buffer(data, length);
    return pb_decode(&stream, esp32monitor_companion_Envelope_fields, out) && out->wire_version == 2;
}

bool monitor_lan_presence(const char *host, const char *device, const char *session,
                          bool online, uint8_t *out, size_t capacity, size_t *length)
{
    if (!out || !length) return false;
    esp32monitor_companion_Envelope *env = large_calloc(sizeof *env);
    if (!env) return false;
    env->wire_version = 2; env->which_payload = esp32monitor_companion_Envelope_presence_tag;
    esp32monitor_companion_Presence *p = &env->payload.presence;
    bool ok = copy_id(p->host_id, host) && copy_id(p->device_id, device) && copy_id(p->session_id, session);
    p->protocol_version = 1; strcpy(p->state, online ? "online" : "offline");
    strcpy(p->reason, online ? "connected" : "transport_lost");
    if (ok) ok = encode(env, out, capacity, length);
    free(env); return ok;
}

monitor_lan_session_t *monitor_lan_create(const char *host, const char *device, const char *pairing,
                                         const char *session, const char *open_request,
                                         const esp32monitor_companion_Config *config,
                                         monitor_lan_send_t send, monitor_lan_apply_t apply, void *user,
                                         uint64_t now)
{
    if (!config || !send || !apply) return NULL;
    monitor_lan_session_t *s = large_calloc(sizeof *s);
    if (!s) return NULL;
    if (!copy_id(s->host, host) || !copy_id(s->device, device) || !copy_id(s->pairing, pairing) ||
        !copy_id(s->session, session) || !copy_id(s->open_request, open_request) ||
        strcmp(config->device_id, device) || strcmp(config->session_id, session) ||
        !copy_id((char[37]){0}, config->request_id) || !copy_id((char[37]){0}, config->config_epoch) ||
        config->protocol_version != 1 || !config->config_revision || !config->time.enabled) goto fail;
    s->outer = large_calloc(sizeof *s->outer); s->inner = large_calloc(sizeof *s->inner);
    if (!s->outer || !s->inner) goto fail;
    s->config = *config; s->send = send; s->apply = apply; s->user = user;
    s->state = MONITOR_LAN_OPEN; s->started_ms = s->last_proof_ms = now;
    return s;
fail: monitor_lan_destroy(s); return NULL;
}
monitor_lan_session_t *monitor_lan_create_serial(const char *host, const char *device, const char *pairing,
                                                 const char *session, const esp32monitor_companion_Config *config,
                                                 monitor_lan_send_t send, monitor_lan_apply_t apply, void *user,
                                                 uint64_t now)
{
    if (!config || !send || !apply) return NULL;
    monitor_lan_session_t *s = large_calloc(sizeof *s);
    if (!s) return NULL;
    if (!copy_id(s->host, host) || !copy_id(s->device, device) || !copy_id(s->pairing, pairing) ||
        !copy_id(s->session, session) ||
        strcmp(config->device_id, device) || strcmp(config->session_id, session) ||
        !copy_id((char[37]){0}, config->request_id) || !copy_id((char[37]){0}, config->config_epoch) ||
        config->protocol_version != 1 || !config->config_revision || !config->time.enabled) goto fail;
    s->outer = large_calloc(sizeof *s->outer); s->inner = large_calloc(sizeof *s->inner);
    if (!s->outer || !s->inner) goto fail;
    s->config = *config; s->send = send; s->apply = apply; s->user = user;
    s->state = MONITOR_LAN_CONFIG; s->started_ms = s->last_proof_ms = now; s->serial = true;
    return s;
fail: monitor_lan_destroy(s); return NULL;
}

bool monitor_lan_config_accepted(monitor_lan_session_t *s, uint64_t now)
{
    if (!s || !s->serial || s->state != MONITOR_LAN_CONFIG) return false;
    s->state = MONITOR_LAN_SNAPSHOT;
    s->started_ms = now;
    return true;
}

uint32_t monitor_lan_snapshot_seq(const monitor_lan_session_t *s)
{
    return s ? s->snapshot_seq : 0;
}

void monitor_lan_destroy(monitor_lan_session_t *s)
{
    if (!s) return;
    free(s->outer); free(s->inner); memset(s, 0, sizeof *s); free(s);
}

static bool control(monitor_lan_session_t *s, const char *topic, const char *request_id, int64_t now)
{
    size_t nested;
    if (!encode(s->inner, s->data, 4096, &nested) || s->output_seq == UINT32_MAX) return false;
    if (s->serial) {
        /* Serial frames carry the inner Envelope directly; the transport owns seq. */
        return s->send(s->user, topic, s->data, nested, 0);
    }
    memset(s->outer, 0, sizeof *s->outer);
    s->outer->wire_version = 2; s->outer->which_payload = esp32monitor_companion_Envelope_control_tag;
    esp32monitor_companion_Control *c = &s->outer->payload.control;
    c->protocol_version = 1; strcpy(c->host_id, s->host); strcpy(c->device_id, s->device); strcpy(c->session_id, s->session);
    c->seq = ++s->output_seq; c->sent_at = now;
    if (request_id) { c->has_request_id = true; strcpy(c->request_id, request_id); }
    c->data.size = nested; memcpy(c->data.bytes, s->data, nested);
    size_t len;
    return encode(s->outer, s->data, sizeof s->data, &len) && s->send(s->user, topic, s->data, len, !strcmp(topic, "ack") ? 0 : 1);
}

bool monitor_lan_start(monitor_lan_session_t *s, int64_t unix_seconds)
{
    if (!s || s->state != MONITOR_LAN_OPEN) return false;
    size_t len;
    if (!monitor_lan_presence(s->host, s->device, s->session, true, s->data, sizeof s->data, &len) ||
        !s->send(s->user, "presence", s->data, len, 1)) return false;
    memset(s->inner, 0, sizeof *s->inner);
    s->inner->wire_version = 2; s->inner->which_payload = esp32monitor_companion_Envelope_open_tag;
    esp32monitor_companion_Open *o = &s->inner->payload.open;
    o->protocol_version=1; strcpy(o->request_id,s->open_request); strcpy(o->pairing_id,s->pairing);
    strcpy(o->device_id,s->device); strcpy(o->device_name,"ESP32 Monitor");
    strcpy(o->firmware_version,"1.0.0"); strcpy(o->transport,"lan");
    return control(s,"open",s->open_request,unix_seconds);
}

bool monitor_lan_update_config(monitor_lan_session_t *s, const esp32monitor_companion_Config *config,
                               uint64_t now, int64_t unix_seconds)
{
    if (!s || !config || s->state < MONITOR_LAN_SNAPSHOT || s->state == MONITOR_LAN_CLOSED ||
        config->protocol_version != 1 || strcmp(config->device_id, s->device) ||
        strcmp(config->session_id, s->session) || strcmp(config->config_epoch, s->config.config_epoch) ||
        s->config.config_revision == UINT32_MAX || config->config_revision != s->config.config_revision + 1 ||
        !copy_id((char[37]){0}, config->request_id) || !strcmp(config->request_id,s->config.request_id) || !config->time.enabled) return false;
    s->config = *config; s->snapshot_seq = 0; s->first_snapshot_ms = 0;
    s->started_ms = now; s->state = MONITOR_LAN_CONFIG;
    memset(s->inner, 0, sizeof *s->inner); s->inner->wire_version = 2;
    s->inner->which_payload = esp32monitor_companion_Envelope_config_tag;
    s->inner->payload.config = *config;
    return control(s,"proxy-config",s->config.request_id,unix_seconds);
}

static bool ids(monitor_lan_session_t *s, const char *host, const char *device, const char *session)
{ return !strcmp(s->host,host) && !strcmp(s->device,device) && !strcmp(s->session,session); }

static bool health(const char *state, bool has_age, int64_t age, int64_t stale, int64_t expire)
{
    const char *valid[] = {"disabled", "starting", "healthy", "degraded", "auth_required",
                           "permission_denied", "unsupported", "error"};
    bool found = false;
    for (unsigned i=0; i<sizeof valid/sizeof valid[0]; i++) if (!strcmp(state,valid[i])) found=true;
    return found && (!has_age || age >= 0) && stale >= 0 && expire >= stale;
}
#define VALID_HEALTH(v) health((v).state,(v).has_age_ms,(v).age_ms,(v).stale_after_ms,(v).expire_after_ms)

static bool valid_snapshot(const esp32monitor_companion_Snapshot *s)
{
    const esp32monitor_companion_Sections *x=&s->sections;
    if (!VALID_HEALTH(x->github) || !VALID_HEALTH(x->codex) || !VALID_HEALTH(x->music) || !VALID_HEALTH(x->proxy)) return false;
    if (x->github.has_data) {
        const esp32monitor_companion_GitHubData *g=&x->github.data;
        if (!g->days || g->days>371 || strlen(g->levels)!=g->days || strlen(g->start_date)!=10) return false;
        for (unsigned i=0;i<g->days;i++) if (g->levels[i]<'0' || g->levels[i]>'4') return false;
    }
    if (x->codex.has_data) {
        const esp32monitor_companion_CodexData *c=&x->codex.data;
        if (!VALID_HEALTH(c->quota) || !VALID_HEALTH(c->tasks)) return false;
        if (c->quota.has_data) for (unsigned i=0;i<c->quota.data.windows_count;i++) {
            const esp32monitor_companion_QuotaWindow *w=&c->quota.data.windows[i];
            if (w->has_used_percent && (!isfinite(w->used_percent) || w->used_percent<0 || w->used_percent>100)) return false;
        }
        if (c->tasks.has_data) {
            const char *state=c->tasks.data.state;
            const char *valid[]={"unknown","idle","running","waiting_input","waiting_approval","completed","failed","cancelled"};
            bool found=false; for (unsigned i=0;i<sizeof valid/sizeof valid[0];i++) if (!strcmp(state,valid[i])) found=true;
            if (!found) return false;
        }
    }
    if (x->music.has_data && x->music.data.has_current) {
        const esp32monitor_companion_CurrentMusic *m=&x->music.data.current;
        if ((strcmp(m->state,"playing") && strcmp(m->state,"not_playing")) ||
            (m->has_duration && (!isfinite(m->duration) || m->duration<0)) ||
            (m->has_position && (!isfinite(m->position) || m->position<0))) return false;
    }
    if (x->proxy.has_data) {
        const esp32monitor_companion_ProxyData *p=&x->proxy.data;
        if (!VALID_HEALTH(p->time) || !VALID_HEALTH(p->weather) || !VALID_HEALTH(p->aqi) || !VALID_HEALTH(p->devices)) return false;
        if (p->time.has_data && (p->time.data.utc_offset_minutes < -720 || p->time.data.utc_offset_minutes > 840)) return false;
        if (p->devices.has_data) {
            const esp32monitor_companion_DeviceSummary *d=&p->devices.data;
            if (d->device_count < 0 || d->device_count != d->devices_count) return false;
            for (unsigned i=0;i<d->devices_count;i++) {
                const esp32monitor_companion_DeviceCard *card=&d->devices[i];
                if (card->metric_count < 0 || card->metric_count != card->metrics_count) return false;
                for (unsigned j=0;j<card->metrics_count;j++)
                    if (card->metrics[j].has_percent && (card->metrics[j].percent < 0 || card->metrics[j].percent > 100)) return false;
            }
        }
    }
    return true;
}

static bool ack(monitor_lan_session_t *s, uint32_t seq, int64_t unix_seconds)
{
    memset(s->inner,0,sizeof *s->inner); s->inner->wire_version=2;
    s->inner->which_payload=esp32monitor_companion_Envelope_ack_tag;
    esp32monitor_companion_Ack *a=&s->inner->payload.ack;
    a->protocol_version=1; strcpy(a->session_id,s->session); a->ack_seq=seq; strcpy(a->status,"accepted");
    return control(s,"ack",NULL,unix_seconds);
}

static bool request_resync(monitor_lan_session_t *s, uint64_t now, int64_t unix_seconds)
{
    if (s->last_resync_ms && now - s->last_resync_ms < 10000) return true;
    memset(s->inner,0,sizeof *s->inner); s->inner->wire_version=2;
    s->inner->which_payload=esp32monitor_companion_Envelope_resync_tag;
    esp32monitor_companion_Resync *r=&s->inner->payload.resync;
    strcpy(r->reason,"missing_snapshot");
    if (s->snapshot_seq) { r->has_last_snapshot_seq=true; r->last_snapshot_seq=s->snapshot_seq; }
    if (!control(s,"resync",NULL,unix_seconds)) return false;
    s->last_resync_ms=now;
    return true;
}

bool monitor_lan_receive(monitor_lan_session_t *s, const char *suffix, const uint8_t *data,
                         size_t length, uint64_t now, int64_t unix_seconds)
{
    if (!s || !suffix || s->state == MONITOR_LAN_CLOSED || !decode(s->outer,data,length)) return false;
    esp32monitor_companion_Envelope *e=s->outer;
    if (!strcmp(suffix,"snapshot")) {
        if (e->which_payload != esp32monitor_companion_Envelope_snapshot_tag) return false;
        esp32monitor_companion_Snapshot *x=&e->payload.snapshot;
        if (x->protocol_version != 1 || !ids(s,x->host_id,x->device_id,x->session_id) || x->seq <= s->input_seq) return false;
        if (s->state == MONITOR_LAN_CONFIG && !strcmp(x->config_epoch,s->config.config_epoch) &&
            x->config_revision < s->config.config_revision) { s->input_seq=x->seq; return true; }
        if (s->state < MONITOR_LAN_SNAPSHOT ||
            strcmp(x->config_epoch,s->config.config_epoch) || x->config_revision != s->config.config_revision ||
            (x->sections.proxy.has_data && (strcmp(x->sections.proxy.data.config_epoch,x->config_epoch) || x->sections.proxy.data.config_revision != x->config_revision)) ||
            !valid_snapshot(x)) return false;
        if (!s->apply(s->user,x,NULL)) return false;
        uint32_t seq=x->seq;
        s->input_seq=s->snapshot_seq=seq; s->last_proof_ms=now;
        if (!s->first_snapshot_ms) s->first_snapshot_ms=now;
        if (s->state != MONITOR_LAN_CONNECTED) s->state=MONITOR_LAN_PROVING;
        return ack(s,seq,unix_seconds);
    }
    if (!strcmp(suffix,"heartbeat")) {
        if (e->which_payload != esp32monitor_companion_Envelope_heartbeat_tag || s->state < MONITOR_LAN_PROVING) return false;
        esp32monitor_companion_Heartbeat *h=&e->payload.heartbeat;
        if (h->protocol_version != 1 || !ids(s,h->host_id,h->device_id,h->session_id) || h->seq <= s->input_seq ||
            !h->has_config_epoch || strcmp(h->config_epoch,s->config.config_epoch) ||
            !h->has_config_revision || h->config_revision != s->config.config_revision) return false;
        bool missing = !h->has_last_snapshot_seq || h->last_snapshot_seq != s->snapshot_seq;
        for (unsigned i=0;i<h->source_updates_count;i++)
            if (h->source_updates[i].snapshot_seq != s->snapshot_seq) missing=true;
        if (missing) {
            uint32_t seq=h->seq; s->input_seq=seq; s->last_proof_ms=now;
            if (!request_resync(s,now,unix_seconds)) return false;
            return ack(s,seq,unix_seconds);
        }
        if (!s->apply(s->user,NULL,h)) return false;
        uint32_t seq=h->seq; s->input_seq=seq; s->last_proof_ms=now;
        if (now-s->first_snapshot_ms >= 1000) s->state=MONITOR_LAN_CONNECTED;
        return ack(s,seq,unix_seconds);
    }
    if (e->which_payload != esp32monitor_companion_Envelope_control_tag) return false;
    esp32monitor_companion_Control *c=&e->payload.control;
    if (c->protocol_version != 1 || !ids(s,c->host_id,c->device_id,c->session_id) || c->seq <= s->input_seq ||
        !decode(s->inner,c->data.bytes,c->data.size)) return false;
    if (!strcmp(suffix,"session")) {
        esp32monitor_companion_Session *v=&s->inner->payload.session;
        if (s->state != MONITOR_LAN_OPEN || s->inner->which_payload != esp32monitor_companion_Envelope_session_tag ||
            !c->has_request_id || strcmp(c->request_id,s->open_request) || v->protocol_version != 1 ||
            !ids(s,v->host_id,v->device_id,v->session_id) || strcmp(v->pairing_id,s->pairing) ||
            strcmp(v->transport,"lan") || !v->config_required || v->disconnect_timeout_ms != 90000) return false;
        s->input_seq=c->seq;
        memset(s->inner,0,sizeof *s->inner); s->inner->wire_version=2;
        s->inner->which_payload=esp32monitor_companion_Envelope_config_tag; s->inner->payload.config=s->config;
        s->state=MONITOR_LAN_CONFIG;
        return control(s,"proxy-config",s->config.request_id,unix_seconds);
    }
    if (!strcmp(suffix,"config-ack")) {
        esp32monitor_companion_ConfigAck *v=&s->inner->payload.config_ack;
        if (s->state != MONITOR_LAN_CONFIG || s->inner->which_payload != esp32monitor_companion_Envelope_config_ack_tag ||
            !c->has_request_id || strcmp(c->request_id,s->config.request_id) || v->protocol_version != 1 ||
            !v->has_request_id || strcmp(v->request_id,s->config.request_id) || strcmp(v->device_id,s->device) ||
            strcmp(v->session_id,s->session) || !v->has_config_epoch || strcmp(v->config_epoch,s->config.config_epoch) ||
            !v->has_config_revision || v->config_revision != s->config.config_revision || strcmp(v->state,"applied") ||
            v->weather_configured != s->config.weather.enabled || v->devices_configured != s->config.devices.enabled) return false;
        s->input_seq=c->seq; s->state=MONITOR_LAN_SNAPSHOT; return true;
    }
    if (!strcmp(suffix,"goodbye") && s->inner->which_payload == esp32monitor_companion_Envelope_goodbye_tag &&
        s->inner->payload.goodbye.protocol_version == 1 && !strcmp(s->inner->payload.goodbye.session_id,s->session)) {
        snprintf(s->close_reason,sizeof s->close_reason,"%.*s",(int)(sizeof s->close_reason-1),s->inner->payload.goodbye.reason);
        s->state=MONITOR_LAN_CLOSED; return true;
    }
    return false;
}

bool monitor_lan_goodbye(monitor_lan_session_t *s, const char *reason, int64_t unix_seconds)
{
    if (!s || !reason || !*reason || strlen(reason) >= sizeof s->inner->payload.goodbye.reason ||
        s->state == MONITOR_LAN_CLOSED) return false;
    memset(s->inner,0,sizeof *s->inner);
    s->inner->wire_version=2; s->inner->which_payload=esp32monitor_companion_Envelope_goodbye_tag;
    esp32monitor_companion_Goodbye *g=&s->inner->payload.goodbye;
    g->protocol_version=1; strcpy(g->session_id,s->session); strcpy(g->reason,reason);
    if (!control(s,"goodbye",NULL,unix_seconds)) return false;
    s->state=MONITOR_LAN_CLOSED;
    return true;
}

const char *monitor_lan_close_reason(const monitor_lan_session_t *s)
{
    return s && s->close_reason[0] ? s->close_reason : NULL;
}

bool monitor_lan_alive(const monitor_lan_session_t *s, uint64_t now)
{
    if (!s || s->state == MONITOR_LAN_CLOSED || now < s->started_ms) return false;
    if (s->state != MONITOR_LAN_CONNECTED && now - s->started_ms >= 15000) return false;
    return now >= s->last_proof_ms && now - s->last_proof_ms < 90000;
}
monitor_lan_state_t monitor_lan_state(const monitor_lan_session_t *s) { return s ? s->state : MONITOR_LAN_CLOSED; }
