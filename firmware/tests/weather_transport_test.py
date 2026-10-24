"""Production HTTP callback with real zlib, fragmented wire data and SDK fakes."""
import binascii
import gzip
import random
import sys
from host_harness import REPO, defines, run

source = (REPO/'firmware/components/firmware/monitor_weather.c').read_text()
plain = b'{"code":"200"}'
compressed = gzip.compress(plain, mtime=0)
header = bytearray(compressed[:10])
header[3] = 0x1e  # FHCRC, FEXTRA, FNAME, FCOMMENT
header += b'\x03\x00abcweather.json\x00comment\x00'
header += (binascii.crc32(header) & 0xffff).to_bytes(2, 'little')
extended = header + compressed[10:]
large_header = bytearray(compressed[:10])
large_header[3] = 0x10
large_header += b'x' * 10000 + b'\x00'
large_header += compressed[10:]

rng = random.Random(314159)
large_json = b'{"data":"' + bytes(rng.choice(b'abcdefghijklmnopqrstuvwxyz0123456789') for _ in range(6000)) + b'"}'

code = r'''
#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#include <limits.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <strings.h>
#include <zlib.h>
#define ESP_OK 0
#define ESP_FAIL -1
#define ESP_LOGW(...) ((void)0)
#define TAG "fixture"
#define HTTP_EVENT_ON_DATA 1
#define HTTP_EVENT_ON_HEADER 2
#define HTTP_EVENT_HEADERS_SENT 3
#define HTTP_EVENT_ON_FINISH 4
typedef int esp_err_t;
typedef struct {void *user_data;int event_id,data_len;void *data;char *header_key,*header_value;} esp_http_client_event_t;
typedef struct {const char *url;int timeout_ms,buffer_size;bool disable_auto_redirect;esp_err_t (*event_handler)(esp_http_client_event_t *);void *user_data;void (*crt_bundle_attach)(void);} esp_http_client_config_t;
typedef struct {esp_http_client_config_t config;} *esp_http_client_handle_t;
static struct {esp_http_client_config_t config;} client;
static unsigned created,cleaned,watchdogs,headers,performed,allocations,alloc_calls,fail_alloc;
static unsigned chunk=3;
static bool fail_init,fail_perform,fail_header,complete=true,redirect,partial_output;
static int http_status=200;
static const uint8_t *wire;static size_t wire_size;
static const char *encoding;
static void esp_crt_bundle_attach(void){}
static void feed_task_watchdog(void){++watchdogs;}
static voidpf tracked_alloc(voidpf opaque,uInt count,uInt size){
    (void)opaque;++alloc_calls;if(alloc_calls==fail_alloc)return NULL;
    void *p=calloc(count,size);if(p)++allocations;return p;
}
static void tracked_free(voidpf opaque,voidpf p){
    (void)opaque;if(p){assert(allocations);--allocations;}free(p);
}
static int tracked_init(z_streamp stream,int bits){
    assert(bits==MAX_WBITS+16);
    stream->zalloc=tracked_alloc;stream->zfree=tracked_free;
    return inflateInit2(stream,bits);
}
#undef inflateInit2
#define inflateInit2 tracked_init
static esp_http_client_handle_t esp_http_client_init(const esp_http_client_config_t *cfg){
    assert(cfg->timeout_ms==4000 && cfg->buffer_size==1024 && cfg->crt_bundle_attach && cfg->event_handler);
    if(fail_init)return NULL;
    ++created;client.config=*cfg;return (esp_http_client_handle_t)&client;
}
static int esp_http_client_set_header(esp_http_client_handle_t c,const char *name,const char *value){
    assert(c && !strcmp(name,"Accept-Encoding") && !strcmp(value,"gzip, identity"));
    ++headers;return fail_header?ESP_FAIL:ESP_OK;
}
static int esp_http_client_perform(esp_http_client_handle_t c);
static int esp_http_client_get_status_code(esp_http_client_handle_t c){assert(c);return http_status;}
static bool esp_http_client_is_complete_data_received(esp_http_client_handle_t c){assert(c);return complete;}
static void esp_http_client_cleanup(esp_http_client_handle_t c){assert(c);++cleaned;}
'''
code += defines(source, ['HTTP_TIMEOUT_MS']) + '\n'
code += source[source.index('typedef struct {'):source.index('static bool parse_coordinate')]
for name, body in [('gzip_body', compressed), ('extended', extended), ('large_header', large_header),
                   ('large_json', large_json), ('large_gzip', gzip.compress(large_json, mtime=0)),
                   ('bomb', gzip.compress(b'x'*9000, mtime=0)), ('empty_gzip', gzip.compress(b'', mtime=0))]:
    code += '\nstatic const uint8_t '+name+'[]={'+','.join(map(str,body))+'};\n'
