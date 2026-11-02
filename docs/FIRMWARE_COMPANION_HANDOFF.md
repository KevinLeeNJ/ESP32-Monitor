# 固件 Companion 交接

更新时间：2026-11-02。目标和协议以 [固件能力计划](FIRMWARE_CAPABILITIES_PLAN.md) 与 [Companion 协议](ESP32_COMPANION_PROTOCOL_V1.md) 为准。本文保留当前状态、验收证据和下一步；测试结果只适用于所记录的构建与环境。

## 当前状态

- **Standalone**：直接获取天气、AQI 和 Devices；Devices 保持 30 秒采集周期。天气连接失败最多尝试 3 次、间隔 1 秒；断网或 Companion 接管时取消后续重试，保留最后有效数据。
- **LAN**：发现、配对、认证 MQTT、Config、Snapshot、Heartbeat、ACK、Resync 和掉线恢复已实现；真实 Client 与板端曾连接，天气/AQI 代理有效。认证会话暂停直接天气和 Devices；断开后恢复。Client 是 TCP 18883 broker，发现使用 UDP 18444；LAN 传输无 TLS，首次 LAN 配对需确认明文风险。
- **Serial**：HELLO/AUTH、AES-256-GCM Config、ConfigAck、Snapshot、Heartbeat、ACK 和配对已与真实 Client 互通。认证后等待 ConfigAck 和首个心跳，再关闭 Wi-Fi/AP/DNS；退出后恢复。串口优先于 LAN，一次配对共用两种传输；插拔自动切换已有实机证据。
- **配对与撤销**：LAN/Serial 显示相同六位码，板端 KEY 确认；BOOT 长按撤销只清除 Companion 配对，保留 Wi-Fi、天气和 Devices 配置。双向撤销和无活动会话撤销感知已实机验证；墓碑最多 4 条，仅保留派生 discovery key，不保存 token/serial key。
- **界面与来源**：断线回第一页并禁用翻页、串口 USB 图标、Codex/音乐显示及 KEY 翻页已由用户确认。心跳按来源续租；GitHub 真实数据、Serial 真实来源续租和修复后的音乐/Codex 长稳仍需补证。时间优先级为可信 RTC、NTP、Client。
- **Windows**：已验证 ESP-IDF 环境、生产固件构建和 COM5 烧录；Windows Client 的 GUI、Agent、串口与 LAN **尚未真机验收**。

## 最近修复和验证

### Standalone 天气与 DNS

实板冷启动曾出现天气 DNS `getaddrinfo() returns 202` / `ESP_ERR_HTTP_CONNECT`，随后同一主机的 AQI 成功。板端 DHCP DNS 为 `192.168.110.1`；Mac 经 en0 直接查询同一 DNS，在 1 秒超时的无 EDNS 小样本中 UDP 8 次有 4 次超时，TCP 3 次均成功；公共 DNS UDP 4 次均成功。证据指向当前路由器 DNS 的 UDP 路径不稳定，但未抓包，不能确定是转发器、上游、网络丢包还是本机拦截，也不能将小样本视为长期丢包率。

固件已加入上述有界连接重试，保留 DHCP DNS、TLS 校验及 Wi-Fi 省电策略。最终生产固件烧录并完成哈希校验；无 Client 时连续 3 次启动 `weather=ok aqi=ok`。三次未触发应用层重试，失败恢复和取消路径由故障注入测试验证，尚无实板强制故障证据。天气/Wi-Fi/LAN/Serial 相关测试 **7/7** 通过。

### 内存

