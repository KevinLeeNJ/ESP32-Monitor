"""Run the actual Devices worker loop with virtual time, NVS and request outcomes."""
import sys
from host_harness import REPO, defines, function, run
source=(REPO/'firmware/components/firmware/monitor_devices.c').read_text()
state=source[source.index('typedef struct { char url[193]'):source.index('typedef struct {\n    int64_t deadline;')]
code=r'''
#include <assert.h>
#include <setjmp.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include "monitor_devices.h"
#include "monitor_display_refresh.h"
#define ESP_ERR_INVALID_STATE 0x103
#define ESP_LOGI(...) ((void)0)
#define ESP_LOGW(...) ((void)0)
#define portMUX_INITIALIZER_UNLOCKED 0
#define portENTER_CRITICAL(p) ((void)(p))
#define portEXIT_CRITICAL(p) ((void)(p))
#define pdMS_TO_TICKS(ms) (ms)
#define pdTRUE 1
#define NVS_READONLY 0
typedef int TaskHandle_t,portMUX_TYPE,nvs_handle_t;
typedef uint32_t TickType_t;
typedef struct { int64_t due;bool active; } *esp_timer_handle_t;
static struct {int64_t due;bool active;} expiry_timer;
static int64_t now,finish,disconnect_at,reconnect_at,config_at;
static bool clock_valid=true,notify_clock=true,notified,nvs_ok=true;
static unsigned calls,resets,notices,feeds,scenario;
static uint32_t last_reason;
static int last_status;
static const char *stored_url="http://fixture.test",*stored_token="0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";
static jmp_buf ended;
static void expire_snapshot(void *);
static int64_t esp_timer_get_time(void){return now;}
static bool monitor_clock_is_valid(void){return clock_valid;}
static int esp_task_wdt_add(void *p){(void)p;return ESP_OK;}
static void feed_task_watchdog(void){++feeds;}
static void xTaskNotifyGive(int task){assert(task);notified=true;}
static int esp_timer_stop(esp_timer_handle_t timer){timer->active=false;return ESP_OK;}
static int esp_timer_start_once(esp_timer_handle_t timer,int64_t delay){timer->active=true;timer->due=now+delay;return ESP_OK;}
void monitor_display_refresh_request_reason(uint32_t bits){++notices;last_reason=bits;}
static int nvs_open(const char *name,int mode,int *out){(void)name;(void)mode;if(!nvs_ok)return ESP_FAIL;*out=1;return ESP_OK;}
static int nvs_get_str(int handle,const char *key,char *out,size_t *size){
    (void)handle;const char *s=!strcmp(key,"devices_api_url")?stored_url:stored_token;
    if(!s || strlen(s)+1>*size)return ESP_FAIL;
    strcpy(out,s);*size=strlen(s)+1;return ESP_OK;
}
static void nvs_close(int handle){(void)handle;}
static unsigned ulTaskNotifyTake(int clear,unsigned wait);
'''
code+=defines(source,['PERIOD_US','RETAIN_US'])+'\n'
code+='#include \"'+str(REPO/'firmware/src/display/monitor_devices_data.c')+'\"\n'
code+='static const char *TAG="test";\n' if False else ''
code+= 'static TaskHandle_t s_task=1;\nstatic esp_timer_handle_t s_expiry_timer;\nstatic portMUX_TYPE s_mux;\n'+state+'\n'
code+=r'''
typedef struct {struct {int socket;} guard;} devices_transport_t;
static void transport_reset(devices_transport_t *t){(void)t;++resets;}
static bool fetch(devices_transport_t *transport,const devices_config_t *cfg,monitor_devices_snapshot_t *out,int *status,bool *reused){
    (void)transport;assert(cfg->url[0] && cfg->token[0]);*reused=false;++calls;
    // Each scenario uses the real worker decision logic with a bounded transport result.
    bool ok=scenario==0 || (scenario==1 && calls==1) || (scenario==2 && (calls==1 || calls>=3)) || ((scenario==7 || scenario==8) && calls==1);
    *status=ok?200:(scenario==3 || scenario==8)?401:scenario==4?403:503;
    if(ok){memset(out,0,sizeof(*out));out->status=MONITOR_DEVICES_READY;out->count=1;strcpy(out->devices[0].name,"NAS");}
    if(scenario==5){monitor_devices_set_wifi_available(false);return true;} // response races disconnect
    if(scenario==6){stored_url="http://changed.test";monitor_devices_config_changed();scenario=0;return true;}
    if(scenario==7 && calls>1){now+=RETAIN_US;expire_snapshot(NULL);return false;} // expiry while blocked
    return ok;
}
'''
code+=function(source,'devices_task')+'\n'
code+=r'''
static unsigned ulTaskNotifyTake(int clear,unsigned wait){
    (void)clear;
    if(!wait){unsigned result=notified;notified=false;return result;}
    assert(wait>=1 && wait<=1000); // never a tight loop, and service watchdog every second
    int64_t target=now+wait*1000LL;
    int64_t periodic=(now/PERIOD_US+1)*PERIOD_US;
    if(clock_valid && notify_clock && periodic<target)target=periodic;
    if(disconnect_at>now && disconnect_at<target)target=disconnect_at;
    if(reconnect_at>now && reconnect_at<target)target=reconnect_at;
    if(config_at>now && config_at<target)target=config_at;
    if(expiry_timer.active && expiry_timer.due>now && expiry_timer.due<target)target=expiry_timer.due;
    now=target;
    if(now>=finish){last_status=s_snapshot.status;longjmp(ended,1);}
    if(now==disconnect_at)monitor_devices_set_wifi_available(false);
    if(now==reconnect_at)monitor_devices_set_wifi_available(true);
    if(now==config_at){stored_url="";monitor_devices_config_changed();}
    if(expiry_timer.active && now>=expiry_timer.due){expiry_timer.active=false;expire_snapshot(NULL);}
    if(clock_valid && notify_clock && now%PERIOD_US==0)monitor_devices_request_refresh();
    unsigned result=notified;notified=false;return result;
}
static void reset(void){
    now=0;finish=95000000;disconnect_at=reconnect_at=config_at=-1;
    calls=resets=notices=feeds=scenario=0;last_reason=0;notified=false;
    clock_valid=notify_clock=nvs_ok=true;
    memset(&s_config,0,sizeof(s_config));s_snapshot=(monitor_devices_snapshot_t){0};s_generation=0;s_wifi=false;s_have_snapshot=false;s_last_success=0;
    expiry_timer.active=false;s_expiry_timer=(esp_timer_handle_t)&expiry_timer;
    stored_url="http://fixture.test";stored_token="0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";
    monitor_devices_config_changed();monitor_devices_set_wifi_available(true);
}
static void work(void){if(!setjmp(ended))devices_task(NULL);assert(feeds>0);}
int main(void){
    reset();work();assert(calls==4 && last_status==MONITOR_DEVICES_READY && s_have_snapshot);
    assert(s_snapshot.version==2); // WAITING then first READY; identical polls renew lease silently
    reset();clock_valid=false;work();assert(calls==4); // HTTP fallback still polls at fixed 30s
    reset();stored_url="https://fixture.test";monitor_devices_config_changed();clock_valid=false;work();assert(calls==0);
    reset();monitor_devices_set_wifi_available(false);work();assert(calls==0);
    for(unsigned invalid=0;invalid<5;++invalid){
        reset();
        if(invalid==0)nvs_ok=false;
        if(invalid==1)stored_url="";
        if(invalid==2)stored_token=NULL;
        if(invalid==3)stored_token="invalid";
        if(invalid==4)stored_url="https://user@fixture.test";
        monitor_devices_config_changed();work();assert(calls==0 && last_status==MONITOR_DEVICES_NOT_CONFIGURED);
    }
    reset();scenario=1;finish=89999999;work();assert(s_have_snapshot && last_status==MONITOR_DEVICES_STALE && s_snapshot.count==1);
    reset();scenario=1;finish=90000001;work();assert(!s_have_snapshot && last_status==MONITOR_DEVICES_UNAVAILABLE && !s_snapshot.count);
    reset();scenario=2;work();assert(calls==4 && s_have_snapshot && last_status==MONITOR_DEVICES_READY);
    for(unsigned auth=3;auth<=4;++auth){reset();scenario=auth;work();assert(calls==4 && last_status==MONITOR_DEVICES_AUTH_ERROR);}
    reset();scenario=5;finish=10000000;work();assert(calls==1 && !s_have_snapshot && !s_snapshot.count);
    reset();scenario=6;finish=10000000;work();assert(calls==2 && s_have_snapshot && !strcmp(s_config.url,"http://changed.test"));
    reset();scenario=8;finish=89999999;work();assert(s_have_snapshot && s_snapshot.count==1 && last_status==MONITOR_DEVICES_AUTH_ERROR);
    reset();scenario=8;work();assert(!s_have_snapshot && !s_snapshot.count);
    reset();scenario=7;finish=200000000;work();assert(!s_have_snapshot && !s_snapshot.count);
    reset();disconnect_at=15000000;reconnect_at=45000000;work();assert(calls==4 && s_have_snapshot);
    reset();config_at=15000000;work();assert(calls==1 && !s_have_snapshot && last_status==MONITOR_DEVICES_NOT_CONFIGURED);
    reset();monitor_devices_request_refresh();uint32_t gen=s_generation;monitor_devices_set_wifi_available(true);assert(s_generation==gen);
    monitor_devices_snapshot_t copy;monitor_devices_get(NULL);monitor_devices_get(&copy);assert(copy.status==MONITOR_DEVICES_WAITING);
    puts("Devices worker: missing/invalid config, offline/HTTPS clock gate, fixed polling/no spin, first/unchanged data, 90s stale expiry, auth failure, recovery, disconnect/config races and configuration removal: PASS");
}
'''
run(code,sys.argv[1] if len(sys.argv)>1 else 'cc',cjson=sys.argv[2] if len(sys.argv)>2 else REPO/'firmware/managed_components/espressif__cjson/cJSON')
