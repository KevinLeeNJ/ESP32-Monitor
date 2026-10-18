# ESP32-S3 Monitor

An ESP32-S3 RLCD 4.2 LVGL project with a deterministic desktop display
simulator for agent-assisted UI development.

## Desktop UI simulator

```bash
./simulator/run.sh
```

The command builds the simulator, runs unit and end-to-end tests, and renders
the default `monitor` UI to:

```text
simulator/output/latest/normal.png
```

The simulator is scene-aware and supports future LVGL UI screens:

```bash
./simulator/run.sh --list-scenes
./simulator/run.sh dashboard
```

Each scene produces a PNG preview, raw RGB565 frame, the exact RLCD packed 1bpp
frame, its SHA-256 digest, and JSON metadata. The PNG is reconstructed from the
packed frame, so it exposes panel polarity and packing behavior rather than
showing a raw LVGL screenshot.

To add a screen, implement it under `firmware/app/ui/`, expose a factory that receives
an LVGL parent, and register it in `simulator/src/scene_registry.c`. See
`simulator/README.md` for the complete workflow and artifact contract.

## Display contract

- Panel: 400 x 300 reflective LCD, 1 bit per pixel.
- LVGL: v9.4.0, RGB565 rendering.
- UI source: `firmware/app/ui/`.
- Frame packing: `firmware/src/display/monitor_pack.c`.
- Packed polarity: bit `1` is white and bit `0` is black.

The board pin mapping follows `firmware/Examples/09_LVGL_V9_Test`:

| Signal | GPIO |
|---|---:|
| MOSI | 12 |
| SCK | 11 |
| DC | 5 |
| CS | 40 |
| RST | 41 |
| TE | 6 |

## Build and flash firmware

Open the repository root in VS Code with the Espressif ESP-IDF extension
installed. The workspace settings keep the ESP-IDF build in
`firmware/build`, select the ESP32-S3 target from `firmware/sdkconfig.defaults`, and
use UART flashing. Select the board's serial port with **ESP-IDF: Select Port**
when it is connected, then use **ESP-IDF: Build your Project** and
**ESP-IDF: Flash your Project** (or **Build, Flash and Monitor**).

The extension settings are stored in `.vscode/settings.json`; the port remains
auto-detected so it is not tied to one computer's `/dev/cu.*` device name.

The equivalent command-line workflow is:

```bash
source ~/esp/esp-idf/export.sh
idf.py -C firmware set-target esp32s3
idf.py -C firmware -p /dev/cu.usbmodem1101 flash
```

The firmware renders the current monitor UI once and keeps it on screen.
Hardware flashing is not part of the simulator test loop.

## Tests

`./simulator/run.sh` runs both layers. To rerun tests after a build:

```bash
ctest --test-dir simulator/build --output-on-failure
```

Unit tests cover SHA-256, 1bpp pack/unpack, panel bit layout and polarity,
deterministic LVGL rendering, arbitrary scene factories, and PNG output. The
end-to-end test invokes the actual simulator CLI and validates every generated
artifact.

## Layout

```text
firmware/                       ESP-IDF project and monitor firmware sources
  app/ui/                shared LVGL UI screens
  assets/                UI and firmware assets
  include/               shared display interfaces
  src/display/           shared display conversion and rendering
  components/            ESP-IDF components
  main/                  ESP32-S3 application
  Examples/              board reference examples
simulator/                      desktop simulator, scenes, CLI and tests
third_party/lvgl/       pinned LVGL submodule (v9.4.0)
third_party/lv_conf.h   shared LVGL configuration
```

## Wi-Fi 配网

首次启动或没有已保存的 Wi-Fi 凭据时，固件会创建开放的配置热点 **ESP32-Monitor-XXXX**（`XXXX` 为设备 MAC 后缀，与 Waveshare 组件行为一致），默认地址为 `http://192.168.4.1/`。手机或电脑连接该热点后，固件会运行 Captive Portal：DNS 将配网热点内的域名解析到 `192.168.4.1`，并响应 iOS、Android、Windows 的网络探测 URL，因此系统通常会自动弹出“登录到 Wi-Fi”窗口并直接加载配网页面。若系统未自动弹出，也可以手动访问 `http://192.168.4.1/`。填写 Wi-Fi SSID 和密码并保存。设备会在保留配置热点的同时尝试连接目标网络；连接成功后关闭配置热点和网页服务，左上角 Wi-Fi 图标切换为已连接状态。连接失败时配置热点仍可访问，可重新提交凭据。

配网进入方式参考 Waveshare 固件：同时按住开发板 **BOOT（GPIO0）** 和 **KEY（GPIO18）** 约 3 秒，设备会主动进入配网模式；已连接 Wi-Fi 时也可以这样重新配网，原有凭据不会被立即擦除。松开按键后连接 `ESP32-Monitor-XXXX` 热点并访问上述地址即可。

凭据由 `esp-wifi-connect` 保存在 NVS 的 `wifi` 命名空间中。清除 NVS 或首次使用时会自动进入配网状态。运行时状态通过 `monitor_wifi_get_state()` 对接到 LVGL UI：配网中、已连接、未连接分别使用对应图标。

本项目直接使用 `78/esp-wifi-connect` 组件（版本约束 `~3.3.1`），由组件提供 Captive Portal、DNS、网页扫描和 NVS 多网络保存能力；项目只保留状态适配层和 GPIO0+GPIO18 长按进入配网逻辑。

## Weather location and data sources

The provisioning portal's **Advanced** tab accepts optional latitude and longitude values, plus a QWeather API Host and API key. Both coordinates can be left blank; after the station obtains Wi-Fi, the firmware queries `ipwho.is` to estimate the public-IP location. Weather condition and China AQI data are fetched from QWeather's `weather/now` and `air/now` APIs, using the API Host and API key stored in NVS. The weather service refreshes after Wi-Fi connection and every 15 minutes.

The local `firmware/components/esp_wifi_connect` copy contains the portal extension and persists the location in the `monitor_loc` NVS namespace. If configured coordinates are present, they take precedence over IP geolocation.


## LAN configuration page

After the device connects to the configured Wi-Fi network, it starts the same configuration page on the station interface. Find the IP address in the serial log (`LAN configuration page available at http://...`) or from the router's DHCP client list, then open `http://<device-ip>/` from another device on the same LAN. The configuration AP and captive DNS remain stopped after provisioning; the LAN page does not create a hotspot.