Serial 编码及 Config 中间缓冲、天气响应、MQTT RX/Config 和会话对象优先 PSRAM，失败回退；最终 USB 帧、显示 DMA staging 和任务栈保留现有策略。依据 [ESP-IDF 外部 RAM 指南](https://github.com/espressif/esp-idf/blob/master/docs/en/api-guides/external-ram.rst)。活动构建 `firmware/sdkconfig` 中普通分配内部优先阈值为 8192 B、mbedTLS 使用外部内存、Wi-Fi 静态 RX 为 2；当前源码 Serial 栈 16 KB、常驻 MQTT worker 栈 12 KB。worker 同时服务配对与 LAN，避免运行时申请大块连续内部栈；不要未经验证缩栈。

| 场景 | 三轮最低内部空闲 | 最大连续块 | 口径 |
|---|---:|---:|---|
| LAN，音乐/Codex | 17,255 B | 7,680 B | 约 10 秒一次的连接态采样 |
| Serial，音乐字体已加载 | 13,191 B | 7,680 B | 会话内最低空闲；连续块按关键事件采样 |

三轮均完成认证与重连；不能排除采样间短时低谷。LAN 的 TLS 瞬时最低值和长期碎片仍未测。旧实测 Wi-Fi RX 为 16、历史 MQTT 栈记录为 8 KB，与当前构建不一致，不能用两轮差值计算精确节省。Serial 后重新打开日志串口曾观察到启动日志，随后 LAN 恢复；该次不能作为无复位切换验收。最终天气修复固件 SHA-256：`9429d7f2b1dd6a3e72acebcd57027757492a22ab5e713c964481cfe8c58d3c40`。

### 自动化结果

- ESP-IDF 生产构建和实板烧录最终校验通过。
- 内存优化轮次固件主机与模拟器测试均为 **34/35**；随后为 `monitor_clock_client_time_tests` 夹具补齐 `NULL` / `time_t` 所需标准头文件，固件主机套件重新运行 **35/35 通过**。天气修复轮次相关测试 7/7 通过。
- Client 早前 `go test -race ./...`、`go vet ./...` 与前端 13 项测试通过；这些不是 Windows 真机证据。
- 模拟器 `companion-live` 曾单独渲染并目视检查；上述夹具修复后未重新运行模拟器套件。

## 待办与验收边界

1. **天气/DNS**：在路由器或板端抓包定位 DNS UDP 失败；验证连续失败后的实板重试和恢复，不泄露 API key。检查路由器转发/上游及 UDP 53，再决定 DHCP 备用 DNS。
2. **内存与切换**：增加 LAN 和 TLS 阶段低水位采样；覆盖无复位 Serial→LAN、直连天气恢复、反复重连及音乐字体加载/未加载。保留 30 秒 Devices 周期。
3. **LAN 异常**：实机覆盖 Client Error、旧 session 隔离、重复发现、慢速消费与 Resync 丢快照；90 秒无有效应用回执过期和 Agent 恢复重连已有实机证据。MQTT PUBACK/PINGRESP 不等于应用存活。
4. **真实 UI/来源**：实板验证未配对 Serial 弹窗的 BOOT 取消、GitHub 真实数据与 Serial 来源续租；验证端口改名和重新配对后的 GUI 行刷新。真实 KEY 确认、BOOT 长按撤销及重新配对已有实机证据。
5. **功耗与长稳**：覆盖主机休眠、重连、撤销、反复插拔、HPM/LPM 和异常恢复的 72 小时长稳；Serial 功耗须 USB 断开，在电池接口进行多分钟对比。主机/USB 日志不能证明功耗改善。
6. **Windows P0/P1**：按下方清单在真实 Windows 机器验收；固件 COM5 烧录不能替代 Client 验收。

OTA 不在本轮。NVS/Flash 加密未启用；不要将凭据描述为硬件加密存储，也不要自动修改安全启动或 eFuse。日志、截图与文档不得包含 token、serial key、配对 ticket、Wi-Fi 密码或 NVS/凭据备份。

## 开发约束与定位

- LAN 会话：`firmware/src/companion/monitor_lan_session.c`、`monitor_mqtt_wire.c`；MQTT worker：`firmware/components/firmware/monitor_companion_mqtt.c`。
- Serial：`firmware/components/firmware/monitor_serial_service.c`、`monitor_serial_crypto.c` 和 `firmware/src/companion/monitor_serial_*`。认证后 USB 仅承载协议帧；最终帧按实际长度优先分配内部 RAM，避免已观察到的 PSRAM TX 损坏。
- 天气/网络：`firmware/components/firmware/monitor_weather.c`、`monitor_wifi_manager.cpp`；Client 代理：`client/internal/transport/proxy.go`。Devices 私网 URL 须与 Client 明确授权值逐字匹配，不可默认放行。
- 配对/发现：`firmware/components/firmware/monitor_pairing_*`、`firmware/src/companion/monitor_pairing_*`、`monitor_discovery*`；Client 对应 `client/internal/transport/discovery.go`、`pairing.go`、`session.go`。
- 界面：`firmware/src/companion/monitor_companion_view.c`、`firmware/app/ui/monitor_activity.c`、`monitor_pages.c`、`monitor_pairing_ui.c`。字体使用 `firmware/assets/fonts/music-cjk.bin` 动态加载。
- 共享 UI 同时供固件和模拟器编译；添加共享源文件须更新 `firmware/components/monitor_core/CMakeLists.txt` 和 `simulator/CMakeLists.txt`。保留 400×300、1 bpp、15,000 B、1 白 0 黑的显示契约。
- 开始前检查 `git status --short` 与相关 diff，保留现有未提交改动；不要批量清理或暂存敏感/本地材料。串口连接前确认实际端口及占用，避免会拉高 DTR/RTS 的工具；USB CDC 可能重新枚举。

## 验证入口

```sh
./firmware/tests/run.sh
./simulator/run.sh companion-live
./simulator/run.sh companion-update
ctest --test-dir simulator/build --output-on-failure
cd client && go test -race ./... && go vet ./...
```

```sh
source ~/esp/esp-idf/export.sh
idf.py -C firmware build
idf.py -C firmware -p /dev/cu.usbmodem1101 flash  # 先确认端口和占用
```

诊断重点：`companion poll ... int_free ... int_largest ... psram_free`、`heap worker start/end/freed`、`serial heap close`、`companion view overlay/proxy/weather/aqi`、`session ended`、`monitor_weather` 的 DNS/重试结果。Serial 堆汇总在 Goodbye 前输出，Client 作为噪声丢弃；不要保存原始二进制帧或秘密。生产固件和独立设备测试固件不能同时驻留；运行后者后应恢复生产固件。

## Windows 后续验收

新 Windows 主机是新 Client 身份，新配对会替换旧配对并产生墓碑；不要默认复制 `companion.json`。保留板端 Wi-Fi、天气与 Devices 配置。需要 Windows 10/11 x64、WebView2、.NET Framework 4.7.2+；构建还需要 Go、Node、.NET SDK、MSYS2 UCRT64 `windres`，Inno Setup 可选。以当前仓库依赖和脚本为准。

| 优先级 | 验收内容 |
|---|---|
| P0 构建 | 前端、`go test ./...`、`go vet ./...`、Windows 打包；验证产物及 LHM 助手。没有 cgo/gcc 时不宣称 race 已通过。 |
| P0 进程/权限 | 普通 GUI + 提权 Agent 一次 UAC、单实例、热更新、托盘与登录自启；验证命名管道 DACL/peer SID 和凭据库 ACL。 |
| P0 Serial | VID/PID 筛选、COM10+、占用、端口改名、DTR/RTS=false；配对、撤销、三轮超时重连和 CJK/USB 图标。 |
| P0 LAN | 同一 LAN，选择正确网卡；发现、短码、认证、天气/Devices、重启恢复和双向撤销；仅按需对 Private 网络放行 UDP 18444 / TCP 18883，不静默修改防火墙。 |
| P0 双路由 | USB 优先、LAN→Serial→LAN、3 次插拔与端口变化；无会话冲突和误报连接。 |
| P1 集成 | GSMTC 音乐、Codex Desktop IPC、GitHub 凭据、系统代理、LHM/WMI、WebView2、高 DPI、中文路径与失败提示。 |
| P1 发布 | 绿色包/安装卸载、缺依赖提示、启动项清理、签名状态；2 小时混合长稳，72 小时另行验收。 |

```powershell
. C:\Users\zy201\esp\esp-idf\export.ps1  # 按本机实际路径调整，必须点号加载
idf.py -C firmware build
idf.py -C firmware -p COM5 flash             # 先确认实际 COM 和占用
cd client
npm --prefix frontend ci
npm --prefix frontend test
go test ./...
go vet ./...
powershell -ExecutionPolicy Bypass -File .\scripts\build-windows-app.ps1 -Version 0.1.0
```

Windows UCRT64 的完整固件主机 CMake 套件曾因缺少 `sys/select.h` 无法构建；这不是测试通过。烧录前停止占用串口的 Client/Agent。记录构建、ACL、UAC、COM/VID/PID、防火墙与会话证据时排除秘密。
