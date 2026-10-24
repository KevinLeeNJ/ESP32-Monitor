"""Real HTTP collection/decode/cleanup paths; zlib stands in for ROM tinfl."""
import gzip
import re
import sys
from host_harness import REPO, defines, function, run
source=(REPO/'firmware/components/firmware/monitor_weather.c').read_text()
fixture=gzip.compress(b'{"code":"200"}',mtime=0)
code=r'''
#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <zlib.h>
#define ESP_OK 0
#define ESP_FAIL -1
#define ESP_LOGW(...) ((void)0)
#define TAG "fixture"
#define HTTP_EVENT_ON_DATA 1
typedef int esp_err_t;
typedef struct {void *user_data;int event_id,data_len;void *data;} esp_http_client_event_t;
typedef struct {const char *url;int timeout_ms;bool disable_auto_redirect;esp_err_t (*event_handler)(esp_http_client_event_t *);void *user_data;void (*crt_bundle_attach)(void);} esp_http_client_config_t;
typedef struct {esp_http_client_config_t config;} *esp_http_client_handle_t;
static struct {esp_http_client_config_t config;} client;
static unsigned created,cleaned,watchdogs,allocations,alloc_calls,fail_alloc;
static unsigned chunk=3;
static bool fail_init,fail_perform;
static int http_status=200;
static const uint8_t *wire;static size_t wire_size;
static void esp_crt_bundle_attach(void){}
static void feed_task_watchdog(void){++watchdogs;}
static esp_http_client_handle_t esp_http_client_init(const esp_http_client_config_t *cfg){
    assert(cfg->timeout_ms==4000 && cfg->crt_bundle_attach && cfg->event_handler);
    if(fail_init)return NULL;
    ++created;client.config=*cfg;return (esp_http_client_handle_t)&client;
}
static int esp_http_client_perform(esp_http_client_handle_t c){
    if(fail_perform)return ESP_FAIL;
    for(size_t offset=0;offset<wire_size;){
        size_t n=wire_size-offset;if(n>chunk)n=chunk;
        esp_http_client_event_t event={.user_data=c->config.user_data,.event_id=HTTP_EVENT_ON_DATA,.data_len=n,.data=(void *)(wire+offset)};
        if(c->config.event_handler(&event)!=ESP_OK)return ESP_FAIL;
        offset+=n;
    }
    return ESP_OK;
}
static int esp_http_client_get_status_code(esp_http_client_handle_t c){assert(c);return http_status;}
static void esp_http_client_cleanup(esp_http_client_handle_t c){assert(c);++cleaned;}
static void *test_malloc(size_t size){++alloc_calls;if(alloc_calls==fail_alloc)return NULL;void *p=malloc(size);if(p)++allocations;return p;}
static void test_free(void *p){if(p){assert(allocations);--allocations;}free(p);}
#define malloc test_malloc
#define free test_free
#define TINFL_FLAG_USING_NON_WRAPPING_OUTPUT_BUF 1
#define TINFL_STATUS_DONE 0
typedef int tinfl_status;
typedef uint8_t mz_uint8;
typedef struct {int unused;} tinfl_decompressor;
#define tinfl_init(p) ((void)(p))
static tinfl_status tinfl_decompress(tinfl_decompressor *d,const uint8_t *input,size_t *in_size,uint8_t *start,uint8_t *out,size_t *out_size,int flags){
    (void)d;assert(start==out && flags==TINFL_FLAG_USING_NON_WRAPPING_OUTPUT_BUF);
    z_stream stream={0};assert(inflateInit2(&stream,-MAX_WBITS)==Z_OK);
    stream.next_in=(Bytef *)input;stream.avail_in=*in_size;stream.next_out=out;stream.avail_out=*out_size;
    int status=inflate(&stream,Z_FINISH);*in_size=stream.total_in;*out_size=stream.total_out;inflateEnd(&stream);
    return status==Z_STREAM_END?TINFL_STATUS_DONE:-1;
}
'''
code+=defines(source,['HTTP_TIMEOUT_MS'])+'\n'
code+=re.search(r'typedef struct \{ char \*buffer;.*?response_context_t;',source)[0]+'\n'
code+='\n'.join(function(source,name) for name in ['http_collect_event','http_get'])
code+='\nstatic const uint8_t gzip_body[]={'+','.join(str(x) for x in fixture)+'};\n'
code+=r'''
static void reset(void){created=cleaned=watchdogs=allocations=alloc_calls=fail_alloc=0;chunk=3;fail_init=fail_perform=false;http_status=200;wire=(const uint8_t *)"{\"code\":\"200\"}";wire_size=strlen((const char *)wire);}
int main(void){
    char out[64];reset();assert(http_get("https://fixture.test",out,sizeof(out),NULL));
    assert(!strcmp(out,"{\"code\":\"200\"}") && created==cleaned && watchdogs>1);
    for(unsigned n=1;n<=wire_size+1;++n){reset();chunk=n;assert(http_get("url",out,sizeof(out),NULL));assert(created==cleaned);}
    reset();assert(!http_get("url",out,0,NULL) && !created);
    reset();fail_init=true;assert(!http_get("url",out,sizeof(out),NULL) && !created && !out[0]);
    reset();fail_perform=true;assert(!http_get("url",out,sizeof(out),NULL) && created==cleaned);
    const int errors[]={301,400,401,403,404,429,500,503};
    for(unsigned i=0;i<sizeof(errors)/sizeof(*errors);++i){reset();http_status=errors[i];assert(!http_get("url",out,sizeof(out),NULL) && created==cleaned);}
    reset();wire_size=0;assert(!http_get("url",out,sizeof(out),NULL) && created==cleaned);
    reset();assert(!http_get("url",out,5,NULL) && created==cleaned); // fragmented body overflow
    reset();assert(http_get("url",out,wire_size+1,NULL) && out[wire_size]==0);
    reset();wire=gzip_body;wire_size=sizeof(gzip_body);assert(http_get("url",out,sizeof(out),NULL));
    assert(!strcmp(out,"{\"code\":\"200\"}") && !allocations && created==cleaned);
    for(unsigned fault=1;fault<=2;++fault){reset();wire=gzip_body;wire_size=sizeof(gzip_body);fail_alloc=fault;assert(!http_get("url",out,sizeof(out),NULL));assert(!allocations && created==cleaned);}
    uint8_t corrupt[sizeof(gzip_body)];memcpy(corrupt,gzip_body,sizeof(corrupt));memset(corrupt+10,0xff,sizeof(corrupt)-18);
    reset();wire=corrupt;wire_size=sizeof(corrupt);assert(!http_get("url",out,sizeof(out),NULL) && !allocations && created==cleaned);
    reset();wire=gzip_body;wire_size=sizeof(gzip_body);assert(!http_get("url",out,12,NULL) && !allocations && created==cleaned);
    // Empty/negative-length/unrelated events must not mutate a valid collection buffer.
    char buffer[4]="x";response_context_t ctx={.buffer=buffer,.capacity=sizeof(buffer),.length=1};
    esp_http_client_event_t e={.user_data=&ctx,.event_id=HTTP_EVENT_ON_DATA,.data_len=-1};
    assert(http_collect_event(&e)==ESP_OK && ctx.length==1 && !strcmp(buffer,"x"));
    e.user_data=NULL;e.data_len=1;assert(http_collect_event(&e)==ESP_OK);
    puts("Weather HTTP: 4s SDK timeout, all chunk sizes, exact buffer/overflow, HTTP/auth/rate-limit/server errors, empty body, gzip success/failure/allocations and client cleanup: PASS");
}
'''
run(code,sys.argv[1] if len(sys.argv)>1 else 'cc',libraries=['-lz'])
