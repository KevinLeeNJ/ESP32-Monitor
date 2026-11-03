# 固件 Companion 交接

更新时间：2026-11-03。目标和协议以 [固件能力计划](FIRMWARE_CAPABILITIES_PLAN.md) 与 [Companion 协议](ESP32_COMPANION_PROTOCOL_V1.md) 为准。本文保留当前状态、验收证据和下一步；测试结果只适用于所记录的构建与环境。

## 当前状态

- **Standalone**：直接获取天气、AQI 和 Devices；Devices 保持 30 秒采集周期。天气连接失败最多尝试 3 次、间隔 1 秒；断网或 Companion 接管时取消后续重试，保留最后有效数据。
- **LAN**：发现、配对、认证 MQTT、Config、Snapshot、Heartbeat、ACK、Resync 和掉线恢复已实现；用户确认 LAN 模式可用。Client 配对监听使用所选 LAN 地址的 TCP 18080，MQTT broker 使用 TCP 18883，发现使用 UDP 18444；LAN 传输无 TLS，首次 LAN 配对需确认明文风险。当前发现与首次配对代码没有检查 Config 状态；普通模式也可能被发现，不应将 Config 视为已实现的访问限制。
- **Serial**：HELLO/AUTH、AES-256-GCM Config、ConfigAck、Snapshot、Heartbeat、ACK 和配对已与真实 Client 互通。认证后等待 ConfigAck 和首个心跳，再关闭 Wi-Fi/AP/DNS；退出后恢复。串口优先于 LAN，一次配对共用两种传输；插拔自动切换已有实机证据。
- **配对与撤销**：LAN/Serial 显示相同六位码，板端 KEY 确认；BOOT 长按撤销只清除 Companion 配对，保留 Wi-Fi、天气和 Devices 配置。双向撤销和无活动会话撤销感知已实机验证；墓碑最多 4 条，仅保留派生 discovery key，不保存 token/serial key。
- **界面与来源**：断线回第一页并禁用翻页、串口 USB 图标、Codex/音乐显示及 KEY 翻页已由用户确认。心跳按来源续租；GitHub 真实数据、Serial 真实来源续租和修复后的音乐/Codex 长稳仍需补证。时间优先级为可信 RTC、NTP、Client。
- **Windows**：已验证 ESP-IDF 环境、生产固件构建和 COM5 烧录；Windows Client 的 GUI、Agent、串口与 LAN **尚未真机验收**。

## 最近修复和验证

### Devices 启用后配置页重启（2026-11-03）

Windows / ESP32-S3 实板在配置 Devices 数据源后，独立运行时天气偶发无法加载，长按进入配置模式时黑屏并重启。禁用设备 Serial Companion、使用 USB Serial/JTAG monitor 且临时关闭 light sleep 的诊断固件复现了配置页故障：进入前采样内部空闲仅 **315 B**、最大连续块 **256 B**，PSRAM 空闲约 4.3 MB；`httpd_start()` 创建信号量失败，返回 `ESP_ERR_HTTPD_ALLOC_MEM`，配置页代码的 `ESP_ERROR_CHECK` 调用 `abort()` 后重启。这确认了该次重启的直接原因；普通生产固件中天气失败及所有其他重启是否同源仍需分别验证。

当前工作区修复使 ESP32 的 LVGL 分配优先使用 PSRAM、失败时回退内部 RAM；配置 HTTP 服务启动或路由注册失败时返回错误，避免该路径直接重启。修复后的**诊断固件**在 COM10 烧录并校验：Devices 返回 HTTP 200，`weather=ok aqi=ok`；用户确认 Configuring 页面正常显示，配置主页和 `/advanced/config` 均返回 HTTP 200，最大连续内部块约 31 KB。该次诊断关闭了 Serial Companion 和 light sleep，不能替代生产配置及 LAN/Serial 会话下的验证。Windows 模拟器现有测试 34/34 通过；配置启动失败的主机回归通过。

测试中 LAN 配对还遇到 `pairing_listen_failed`：临时 Devices 模拟服务占用了 Client 的 TCP 18080。模拟服务已迁到 18081；端口冲突 toast 已在源码中并通过占用测试，尚未在运行中的 Windows Client 验证。两端取消配对后，板端日志为 `pairing record: absent`；撤销记录不等于有效配对。用户随后确认 LAN 模式可用。UDP 发现与 TCP 18080 配对监听是不同环节，之前普通模式下广播发现失败的原因尚未确认。

已重新启用 Serial Companion 和 light sleep，固件经 COM10 烧录及哈希校验。串口配对后 Client 曾循环报告 `proxy-config invalid_config` 并关闭会话，界面显示 `context canceled`；为运行中 Agent 临时授权 Devices 模拟地址后，串口达到 `connected`。自动切换尚待实机验收。

