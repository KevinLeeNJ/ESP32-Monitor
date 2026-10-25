#include "monitor_wifi_manager.h"
#include "monitor_clock.h"
#include "monitor_weather.h"
#include "monitor_devices.h"
#include "monitor_display_refresh.h"
#include "monitor_pm_profile.h"

#include <string>
#include <atomic>

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "driver/gpio.h"
#include "esp_sleep.h"
#include "esp_log.h"
#include "esp_timer.h"
#include "ssid_manager.h"
#include "wifi_manager.h"

#define TAG "monitor_wifi"
#define BOOT_BUTTON_GPIO GPIO_NUM_0
#define KEY_BUTTON_GPIO GPIO_NUM_18
#define CONFIG_HOLD_MS 3000
#define BTN_BOOT BIT0
#define BTN_KEY BIT1
#define BTN_TIMEOUT BIT2
#define BTN_EXIT BIT3
#define BTN_ACTIVITY BIT4
#define BTN_STATION_TIMEOUT BIT5
#define BTN_RECONNECT BIT6

static volatile monitor_wifi_state_t s_state = MONITOR_WIFI_DISCONNECTED;
static volatile bool s_provisioning = false;
static bool s_started = false;
static bool s_config_session_active = false;
static std::atomic<int64_t> s_config_deadline{0};
static esp_timer_handle_t s_station_timeout_timer = nullptr;
static std::atomic<int64_t> s_station_deadline{0};
#define STATION_CONNECT_TIMEOUT_US (30LL * 1000000)
static esp_timer_handle_t s_config_timeout_timer = nullptr;
static TaskHandle_t s_manager_task = nullptr;
#define CONFIG_SESSION_TIMEOUT_US (2LL * 60 * 1000000)
#define CONFIG_RESPONSE_DRAIN_MS 250

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
    if (s_manager_task != nullptr) xTaskNotify(s_manager_task, BTN_TIMEOUT, eSetBits);
}

static void start_config_timeout(void)
{
    s_config_session_active = true;
    s_config_deadline = esp_timer_get_time() + CONFIG_SESSION_TIMEOUT_US;
    if (s_config_timeout_timer) {
        esp_timer_stop(s_config_timeout_timer);
        esp_timer_start_once(s_config_timeout_timer, CONFIG_SESSION_TIMEOUT_US);
    }
}

static void start_station_attempt(void)
{
    if (SsidManager::GetInstance().GetSsidList().empty()) {
        update_wifi_state(MONITOR_WIFI_OFFLINE);
        ESP_LOGI(TAG, "Offline: no saved Wi-Fi credentials");
        return;
    }
    update_wifi_state(MONITOR_WIFI_DISCONNECTED);
    esp_timer_stop(s_station_timeout_timer);
    s_station_deadline = esp_timer_get_time() + STATION_CONNECT_TIMEOUT_US;
    esp_timer_start_once(s_station_timeout_timer, STATION_CONNECT_TIMEOUT_US);
    WifiManager::GetInstance().StartStation();
}

