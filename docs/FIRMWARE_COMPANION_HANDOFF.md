# 固件 Companion 交接

更新时间：2026-11-03。目标与协议以 [固件能力计划](FIRMWARE_CAPABILITIES_PLAN.md)、[Companion 协议](ESP32_COMPANION_PROTOCOL_V1.md) 和 [Server/Client 协议](SERVER_CLIENT_PROTOCOL_V2.md) 为准。测试结论只适用于记录的环境；历史细节见 git 记录。

## 当前状态

- **Standalone**：直接获取天气、AQI 和 Devices；Devices 保持 30 秒采集周期。天气连接失败最多尝试 3 次、间隔 1 秒；断网或 Companion 接管时取消后续重试，保留最后有效数据。
- **LAN**：发现、配对、认证 MQTT、Config、Snapshot、Heartbeat、ACK、Resync 和掉线恢复已实现，用户确认 LAN 模式可用。已配对设备自动恢复：单次探测 5 秒，退避 5/10/20/30/60 秒。板端使用 `WIFI_PS_MAX_MODEM`、`listen_interval=10`，约 1 秒一个 DTIM；发现列表主要面向未配对设备，已配对设备走自动重连。
- **Serial**：HELLO/AUTH、AES-256-GCM Config、ConfigAck、Snapshot、Heartbeat、ACK 和配对已与真实 Client 互通。串口优先于 LAN，一次配对共用两种传输；插拔自动切换已有实机证据。
- **配对与撤销**：LAN/Serial 显示相同六位码，板端 KEY 确认；BOOT 长按撤销只清除 Companion 配对，保留 Wi-Fi、天气和 Devices 配置。墓碑最多 4 条，仅保留派生 discovery key。
- **界面与来源**：断线回第一页并禁用翻页、串口 USB 图标、Codex/音乐显示及 KEY 翻页已由用户确认。音乐标题/歌手使用 MiSans，韩文与音符回退 Fusion Pixel。GitHub 真实数据、Serial 真实来源续租和音乐/Codex 长稳仍需补证。
- **Windows**：ESP-IDF 环境、生产固件构建和 COM10 烧录已验证；Windows Client 的 GUI、Agent、串口与 LAN **尚未真机验收**。
- **Linux**：AppImage 构建脚本、GTK4/WebKitGTK 6.0 GUI、XDG 自启、StatusNotifier 托盘、Unix socket IPC、MPRIS、原生采集和 Secret Service 已有实现；**尚无 Linux 真机验收**，差距见下。

## 已验证要点与限制

- **Devices 内存故障**：配置 Devices 后内部空闲仅 315 B、最大连续块 256 B，`httpd_start()` 失败后重启。当前源码让 LVGL 优先使用 PSRAM，并把配置 HTTP 启动/路由失败改为可恢复错误；诊断固件验证配置页可打开。生产模式、LAN 和 Serial 下仍需复测。
- **天气/DNS**：实板冷启动曾出现 `getaddrinfo() returns 202`，同一主机的 AQI 随后成功；证据指向路由器 DNS 的 UDP 路径不稳定，未抓包。固件已加入有界连接重试；无 Client 时连续 3 次启动 `weather=ok aqi=ok`，故障注入覆盖重试/取消路径。
- **内存**：内部 RAM 紧张，PSRAM 空闲不代表安全。Serial 栈 16 KB 不要收紧；常驻 MQTT worker 10 KB、Devices 8 KB、Discovery 16 KB。历史低水位：LAN/音乐约 17.3 KB，Serial/音乐字体约 13.2 KB；采样间短时低谷未排除。
- **音乐字体**：18px 标题、12px 歌手，MiSans Regular 主字体，韩文与 `♪/♫` 由 Fusion Pixel 回退；18px 拆主字体 + fallback，12px 合并。`generate_music_font.py` 需 `--misans`，资源用 `#embed` 和 8 字节描述表。模拟器 `activity-long-music`/`activity-cjk-music` 已目视检查。
- **Client 修复**：Agent 退出改为有界等待（5 秒），避免协程卡住；LAN 恢复探测 5 秒并加快退避；天气代理记录 `proxy_worker_start` / `proxy_weather_result`（不含 Host/Key）。
- **自动化**：模拟器 `ctest` **34/34**；ESP-IDF 生产构建成功，镜像 4,294,416 B，8 MB app 分区剩余 49%；Client 完整 transport 集成测试通过，`make test-fast` 约 21 秒。Windows 固件主机套件仍受 POSIX 头文件限制；模拟器结论不代表硬件。
- **未验证**：新字体、LAN 恢复参数、天气代理日志和 Agent 退出修复均需实板/长时间复测；Linux 尚未构建或运行验收。