code += r'''
static void emit(esp_http_client_handle_t c,int id,void *data,int len,const char *key,const char *value){
    esp_http_client_event_t e={.user_data=c->config.user_data,.event_id=id,.data_len=len,.data=data,
                              .header_key=(char *)key,.header_value=(char *)value};
    // Deliberately ignore callback errors, matching SDK dispatch behavior.
    (void)c->config.event_handler(&e);
}
static int esp_http_client_perform(esp_http_client_handle_t c){
    assert(headers==1);++performed;
    if(fail_perform)return ESP_FAIL;
    emit(c,HTTP_EVENT_HEADERS_SENT,NULL,0,NULL,NULL);
    if(redirect){
        emit(c,HTTP_EVENT_ON_HEADER,NULL,0,"Content-Encoding","gzip");
        emit(c,HTTP_EVENT_ON_DATA,(void *)gzip_body,15,NULL,NULL);
        emit(c,HTTP_EVENT_HEADERS_SENT,NULL,0,NULL,NULL);
    }
    if(encoding)emit(c,HTTP_EVENT_ON_HEADER,NULL,0,"cOnTeNt-EnCoDiNg",encoding);
    for(size_t offset=0;offset<wire_size;){
        size_t n=wire_size-offset;if(n>chunk)n=chunk;
        uint8_t *data=malloc(n);assert(data);memcpy(data,wire+offset,n);
        emit(c,HTTP_EVENT_ON_DATA,data,(int)n,NULL,NULL);
        offset+=n;
        response_context_t *ctx=c->config.user_data;
        if(offset<wire_size && ctx->length)partial_output=true;
        memset(data,0xa5,n);free(data); // SDK data expires on callback return
    }
    emit(c,HTTP_EVENT_ON_FINISH,NULL,0,NULL,NULL);
    return ESP_OK;
}
static void reset(void){
    assert(!allocations);
    created=cleaned=watchdogs=headers=performed=alloc_calls=fail_alloc=0;
    chunk=3;fail_init=fail_perform=fail_header=redirect=partial_output=false;
    complete=true;encoding=NULL;http_status=200;
    wire=(const uint8_t *)"{\"code\":\"200\"}";wire_size=strlen((const char *)wire);
}
static void check(const uint8_t *body,size_t size,unsigned n,bool expected,size_t capacity){
    reset();encoding="gzip";wire=body;wire_size=size;chunk=n;
    char out[64];assert(capacity<=sizeof(out));
    bool actual=http_get("url",out,capacity,NULL);
    if(actual!=expected)fprintf(stderr,"size=%zu chunk=%u cap=%zu expected=%d actual=%d\n",size,n,capacity,expected,actual);
    assert(actual==expected);
    assert(created==cleaned && !allocations);
    if(expected)assert(!strcmp(out,"{\"code\":\"200\"}"));else assert(!out[0]);
}
int main(void){
    char out[64];reset();assert(http_get("https://fixture.test",out,sizeof(out),NULL));
    assert(!strcmp(out,"{\"code\":\"200\"}") && created==cleaned && watchdogs>1);
    for(unsigned n=1;n<=wire_size+1;++n){reset();chunk=n;assert(http_get("url",out,sizeof(out),NULL));assert(created==cleaned);}
    reset();encoding="identity";assert(http_get("url",out,sizeof(out),NULL));
    reset();assert(!http_get("url",out,0,NULL) && !created);
    reset();fail_init=true;assert(!http_get("url",out,sizeof(out),NULL) && !created && !out[0]);
    reset();fail_header=true;assert(!http_get("url",out,sizeof(out),NULL) && headers==1 && !performed && created==cleaned);
    reset();fail_perform=true;assert(!http_get("url",out,sizeof(out),NULL) && created==cleaned);
    const int errors[]={301,400,401,403,404,429,500,503};
    for(unsigned i=0;i<sizeof(errors)/sizeof(*errors);++i){reset();http_status=errors[i];assert(!http_get("url",out,sizeof(out),NULL) && created==cleaned);}
    reset();wire_size=0;assert(!http_get("url",out,sizeof(out),NULL) && created==cleaned);
    reset();assert(!http_get("url",out,5,NULL) && !out[0] && created==cleaned);
    reset();assert(http_get("url",out,wire_size+1,NULL) && out[wire_size]==0);
    reset();complete=false;assert(!http_get("url",out,sizeof(out),NULL) && !out[0]);
    reset();encoding="br";assert(!http_get("url",out,sizeof(out),NULL) && !out[0]);
    for(unsigned n=1;n<=sizeof(gzip_body)+1;++n)check(gzip_body,sizeof(gzip_body),n,true,sizeof(out));
    for(unsigned n=1;n<=sizeof(extended)+1;++n)check(extended,sizeof(extended),n,true,sizeof(out));
    check(extended,sizeof(extended),1,true,sizeof(out));
    assert(partial_output); // output exists before final wire chunk arrives
    check(large_header,sizeof(large_header),1,true,sizeof(out));
    check(large_header,sizeof(large_header),1024,true,sizeof(out));
    check(gzip_body,sizeof(gzip_body),1,true,15); // exact JSON plus NUL, fragmented trailer
    check(gzip_body,sizeof(gzip_body),1,false,14);
    const unsigned large_chunks[]={1,7,1024,sizeof(large_gzip)};
    for(unsigned i=0;i<sizeof(large_chunks)/sizeof(*large_chunks);++i){
        reset();encoding="gzip";wire=large_gzip;wire_size=sizeof(large_gzip);chunk=large_chunks[i];
        char decoded[8192];assert(http_get("url",decoded,sizeof(decoded),NULL));
        assert(!memcmp(decoded,large_json,sizeof(large_json)) && decoded[sizeof(large_json)]==0);
        assert(!allocations && created==cleaned);
    }
    check(bomb,sizeof(bomb),1,false,sizeof(out));
    check(empty_gzip,sizeof(empty_gzip),1,false,sizeof(out));
    for(size_t n=0;n<sizeof(gzip_body);++n)check(gzip_body,n,1,false,sizeof(out));
    uint8_t corrupt[sizeof(extended)];
    const unsigned faults[]={2,3,10,11,sizeof(extended)-sizeof(gzip_body)+8,sizeof(extended)-8,sizeof(extended)-4};
    for(unsigned i=0;i<sizeof(faults)/sizeof(*faults);++i){
        memcpy(corrupt,extended,sizeof(extended));corrupt[faults[i]]^=0x80;
        check(corrupt,sizeof(corrupt),1,false,sizeof(out));
    }
    uint8_t trailing[2*sizeof(gzip_body)];memcpy(trailing,gzip_body,sizeof(gzip_body));
    memcpy(trailing+sizeof(gzip_body),gzip_body,sizeof(gzip_body));
    check(trailing,sizeof(trailing),1,false,sizeof(out));
    check(trailing,sizeof(trailing),sizeof(trailing),false,sizeof(out));
    for(unsigned fault=1;fault<=2;++fault){
        reset();wire=gzip_body;wire_size=sizeof(gzip_body);encoding="gzip";chunk=1;fail_alloc=fault;
        assert(!http_get("url",out,sizeof(out),NULL) && !out[0] && !allocations && created==cleaned);
    }
    reset();wire=gzip_body;wire_size=sizeof(gzip_body);encoding="GZIP";redirect=true;
    assert(http_get("url",out,sizeof(out),NULL) && !strcmp(out,"{\"code\":\"200\"}") && !allocations);
    char buffer[4]="x";response_context_t ctx={.buffer=buffer,.capacity=sizeof(buffer),.length=1};
    esp_http_client_event_t e={.user_data=&ctx,.event_id=HTTP_EVENT_ON_DATA,.data_len=-1};
    assert(http_collect_event(&e)==ESP_OK && ctx.length==1 && !strcmp(buffer,"x"));
    e.user_data=NULL;e.data_len=1;assert(http_collect_event(&e)==ESP_OK);
    puts("Weather HTTP: incremental gzip, all chunk boundaries, optional headers/FHCRC, CRC/ISIZE, truncation, output limit, sticky errors, allocation cleanup, redirects and plain JSON: PASS");
}
'''
zlib_dir = REPO/'firmware/managed_components/espressif__zlib/zlib'
# Prefer the locked board library; keep desktop-only tests runnable with host zlib.
sources = ['adler32.c', 'crc32.c', 'inffast.c', 'inflate.c', 'inftrees.c', 'zutil.c']
libraries = (['-I'+str(zlib_dir), *[str(zlib_dir/name) for name in sources]]
             if (zlib_dir/'zlib.h').is_file() else ['-lz'])
run(code, sys.argv[1] if len(sys.argv)>1 else 'cc', libraries=libraries)
