"""Actual station event handler, scan backoff and stop lifecycle with SDK fakes."""
import sys
from host_harness import REPO, defines, function, run
source=(REPO/'firmware/components/esp_wifi_connect/wifi_station.cc').read_text()
code=r'''
#include <cassert>
#include <cstdint>
#include <functional>
#include <string>
#include <vector>
#define ESP_LOGI(...) ((void)0)
#define ESP_OK 0
#define BIT0 1
#define BIT1 2
#define BIT2 4
#define WIFI_EVENT 1
#define IP_EVENT 2
#define ESP_EVENT_ANY_ID 0
#define IP_EVENT_STA_GOT_IP 1
#define WIFI_EVENT_STA_START 1
#define WIFI_EVENT_SCAN_DONE 2
#define WIFI_EVENT_STA_DISCONNECTED 3
#define WIFI_EVENT_STA_CONNECTED 4
using esp_event_base_t=int;
struct wifi_event_sta_disconnected_t {int reason;};
struct ip_event_got_ip_t {struct {int ip;} ip_info;};
struct wifi_ap_record_t {uint8_t primary;};
enum class WifiPowerSaveLevel { LOW_POWER };
static unsigned connects,scans,closes,unregistered;
static unsigned timer_starts,timer_stops,timer_deletes;
static int64_t last_delay;
static int bits;
static bool stopped;
static int xEventGroupClearBits(int,int flags) {bits&=~flags;return bits;}
static int xEventGroupSetBits(int,int flags) {bits|=flags;return bits;}
static int esp_wifi_connect() {assert(!stopped);++connects;return ESP_OK;}
static int esp_timer_start_once(void *timer,int64_t delay) {assert(timer && !stopped);++timer_starts;last_delay=delay;return ESP_OK;}
static int esp_timer_stop(void *) {++timer_stops;return ESP_OK;}
static int esp_timer_delete(void *) {assert(unregistered==2);++timer_deletes;return ESP_OK;}
static int esp_event_handler_instance_unregister(int,int,void *) {++unregistered;return ESP_OK;}
static void esp_wifi_scan_stop() {assert(unregistered==2);}
static void esp_wifi_disconnect() {assert(unregistered==2);}
static void esp_wifi_stop() {assert(unregistered==2 && timer_deletes==1);stopped=true;++closes;}
static void esp_netif_destroy_default_wifi(void *) {assert(stopped);}
static void esp_ip4addr_ntoa(const int *,char *out,unsigned) {out[0]='1';out[1]=0;}
static int esp_wifi_sta_get_ap_info(wifi_ap_record_t *out) {out->primary=11;return ESP_OK;}
struct SsidManager {
    unsigned updates=0;
    static SsidManager& GetInstance(){static SsidManager s;return s;}
    void UpdateSsidChannel(const std::string&,int channel){assert(channel==11);++updates;}
};
class WifiStation {
public:
    int event_group_=1,reconnect_count_=0;
    void *timer_handle_=(void *)1,*instance_any_id_=(void *)2,*instance_got_ip_=(void *)3,*station_netif_=(void *)4;
    bool was_connected_=false,use_saved_channels_scan_=false,last_scan_used_saved_channels_=false;
    WifiPowerSaveLevel power=WifiPowerSaveLevel::LOW_POWER;
    void SetPowerSaveLevel(WifiPowerSaveLevel level){power=level;}
    int scan_min_interval_microseconds_=10000000,scan_max_interval_microseconds_=300000000,scan_current_interval_microseconds_=10000000;
    std::string ssid_="Saved",ip_address_;
    std::vector<int> connect_queue_;
    std::function<void()> on_scan_begin_;
    std::function<void(int)> on_disconnected_;
    std::function<void(const std::string&)> on_connected_;
    unsigned queue_attempts=0;
    void StartScan(){assert(!stopped);++scans;}
    void HandleScanResult(){}
    void StartConnect(){++queue_attempts;connect_queue_.erase(connect_queue_.begin());}
    void Stop();void UpdateScanInterval();void SetScanIntervalRange(int,int);
    static void WifiEventHandler(void*,esp_event_base_t,int32_t,void*);
    static void IpEventHandler(void*,esp_event_base_t,int32_t,void*);
};
'''
code+=defines(source,['WIFI_EVENT_CONNECTED','WIFI_EVENT_STOPPED','WIFI_EVENT_SCAN_DONE_BIT','MAX_RECONNECT_COUNT'])+'\n'
code+='\n'.join(function(source,'WifiStation::'+name) for name in ['SetScanIntervalRange','UpdateScanInterval','WifiEventHandler','IpEventHandler','Stop'])
code+=r'''
int main() {
    WifiStation s;unsigned notices=0,successes=0;
    s.on_disconnected_=[&](int reason){assert(reason==201);++notices;};
    s.on_connected_=[&](const std::string& ssid){assert(ssid=="Saved");++successes;};
    s.was_connected_=true;bits=WIFI_EVENT_CONNECTED;
    wifi_event_sta_disconnected_t event{201};
    // Exactly the production count, then scan backoff; only the first loss notifies.
    for(unsigned i=0;i<5;++i) WifiStation::WifiEventHandler(&s,0,WIFI_EVENT_STA_DISCONNECTED,&event);
    assert(connects==5 && notices==1 && !(bits&WIFI_EVENT_CONNECTED) && !timer_starts);
    WifiStation::WifiEventHandler(&s,0,WIFI_EVENT_STA_DISCONNECTED,&event);
    assert(connects==5 && last_delay==10000000 && s.scan_current_interval_microseconds_==20000000);
    const int64_t delays[]={20000000,40000000,80000000,160000000,300000000,300000000};
    for(auto delay:delays) {
        WifiStation::WifiEventHandler(&s,0,WIFI_EVENT_STA_DISCONNECTED,&event);
        assert(last_delay==delay && connects==5 && notices==1);
    }
    s.SetScanIntervalRange(7,25);
    assert(s.scan_current_interval_microseconds_==7000000);
    s.UpdateScanInterval();assert(s.scan_current_interval_microseconds_==14000000);
    s.UpdateScanInterval();assert(s.scan_current_interval_microseconds_==25000000);
    for(unsigned i=0;i<1000;++i)s.UpdateScanInterval();
    assert(s.scan_current_interval_microseconds_==25000000);
    // Next candidate AP is attempted only after the direct retry budget is gone.
    s.connect_queue_.push_back(1);unsigned timers=timer_starts;
    WifiStation::WifiEventHandler(&s,0,WIFI_EVENT_STA_DISCONNECTED,&event);
    assert(s.queue_attempts==1 && timer_starts==timers);
    // A visible but unusable AP on the cached channel must not hide other APs.
    s.last_scan_used_saved_channels_=s.use_saved_channels_scan_=true;
    unsigned scans_before=scans;
    WifiStation::WifiEventHandler(&s,0,WIFI_EVENT_STA_DISCONNECTED,&event);
    assert(scans==scans_before+1 && !s.use_saved_channels_scan_ && !s.last_scan_used_saved_channels_);
    assert(timer_starts==timers);
    ip_event_got_ip_t ip{};WifiStation::IpEventHandler(&s,0,0,&ip);
    assert(successes==1 && s.was_connected_ && s.reconnect_count_==0 && s.connect_queue_.empty());
    assert(s.use_saved_channels_scan_ && s.scan_current_interval_microseconds_==7000000 && (bits&WIFI_EVENT_CONNECTED));
    assert(s.power==WifiPowerSaveLevel::LOW_POWER);
    unsigned before=scans;
    WifiStation::WifiEventHandler(&s,0,WIFI_EVENT_STA_START,nullptr);assert(scans==before+1);
    s.connect_queue_={1,2};s.reconnect_count_=5;
    s.Stop();
    assert(stopped && !s.timer_handle_ && !s.instance_any_id_ && !s.instance_got_ip_ && !s.station_netif_);
    assert(!s.was_connected_ && !(bits&WIFI_EVENT_CONNECTED) && (bits&WIFI_EVENT_STOPPED));
    assert(s.connect_queue_.empty() && s.reconnect_count_==0 && s.ip_address_.empty());
    // Host never delivers callbacks after unregister; the stop order prevents new scans.
    assert(closes==1 && timer_deletes==1 && unregistered==2);
    puts("Station five-retry limit, disconnect notification coalescing, 10/20/40/80/160/300s capped backoff, custom cap, AP switching, success reset and stop cleanup: PASS");
}
'''
run(code,sys.argv[1] if len(sys.argv)>1 else 'c++',cpp=True)
