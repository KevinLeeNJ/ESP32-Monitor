"""Test the production HTTP adapter using a bounded fake HTTP client."""
import pathlib
import subprocess
import sys
import tempfile

root = pathlib.Path(__file__).resolve().parents[2] / 'firmware'
with tempfile.TemporaryDirectory() as tmp:
    p = pathlib.Path(tmp)
    (p/'esp_err.h').write_text('''#pragma once
typedef int esp_err_t;
#define ESP_OK 0
#define ESP_FAIL -1
#define ESP_ERR_INVALID_ARG 1
#define ESP_ERR_NO_MEM 2
''')
    (p/'esp_http_client.h').write_text('''#pragma once
#include "esp_err.h"
#include <stdbool.h>
typedef void *esp_http_client_handle_t;
typedef struct { int event_id; void *user_data; void *data; int data_len; } esp_http_client_event_t;
enum { HTTP_EVENT_ON_DATA=1, HTTP_METHOD_POST=2 };
typedef struct { const char *url; int timeout_ms; bool disable_auto_redirect; esp_err_t (*event_handler)(esp_http_client_event_t *); void *user_data; } esp_http_client_config_t;
esp_http_client_handle_t esp_http_client_init(const esp_http_client_config_t *);
esp_err_t esp_http_client_set_method(esp_http_client_handle_t,int);
esp_err_t esp_http_client_set_header(esp_http_client_handle_t,const char *,const char *);
esp_err_t esp_http_client_set_post_field(esp_http_client_handle_t,const char *,int);
esp_err_t esp_http_client_perform(esp_http_client_handle_t);
int esp_http_client_get_status_code(esp_http_client_handle_t);
esp_err_t esp_http_client_cleanup(esp_http_client_handle_t);
''')
    (p/'test.c').write_text(r'''
#include "monitor_pairing_http.h"
#include "esp_http_client.h"
#include <assert.h>
#include <string.h>
static esp_http_client_config_t cfg; static int status=201, mode, calls;
static unsigned char vector[] = {0x08,0x02,0x6a,0x2f,0x08,0x02,0x12,0x04,0x70,0x61,0x69,0x72,0x1a,0x07,0x70,0x65,0x6e,0x64,0x69,0x6e,0x67,0x22,0x04,0x68,0x6f,0x73,0x74,0x2a,0x03,0x4d,0x61,0x63,0x32,0x05,0x6e,0x6f,0x6e,0x63,0x65,0x3a,0x06,0x74,0x69,0x63,0x6b,0x65,0x74,0x40,0x80,0xd3,0x0e};
void *esp_http_client_init(const esp_http_client_config_t *c) { assert(!strcmp(c->url,"http://192.168.1.4:18080/companion/v1/pair")); assert(c->timeout_ms==4000 && c->disable_auto_redirect); cfg=*c; calls++; return &cfg; }
int esp_http_client_set_method(void *h,int m) { (void)h; assert(m==HTTP_METHOD_POST); return 0; }
int esp_http_client_set_header(void *h,const char *k,const char *v) { (void)h;(void)k;assert(!strcmp(v,"application/x-protobuf"));return 0; }
int esp_http_client_set_post_field(void *h,const char *p,int n) { (void)h;assert(n>0 && p[0]==8);return 0; }
int esp_http_client_perform(void *h) { (void)h; static char oversized[4097]; esp_http_client_event_t e={.event_id=HTTP_EVENT_ON_DATA,.user_data=cfg.user_data}; if(mode==1) { e.data=oversized;e.data_len=sizeof oversized; return cfg.event_handler(&e); } e.data=vector;e.data_len=10; assert(!cfg.event_handler(&e)); e.data=vector+10;e.data_len=sizeof(vector)-10; return cfg.event_handler(&e); }
int esp_http_client_get_status_code(void *h) { (void)h;return status; }
int esp_http_client_cleanup(void *h) { (void)h;return 0; }
int main(void) { esp32monitor_companion_PairRequest req=esp32monitor_companion_PairRequest_init_zero; req.protocol_version=1; strcpy(req.step,"begin");strcpy(req.request_id,"id"); esp32monitor_companion_PairResponse out=esp32monitor_companion_PairResponse_init_zero;
 assert(!monitor_pairing_http_exchange("192.168.1.4",18080,&req,&out)); assert(!strcmp(out.state,"pending"));
 status=302; assert(monitor_pairing_http_exchange("192.168.1.4",18080,&req,&out)==ESP_FAIL); assert(!strcmp(out.state,"pending"));
 status=200;mode=1;assert(monitor_pairing_http_exchange("192.168.1.4",18080,&req,&out)==ESP_FAIL);
 assert(monitor_pairing_http_exchange("example.com",18080,&req,&out)==ESP_ERR_INVALID_ARG);assert(calls==3); return 0; }
''')
    cmd=[sys.argv[1],'-std=c11','-Wall','-Wextra','-Werror','-I'+str(p),'-I'+str(root/'components/firmware'),'-I'+str(root/'src/companion'),'-I'+str(root/'third_party/nanopb'),str(p/'test.c'),str(root/'components/firmware/monitor_pairing_http.c'),str(root/'src/companion/monitor_pairing_wire.c'),str(root/'src/companion/companion.pb.c'),*[str(root/'third_party/nanopb'/x) for x in ('pb_common.c','pb_encode.c','pb_decode.c')],'-o',str(p/'test')]
    subprocess.run(cmd,check=True)
    subprocess.run([str(p/'test')],check=True)