### 地址与配置提示（2026-11-03）

Client 源码已移除 Devices URL 的额外精确授权，信任设备提供的 HTTP/HTTPS 地址；配置被拒时通过设备状态显示 toast。Server 地址原已支持局域网 IP 和域名。固件配置页要求天气和 Devices 基地址包含 `http://` 或 `https://`，旧天气纯主机名仍按 HTTPS 兼容；独立天气和 Client 代理均使用配置的协议。基地址不包含凭据、路径、查询或片段。固件构建、天气/Devices 主机测试、Client 定向竞态测试及前端测试通过；**这些 URL 修改尚未烧录到开发板，Client 也尚未重新打包运行**。下一步更新 Client 和板端后复测配置页、串口及 LAN 自动切换，并观察内部内存余量。

### 设备卡片与进度条

共享 UI 使用 10 格方块进度条，非零低值至少点亮一格；设备卡片放大指标数字并使用像素字体。需在新固件上复核显示及配置模式内存余量；模拟器不代表硬件表现。

### Standalone 天气与 DNS

实板冷启动曾出现天气 DNS `getaddrinfo() returns 202` / `ESP_ERR_HTTP_CONNECT`，随后同一主机的 AQI 成功。板端 DHCP DNS 为 `192.168.110.1`；Mac 经 en0 直接查询同一 DNS，在 1 秒超时的无 EDNS 小样本中 UDP 8 次有 4 次超时，TCP 3 次均成功；公共 DNS UDP 4 次均成功。证据指向当前路由器 DNS 的 UDP 路径不稳定，但未抓包，不能确定是转发器、上游、网络丢包还是本机拦截，也不能将小样本视为长期丢包率。

固件已加入上述有界连接重试，保留 DHCP DNS、TLS 校验及 Wi-Fi 省电策略。最终生产固件烧录并完成哈希校验；无 Client 时连续 3 次启动 `weather=ok aqi=ok`。三次未触发应用层重试，失败恢复和取消路径由故障注入测试验证，尚无实板强制故障证据。天气/Wi-Fi/LAN/Serial 相关测试 **7/7** 通过。

### 内存

