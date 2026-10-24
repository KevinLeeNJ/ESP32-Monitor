"""Execute production Wi-Fi session transitions with a fake clock and radio."""
from pathlib import Path
import re
import subprocess
import tempfile

repo = Path(__file__).resolve().parents[2]
source = (repo / 'firmware/components/firmware/monitor_wifi_manager.cpp').read_text()

def function(name):
    match = re.search(r'^static (?:void|esp_err_t) ' + name + r'\(', source, re.M)
    assert match, name
    start = match.start()
    end = source.index('{', start) + 1
    depth = 1
    while depth:
        depth += (source[end] == '{') - (source[end] == '}')
        end += 1
    return source[start:end]

wifi_header = (repo / 'firmware/components/esp_wifi_connect/include/wifi_manager.h').read_text()
ui_header = (repo / 'firmware/include/monitor_ui.h').read_text()
events = re.search(r'enum class WifiEvent \{.*?\};', wifi_header, re.S)[0]
states = re.search(r'typedef enum \{\s*MONITOR_WIFI_PROVISIONING.*?monitor_wifi_state_t;', ui_header, re.S)[0]
defines = '\n'.join(line for line in source.splitlines() if line.startswith('#define BTN_') or
                    line.startswith('#define CONFIG_SESSION_') or line.startswith('#define CONFIG_RESPONSE_') or line.startswith('#define STATION_CONNECT_'))
