# 固件 Companion 交接

更新时间：2026-11-04。目标和协议以 [固件能力计划](FIRMWARE_CAPABILITIES_PLAN.md)、[Companion 协议](ESP32_COMPANION_PROTOCOL_V1.md) 和 [Server/Client 协议](SERVER_CLIENT_PROTOCOL_V2.md) 为准；历史细节见 git 记录。

## 当前状态

- **Standalone**：直接获取天气、AQI 和 Devices，Devices 每 30 秒采集。天气连接失败最多尝试 3 次、间隔 1 秒；断网或 Companion 接管时取消后续重试，保留最后有效数据。
- **LAN / Serial**：发现、配对、认证、Config、Snapshot、Heartbeat、ACK、Resync 和恢复已实现并与真实 Client 互通。Serial 优先于 LAN，两种传输共用配对，插拔切换已实测。LAN 探测 5 秒，退避 5/10/20/30/60 秒；断网恢复已实测。板端使用 `WIFI_PS_MAX_MODEM`、`listen_interval=10`；发现列表主要面向未配对设备。
- **配对与界面**：六位码由 KEY 确认；BOOT 长按只撤销 Companion 配对，保留 Wi-Fi、天气和 Devices 配置。墓碑最多 4 条，只保留派生 discovery key。断线回第一页并禁用翻页、USB 图标、Codex/音乐显示和 KEY 翻页已确认。Serial 音乐续租连续 5 分钟、暂停保留及停止清空均已实测；长稳仍待验证。
- **Linux Client**：Debian 13 / KDE Wayland 的 A1–A7 真机验收已通过，覆盖桌面集成、采集、MPRIS、GitHub、Codex 及串口/LAN 配对恢复。Ubuntu 24.04 容器发布基线、AppImage、Xvfb 冒烟和 `make ci-linux` 已通过。GTK/WebKit 缺失和沙箱降级提示、GNOME 托盘、KDE 代理及无可用 keyring 场景仍待验收。
- **Client 修复**：Agent 退出等待上限 5 秒；托盘和第二实例回调派发至 GTK 主循环，并用单实例锁拉起已有窗口；配对存储保留最近在线时间。天气代理日志不包含 Host/Key。
- **Windows**：ESP-IDF 构建及 COM10 烧录已验证；Client GUI、Agent、串口与 LAN 尚未真机验收。

## 硬件验证与风险

- **Devices 内存（已实测）**：曾出现内部空闲 315 B、最大连续块 256 B，导致配置 HTTP 启动失败并重启。现有源码让 LVGL 优先使用 PSRAM，HTTP 启动/路由失败可恢复；诊断固件验证配置页可打开。2026-11-04 使用本地四设备摘要模拟服务，实板显示四张卡片后日志为内部空闲 **57,519 B**、最大连续块 **31,744 B**、PSRAM 空闲 **4,015,520 B**。这是单次快照，不代表所有并发场景的最低水位。
- **内存约束**：PSRAM 空闲不代表内部 RAM 安全。Serial 栈 16 KB 不要收紧；MQTT worker 10 KB、Devices 8 KB、Discovery 16 KB。历史低水位：LAN/音乐约 17.3 KB，Serial/音乐字体约 13.2 KB，采样间短时低谷未排除。
- **电池 ADC**：采样和 eFuse 曲线校准均使用 6 dB，三倍分压换算。实板一次采样 ADC 原始值 3064、引脚 1.389 V、电池 4.167 V；万用表 4.14 V，相差 27 mV。尚未验证多电压点或施加软件补偿。
- **天气/DNS**：冷启动曾出现 `getaddrinfo() returns 202`，证据指向路由器 DNS 的 UDP 路径不稳定，尚未抓包。固件已有有界重试；无 Client 时连续 3 次启动天气/AQI 成功，故障注入覆盖重试和取消。
- **音乐字体**：标题 18px、歌手 12px，MiSans 主字体，韩文及 `♪/♫` 使用 Fusion Pixel 回退。长中文、英文、韩文和音符已实板检查；字体资源使用 `#embed`，生成脚本需要 `--misans`。
- **构建和测试**：2026-11-04 ESP-IDF 生产固件构建成功；模拟器/固件主机 35 项测试分别通过，分段进度条正常/反色等场景与修改前逐像素一致。主机 GCC 14 不支持字体的 `#embed`；本次测试临时展开资源，且 `monitor_pm_profile_tests` 单独启用 `_GNU_SOURCE`。固件构建临时修复了已有的 LVGL `Kconfig` 断链，之后已恢复。Windows 固件主机套件仍受 POSIX 头文件限制。

## 待办

1. **固件**：生产 Server 卡片（B7）；天气重试和代理日志（B2）；音乐/Codex 72 小时长稳（B6）。Devices 内存（B1）及 B3/B4/B5 已实板验证。
2. **Linux**：验证缺库、沙箱、无托盘和无 Secret Service 的降级路径；检查串口 `dialout`/udev 提示。KDE 代理、AMD/Intel GPU、风扇/电压、live collector 自动化仍有缺口。SMART 依赖 UDisks2、电量依赖 UPower、NVIDIA GPU 依赖 `nvtop`；Secret Service 需解锁。
3. **发布**：按需接入托管 CI，补 AppImage 签名/更新并评估 Flatpak/deb；当前 Linux 基线为 Ubuntu 24.04+/Debian 13+，不支持 Ubuntu 22.04/Debian 12。Windows 真机验收按需进行。

## 定位与验证

- LAN：`firmware/src/companion/monitor_lan_session.c`、`monitor_mqtt_wire.c`、`firmware/components/firmware/monitor_companion_mqtt.c`；Serial：`monitor_serial_service.c`、`monitor_serial_crypto.c`；Client：`client/internal/transport/`。
- 天气和网络：`firmware/components/firmware/monitor_weather.c`、`monitor_wifi_manager.cpp`，Client 代理 `client/internal/transport/proxy.go`。Devices 读取配置的 HTTP/HTTPS 地址。
- 共享界面：`firmware/app/ui/`、`firmware/src/companion/monitor_companion_view.c`。新增共享源须同步固件和模拟器 CMake；保留 400×300、1 bpp、15,000 B、1 白 0 黑。
- 常用命令：`./firmware/tests/run.sh`、`./simulator/run.sh companion-live`、`./simulator/run.sh companion-update`、`ctest --test-dir simulator/build --output-on-failure`；Client 使用 `make test`、`make test-fast`、`make ci-linux`。
- 固件：先执行 `source ~/esp/esp-idf/export.sh`，再执行 `idf.py -C firmware build`；烧录或读取串口前确认端口和占用，避免无意复位或中断 LAN 会话。Windows Client 会自动抢占已配对串口，读取 LAN 日志时需按仓库指引处理 DTR/RTS。
- 诊断关注 `int_free`、`int_largest`、`psram_free`、天气 DNS/重试及 Companion/代理日志。日志和提交不得包含 Token、配对密钥、Wi-Fi 密码或 NVS 备份。开始前检查工作区状态，勿清理不相关修改。