Serial 编码及 Config 中间缓冲、天气响应、MQTT RX/Config 和会话对象优先 PSRAM，失败回退；最终 USB 帧、显示 DMA staging 和任务栈保留现有策略。依据 [ESP-IDF 外部 RAM 指南](https://github.com/espressif/esp-idf/blob/master/docs/en/api-guides/external-ram.rst)。当前源码 Serial 栈 16 KB、常驻 MQTT worker 栈 10 KB、Devices 栈 8 KB、Discovery 栈 16 KB。Serial 收紧到 12 KB 后曾出现认证问题，已恢复 16 KB，后续不要收紧。2026-11-03 通过 COM10 采集到 Serial 历史最低剩余 1920 B，但未确认板上构建与当前源码一致，且未覆盖所有峰值场景；临时水位采集代码已移除。worker 同时服务配对与 LAN，避免运行时申请大块连续内部栈。

| 场景 | 三轮最低内部空闲 | 最大连续块 | 口径 |
|---|---:|---:|---|
| LAN，音乐/Codex | 17,255 B | 7,680 B | 约 10 秒一次的连接态采样 |
| Serial，音乐字体已加载 | 13,191 B | 7,680 B | 会话内最低空闲；连续块按关键事件采样 |

三轮均完成认证与重连；不能排除采样间短时低谷。LAN 的 TLS 瞬时最低值和长期碎片仍未测。旧实测 Wi-Fi RX 为 16、历史 MQTT 栈记录为 8 KB，与当前构建不一致，不能用两轮差值计算精确节省。Serial 后重新打开日志串口曾观察到启动日志，随后 LAN 恢复；该次不能作为无复位切换验收。最终天气修复固件 SHA-256：`9429d7f2b1dd6a3e72acebcd57027757492a22ab5e713c964481cfe8c58d3c40`。

### 音乐字体与活动卡片

- 歌曲标题为 18px，歌手为 12px（与 Codex 卡片 “5h Left” 同字号）。标题截断的 “...” 单独使用 12px 字体，可比整字号省略号多显示约两个字。字体范围包含 Latin-1、常用标点、♪★、日韩与全角字符，`×` 等符号不再显示方框。音乐图标已放大，Codex 任务图标扩大了显示区域并保留图标视觉大小。
- 播放位置/时长与 Client 一样向下取整。仅 Serial 会话在板端每秒本地推进播放时间；LAN 保持原有快照节奏，避免唤醒和耗电。Client 的 GSMTC 采集改为 250 ms 状态检查、1 s 数据发布，Companion 页面在可见时每秒刷新，暂停后不会继续走一两秒再退回。
- 音乐字体由 `firmware/assets/fonts/generate_music_font.py` 生成静态 LVGL 字体。位图和字形描述表以 `.bin` 保存并由 `#embed` 编译进只读数据，构建工具链必须支持 `#embed`。字形描述按 `LV_FONT_FMT_TXT_LARGE=0` 的 8 字节布局打包（小端，位域低位在前），生成的 C 用 `_Static_assert` 校验字节序和结构大小；若启用 `LV_FONT_FMT_TXT_LARGE`，必须重新生成。`cmaps` 表含指针，保留 C 代码；`unicode_list`/`glyph_id_ofs_list` 数据量小且表项多，未拆分。生成的 `.c` 由约 3.24/2.16/1.09 MB 降至约 160/151/12 KB。当前配置将只读数据放在 PSRAM，避免旧的动态字体解析和重复驻留；本轮 map 显示主字体描述表位于 `0x3c17xxxx`，4 字节对齐。
- 18px 字体拆为主字体和 `monitor_music_18_extra`，通过 LVGL fallback 查找额外字形：当前 `LV_FONT_FMT_TXT_LARGE=0` 时单个字体的位图索引只有 20 位，完整位图超过其范围。12px 字体无需拆分。
- COM10 实板临时测试中，切换到静态 18px 字体后，测试页面切换完成时 PSRAM 空闲量比旧动态 20px 方案多 2,542,056 B；两次启动基线和字号不同，不能将差值视为严格的同条件对照。混合中日韩英文样本的预热渲染均值分别约为 11.624 和 12.314 ms/帧，不能推及所有文本。内部堆低水位按区域分别统计，不能直接当作同一时刻的总空闲量。
- 此前生产固件构建并烧录到 COM10，镜像为 4,043,536 B；该证据早于上方 Devices 内存修复和当前诊断固件。启动日志显示 `weather=ok aqi=ok`，无 Companion 稳态采样 `psram_free=4,310,100 B`、`int_free≈22.3 KB`、`int_largest=14,336 B`。模拟器 unit、animation 测试 2/2 通过，`activity-long-music` 和 `activity-cjk-music` 预览已检查。测试使用临时插桩，测试后已移除；Windows 下模拟器测试使用了忽略目录中的本地构建适配，不能代表完整模拟器套件通过。

### 自动化结果

- ESP-IDF 生产构建和实板烧录最终校验通过。
- 本轮字体资源以 lv_font_conv 参考输出逐字段比对：18px 21,272、18px extra 11,346、12px 32,617 个字形，描述字段、位图字节和字符映射数组全部一致。
- Client 侧改动通过 `go test ./internal/nowplaying/... ./internal/transport/...` 与前端构建；`client/bin` 和 `client/build/windows/ESP32-Monitor` 下的 Client/Agent exe 已重建。
- 内存优化轮次固件主机与模拟器测试均为 **34/35**；随后为 `monitor_clock_client_time_tests` 夹具补齐 `NULL` / `time_t` 所需标准头文件，固件主机套件重新运行 **35/35 通过**。天气修复轮次相关测试 7/7 通过。
- Client 早前 `go test -race ./...`、`go vet ./...` 与前端 13 项测试通过；这些不是 Windows 真机证据。
- 模拟器 `companion-live` 曾单独渲染并目视检查。Windows 完整 host/simulator 套件仍因 `sys/select.h`、`socketpair` 等既有平台差异无法构建；本轮用忽略目录中的 shim 只构建并通过 unit、animation 目标和音乐预览，不代表完整套件通过。

## 待办与验收边界

**下一步：优先完成上方 Devices 内存修复的生产模式、LAN 和 Serial 实板验收。** 随后复测字体和播放时间：确认标题/歌手、日文/韩文、`×` 与 fallback 字形；Serial 下播放时间应每秒递增、暂停时与 Client 一致，LAN 下不应每秒唤醒，保持快照节奏。同时比较 PSRAM/内部堆水位、页面切换和长文本渲染耗时。若后续启用 `LV_FONT_FMT_TXT_LARGE`，必须按新布局重新生成描述表。

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
- 天气/网络：`firmware/components/firmware/monitor_weather.c`、`monitor_wifi_manager.cpp`；Client 代理：`client/internal/transport/proxy.go`。Devices 使用设备配置的 HTTP/HTTPS 地址，无需额外授权。
- 配对/发现：`firmware/components/firmware/monitor_pairing_*`、`firmware/src/companion/monitor_pairing_*`、`monitor_discovery*`；Client 对应 `client/internal/transport/discovery.go`、`pairing.go`、`session.go`。
- 界面：`firmware/src/companion/monitor_companion_view.c`、`firmware/app/ui/monitor_activity.c`、`monitor_pages.c`、`monitor_pairing_ui.c`。音乐字体使用 `firmware/app/ui/fonts/monitor_music_*.c` 和 `#embed` 位图资源。
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
