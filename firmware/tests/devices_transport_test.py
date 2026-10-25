"""Exercise production Devices HTTP transport with deterministic SDK fakes."""
from pathlib import Path
import subprocess
import sys
import re
import tempfile

repo = Path(__file__).resolve().parents[2]
source = (repo / 'firmware/components/firmware/monitor_devices.c').read_text()
start = source.index('typedef struct {\n    int64_t deadline;')
end = source.index('static void devices_task(', start)
production = source[start:end]
prefix = r'''
#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#define ESP_OK 0
#define ESP_FAIL -1
__REQUEST_DEFINE__
#define MONITOR_DEVICES_URL_MAX 192
#define MONITOR_DEVICES_TOKEN_MAX 128
#define MONITOR_DEVICES_BODY_MAX 4096
#define portMAX_DELAY UINT32_MAX
#define SHUT_RDWR 2
typedef int esp_err_t;
typedef struct { char url[193],token[129]; } devices_config_t;
typedef struct { int valid; } monitor_devices_snapshot_t;
typedef struct { bool held; } *SemaphoreHandle_t;
static unsigned mutexes;
static bool fail_mutex,fail_client,fail_timer,fail_start,fail_body;
static const char *fail_header;
static void *test_malloc(size_t size){return fail_body?NULL:malloc(size);}
static SemaphoreHandle_t xSemaphoreCreateMutex(void) { if(fail_mutex)return NULL;mutexes++; return calloc(1,sizeof(struct {bool held;})); }
static void xSemaphoreTake(SemaphoreHandle_t lock,unsigned timeout) { (void)timeout;assert(lock&&!lock->held);lock->held=true; }
static void xSemaphoreGive(SemaphoreHandle_t lock) { assert(lock&&lock->held);lock->held=false; }
static void vSemaphoreDelete(SemaphoreHandle_t lock) { assert(!lock->held);free(lock);mutexes--; }
typedef struct client *esp_http_client_handle_t;
enum {HTTP_EVENT_ON_CONNECTED, HTTP_EVENT_DISCONNECTED, HTTP_EVENT_ON_HEADER};
typedef struct { int event_id;void *user_data;esp_http_client_handle_t client;char *header_key,*header_value; } esp_http_client_event_t;
typedef struct {
    const char *url;int timeout_ms;bool disable_auto_redirect;
    void (*crt_bundle_attach)(void);
    esp_err_t (*event_handler)(esp_http_client_event_t *);
    void *user_data;bool keep_alive_enable;int keep_alive_idle,keep_alive_interval,keep_alive_count;
} esp_http_client_config_t;
static void esp_crt_bundle_attach(void) {}
typedef struct {void (*callback)(void*);void *arg;const char *name;} esp_timer_create_args_t;
typedef struct timer {void (*callback)(void*);void *arg;int64_t due;bool active;} *esp_timer_handle_t;
static esp_timer_handle_t active_timer;
static unsigned timers,shutdowns,created,closed,opens,handshakes;
static int64_t now;
static int64_t esp_timer_get_time(void) {return now;}
static int esp_timer_create(const esp_timer_create_args_t *args,esp_timer_handle_t *out) {
    if(fail_timer)return ESP_FAIL;
    *out=calloc(1,sizeof(**out));(*out)->callback=args->callback;(*out)->arg=args->arg;timers++;return ESP_OK;
}
static int esp_timer_start_once(esp_timer_handle_t t,int64_t delay) {if(fail_start)return ESP_FAIL;assert(!active_timer || !active_timer->active);t->due=now+delay;t->active=true;active_timer=t;return ESP_OK;}
static int esp_timer_stop_blocking(esp_timer_handle_t t,unsigned wait) {(void)wait;t->active=false;active_timer=NULL;return ESP_OK;}
static int esp_timer_delete(esp_timer_handle_t t) {assert(!t->active);free(t);timers--;return ESP_OK;}
static void advance(int64_t duration) {
    int64_t target=now+duration;
    while(active_timer && active_timer->active && target>=active_timer->due) {
        now=active_timer->due;active_timer->active=false;active_timer->callback(active_timer->arg);
    }
    now=target;
}
static int shutdown(int socket,int how) {assert(socket>=0&&how==SHUT_RDWR);shutdowns++;return 0;}
typedef struct {
    const char *content_type,*encoding;int payload_len;
    int status,fail_stage;bool close,missing_type,invalid_json,overflow,incomplete;
    int64_t open_delay,connect_delay,header_delay;
} reply_t;
static reply_t replies[8];static unsigned reply_index;
struct client {
    esp_http_client_config_t config;bool connected;int timeout;unsigned pos;reply_t reply;
};
static void event(esp_http_client_handle_t c,int id,char *key,char *value) {
    esp_http_client_event_t e={.event_id=id,.user_data=c->config.user_data,.client=c,.header_key=key,.header_value=value};
    c->config.event_handler(&e);
}
static esp_http_client_handle_t esp_http_client_init(const esp_http_client_config_t *config) {
    assert(config->keep_alive_enable && config->disable_auto_redirect);
    if(fail_client)return NULL;
    assert(strstr(config->url,"/v1/devices/summary"));
    esp_http_client_handle_t c=calloc(1,sizeof(*c));c->config=*config;created++;return c;
}
static int esp_http_client_get_socket(esp_http_client_handle_t c) {return c->connected?42:-1;}
static int esp_http_client_set_timeout_ms(esp_http_client_handle_t c,int ms) {assert(ms>0&&ms<=(REQUEST_US / 1000));c->timeout=ms;return ESP_OK;}
static int esp_http_client_set_header(esp_http_client_handle_t c,const char *key,const char *value) {
    (void)c;if(fail_header && !strcmp(key,fail_header))return ESP_FAIL;
    if(!strcmp(key,"Connection"))assert(!strcmp(value,"keep-alive"));return ESP_OK;
}
static int esp_http_client_open(esp_http_client_handle_t c,int length) {
    assert(length==0);assert(reply_index<8);opens++;c->reply=replies[reply_index++];c->pos=0;
    if(!c->connected) {advance(c->reply.connect_delay);c->connected=true;handshakes++;event(c,HTTP_EVENT_ON_CONNECTED,NULL,NULL);}
    advance(c->reply.open_delay?c->reply.open_delay:10000);
    return c->reply.fail_stage==1?ESP_FAIL:ESP_OK;
}
static int64_t esp_http_client_fetch_headers(esp_http_client_handle_t c) {
    advance(c->reply.header_delay?c->reply.header_delay:1000);
    if(c->reply.fail_stage==2)return -1;
    if(!c->reply.missing_type)event(c,HTTP_EVENT_ON_HEADER,"Content-Type",(char *)(c->reply.content_type?c->reply.content_type:"application/json"));
    if(c->reply.encoding)event(c,HTTP_EVENT_ON_HEADER,"Content-Encoding",(char *)c->reply.encoding);
    if(c->reply.overflow)event(c,HTTP_EVENT_ON_HEADER,"Content-Length","99999");
    return 2;
}
static int esp_http_client_get_status_code(esp_http_client_handle_t c) {return c->reply.status?c->reply.status:200;}
static int esp_http_client_read(esp_http_client_handle_t c,char *out,int size) {
    advance(1000);if(c->reply.fail_stage==3)return -1;
    if(c->reply.payload_len){
        int remaining=c->reply.payload_len-(int)c->pos;if(!remaining)return 0;
        int n=remaining<size?remaining:size;assert(n>0);memset(out,'x',n);c->pos+=n;return n;
    }
    if(c->pos)return 0;
    assert(size>=2);memcpy(out,c->reply.invalid_json?"xx":"{}",2);c->pos=2;return 2;
}
static bool esp_http_client_is_complete_data_received(esp_http_client_handle_t c) {return c->pos==2&&!c->reply.incomplete;}
static bool esp_http_client_is_persistent_connection(esp_http_client_handle_t c) {assert(c);return !c->reply.close;}
static void esp_http_client_cleanup(esp_http_client_handle_t c) {
    if(c->connected)event(c,HTTP_EVENT_DISCONNECTED,NULL,NULL);closed++;free(c);
}
static bool monitor_devices_parse(const char *body,size_t size,monitor_devices_snapshot_t *out) {
    out->valid=size==2&&!memcmp(body,"{}",2);return out->valid;
}
'''
tests = r'''
static void reset_replies(void) {memset(replies,0,sizeof(replies));reply_index=0;}
static void check_clean(void) {assert(!active_timer && !timers && !mutexes && created==closed);}
int main(void) {
    devices_transport_t t={.guard.socket=-1};devices_config_t cfg={.url="https://example.test",.token="fixture"};
    monitor_devices_snapshot_t snapshot;int status=0;bool reused=false;
    assert(fetch(&t,&cfg,&snapshot,&status,&reused) && status==200 && !reused);
    assert(created==1 && !closed && handshakes==1 && !active_timer);
    advance(30000000); // no timer callback can shut down an idle retained socket
    assert(!shutdowns);
    assert(fetch(&t,&cfg,&snapshot,&status,&reused) && reused);
    assert(created==1 && handshakes==1 && !closed);
    transport_reset(&t);check_clean();
    puts("Two polls 30s apart reuse one handle/connection; idle timer disarmed and context remains alive: PASS");
    for(int stage=1;stage<=3;stage++) {
        reset_replies();assert(fetch(&t,&cfg,&snapshot,&status,&reused));
        unsigned before=handshakes;replies[1].fail_stage=stage;
        assert(fetch(&t,&cfg,&snapshot,&status,&reused) && !reused);
        assert(handshakes==before+1 && reply_index==3);
        transport_reset(&t);check_clean();
    }
    puts("Expired idle connection at write/header/body phase retries once with a new connection: PASS");
    reset_replies();replies[0].close=true;
    assert(fetch(&t,&cfg,&snapshot,&status,&reused) && !t.client);
    assert(fetch(&t,&cfg,&snapshot,&status,&reused) && !reused);
    transport_reset(&t);check_clean();
    puts("Connection: close releases handle; next poll reconnects: PASS");
    for(unsigned fault=0;fault<11;fault++) {
        reset_replies();assert(fetch(&t,&cfg,&snapshot,&status,&reused));
        if(fault==0)replies[1].status=401;
        if(fault==1)replies[1].status=403;
        if(fault==2)replies[1].missing_type=true;
        if(fault==3)replies[1].overflow=true;
        if(fault==4)replies[1].invalid_json=true;
        if(fault==5)replies[1].status=503;
        const int statuses[]={301,400,404,429,500};
        if(fault>=6)replies[1].status=statuses[fault-6];
        assert(!fetch(&t,&cfg,&snapshot,&status,&reused) && !t.client && reply_index==2);
        transport_reset(&t);check_clean();
    }
    puts("Auth/HTTP errors, missing content type, oversize and invalid JSON discard connection without retry: PASS");
    reset_replies();assert(fetch(&t,&cfg,&snapshot,&status,&reused));
    replies[1].fail_stage=1;replies[1].open_delay=REQUEST_US*3/4;replies[2].open_delay=REQUEST_US;
    int64_t started=now;unsigned shutdowns_before=shutdowns;
    assert(!fetch(&t,&cfg,&snapshot,&status,&reused) && !t.client);
    assert(now-started==REQUEST_US*7/4 && shutdowns==shutdowns_before+1);
    transport_reset(&t);check_clean();
    puts("Reconnect has bounded setup/response phases and cannot accept a late response: PASS");
    reset_replies();replies[0].connect_delay=3000000;replies[0].header_delay=2000000;
    started=now;shutdowns_before=shutdowns;
    assert(fetch(&t,&cfg,&snapshot,&status,&reused) && now-started>REQUEST_US);
    assert(now-started<REQUEST_US*2 && shutdowns==shutdowns_before);
    transport_reset(&t);check_clean();
    reset_replies();replies[0].connect_delay=REQUEST_US+1;
    assert(!fetch(&t,&cfg,&snapshot,&status,&reused) && t.guard.timed_out);
    transport_reset(&t);check_clean();
    reset_replies();replies[0].header_delay=REQUEST_US+1;
    assert(!fetch(&t,&cfg,&snapshot,&status,&reused) && t.guard.timed_out);
    transport_reset(&t);check_clean();
    puts("Slow cold TLS gets a separate response budget; setup/response timeouts still reject late data: PASS");
    reset_replies();replies[0].fail_stage=1;
    assert(!fetch(&t,&cfg,&snapshot,&status,&reused) && reply_index==1);
    transport_reset(&t);check_clean();
    puts("Fresh connection failure is not retried; reset frees all connection/guard resources: PASS");
    reset_replies();assert(fetch(&t,&cfg,&snapshot,&status,&reused));
    unsigned old=created;transport_reset(&t);strcpy(cfg.token,"changed-fixture");
    assert(fetch(&t,&cfg,&snapshot,&status,&reused) && !reused && created==old+1);
    transport_reset(&t);check_clean();
    puts("Configuration/Wi-Fi generation reset prevents reuse of the old client: PASS");
    const char *types[]={"text/html","application/jsonp","application/jsonx",""};
    for(unsigned i=0;i<sizeof(types)/sizeof(*types);++i){
        reset_replies();replies[0].content_type=types[i];
        assert(!fetch(&t,&cfg,&snapshot,&status,&reused) && reply_index==1);
        transport_reset(&t);check_clean();
    }
    reset_replies();replies[0].content_type="Application/JSON; charset=utf-8";
    assert(fetch(&t,&cfg,&snapshot,&status,&reused));transport_reset(&t);check_clean();
    reset_replies();replies[0].encoding="gzip";
    assert(!fetch(&t,&cfg,&snapshot,&status,&reused));transport_reset(&t);check_clean();
    reset_replies();replies[0].encoding="identity";
    assert(fetch(&t,&cfg,&snapshot,&status,&reused));transport_reset(&t);check_clean();
    for(int n=4096;n<=4097;++n){reset_replies();replies[0].payload_len=n;
        assert(!fetch(&t,&cfg,&snapshot,&status,&reused) && reply_index==1);
        transport_reset(&t);check_clean();
    }
    reset_replies();assert(fetch(&t,&cfg,&snapshot,&status,&reused));
    replies[1].incomplete=replies[2].incomplete=true;
    assert(!fetch(&t,&cfg,&snapshot,&status,&reused) && reply_index==3);
    transport_reset(&t);check_clean();
    // Allocate/init/start/header failures must not leak guards, timers or clients.
    for(unsigned failure=0;failure<9;++failure){reset_replies();
        fail_mutex=failure==0;fail_body=failure==1;fail_client=failure==2;fail_timer=failure==3;fail_start=failure==4;
        const char *headers[]={"Authorization","Accept","Accept-Encoding","Connection"};
        fail_header=failure>=5?headers[failure-5]:NULL;
        assert(!fetch(&t,&cfg,&snapshot,&status,&reused));
        transport_reset(&t);check_clean();
        fail_mutex=fail_body=fail_client=fail_timer=fail_start=false;fail_header=NULL;
        reset_replies();assert(fetch(&t,&cfg,&snapshot,&status,&reused));transport_reset(&t);check_clean();
    }
    puts("Content type/encoding, actual body overflow, incomplete stream bounded retry, allocation/SDK/header failures and subsequent recovery: PASS");
    return 0;
}
'''
with tempfile.TemporaryDirectory(prefix='monitor-devices-') as directory:
    c = Path(directory) / 'transport.c'
    c.write_text(prefix.replace('__REQUEST_DEFINE__', re.search(r'^#define REQUEST_US[^\n]*', source, re.M)[0]) + '\n#define malloc test_malloc\n' + production + tests)
    binary = Path(directory) / 'transport'
    subprocess.run([sys.argv[1] if len(sys.argv)>1 else 'cc', '-std=c11', '-Wall', '-Wextra', '-Werror', str(c), '-o', str(binary)], check=True)
    subprocess.run([str(binary)], check=True)
