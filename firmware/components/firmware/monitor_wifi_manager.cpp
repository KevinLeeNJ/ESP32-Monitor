#include "monitor_wifi_manager.h"
#include "monitor_clock.h"
#include "monitor_weather.h"
#include "monitor_display_refresh.h"

#include <string>

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "driver/gpio.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "ssid_manager.h"
#include "wifi_manager.h"

#define TAG "monitor_wifi"
#define BOOT_BUTTON_GPIO GPIO_NUM_0
#define KEY_BUTTON_GPIO GPIO_NUM_18
#define CONFIG_HOLD_MS 3000

static volatile monitor_wifi_state_t s_state = MONITOR_WIFI_DISCONNECTED;
static volatile bool s_provisioning = false;
static bool s_started = false;
static bool s_config_session_active = false;
static esp_timer_handle_t s_config_timeout_timer = nullptr;
#define CONFIG_SESSION_TIMEOUT_US (2LL * 60 * 1000000)

static esp_err_t enter_config_mode(void);

static void update_wifi_state(monitor_wifi_state_t state)
{
    if (s_state == state) return;
    s_state = state;
    monitor_display_refresh_request();
}

static void config_timeout_callback(void *arg)
{
    (void)arg;
    if (!s_config_session_active) return;
    ESP_LOGI(TAG, "Configuration session timed out; closing web server");
    // StopConfigAp closes either the captive AP or the LAN web server and
    // emits ConfigModeExit.
    WifiManager::GetInstance().StopConfigAp();
}

static void start_config_timeout(void)
{
    s_config_session_active = true;
    if (s_config_timeout_timer) {
        esp_timer_stop(s_config_timeout_timer);
        esp_timer_start_once(s_config_timeout_timer, CONFIG_SESSION_TIMEOUT_US);
    }
}

static void set_event(WifiEvent event, const std::string &data)
{
    (void)data;
    switch(event) {
        case WifiEvent::ConfigModeEnter:
            // A connected station serves the configuration page over LAN.
            // Disable modem sleep for this session so HTTP responses are not
            // delayed by the station listen interval.
            if (WifiManager::GetInstance().IsConnected()) {
                WifiManager::GetInstance().SetPowerSaveLevel(WifiPowerSaveLevel::PERFORMANCE);
            }
            s_provisioning = true;
            update_wifi_state(MONITOR_WIFI_PROVISIONING);
            start_config_timeout();
            break;
        case WifiEvent::ConfigModeExit:
            s_provisioning = false;
            s_config_session_active = false;
            if (s_config_timeout_timer) esp_timer_stop(s_config_timeout_timer);
            update_wifi_state(MONITOR_WIFI_DISCONNECTED);
            // Restore normal station power saving after a LAN configuration
            // session. Captive-AP sessions have no connected station here.
            if (WifiManager::GetInstance().IsConnected()) {
                WifiManager::GetInstance().SetPowerSaveLevel(WifiPowerSaveLevel::BALANCED);
            }
            /* StopConfigAp() has already stopped the AP, DNS server and web
               server. Retry station mode only when credentials are available;
               a timed-out first-time setup must not immediately start a loop. */
            ESP_LOGI(TAG, "Wi-Fi configuration session ended");
            if (WifiManager::GetInstance().IsConnected()) {
                update_wifi_state(MONITOR_WIFI_CONNECTED);
            } else if (!SsidManager::GetInstance().GetSsidList().empty()) {
                WifiManager::GetInstance().StartStation();
            }
            break;
        case WifiEvent::AdvancedConfigSaved:
            if (WifiManager::GetInstance().IsConnected()) {
                monitor_weather_on_wifi_connected();
            }
            break;
        case WifiEvent::Connected:
            s_provisioning = false;
            update_wifi_state(MONITOR_WIFI_CONNECTED);
            monitor_clock_on_wifi_connected();
            monitor_weather_on_wifi_connected();
            break;
        case WifiEvent::Connecting:
        case WifiEvent::Scanning:
            if(!s_provisioning) update_wifi_state(MONITOR_WIFI_DISCONNECTED);
            break;
        case WifiEvent::Disconnected:
            if(!s_provisioning) {
                update_wifi_state(MONITOR_WIFI_DISCONNECTED);
                s_config_session_active = false;
                if (s_config_timeout_timer) esp_timer_stop(s_config_timeout_timer);
                // Do not automatically enter configuration mode on a failed
                // reconnect. A saved Wi-Fi configuration must remain the
                // normal boot path; configuration mode is entered only when
                // no credentials exist or the user requests it with the
                // hardware button.
            }
            break;
    }
}