## Linux Client 差距审计（2026-11-03）

Linux 并非从零开始：MPRIS 音乐、原生采集（`/proc`、`/sys`、gopsutil、UDisks2、UPower、nvtop）、Secret Service、XDG 自启、AppImage 桌面集成、Unix socket `SO_PEERCRED` 和 managed Agent 均已有实现。主要缺口如下：

| 优先级 | 领域 | Windows / macOS | Linux 现状与缺口 | 建议 |
|---|---|---|---|---|
| P0 | 构建与运行 | Windows WebView2；macOS WKWebView | GTK4 + WebKitGTK 6.0；AppImage 在 Debian 13/glibc 2.41 构建，Ubuntu 24.04（glibc 2.39）可能无法运行 | 在最低支持发行版或 Flatpak runtime 构建；明确最低 glibc/GTK/WebKit 版本 |
| P0 | CI 与验收 | 手工 Windows/macOS 打包 | 无 `.github` CI，无 Linux AppImage 冒烟测试 | 增加 Ubuntu 24.04 CI：`make test-fast`、AppImage 构建、xvfb/WebKit 启动检查 |
| P0 | 运行时依赖 | WebView2 运行时可安装；macOS 自带 | 需 GTK4、WebKitGTK 6.0、bubblewrap、xdg-dbus-proxy；容器/未启用 user namespace 时沙箱启动失败 | 启动前检测并给出可操作提示；完善 README/AppImage 提示 |
| P0 | Agent 权限与采集 | 提权 Agent + LHM/PawnIO/WMI | Linux 为同用户 managed Agent，无提权 helper；SMART 依赖 UDisks2、电量依赖 UPower、GPU 仅 `nvtop`（NVIDIA） | 明确各指标的宿主依赖；增加缺失工具提示；评估 AMD/Intel GPU 与风扇/电压 |
| P0 | 串口 | COM 端口即插即用 | `/dev/ttyACM*`/`ttyUSB*` 需 `dialout` 权限；无 udev 规则 | 检测权限失败并提示 `dialout`/udev；文档化 |
| P1 | GitHub 凭据 | Credential Manager / Keychain | 依赖已解锁的 Secret Service；无文件回退，headless 环境不可用 | 增加明确错误提示；评估加密文件回退 |
| P1 | 系统代理 | WinINET / `scutil` | 仅 GNOME `gsettings` + 环境变量；KDE/其他桌面不读取系统代理 | 读取 KDE/portal 配置或明确只支持 GNOME |
| P1 | 托盘 | 原生通知区/菜单栏 | Wails StatusNotifierItem；GNOME 需 AppIndicator 扩展，否则无托盘 | 文档化；无托盘时保留窗口/自启路径 |
| P1 | Codex Desktop | Windows 命名管道；macOS Unix socket | Unix socket/线程发现已实现；取决于 Codex Desktop 是否发布 Linux 版，CLI 路径可用 | 以 Codex CLI 为主路径验收；Desktop 观测标注依赖 |
| P1 | 打包 | Inno Setup / .app（ad-hoc 签名） | 仅 AppImage；无 deb/rpm/Flatpak、无签名/更新机制 | 增加 Flatpak 或 deb；明确签名/更新策略 |
| P2 | 测试 | 有平台测试与真机验收 | MPRIS 测试需 `dbus-run-session`；原生采集只有解析测试，无 live probe；无 Linux 真机证据 | 增加 Linux live collector/MPRIS/串口测试与真机清单 |

