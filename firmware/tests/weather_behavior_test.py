"""Real location/configuration/JSON update code with bounded HTTP and NVS fakes."""
import sys
from host_harness import REPO, defines, function, run
source=(REPO/'firmware/components/firmware/monitor_weather.c').read_text(encoding='utf-8')
code=r'''
#include <assert.h>
#include <stdarg.h>
#include <setjmp.h>
#include <math.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include "monitor_weather.h"
#include "monitor_display_refresh.h"
#include "cJSON.h"
#define TAG "weather"
static void test_log(const char *tag,const char *fmt,...){(void)tag;(void)fmt;}
#define ESP_LOGW(...) test_log(__VA_ARGS__)
#define ESP_LOGI(...) test_log(__VA_ARGS__)
#define ESP_LOGE(...) test_log(__VA_ARGS__)
#define NVS_READONLY 0
#define portENTER_CRITICAL(p) ((void)(p))
#define portEXIT_CRITICAL(p) ((void)(p))
typedef int nvs_handle_t;
static const char *lat_text="31.2",*lon_text="121.5",*host="fixture.test",*key="fixture-key";
static bool nvs_ok=true, alloc_ok=true;
static unsigned opened,closed,notifications,http_calls[3];
static uint32_t reason;
static int s_data_mux;
#define BIT0 1
#define BIT1 2
#define pdMS_TO_TICKS(ms) (ms)
#define pdPASS 1
#define portMAX_DELAY UINT32_MAX
#define ESP_ERROR_CHECK(c) assert((c)==ESP_OK)
#define eSetBits 0
static void *s_task;
static bool s_wifi_available,clock_ready=true,task_ok=true;
static bool s_companion_active;
static unsigned worker_step,task_creates,queued;
static jmp_buf worker_done;
static bool monitor_clock_is_valid(void){return clock_ready;}
static bool watchdog_active;
static int esp_task_wdt_add(void *p){(void)p;assert(!watchdog_active);watchdog_active=true;return ESP_OK;}
static int esp_task_wdt_delete(void *p){(void)p;assert(watchdog_active);watchdog_active=false;return ESP_OK;}
static int xTaskCreate(void (*fn)(void *),const char *name,unsigned stack,void *arg,unsigned priority,void **out){
    (void)fn;(void)name;(void)arg;(void)priority;assert(stack==8192);
    ++task_creates;if(task_ok)*out=(void *)1;return task_ok?pdPASS:0;
}
static void xTaskNotify(void *task,unsigned bits,int action){assert(task && action==0);queued|=bits;}
static void xTaskNotifyWait(unsigned a,unsigned b,uint32_t *bits,unsigned timeout){
    (void)a;(void)b;assert(timeout==portMAX_DELAY && !watchdog_active);
    if(worker_step==0){s_wifi_available=false;clock_ready=true;*bits=1;}
    if(worker_step==1){s_wifi_available=true;clock_ready=false;*bits=1;}
    if(worker_step==2){s_wifi_available=true;clock_ready=true;*bits=2;}
    if(worker_step==3){*bits=0;}
    if(worker_step==4){*bits=1;}
    if(worker_step==5){s_wifi_available=false;*bits=1;}
    if(worker_step==6){s_wifi_available=true;*bits=1;}
    if(worker_step==7)longjmp(worker_done,1);
    ++worker_step;
}
static monitor_weather_data_t s_data;
static int64_t s_last_weather_slot = -1;
static const char *ip_body="{\"success\":true,\"latitude\":31.2,\"longitude\":121.5}";
static const char *weather_body="{\"code\":\"200\",\"now\":{\"icon\":\"100\"}}";
static const char *aqi_body="{\"indexes\":[{\"aqiDisplay\":\"42\"}]}";
static unsigned ip_fail,delays[8],delay_count;
static bool companion_flip;
static void vTaskDelay(unsigned ticks){assert(delay_count<8);delays[delay_count++]=ticks;}
static void *test_malloc(size_t size){return alloc_ok?malloc(size):NULL;}
#define malloc test_malloc
#define MALLOC_CAP_SPIRAM 1u
#define MALLOC_CAP_INTERNAL 2u
#define MALLOC_CAP_8BIT 4u
static void *heap_caps_malloc_prefer(size_t size,size_t count,...) {
    va_list caps;va_start(caps,count);
    assert(count==2 && va_arg(caps,unsigned)==(MALLOC_CAP_SPIRAM|MALLOC_CAP_8BIT));
    assert(va_arg(caps,unsigned)==(MALLOC_CAP_INTERNAL|MALLOC_CAP_8BIT));
    va_end(caps);return test_malloc(size);
}
static unsigned esp_get_free_heap_size(void){return 100000;}
static void feed_task_watchdog(void) {}
static void log_dns_servers(void) {}
static int nvs_open(const char *ns,int mode,nvs_handle_t *out){assert(!strcmp(ns,"monitor_loc") && mode==0);if(!nvs_ok)return ESP_FAIL;*out=1;++opened;return ESP_OK;}
static int nvs_get_str(int handle,const char *name,char *out,size_t *size){
    assert(handle==1);
    const char *s=!strcmp(name,"latitude")?lat_text:!strcmp(name,"longitude")?lon_text:!strcmp(name,"qweather_host")?host:key;
    if(!s || strlen(s)+1>*size)return ESP_FAIL;
    strcpy(out,s);*size=strlen(s)+1;return ESP_OK;
}
static void nvs_close(int handle){assert(handle==1);++closed;}
void monitor_display_refresh_request_reason(uint32_t bits){++notifications;reason=bits;}
static bool http_get(const char *url,char *out,size_t size,const char *api_key){
    if(worker_step)assert(watchdog_active);
    const char *body;
    if(strstr(url,"ipwho.is")){++http_calls[0];assert(!api_key);
        if(companion_flip){s_companion_active=true;companion_flip=false;}
        if(ip_fail){--ip_fail;return false;}body=ip_body;}
    else {assert(api_key && !strcmp(api_key,"fixture-key"));
        if (!strncmp(host,"http://",7) || !strncmp(host,"https://",8)) assert(!strncmp(url,host,strlen(host)));
        else assert(!strncmp(url,"https://fixture.test/",21));
        if(strstr(url,"/v7/weather/")){++http_calls[1];assert(strstr(url,"location=121.50000,31.20000"));body=weather_body;}
        else {++http_calls[2];assert(strstr(url,"/current/31.20000/121.50000"));body=aqi_body;}
    }
    if(!body)return false;
    assert(strlen(body)<size);strcpy(out,body);return true;
}
'''
code+=defines(source,['RESPONSE_MAX','WEATHER_EVT_UPDATE','WEATHER_EVT_WIFI_CHANGED','LOCATION_ATTEMPTS','LOCATION_RETRY_BASE_MS'])+'\n'
code+='\n'.join(function(source,name) for name in ['parse_coordinate','json_nonnegative_int','read_configured_location','locate_by_public_ip','locate_with_retry','condition_from_qweather','read_qweather_config','update_weather','monitor_weather_get','monitor_weather_refresh_delay_ms','weather_task','monitor_weather_request_refresh','monitor_weather_set_companion_active','monitor_weather_on_wifi_connected','monitor_weather_on_wifi_disconnected','monitor_weather_on_time_valid','monitor_weather_start'])
code+=r'''
static void reset(void){
    s_data=(monitor_weather_data_t){.aqi=-1};s_last_weather_slot=-1;clock_ready=true;
    notifications=opened=closed=reason=0;memset(http_calls,0,sizeof(http_calls));
    ip_fail=delay_count=0;s_wifi_available=false;companion_flip=false;worker_step=0;
    lat_text="31.2";lon_text="121.5";host="fixture.test";key="fixture-key";nvs_ok=alloc_ok=true;
    ip_body="{\"success\":true,\"latitude\":31.2,\"longitude\":121.5}";
    weather_body="{\"code\":\"200\",\"now\":{\"icon\":\"100\"}}";
    aqi_body="{\"indexes\":[{\"aqiDisplay\":\"42\"}]}";
}
int main(void){
    reset();update_weather();
    assert(s_data.weather_valid && s_data.aqi_valid && s_data.location_valid && s_data.aqi==42 && !http_calls[0]);
    assert(notifications==1 && reason==MONITOR_REFRESH_RECOVER && opened==closed);
    reset();host="http://192.168.1.2:8080";update_weather();assert(s_data.weather_valid && s_data.aqi_valid);
    reset();host="https://fixture.test";update_weather();assert(s_data.weather_valid && s_data.aqi_valid);
    update_weather();assert(notifications==1); // unchanged success is silent
    monitor_weather_data_t before=s_data;
    weather_body=aqi_body=NULL;update_weather();
    assert(!memcmp(&before,&s_data,sizeof(before)) && notifications==1);
    weather_body=aqi_body="\x1f\x8b\x08";update_weather();
    assert(!memcmp(&before,&s_data,sizeof(before)) && notifications==1);
    // Independent sources: one failed endpoint must not destroy the other cache.
    aqi_body="{\"indexes\":[{\"aqi\":100}]}";update_weather();
    assert(s_data.weather_valid && s_data.aqi==100 && reason==MONITOR_REFRESH_WEATHER);
    weather_body="{";aqi_body="{}";before=s_data;update_weather();assert(!memcmp(&before,&s_data,sizeof(before)));
    reset();lat_text=lon_text="";update_weather();assert(http_calls[0]==1 && s_data.location_valid);
    update_weather();assert(http_calls[0]==1); // cached location survives IP service loss
    reset();lat_text=lon_text="";ip_body=NULL;update_weather();assert(!s_data.weather_valid && !http_calls[1]);
    const char *invalid_ip[]={"{}","{","{\"success\":false,\"latitude\":1,\"longitude\":2}","{\"success\":true,\"latitude\":91,\"longitude\":0}","{\"success\":true,\"latitude\":0,\"longitude\":181}","{\"success\":true,\"latitude\":\"31\",\"longitude\":121}"};
    for(unsigned i=0;i<sizeof(invalid_ip)/sizeof(*invalid_ip);++i){
        reset();lat_text=lon_text="";ip_body=invalid_ip[i];float a=7,b=8;char city[32]="keep";
        assert(!locate_by_public_ip(&a,&b,city,sizeof(city)) && a==7 && b==8 && !strcmp(city,"keep"));
    }
    const char *invalid_coord[]={"bad","31junk","nan","inf","91","","1e999"};
    for(unsigned i=0;i<sizeof(invalid_coord)/sizeof(*invalid_coord);++i){
        reset();lat_text=invalid_coord[i];float a,b;assert(!read_configured_location(&a,&b));
    }
    const char *invalid_lon[]={"181","-181","121junk","nan",""};
    for(unsigned i=0;i<sizeof(invalid_lon)/sizeof(*invalid_lon);++i){reset();lon_text=invalid_lon[i];float a,b;assert(!read_configured_location(&a,&b));}
    const char *bad_numbers[]={"-1","2147483648","1.5","1e999","true","null","\"999999999999999999999\"","\"42junk\"","\"\""};
    for(unsigned i=0;i<sizeof(bad_numbers)/sizeof(*bad_numbers);++i){
        cJSON *item=cJSON_Parse(bad_numbers[i]);int value=7;
        assert(!json_nonnegative_int(item,&value) && value==7);cJSON_Delete(item);
    }
    cJSON *limit=cJSON_Parse("2147483647");int value;
    assert(json_nonnegative_int(limit,&value) && value==INT_MAX);cJSON_Delete(limit);
    reset();lat_text="-90";lon_text="180";float a,b;assert(read_configured_location(&a,&b) && a==-90 && b==180);
    reset();nvs_ok=false;assert(!read_configured_location(&a,&b));
    char h[192],k[128];assert(!read_qweather_config(h,sizeof(h),k,sizeof(k)) && !h[0] && !k[0]);
    reset();host="";assert(!read_qweather_config(h,sizeof(h),k,sizeof(k)));
    lat_text=lon_text="";update_weather();assert(!http_calls[0] && !http_calls[1] && !http_calls[2]);
    reset();key=NULL;assert(!read_qweather_config(h,sizeof(h),k,sizeof(k)));
    reset();alloc_ok=false;update_weather();assert(!s_data.weather_valid && !http_calls[1]);
    const char *invalid_weather[]={"{}","{","{\"code\":\"401\",\"now\":{\"icon\":\"100\"}}","{\"code\":\"200\",\"now\":{\"icon\":\"100junk\"}}","{\"code\":\"200\",\"now\":{\"icon\":\"\"}}","{\"code\":\"200\",\"now\":{\"icon\":100}}"};
    for(unsigned i=0;i<sizeof(invalid_weather)/sizeof(*invalid_weather);++i){
        reset();weather_body=invalid_weather[i];aqi_body=NULL;update_weather();assert(!s_data.weather_valid);
    }
    const char *invalid_aqi[]={"{}","{","{\"indexes\":[]}","{\"indexes\":[{\"aqiDisplay\":\"oops\"}]}","{\"indexes\":[{\"aqiDisplay\":\"-1\"}]}","{\"indexes\":[{\"aqi\":1.5}]}","{\"indexes\":[{\"aqi\":1e999}]}","{\"indexes\":[{\"aqiDisplay\":\"42junk\"}]}"};
    for(unsigned i=0;i<sizeof(invalid_aqi)/sizeof(*invalid_aqi);++i){
        reset();aqi_body=invalid_aqi[i];weather_body=NULL;update_weather();assert(!s_data.aqi_valid);
    }
    const char *valid_aqi[]={"{\"indexes\":[{\"aqiDisplay\":\"0\"}]}","{\"indexes\":[{\"aqi\":500}]}","{\"indexes\":[{\"aqi\":\"42\"}]}"};
    for(unsigned i=0;i<sizeof(valid_aqi)/sizeof(*valid_aqi);++i){reset();aqi_body=valid_aqi[i];update_weather();assert(s_data.aqi_valid);}
    reset();monitor_weather_data_t out;monitor_weather_get(NULL);monitor_weather_get(&out);assert(out.aqi==-1);
    reset();s_task=NULL;task_creates=0;task_ok=false;
    assert(monitor_weather_start()==ESP_ERR_NO_MEM && !s_task);
    task_ok=true;assert(monitor_weather_start()==ESP_OK && task_creates==2);
    assert(monitor_weather_start()==ESP_OK && task_creates==2);
    queued=0;monitor_weather_on_wifi_disconnected();assert(!s_wifi_available && queued==WEATHER_EVT_WIFI_CHANGED);
    queued=0;monitor_weather_on_wifi_connected();assert(s_wifi_available && queued==WEATHER_EVT_UPDATE);
    queued=0;monitor_weather_on_time_valid();assert(queued==WEATHER_EVT_UPDATE);
    // Pausing for a companion session is silent; resuming in Standalone refreshes.
    queued=0;s_companion_active=false;monitor_weather_set_companion_active(true);assert(!queued);
    queued=0;monitor_weather_set_companion_active(false);assert(queued==WEATHER_EVT_UPDATE);
    reset();worker_step=0;if(!setjmp(worker_done))weather_task(NULL);
    assert(http_calls[1]==2 && http_calls[2]==2 && !http_calls[0]);
    // Companion takeover with cached Standalone data starts no location lookup
    // and no weather request; the Client owns both until the session ends.
    reset();s_companion_active=true;worker_step=4;
    ip_body="{\"success\":true,\"latitude\":31.2,\"longitude\":121.5,\"city\":\"Beijing\"}";
    if(!setjmp(worker_done))weather_task(NULL);
    assert(!http_calls[0] && !http_calls[1] && !http_calls[2] && !notifications);
    assert(!s_data.location_valid && !s_data.weather_valid && !s_data.aqi_valid);
    s_companion_active=false;
    // IP location backs off (1s/2s/4s) and only then gates the weather fetch.
    reset();lat_text=lon_text="";s_wifi_available=true;ip_fail=2;
    ip_body="{\"success\":true,\"latitude\":31.2,\"longitude\":121.5,\"city\":\"Beijing\"}";
    update_weather();
    assert(http_calls[0]==3 && delay_count==2 && delays[0]==1000 && delays[1]==2000);
    assert(s_data.location_valid && !strcmp(s_data.city,"Beijing") && s_data.weather_valid && s_data.aqi_valid);
    // Exhausted retries must not trigger weather/AQI requests.
    reset();lat_text=lon_text="";s_wifi_available=true;ip_fail=99;
    update_weather();
    assert(http_calls[0]==LOCATION_ATTEMPTS && delay_count==LOCATION_ATTEMPTS-1);
    assert(delays[2]==4000 && !s_data.location_valid && !s_data.weather_valid && !http_calls[1] && !http_calls[2]);
    // Wi-Fi loss or a Companion ownership change cancels the backoff.
    reset();s_wifi_available=false;ip_fail=99;float ra,rb;char retry_city[32];
    assert(!locate_with_retry(&ra,&rb,retry_city,sizeof(retry_city)) && http_calls[0]==1 && delay_count==0);
    reset();s_wifi_available=true;ip_fail=99;companion_flip=true;
    assert(!locate_with_retry(&ra,&rb,retry_city,sizeof(retry_city)) && http_calls[0]==1 && delay_count==0);
    s_companion_active=false;
    // Manual coordinates skip IP lookup and keep any previously resolved city.
    reset();s_wifi_available=true;strcpy(s_data.city,"Cached");
    update_weather();
    assert(!http_calls[0] && s_data.weather_valid && !strcmp(s_data.city,"Cached"));
    // A companion takeover waits for the device's next 900 s slot.
    reset();assert(monitor_weather_refresh_delay_ms()==0); // never serviced
    update_weather();
    uint32_t refresh_delay=monitor_weather_refresh_delay_ms();
    assert(refresh_delay>0 && refresh_delay<=900000);
    clock_ready=false;assert(monitor_weather_refresh_delay_ms()==0);
    puts("Weather/AQI first data, unchanged/partial/error cache, no location, cached/IP location, NVS failure, strict coordinates/API code/metrics, allocation failure and recovery: PASS");
}
'''
run(code,sys.argv[1] if len(sys.argv)>1 else None,cjson=sys.argv[2] if len(sys.argv)>2 else REPO/'firmware/managed_components/espressif__cjson/cJSON')