static esp_err_t enter_config_mode(void)
{
    if (s_provisioning) return ESP_OK;
    s_provisioning = true;
    update_wifi_state(MONITOR_WIFI_PROVISIONING);
    s_config_session_active = true;
    WifiManager::GetInstance().StartConfigAp();
    return ESP_OK;
}

static void button_task(void *arg)
{
    (void)arg;
    gpio_config_t config = {
        .pin_bit_mask = (1ULL << BOOT_BUTTON_GPIO) | (1ULL << KEY_BUTTON_GPIO),
        .mode = GPIO_MODE_INPUT,
        .pull_up_en = GPIO_PULLUP_ENABLE,
        .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type = GPIO_INTR_DISABLE,
    };
    gpio_config(&config);
    TickType_t pressed_at = 0;
    bool triggered = false;
    for(;;) {
        const bool both_pressed = gpio_get_level(BOOT_BUTTON_GPIO) == 0 &&
                                  gpio_get_level(KEY_BUTTON_GPIO) == 0;
        if(both_pressed) {
            if(pressed_at == 0) pressed_at = xTaskGetTickCount();
            if(!triggered && xTaskGetTickCount() - pressed_at >= pdMS_TO_TICKS(CONFIG_HOLD_MS)) {
                if(enter_config_mode() == ESP_OK) triggered = true;
            }
        } else {
            pressed_at = 0;
            triggered = false;
        }
        vTaskDelay(pdMS_TO_TICKS(50));
    }
}

extern "C" esp_err_t monitor_wifi_start(void)
{
    if(s_started) return ESP_OK;
    auto &manager = WifiManager::GetInstance();
    WifiManagerConfig config;
    config.ssid_prefix = "ESP32-Monitor";
    config.language = "zh-CN";
    config.show_ota_config = false;
    config.show_sleep_config = false;
    manager.SetEventCallback(set_event);
    esp_timer_create_args_t timer_args = {
        .callback = config_timeout_callback,
        .arg = NULL,
        .dispatch_method = ESP_TIMER_TASK,
        .name = "config_session_timeout",
        .skip_unhandled_events = false,
    };
    if (esp_timer_create(&timer_args, &s_config_timeout_timer) != ESP_OK) {
        ESP_LOGE(TAG, "failed to create configuration timeout timer");
        return ESP_ERR_NO_MEM;
    }
    if(!manager.Initialize(config)) {
        ESP_LOGE(TAG, "esp-wifi-connect initialization failed");
        return ESP_FAIL;
    }
    s_started = true;
    if(SsidManager::GetInstance().GetSsidList().empty()) {
        ESP_LOGI(TAG, "no saved Wi-Fi credentials; entering esp-wifi-connect config mode");
        enter_config_mode();
    } else {
        ESP_LOGI(TAG, "saved Wi-Fi credentials found; starting esp-wifi-connect station");
        manager.StartStation();
    }
    xTaskCreate(button_task, "wifi_config_button", 3072, NULL, 4, NULL);
    return ESP_OK;
}

extern "C" monitor_wifi_state_t monitor_wifi_get_state(void) { return s_state; }
extern "C" bool monitor_wifi_is_provisioning(void) { return s_provisioning; }