**Linux 开发环境要点**：Go、Node.js、pkg-config、GTK4/WebKitGTK 6.0 开发包、appimagetool；测试 MPRIS 需 `dbus-run-session`；采集验证需 `udisks2`、`upower`、`nvtop`。AppImage 构建使用 `make build-appimage-linux`。

## 下一步（Linux 优先）

1. **P0 Linux 构建/CI**：在 Ubuntu 24.04 建立可重复构建，跑 `make test-fast` 和 AppImage 启动冒烟；确定 glibc/GTK/WebKit 支持范围。
2. **P0 依赖与权限**：对 GTK/WebKit、bubblewrap、xdg-dbus-proxy、UDisks2、UPower、nvtop、Secret Service、串口 `dialout` 做启动检测和可操作提示。
3. **P0 采集与音乐验收**：在真实 Linux 桌面验证 CPU/内存/磁盘/网络/电量/GPU、MPRIS 播放与串口/LAN；补 live collector 测试。
4. **P1 平台覆盖**：KDE/非 GNOME 代理、无托盘桌面、无 Secret Service 的降级路径；评估 Flatpak/deb。
5. **固件遗留**：天气 DNS 抓包与实板重试；Devices 内存修复在 LAN/Serial/生产模式复测；LAN 异常、UI 来源续租、72 小时长稳；Windows 真机验收按需恢复。

## 开发约束与定位

- LAN：`firmware/src/companion/monitor_lan_session.c`、`monitor_mqtt_wire.c`；MQTT worker：`firmware/components/firmware/monitor_companion_mqtt.c`。Client：`client/internal/transport/{discovery,lan_e2e,proxy}.go`。
- Serial：`firmware/components/firmware/monitor_serial_service.c`、`monitor_serial_crypto.c`；Client：`client/internal/transport/serial*.go`。串口前确认端口/占用，避免拉高 DTR/RTS；Windows 下可用抢占 COM 的方式在 LAN 会话中读板端日志。
- 天气/网络：`monitor_weather.c`、`monitor_wifi_manager.cpp`；Client 代理 `transport/proxy.go`。Devices 使用设备配置的 HTTP/HTTPS 地址，无需额外授权。
- 界面：`firmware/src/companion/monitor_companion_view.c`、`firmware/app/ui/monitor_activity.c`、`monitor_pages.c`；音乐字体 `firmware/app/ui/fonts/monitor_music_*.c`。
- 共享 UI 同时供固件和模拟器编译；新增共享源文件须更新 `firmware/components/monitor_core/CMakeLists.txt` 和 `simulator/CMakeLists.txt`。保留 400×300、1 bpp、15,000 B、1 白 0 黑。
- 开始前检查 `git status --short` 与相关 diff；不要批量清理、提交敏感 NVS/凭据或原始二进制帧。

## 验证入口

```sh
./firmware/tests/run.sh
./simulator/run.sh companion-live
./simulator/run.sh companion-update
ctest --test-dir simulator/build --output-on-failure
cd client && make test && make test-fast
cd client && make build-appimage-linux
```

```sh
source ~/esp/esp-idf/export.sh
idf.py -C firmware build
idf.py -C firmware -p /dev/ttyACM0 flash  # 先确认端口和占用
```

诊断重点：`companion poll ... int_free ... int_largest ... psram_free`、`companion view overlay/proxy/weather/aqi`、`proxy_worker_start/result`、`lan_recovery_connected`、`agent_quit_failed/unreachable`、`monitor_weather` 的 DNS/重试结果。日志不得包含 token、serial key、配对 ticket、Wi-Fi 密码或 NVS/凭据备份。OTA、NVS/Flash 加密和自动修改安全启动/eFuse 不在本轮。
