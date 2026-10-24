"""Run production portal scanning against a consumable driver result list."""
from pathlib import Path
import re
import subprocess
import tempfile

repo = Path(__file__).resolve().parents[2]
station = (repo / 'firmware/components/esp_wifi_connect/wifi_station.cc').read_text()
portal = (repo / 'firmware/components/esp_wifi_connect/wifi_configuration_ap.cc').read_text()

def method(source, name):
    match = re.search(r'^(?:void|std::vector<wifi_ap_record_t>) ' + re.escape(name) + r'\(', source, re.M)
    assert match, name
    end = source.index('{', match.start()) + 1
    depth = 1
    while depth:
        depth += (source[end] == '{') - (source[end] == '}')
        end += 1
    return source[match.start():end]

prefix = r'''
#include <algorithm>
#include <cassert>
#include <cstdint>
#include <cstring>
#include <functional>
#include <mutex>
#include <string>
#include <vector>
#define ESP_OK 0
#define ESP_FAIL -1
#define ESP_LOGI(...) ((void)0)
#define ESP_LOGW(...) ((void)0)
#define ESP_ERROR_CHECK(e) assert((e) == ESP_OK)
#define ESP_TIMER_TASK 0
using esp_err_t = int;
[[maybe_unused]] static const char *esp_err_to_name(int) { return "fake"; }
struct wifi_ap_record_t { uint8_t ssid[33]; int rssi, primary, authmode; uint8_t bssid[6]; };
struct WifiApRecord { std::string ssid, password; int channel, authmode; uint8_t bssid[6]; };
struct SsidItem { std::string ssid, password; };
struct SsidManager {
    std::vector<SsidItem> saved{{"Saved", "password"}};
    static SsidManager& GetInstance() { static SsidManager s; return s; }
    const auto& GetSsidList() { return saved; }
};
struct esp_timer_create_args_t {
    void (*callback)(void *); void *arg; int dispatch_method; const char *name; bool skip_unhandled_events;
};
using httpd_handle_t = void *;
static esp_timer_create_args_t timer;
static bool periodic, timer_active;
static unsigned scans, reads, closes;
static std::vector<wifi_ap_record_t> driver_results;
static int scan_error, scan_num_error, scan_records_error;
static unsigned scheduled_scans;
static int esp_wifi_scan_start(void *config, bool blocking) {
    assert(!config && !blocking); ++scans; return scan_error;
}
static int esp_wifi_scan_get_ap_num(uint16_t *count) { if(scan_num_error)return ESP_FAIL;*count = driver_results.size(); return ESP_OK; }
static int esp_wifi_scan_get_ap_records(uint16_t *count, wifi_ap_record_t *records) {
    ++reads; if(scan_records_error)return ESP_FAIL;std::copy(driver_results.begin(), driver_results.end(), records);
    assert(*count == driver_results.size()); driver_results.clear(); return ESP_OK;
}
static int esp_timer_create(esp_timer_create_args_t *args, int *handle) { timer = *args; *handle = 1; return ESP_OK; }
static int esp_timer_start_periodic(int, int64_t interval) {
    assert(interval == 10000000); periodic = timer_active = true; return ESP_OK;
}
static int esp_timer_start_once(int, int64_t) { ++scheduled_scans;return ESP_OK; }
static int esp_timer_stop(int) { timer_active = false; return ESP_OK; }
static int esp_timer_delete(int) { timer.callback = nullptr; return ESP_OK; }
static int httpd_stop(httpd_handle_t);
class WifiConfigurationAp {
public:
    std::mutex mutex_;
    httpd_handle_t server_ = nullptr;
    int scan_timer_ = 0;
    bool station_web_mode_ = false, is_connecting_ = false;
    std::vector<wifi_ap_record_t> ap_records_;
    std::function<void()> on_station_scan_requested_;
    void StartStationWebServer(); void StopWebServer(); void StartScanTimer();
    void StartWebServer() { server_ = this; }
    void SetAccessPoints(const std::vector<wifi_ap_record_t>&);
    std::vector<wifi_ap_record_t> GetAccessPoints();
};
static int httpd_stop(httpd_handle_t server) {
    // A request finishing while stop waits can still read the cache.
    static_cast<WifiConfigurationAp *>(server)->GetAccessPoints(); ++closes; return ESP_OK;
}
class WifiStation {
public:
    bool connected = true, last_scan_used_saved_channels_ = false, use_saved_channels_scan_ = true;
    int timer_handle_ = 1, scan_current_interval_microseconds_ = 10000000;
    unsigned reconnects = 0, reconnect_scans = 0;
    std::vector<WifiApRecord> connect_queue_;
    std::function<void(const std::vector<wifi_ap_record_t>&)> on_scan_results_;
    bool IsConnected() { return connected; }
    void ScanForConfig(); void HandleScanResult();
    void StartConnect() { ++reconnects; }
    void StartScan() { ++reconnect_scans; }
    void UpdateScanInterval() {}
};
'''
tests = r'''
int main() {
    WifiConfigurationAp portal; WifiStation station;
    station.on_scan_results_ = [&](const auto& records) { portal.SetAccessPoints(records); };
    portal.on_station_scan_requested_ = [&]() { station.ScanForConfig(); };
    portal.StartStationWebServer();
    assert(scans == 1 && periodic && timer_active);
    driver_results = {{{'S','a','v','e','d',0}, -70, 1, 3, {}}, {{'N','e','a','r',0}, -30, 6, 3, {}}};
    station.HandleScanResult();
    auto records = portal.GetAccessPoints();
    assert(reads == 1 && records.size() == 2 && records[0].rssi == -30);
    assert(station.connected && station.connect_queue_.empty() && !station.reconnects);
    timer.callback(timer.arg); assert(scans == 2);
    // Connection loss leaves radio ownership with the station reconnect flow.
    station.connected = false;
    timer.callback(timer.arg); assert(scans == 2);
    station.connect_queue_.push_back({"Saved", "obsolete password", 11, 3, {}});
    driver_results = {{{'S','a','v','e','d',0}, -50, 1, 3, {}}};
    station.HandleScanResult(); assert(station.reconnects == 1);
    assert(station.connect_queue_.size() == 1 && station.connect_queue_[0].password == "password");
    assert(portal.GetAccessPoints().size() == 1);
    portal.StopWebServer();
    assert(!timer_active && !timer.callback && !portal.server_ && closes == 1);
    // New sessions start another full scan and cannot display stale results.
    station.connected = true; portal.StartStationWebServer();
    assert(scans == 3 && portal.GetAccessPoints().empty());
    portal.StopWebServer();
    // AP sessions still request scans directly through the same timer.
    portal.station_web_mode_ = false;
    portal.StartScanTimer(); assert(scans == 4);
    timer.callback(timer.arg); assert(scans == 5);
    portal.StopWebServer();
    station.connect_queue_.clear();
    // SDK scan failures retain the last published list, rather than clearing it.
    driver_results={{{'S','a','v','e','d',0},-50,1,3,{}}};portal.SetAccessPoints(driver_results);
    auto cached=portal.GetAccessPoints();scan_num_error=1;
    station.HandleScanResult();assert(portal.GetAccessPoints().size()==cached.size());scan_num_error=0;
    scan_records_error=1;station.HandleScanResult();assert(portal.GetAccessPoints()[0].rssi==-50);scan_records_error=0;
    station.HandleScanResult();assert(driver_results.empty());
    // A successful empty scan removes stale portal results without reconnecting a connected STA.
    station.connected=true;station.HandleScanResult();assert(portal.GetAccessPoints().empty());
    station.connected=false;station.last_scan_used_saved_channels_=false;
    unsigned before=scheduled_scans;station.HandleScanResult();assert(scheduled_scans==before+1);
    station.last_scan_used_saved_channels_=true;before=station.reconnect_scans;
    station.HandleScanResult();assert(station.reconnect_scans==before+1 && !station.last_scan_used_saved_channels_);
    // A failed connected scan is deferred and cannot modify connection ownership.
    station.connected=true;scan_error=ESP_FAIL;before=station.reconnects;
    station.ScanForConfig();assert(station.reconnects==before && station.connect_queue_.empty());
}
'''
code = prefix
for name in ['GetAccessPoints', 'SetAccessPoints', 'StartStationWebServer', 'StartScanTimer', 'StopWebServer']:
    code += method(portal, 'WifiConfigurationAp::' + name) + '\n'
for name in ['ScanForConfig', 'HandleScanResult']:
    code += method(station, 'WifiStation::' + name) + '\n'
# Match ESP-IDF's opaque timer handle in this host harness.
code = code.replace('scan_timer_ = nullptr', 'scan_timer_ = 0')
with tempfile.TemporaryDirectory() as temp:
    unit = Path(temp) / 'scan.cc'; unit.write_text(code + tests)
    subprocess.run(['c++', '-std=c++17', '-Wall', '-Wextra', '-Werror', str(unit), '-o', str(Path(temp) / 'test')], check=True)
    subprocess.run([str(Path(temp) / 'test')], check=True, timeout=5)
print('Portal scan checks passed: immediate/periodic scan, shared results, no connected reconnect, disconnect ownership, cleanup, AP mode')
