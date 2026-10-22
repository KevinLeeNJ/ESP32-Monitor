# ESP32-S3 Monitor

基于 ESP32-S3 的监控终端，配备 400 × 300 反射式液晶屏（RLCD）和 LVGL 界面。桌面模拟器直接渲染与固件共用的 UI 代码。

[English](README.md)

## 项目功能

- ESP32-S3 固件与桌面模拟器共用 LVGL v9.4 UI。
- 1 位 RLCD 输出，像素布局和极性有明确约定。
- 通过 Captive Portal 配置 Wi-Fi，并可在局域网访问配置页面。
- 可选接入和风天气及空气质量数据。
- 模拟器支持场景渲染，并输出面板帧和机器可读元数据。

## 快速开始：桌面模拟器

在仓库根目录运行以下命令，构建模拟器、运行测试并渲染默认的 `monitor` 场景：

```sh
./simulator/run.sh
```

主预览图位于 `simulator/output/latest/normal.png`。查看场景列表或渲染指定场景：

```sh
./simulator/run.sh --list-scenes
./simulator/run.sh dashboard
```

每次渲染都会生成由 RLCD 打包帧重建的 PNG、原始 RGB565 数据、1bpp 打包帧、SHA-256 摘要和 JSON 元数据。场景开发、CLI、产物格式和测试说明见[模拟器指南](simulator/README.zh_CN.md)。

## 构建并烧录固件

安装面向 ESP32-S3 的 ESP-IDF，并使用安装了 Espressif ESP-IDF 扩展的 VS Code 打开仓库；也可以使用命令行：

```sh
source ~/esp/esp-idf/export.sh
idf.py -C firmware set-target esp32s3
idf.py -C firmware build
idf.py -C firmware -p PORT flash monitor
```

将 `PORT` 替换为开发板串口。固件和模拟器分别构建，固件产物位于 `firmware/build/`。此处的固件工作流用于构建和烧录开发板，模拟器用于桌面渲染。

## 显示屏与开发板

| 属性 | 规格 |
|---|---|
| 面板 | 400 × 300 反射式液晶屏，1 bit/pixel |
| LVGL 渲染格式 | RGB565 |
| 打包极性 | `1` = 白色，`0` = 黑色 |
| UI 源码 | `firmware/app/ui/` |
| RGB565 到 RLCD 转换 | `firmware/src/display/monitor_pack.c` |

GPIO 映射参考 `firmware/Examples/09_LVGL_V9_Test`：

| 信号 | GPIO |
|---|---:|
| MOSI | 12 |
| SCK | 11 |
| DC | 5 |
| CS | 40 |
| RST | 41 |
| TE | 6 |

## Wi-Fi 与天气配置

首次启动或尚无已保存的 Wi-Fi 凭据时，设备会创建 `ESP32-Monitor-XXXX` 配网热点。连接热点后访问 `http://192.168.4.1/`。如需再次进入配网模式，同时按住 BOOT（GPIO0）和 KEY（GPIO18）约 3 秒。

设备连接 Wi-Fi 后，也会在局域网上提供配置页面。可从串口日志或路由器 DHCP 客户端列表查看设备 IP，再访问 `http://<device-ip>/`。

配网页面的 Advanced 选项卡支持填写经纬度、和风天气 API Host 与 Key。经纬度留空时，固件通过 `ipwho.is` 估算位置。天气和 AQI 数据在 Wi-Fi 连接后刷新，此后每 15 分钟刷新一次。Telegraf 主机监控见[客户端指南](client/README.zh_CN.md)，NAS 服务见[服务端指南](server/README.zh_CN.md)。

## 仓库结构

| 路径 | 内容 |
|---|---|
| `firmware/app/ui/` | 共用的 LVGL 界面 |
| `firmware/components/firmware/` | ESP32-S3 驱动与服务 |
| `firmware/components/esp_wifi_connect/` | Wi-Fi 配网组件和网页 |
| `firmware/src/display/` | 共用渲染和 RLCD 转换逻辑 |
| `simulator/` | 桌面渲染器、场景、CLI 和测试 |
| `client/` | Telegraf 配置模板与部署指南 |
| `server/` | Go 服务、配置与开发指南 |
| `third_party/lvgl/` | 固定版本的 LVGL 子模块（v9.4.0） |

## 相关指南

- [桌面模拟器](simulator/README.zh_CN.md)
- [Telegraf 客户端配置](client/README.zh_CN.md)
- [Monitor Server](server/README.zh_CN.md)
- [Wi-Fi 配网组件](firmware/components/esp_wifi_connect/README.zh_CN.md)
- [天气图标资源](firmware/assets/weather/README.zh_CN.md)
- [指标图标资源](firmware/assets/metrics/README.zh_CN.md)
