# ESP-WiFi-Connect

ESP-WiFi-Connect 管理 Station 连接，并提供用于配置 Wi-Fi 凭据的网页。本项目使用该组件的本地副本并包含针对应用的扩展；设备进入配网模式的方式见仓库根目录的中文 [README](../../../README.zh_CN.md)。

[English](README.md)

组件会优先使用已保存的凭据，也可以启动配网热点和网页服务，默认地址为 `http://192.168.4.1/`。

## 配网页面

下图为 v3.2 配网页面：

<img src="assets/ap_v3_2.jpg" width="320" alt="Wi-Fi 配网页面 v3.2">

## 更新记录

### v3.3.1

- 将 SSID 和密码复制到 `wifi_config` 中，避免使用 `strcpy` 时完整 32 字节 SSID 导致缓冲区溢出。
- 配网页面和 SmartConfig 支持最长 SSID，避免截断或越界读取。

### v3.3.0

- 将每个已保存 AP 的信道持久化到 NVS，键名为 `channel`、`channel1` … `channel9`，索引与 `password` / `passwordN` 相同。2.4 GHz（1–14）和 5 GHz（36–177）信道号不重叠，因此共用 `uint8` 键。
- Station 启动后的首次扫描只访问已保存的信道；未找到匹配 AP 时再执行全频段扫描。
- 连接成功后保存当前信道，以便后续启动时使用最新信息。

### v3.2.0

- Station 模式连接同名 SSID 中信号最强的 AP。`StartConnect()` 在 Station 配置中设置 `WIFI_ALL_CHANNEL_SCAN` 和 `WIFI_CONNECT_AP_BY_SIGNAL`，由驱动选择信号最强的 AP，而非默认的首个匹配 AP（旧默认值为 `WIFI_FAST_SCAN`）。
- 在 `WifiManagerConfig` 中新增 `station_failure_retry_cnt`（默认值 3）。驱动在回退到同名 SSID 的较弱 AP 前，会重试连接信号最强的 AP，以避免一次临时认证失败就触发回退。
- 在 `WifiManagerConfig` 中新增 `show_ota_config` 和 `show_sleep_config`，默认均为 `false`，用于控制是否在 Advanced 页面显示 OTA URL 和 Sleep Mode 选项。

### v3.1.0

- 事件回调新增 `data` 参数，用于传递附加信息。
- 断开事件通过 `data` 参数提供断开原因码。

### v3.0.0

- 新增 `WifiManager` 类，统一管理 Wi-Fi 连接。
- 改进 `DnsServer` 和 `WifiConfigurationAp` 的资源管理。
- 配置成功提示改为使用退出端点，而不是重启设备。
- 改进错误处理、状态管理和线程安全，并清理未使用代码。

### v2.6.0

- 支持 ESP32-C5 5G 模式。

### v2.4.0

- 新增日语和繁体中文语言。
- 新增 Advanced 页面。
- 添加 `Connection: close` 响应头以减少保持打开的套接字。

### v2.3.0

- 支持按语言请求页面。

### v2.2.0

- 支持 ESP32 SmartConfig（ESPTouch v2）。

### v2.1.0

- 改进 Wi-Fi 连接逻辑。

### v2.0.0

- 支持保存多个 Wi-Fi SSID，并自动切换到可用网络中信号较强的 AP。
- 新增 Captive Portal 配网和中英文界面。

## 配置

Wi-Fi 凭据保存在 NVS 的 `wifi` 命名空间中。

键名为 `ssid` 至 `ssid9`、`password` 至 `password9`，以及 `channel` 至 `channel9`。信道使用 `uint8`：`0` 表示未知，`1–14` 表示 2.4 GHz，`36–177` 表示 5 GHz。

## 集成示例

```cpp
#include <wifi_manager.h>
#include <ssid_manager.h>

// Initialize the default event loop
ESP_ERROR_CHECK(esp_event_loop_create_default());

// Initialize NVS flash for Wi-Fi configuration
esp_err_t ret = nvs_flash_init();
if (ret == ESP_ERR_NVS_NO_FREE_PAGES || ret == ESP_ERR_NVS_NEW_VERSION_FOUND) {
    ESP_ERROR_CHECK(nvs_flash_erase());
    ret = nvs_flash_init();
}
ESP_ERROR_CHECK(ret);

// Get the WifiManager singleton
auto& wifi_manager = WifiManager::GetInstance();

// Initialize with configuration
WifiManagerConfig config;
config.ssid_prefix = "ESP32";  // AP mode SSID prefix
config.language = "zh-CN";     // Web UI language

// Optional: tune connection-to-strongest-AP behavior
// config.station_failure_retry_cnt = 3;  // retries before falling back to weaker AP

// Optional: show advanced fields in the config portal
// config.show_ota_config = true;    // show Custom OTA URL input
// config.show_sleep_config = true;  // show Sleep Mode toggle

wifi_manager.Initialize(config);

// Set event callback to handle WiFi events
// The callback receives the event type and optional data (e.g., disconnect reason)
wifi_manager.SetEventCallback([](WifiEvent event, const std::string& data) {
    switch (event) {
        case WifiEvent::Scanning:
            ESP_LOGI("WiFi", "Scanning for networks...");
            break;
        case WifiEvent::Connecting:
            ESP_LOGI("WiFi", "Connecting to network...");
            break;
        case WifiEvent::Connected:
            ESP_LOGI("WiFi", "Connected successfully!");
            break;
        case WifiEvent::Disconnected:
            // data contains the disconnect reason code
            ESP_LOGW("WiFi", "Disconnected from network, reason: %s", data.c_str());
            break;
        case WifiEvent::ConfigModeEnter:
            ESP_LOGI("WiFi", "Entered config mode");
            break;
        case WifiEvent::ConfigModeExit:
            ESP_LOGI("WiFi", "Exited config mode");
            break;
    }
});

// Check if there are saved Wi-Fi credentials
auto& ssid_list = SsidManager::GetInstance().GetSsidList();
if (ssid_list.empty()) {
    // No credentials saved, start config AP mode
    wifi_manager.StartConfigAp();
} else {
    // Try to connect to the saved Wi-Fi network
    wifi_manager.StartStation();
}
```

更多用法见 [78/xiaozhi-esp32](https://github.com/78/xiaozhi-esp32)。