static void set_event(WifiEvent event, const std::string &data)
{
    (void)data;
    switch(event) {
        case WifiEvent::ConfigActivity:
            s_config_deadline = esp_timer_get_time() + CONFIG_SESSION_TIMEOUT_US;
            if (s_manager_task) xTaskNotify(s_manager_task, BTN_ACTIVITY, eSetBits);
            break;
        case WifiEvent::ConfigExitRequested:
            if (s_manager_task) xTaskNotify(s_manager_task, BTN_EXIT, eSetBits);
            break;
        case WifiEvent::ConfigModeEnter:
            esp_timer_stop(s_station_timeout_timer);
            // A connected station serves the configuration page over LAN.
            // Disable modem sleep for this session so HTTP responses are not
            // delayed by the station listen interval.
            if (WifiManager::GetInstance().IsConnected()) {
                WifiManager::GetInstance().SetPowerSaveLevel(WifiPowerSaveLevel::PERFORMANCE);
            }
            if (!WifiManager::GetInstance().IsConnected()) {
                monitor_devices_set_wifi_available(false);
                monitor_clock_on_wifi_disconnected();
                monitor_weather_on_wifi_disconnected();
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
                WifiManager::GetInstance().SetPowerSaveLevel(WifiPowerSaveLevel::LOW_POWER);
            }
            /* StopConfigAp() has already stopped the AP, DNS server and web
               server. Retry station mode only when credentials are available;
               a timed-out first-time setup must not immediately start a loop. */
            ESP_LOGI(TAG, "Wi-Fi configuration session ended");
            if (WifiManager::GetInstance().IsConnected()) {
                update_wifi_state(MONITOR_WIFI_CONNECTED);
            } else {
                start_station_attempt();
            }
            break;
        case WifiEvent::AdvancedConfigSaved:
            monitor_devices_config_changed();
            if (WifiManager::GetInstance().IsConnected()) {
                monitor_weather_on_wifi_connected();
            }
            break;
        case WifiEvent::Connected:
            esp_timer_stop(s_station_timeout_timer);
            if (!s_provisioning) update_wifi_state(MONITOR_WIFI_CONNECTED);
            monitor_clock_on_wifi_connected();
            monitor_weather_on_wifi_connected();
            monitor_devices_set_wifi_available(true);
            break;
        case WifiEvent::Connecting:
        case WifiEvent::Scanning:
            if(!s_provisioning) update_wifi_state(MONITOR_WIFI_DISCONNECTED);
            break;
        case WifiEvent::Disconnected:
            monitor_devices_set_wifi_available(false);
            monitor_clock_on_wifi_disconnected();
            monitor_weather_on_wifi_disconnected();
            if(!s_provisioning && s_state != MONITOR_WIFI_OFFLINE) {
                update_wifi_state(MONITOR_WIFI_DISCONNECTED);
                if (s_manager_task) xTaskNotify(s_manager_task, BTN_RECONNECT, eSetBits);
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

static void IRAM_ATTR button_gpio_isr(void *arg)
{
    const uint32_t bit = (uint32_t)(uintptr_t)arg;
    /* gpio_wakeup_enable() also selects the GPIO's level interrupt type.
     * Mask that level before notifying the task, otherwise a held/released
     * button can retrigger continuously and starve the tick interrupt. */
    gpio_intr_disable(bit == BTN_BOOT ? BOOT_BUTTON_GPIO : KEY_BUTTON_GPIO);
    BaseType_t wake = pdFALSE;
    if (s_manager_task != nullptr) xTaskNotifyFromISR(s_manager_task, bit, eSetBits, &wake);
    if (wake) portYIELD_FROM_ISR();
}

static void configure_wake_levels(bool boot_pressed, bool key_pressed)
{
    gpio_intr_disable(BOOT_BUTTON_GPIO);
    gpio_intr_disable(KEY_BUTTON_GPIO);
#if CONFIG_PM_SLP_DISABLE_GPIO
    gpio_sleep_sel_dis(BOOT_BUTTON_GPIO);
    gpio_sleep_sel_dis(KEY_BUTTON_GPIO);
#endif
    gpio_wakeup_disable(BOOT_BUTTON_GPIO);
    gpio_wakeup_disable(KEY_BUTTON_GPIO);
    gpio_wakeup_enable(BOOT_BUTTON_GPIO, boot_pressed ? GPIO_INTR_HIGH_LEVEL : GPIO_INTR_LOW_LEVEL);
    gpio_wakeup_enable(KEY_BUTTON_GPIO, key_pressed ? GPIO_INTR_HIGH_LEVEL : GPIO_INTR_LOW_LEVEL);
    esp_sleep_enable_gpio_wakeup();
    /* Rearm only after both pins target the opposite debounced level. */
    gpio_intr_enable(BOOT_BUTTON_GPIO);
    gpio_intr_enable(KEY_BUTTON_GPIO);
}

static void process_manager_events(uint32_t pending)
{
    if (pending & BTN_ACTIVITY) {
        if (s_config_session_active) {
            esp_timer_stop(s_config_timeout_timer);
            int64_t remaining = s_config_deadline.load() - esp_timer_get_time();
            esp_timer_start_once(s_config_timeout_timer, remaining > 0 ? remaining : 1);
        }
    }
    if (pending & BTN_EXIT) {
        if (s_config_session_active) {
            /* httpd_resp_send() can return while TCP/Wi-Fi still has the
             * success response queued. Keep the current interface alive long
             * enough to transmit it before reconnecting with saved settings. */
            vTaskDelay(pdMS_TO_TICKS(CONFIG_RESPONSE_DRAIN_MS));
            /* Reconnect after saving so LAN edits also apply new credentials. */
            WifiManager::GetInstance().StopStation();
            WifiManager::GetInstance().StopConfigAp();
        }
    }
    if (pending & BTN_RECONNECT) {
        if (!s_provisioning && s_state != MONITOR_WIFI_OFFLINE &&
            !WifiManager::GetInstance().IsConnected() &&
            !esp_timer_is_active(s_station_timeout_timer)) {
            esp_timer_stop(s_station_timeout_timer);
            s_station_deadline = esp_timer_get_time() + STATION_CONNECT_TIMEOUT_US;
            esp_timer_start_once(s_station_timeout_timer, STATION_CONNECT_TIMEOUT_US);
        }
    }
    if (pending & BTN_STATION_TIMEOUT) {
        if (!s_provisioning && !WifiManager::GetInstance().IsConnected() &&
            esp_timer_get_time() >= s_station_deadline.load()) {
            // Authentication retries and DHCP can exceed this initial window.
            // Leave the station running: it owns retry limits and scan backoff,
            // and must be able to recover when the access point becomes ready.
            esp_timer_stop(s_station_timeout_timer);
            update_wifi_state(MONITOR_WIFI_DISCONNECTED);
            ESP_LOGI(TAG, "Wi-Fi connection pending; continuing station retries");
        }
    }
    if (pending & BTN_TIMEOUT) {
        if (s_config_session_active && esp_timer_get_time() >= s_config_deadline.load()) {
            ESP_LOGI(TAG, "Configuration idle for 2 minutes; closing server");
            WifiManager::GetInstance().StopConfigAp();
        }
    }
}

static void button_task(void *arg)
{
    (void)arg;
    s_manager_task = xTaskGetCurrentTaskHandle();
    gpio_config_t config = {
        .pin_bit_mask = (1ULL << BOOT_BUTTON_GPIO) | (1ULL << KEY_BUTTON_GPIO),
        .mode = GPIO_MODE_INPUT,
        .pull_up_en = GPIO_PULLUP_ENABLE,
        .pull_down_en = GPIO_PULLDOWN_DISABLE,
        .intr_type = GPIO_INTR_ANYEDGE,
    };
    if (gpio_config(&config) != ESP_OK) { vTaskDelete(NULL); return; }
    // app_main owns the shared IRAM-safe GPIO ISR service (buttons and TE).
    if (gpio_isr_handler_add(BOOT_BUTTON_GPIO, button_gpio_isr, (void *)(uintptr_t)BTN_BOOT) != ESP_OK ||
        gpio_isr_handler_add(KEY_BUTTON_GPIO, button_gpio_isr, (void *)(uintptr_t)BTN_KEY) != ESP_OK) {
        vTaskDelete(NULL); return;
    }
    bool stable[2] = {gpio_get_level(BOOT_BUTTON_GPIO) == 0, gpio_get_level(KEY_BUTTON_GPIO) == 0};
    bool both_timing = stable[0] && stable[1], triggered = false;
    TickType_t both_since = xTaskGetTickCount();
#if CONFIG_MONITOR_PM_PROFILE
    bool key_timing = stable[1] && !stable[0], key_triggered = both_timing;
    TickType_t key_since = both_since;
#endif
    configure_wake_levels(stable[0], stable[1]);
    uint32_t pending = 0;
    for (;;) {
        TickType_t wait = portMAX_DELAY;
        if (both_timing && !triggered) {
            TickType_t elapsed = xTaskGetTickCount() - both_since;
            TickType_t hold = pdMS_TO_TICKS(CONFIG_HOLD_MS);
            wait = elapsed >= hold ? 0 : hold - elapsed;
        }
#if CONFIG_MONITOR_PM_PROFILE
        if (key_timing && !key_triggered) {
            TickType_t elapsed = xTaskGetTickCount() - key_since;
            TickType_t hold = pdMS_TO_TICKS(CONFIG_HOLD_MS);
            TickType_t remaining = elapsed >= hold ? 0 : hold - elapsed;
            if (remaining < wait) wait = remaining;
        }
#endif
        uint32_t events = 0;
        xTaskNotifyWait(0, UINT32_MAX, &events, wait);
        pending |= events;
        process_manager_events(pending);
        pending &= BTN_BOOT | BTN_KEY;
        if (pending & (BTN_BOOT | BTN_KEY)) {
            vTaskDelay(pdMS_TO_TICKS(30));
            bool next[2] = {gpio_get_level(BOOT_BUTTON_GPIO) == 0, gpio_get_level(KEY_BUTTON_GPIO) == 0};
            stable[0] = next[0]; stable[1] = next[1];
            pending = 0;
            configure_wake_levels(stable[0], stable[1]);
            const bool both = stable[0] && stable[1];
            if (!both) { both_timing = false; triggered = false; }
            else if (!both_timing) { both_timing = true; both_since = xTaskGetTickCount(); }
#if CONFIG_MONITOR_PM_PROFILE
            if (both) { key_timing = false; key_triggered = true; }
            else if (!stable[1]) { key_timing = false; key_triggered = false; }
            else if (!key_timing && !key_triggered) { key_timing = true; key_since = xTaskGetTickCount(); }
#endif
        }
        if (both_timing && !triggered &&
            xTaskGetTickCount() - both_since >= pdMS_TO_TICKS(CONFIG_HOLD_MS)) {
            if (enter_config_mode() == ESP_OK) triggered = true;
        }
#if CONFIG_MONITOR_PM_PROFILE
        if (key_timing && !key_triggered &&
            xTaskGetTickCount() - key_since >= pdMS_TO_TICKS(CONFIG_HOLD_MS)) {
            key_triggered = true;
            monitor_pm_profile_key_hold();
        }
#endif
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
        esp_timer_delete(s_config_timeout_timer);
        s_config_timeout_timer = nullptr;
        return ESP_FAIL;
    }
    timer_args.callback = [](void *) {
        if (s_manager_task) xTaskNotify(s_manager_task, BTN_STATION_TIMEOUT, eSetBits);
    };
    timer_args.name = "station_connect_timeout";
    esp_err_t timer_err = esp_timer_create(&timer_args, &s_station_timeout_timer);
    if (timer_err != ESP_OK) {
        esp_timer_delete(s_config_timeout_timer);
        s_config_timeout_timer = nullptr;
        return timer_err;
    }
    if (xTaskCreate(button_task, "wifi_config_button", 4096, NULL, 4, &s_manager_task) != pdPASS) {
        esp_timer_delete(s_station_timeout_timer);
        esp_timer_delete(s_config_timeout_timer);
        s_station_timeout_timer = s_config_timeout_timer = nullptr;
        s_manager_task = nullptr;
        return ESP_ERR_NO_MEM;
    }
    s_started = true;
    if(SsidManager::GetInstance().GetSsidList().empty()) {
        ESP_LOGI(TAG, "no saved Wi-Fi credentials; entering esp-wifi-connect config mode");
        enter_config_mode();
    } else {
        ESP_LOGI(TAG, "saved Wi-Fi credentials found; starting esp-wifi-connect station");
        start_station_attempt();
    }
    return ESP_OK;
}

extern "C" monitor_wifi_state_t monitor_wifi_get_state(void) { return s_state; }
extern "C" bool monitor_wifi_is_provisioning(void) { return s_provisioning; }