prefix = r'''
#include <atomic>
#include <cassert>
#include <cstdint>
#include <string>
#include <vector>
#define BIT0 1
#define BIT1 2
#define BIT2 4
#define BIT3 8
#define BIT4 16
#define BIT5 32
#define BIT6 64
#define ESP_LOGI(...) ((void)0)
#define ESP_OK 0
#define eSetBits 0
#define pdMS_TO_TICKS(ms) (ms)
using esp_err_t = int;
struct Timer { bool active = false; int64_t due = 0; };
static Timer config_timer, station_timer;
static Timer *s_config_timeout_timer = &config_timer, *s_station_timeout_timer = &station_timer;
static int64_t now, response_ready_at;
static int s_manager_task = 1;
static uint32_t queued;
static bool s_provisioning, s_config_session_active;
static std::atomic<int64_t> s_config_deadline{0}, s_station_deadline{0};
static int64_t esp_timer_get_time() { return now; }
static void vTaskDelay(unsigned ms) { now += ms * 1000; }
static void esp_timer_stop(Timer *t) { t->active = false; }
static void esp_timer_start_once(Timer *t, int64_t us) { t->active = true; t->due = now + us; }
static void xTaskNotify(int, uint32_t bits, int) { queued |= bits; }
static void monitor_display_refresh_request() {}
static void monitor_devices_set_wifi_available(bool) {}
static void monitor_devices_config_changed() {}
static void monitor_clock_on_wifi_connected() {}
static void monitor_clock_on_wifi_disconnected() {}
static void monitor_weather_on_wifi_connected() {}
static void monitor_weather_on_wifi_disconnected() {}
enum class WifiPowerSaveLevel { PERFORMANCE, BALANCED };
class SsidManager {
public:
    std::vector<int> saved;
    static SsidManager& GetInstance() { static SsidManager s; return s; }
    const std::vector<int>& GetSsidList() { return saved; }
};
'''
manager = r'''
static monitor_wifi_state_t s_state = MONITOR_WIFI_DISCONNECTED;
static void set_event(WifiEvent, const std::string&);
class WifiManager {
public:
    bool connected = false, config = false, station = false;
    unsigned starts = 0, exits = 0;
    static WifiManager& GetInstance() { static WifiManager m; return m; }
    bool IsConnected() { return connected; }
    void SetPowerSaveLevel(WifiPowerSaveLevel) {}
    void StartStation() { station = true; ++starts; }
    void StopStation() {
        assert(now >= response_ready_at);
        if (!station) return;
        station = connected = false;
        set_event(WifiEvent::Disconnected, "");
    }
    void StopConfigAp() {
        assert(now >= response_ready_at);
        response_ready_at = 0;
        assert(config);
        config = false; ++exits;
        set_event(WifiEvent::ConfigModeExit, "");
    }
};
'''
tests = r'''
static void dispatch(uint32_t bits = 0) {
    auto pending = queued | bits; queued = 0;
    process_manager_events(pending);
}
static void enter() {
    WifiManager::GetInstance().config = true;
    set_event(WifiEvent::ConfigModeEnter, "");
    assert(s_state == MONITOR_WIFI_PROVISIONING);
}
int main() {
    auto& m = WifiManager::GetInstance();
    auto& saved = SsidManager::GetInstance().saved;
    enter();
    now = 119000000;
    set_event(WifiEvent::ConfigActivity, "");
    // An already queued old timeout cannot close a freshly edited session.
    dispatch(BTN_TIMEOUT);
    assert(m.config && config_timer.due == 239000000);
    now = 238999999; dispatch(BTN_TIMEOUT); assert(m.config);
    now = 239000000; dispatch(BTN_TIMEOUT);
    assert(!m.config && s_state == MONITOR_WIFI_OFFLINE && !m.station);
    // Saving exits immediately, then tries the newly saved credentials.
    saved.push_back(1); enter();
    const auto submitted_at = now;
    response_ready_at = now + 250000;
    set_event(WifiEvent::ConfigExitRequested, ""); dispatch();
    assert(!m.config && m.station && s_state == MONITOR_WIFI_DISCONNECTED);
    assert(now - submitted_at >= 250000); // Success response drains before radio teardown.
    assert(!config_timer.active && station_timer.due == now + 30000000);
    now += 29999999; dispatch(BTN_STATION_TIMEOUT); assert(m.station);
    ++now; dispatch(BTN_STATION_TIMEOUT);
    assert(s_state == MONITOR_WIFI_OFFLINE && !m.station);
    dispatch(); assert(!m.station); // Stop event must not restart the radio.
    enter(); set_event(WifiEvent::ConfigExitRequested, ""); dispatch();
    m.connected = true;
    set_event(WifiEvent::Connected, "");
    assert(s_state == MONITOR_WIFI_CONNECTED && !station_timer.active);
    now += 30000000; dispatch(BTN_STATION_TIMEOUT);
    assert(m.connected); // A stale timeout cannot break a successful connection.
    // LAN configuration preserves connectivity on idle timeout.
    enter();
    set_event(WifiEvent::Connected, "");
    assert(s_provisioning && s_state == MONITOR_WIFI_PROVISIONING);
    now += 120000000; dispatch(BTN_TIMEOUT);
    assert(m.connected && !m.config && s_state == MONITOR_WIFI_CONNECTED);
    // LAN save reconnects so changed credentials actually take effect.
    enter(); auto starts = m.starts;
    set_event(WifiEvent::ConfigExitRequested, ""); dispatch(); dispatch();
    assert(m.station && !m.connected && m.starts == starts + 1);
    // Losing an established connection also has a finite reconnect window.
    m.connected = true; set_event(WifiEvent::Connected, "");
    m.connected = false; set_event(WifiEvent::Disconnected, ""); dispatch();
    assert(station_timer.active);
    now += 30000000; dispatch(BTN_STATION_TIMEOUT);
    assert(s_state == MONITOR_WIFI_OFFLINE && !m.station);
}
'''
with tempfile.TemporaryDirectory() as directory:
    path = Path(directory)
    unit = path / 'wifi_session.cc'
    unit.write_text('\n'.join([prefix, events, states, defines, manager]) + '\n' + '\n'.join(function(name) for name in
        ['update_wifi_state', 'start_config_timeout', 'start_station_attempt', 'set_event', 'process_manager_events']) + tests)
    subprocess.run(['c++', '-std=c++17', '-Wall', '-Wextra', '-Werror', str(unit), '-o', str(path / 'test')], check=True)
    subprocess.run([str(path / 'test')], check=True)
print('Wi-Fi session transitions passed: idle edits, immediate save, AP/LAN exit, saved credentials, connection timeout, offline')
