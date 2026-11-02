# 固件 Companion 开发交接（LAN 数据链路、Serial 互操作、Serial PAIR、双端撤销与 Serial 独占模式已验收）

## 2026-11-02 接续记录

- **Windows 真机烧录已完成（2026-11-02 下午）**：在生产构建上执行 `idf.py -C firmware build` 与 `idf.py -C firmware -p COM5 flash`，烧录哈希校验通过；启动日志显示 LVGL、启动动画和面板模式正常。烧录前 `monitor-agent`/`monitor-client` 占用 COM5 导致 `PermissionError(13)`，经用户同意后停止这两个进程再烧录。本轮只验证固件构建/烧录与启动，**Windows Client 仍未验收**；板端保存的 Wi-Fi 在该现场未找到 AP，网络相关验证需回到可用局域网。烧录后 Client 需要重新启动。
- **Windows PowerShell 的 ESP-IDF 环境已可用（2026-11-02）**：在 PowerShell 7 中把当前用户执行策略设为 `RemoteSigned`，设置用户环境变量 `PYTHONUTF8=1`，然后点号加载 `. C:\Users\zy201\esp\esp-idf\export.ps1`，之后 `idf.py -C firmware build` 可直接运行，行为与 macOS 的 `. ./export.sh` 一致。注意 `export.ps1` 定义的是 PowerShell 函数，必须点号加载；`export.bat` 在本机因 LF 行尾无法跳转标签，Git Bash 的 `source export.sh` 会因 `activate.py` 输出的 Windows 反斜杠路径被转义而失败，两者都不要作为 Windows 入口使用。另：`firmware/tests/run.sh` 的完整主机 CMake 套件在 UCRT64 MinGW 下因缺少 `sys/select.h` 不能构建，Windows 上目前只能单独运行不依赖该头的 Python 测试（本轮为两个时钟测试）。
- **音乐/Codex 反复变化的根因已修复（2026-11-02 晚）**：Client 原先只发送 `LastSuccessAt` 前进的 lease，来源健康但没有新观测时整条 lease 被省略；固件对缺失 path 回退到快照年龄，15 秒 TTL 到期后卡片消失，下次观测又出现。同时 `desktop-ipc` 把“已连接但 15 秒无新推送”的 Codex 任务判为 stale，使健康度在 healthy/unknown 间反复。现改为每次心跳重复发送所有健康来源的 lease（age 按发送时重算，重复不会伪造新鲜度），`desktop-ipc` 在连接期间保留最后状态、仅在断开或线程从未观测时失效，并在连接且健康时每个 Snapshot 刷新 `LastSuccessAt`。新增 `TestHeartbeatRepeatsUnchangedSourceLeases` 与 `TestDesktopQuietConnectedStreamKeepsTaskFresh`；Client 全量 race 测试、`go vet` 与前端测试通过，固件已重新构建并烧录。**修复后的长时间音乐/Codex 稳定性仍需在实板复核。**
- **串口配对后的误报“串口连接失败”已修复（2026-11-02）**：板端 3 秒串口看护会结束配对接续会话，Client 侧请求 context 被取消，串口看护在约 2.8 秒后重新连上；GUI 原先把这段正常交接显示为失败。现在 `probeAndConnect` 对 `context canceled`、`session_expired`、`serial_disconnected`、`serial_write_failed` 最多轮询 12 秒等待串口看护重连，成功则更新发现行并提示已连接。配对短码的字体也改为继承 FusionPixel 字体栈，不再回退到系统等宽字体。
- **GitHub 加载状态已修复（2026-11-02）**：连接成功但贡献数据尚未返回时，页面显示三点加载动画与重试说明，而不是直接显示“去登录”；只有 `auth_required`/`disabled` 或功能未启用时才显示登录入口，`prefers-reduced-motion` 下停用动画。
- **时间来源优先级已明确（2026-11-02）**：可信 RTC > NTP > Client。每天 02:00 只失效一次 RTC（绕过 10 分钟 SNTP 去重并强制同步），失效后运行中的系统时钟保持显示，不会退回 SYNCING；只有 RTC 不可信且 Wi-Fi 未连接时才采用 Companion 快照中的 Client 时间，写入 RTC 后重新标记可信。新增 `clock_client_time_test.py` 并扩展 `clock_sync_test.py`；固件已构建、烧录。

- **下一阶段转移到 Windows（2026-11-02）**：Windows 代码路径（`build-windows-app.ps1`、命名管道、凭据 ACL、GSMTC、WMI/LHM、登录任务、托盘与安装包）已经存在；本轮只在 Windows 上验证了 ESP-IDF 环境与固件 `COM5` 烧录，Client 仍未在 Windows 真机验证。下一阶段按“Windows 验证与开发交接（下一阶段）”清单逐项执行；P0 未通过前不得宣称 Windows 客户端已验收。
- **Serial 模式内部 RAM 余量已量化（2026-11-02）**：在无可用局域网、仅 USB 串口的条件下完成多轮（2–3 轮）连接/超时/重连对照。无音乐稳态约 `free=34.6 KB`（`largest` 24.6–26.6 KB）；显示 CJK 音乐元数据后约 `free=21.3 KB / largest=7,680 B`，会话内最低 `free≈11.0 KB`。首会话后的约 8–10 KB 基线下降来自 Wi-Fi/直连生产者恢复，与 Codex 无关；音乐路径额外占约 13 KB 内部 RAM 和约 0.96 MB PSRAM（929 KB 字体）。重复轮次间无累积碎片。LAN 连接态、TLS 峰值与串口↔LAN 切换窗口仍未测。详见下文“内部 RAM 与网络余量（2026-11-02 Serial 实测）”。
- **逐来源续租已实现并实板验证**：固件按心跳 `source_updates` 只续租匹配当前快照的来源，心跳时间单独续租时钟；Client 移除每 10 秒强制整快照的临时方案，Serial 心跳也携带来源续租。顺带修复 LAN 会话未打断启动动画等待的问题（此前连接后要等约 90 秒的 120 秒启动超时才显示第一页）。详见下文“逐来源续租与启动接管”。
- **无活动会话撤销感知已实现并完成真机验收**：板端撤销时保留最多 4 条认证墓碑（host/pairing/派生 discovery key，不存 token/serial_key）；Client 配对探测再次遇到该配对 ID 时，只有用自己 serial_key 验签通过的 `pairing_open=true` 响应才删除本地凭据。旧固件与新 Client 兼容；伪造广播不会触发删除。真机已在“板端未配对”和“板端已与另一 Client 配对”两种状态各验一次，均自动删除原凭据且不影响新配对。详见下文“无活动会话撤销感知”。
- **串口配对短码 GUI 与流程一致性**：Client 在串口配对期间显示板端同一 6 位短码；串口和 LAN 的最终窗口都只显示短码与「取消等待」，不再要求 Client 二次确认，由 Monitor 上的 KEY 完成确认。配对成功后串口行切换为已连接/已配对，串口扫描只保留可能为 ESP32 的端口；连接 RPC 在会话建立后立即返回，避免会话结束时误报“串口连接失败”。详见下文“串口配对 UI 与流程一致性”。
- **同设备双路由自动连接已实现**：串口看护循环自动连接已配对 USB 设备、识别改名端口；LAN 配对后插入 USB 会自动切到串口并取消 LAN 会话；串口连接期间 LAN recovery 暂停；发现流程主动探测可能的 ESP32 串口。LAN 明文风险确认仍保留。详见下文“同设备 LAN/串口共享配对与自动连接”。
- 正式 Client 验收发现反复掉线、页面回退：板端运行时间连续，未捕获 panic 或复位；Client 日志为快照 MQTT PUBACK 等待 4 秒超时后主动关闭会话。板端原来先处理快照并发送应用 ACK，随后发送 MQTT PUBACK；现改为收到并解析主题后先发送 PUBACK，再执行应用协议处理，应用 ACK 仍独立验证。临时诊断捕获调整后多次 PUBACK 成功；去掉诊断日志的固件与正式 Client 已重新构建，需继续验证长时和弱网。
- 后续发现 Client 显示 connected 但板端未进入会话：每 10 秒的完整快照刷新也重置心跳计时，使心跳被持续推迟；首个心跳过早时 Client 可认为已连接，而板端仍在等待符合时间条件的心跳。现分开快照与心跳计时，定期心跳优先于快照，首次 LAN 心跳在首个快照后至少 2 秒发送（Serial 为 1.5 秒），避免刷新快照饿死心跳。LAN 快照刷新与后续心跳回归、Serial 物理配对模拟、Resync 测试均在 race 下通过；实板日志显示 overlay=1、天气/AQI 与 GitHub/Codex/音乐数据可见，用户确认 KEY 翻页和显示正常。主机固件测试 34/34、前端测试 13/13、固件构建与烧录校验通过；72 小时稳定性和电池功耗仍未验收。
- 根据用户要求，移除 Client ESP32 页面上的本地 Devices Server 授权输入；该入口原为调试明确授权私网 HTTP，安全校验与底层能力保留。停止本机模拟 Devices 服务，清除 Client 中此前添加的测试授权，并通过板端配置页面清除模拟 URL 和测试令牌；操作前后核对天气坐标及天气配置保留。真实 Devices 服务未部署，设备数据不再使用模拟值。

- LAN 重新配对无短码已复现并定位：撤销后的配对 offer 需要动态创建 12288 字节任务栈，板端日志 `internal free=9731 largest=3968`、`ESP_ERR_NO_MEM`。改为开机时预留一个 12288 字节内部 RAM 工作任务，由 LAN 会话与配对事务互斥复用；较上一版 8192 字节常驻栈增加 4096 字节，不另建配对栈。任务调度使用结束后清除按钮通知，配对与会话数据仍各自清理；添加不含凭据的配对失败日志。固件构建、34 项主机测试和实板烧录校验通过。重启后 LAN 配对已出现短码、用户核对并按 KEY、Client 确认后 connected；用户确认天气、设备、Wi-Fi 图标和第二页正常，启动直连天气与 AQI 均更新成功。共享栈不消除其他碎片，长稳与内存余量仍需验收。
- 再次 BOOT 撤销后尝试串口配对，刚批准的 LAN 配对窗口仍保留导致 `session_conflict`；重启 Agent 清除窗口后，用户确认串口短码并按 KEY，本次首次配对直接进入 connected，用户确认 USB、天气设备和翻页正常。随后授权 LAN 并重启 Agent，LAN 再次 connected。此前首次接续曾出现 `session_expired`，单次成功不足以确定该偶发问题已修复；配对窗口完成后的释放行为也应继续检查。

- 本轮开始物理验收：用户单独长按 BOOT 约 5 秒，板端日志确认删除 Companion 配对、活动 LAN 会话以 revoked 结束，Client 显示未配对；Devices 直连 HTTP 200，天气更新为 weather=ok、aqi=failed（一次 HTTP 连接失败）。LAN 重新配对请求随后停留 pending，未收到短码，原因尚未确认。转用串口，用户核对短码并按 KEY，Client 状态 approved；首次随后的配置同步以 `session_expired` 结束，已配对串口重连后为 connected，用户确认弹窗消失、USB 图标、天气设备和第二页正常。授权原局域网后，退出串口并重启 Agent，LAN 再次 connected。发现和 Resync 相关 Client transport race 测试通过。LAN 重新配对、首次配对后的直接接续、BOOT 取消和配置完整保留仍需验证。

- 串口切回 LAN 内存修复：先前会话重连时动态创建 8192 字节内部 RAM 任务栈，空闲总量足够但最大连续块只有 7680 字节，导致 `ESP_ERR_NO_MEM`。现在开机网络服务启动前创建并常驻复用 MQTT 工作任务，结束会话仍释放会话缓冲；栈大小未缩小。此方案避免重连时的连续大块申请，并不证明消除了所有堆碎片；代价是空闲时常驻约 8 KB 内部 RAM。初始化失败会记录错误并禁用 LAN 会话，重启后可重新尝试。
- 验证：固件主机测试 34/34、ESP-IDF 构建、实板烧录校验通过。真实 Agent 两次串口接入、超时退出后均重新建立 LAN 认证；两次 worker start 分别为 internal free/largest 9027/4864、8991/6912 字节，Client 均报告 connected。用户确认第二轮串口模式 USB、天气、设备及第二页正常；Client 再次连接后 KEY 进入第二页及数据显示正常。首次开机 LAN 期间天气更新成功；两次串口退出至 LAN 接管之间直接天气请求失败，第二次日志为 `ESP_ERR_HTTP_CONNECT`，后续由 Client 代理接管。断开 Client 后直连天气和 AQI 更新成功，用户确认第一页正常。内存余量仍低，不能认定所有网络资源压力问题已解决。

- 用户授权真实数据联调；LAN 下用户确认 Codex 与音乐显示正常，断线时第二页自动返回第一页且 KEY 不再进入第二页。GitHub 来源返回 `auth_required`，真实 GitHub 数据未验收。Serial 下用户确认播放中的音乐与 USB 图标正常。
- 修正 Serial 模式静态画面仍转 LPM：串口活动期间保持 HPM，状态切换通知显示任务；新增主机测试覆盖进入、保持和退出。首次烧录后开机早期串口连接出现天气/设备空白、无法翻页；分析发现启动动画等待直连数据最长 120 秒，Serial 接管时直连 Wi-Fi 已停。现让串口认证打断启动等待；主机测试通过，用户确认实板天气、设备和翻页恢复。HPM 的面板实际状态和电池功耗仍需独立实测。
- 根据用户反馈，Codex 卡片显示剩余额度：协议仍传已用百分比，视图转换为 `100 - 已用`，标签改为 `5h Left` / `Week Left`；主机边界测试、模拟器 34 项和实板目视确认通过。
- 暂停音乐显示：发现固件尚未消费心跳 `source_updates`，Serial 心跳也不携带续租，导致来源没有语义变化时完整快照的 15 秒 music TTL 到期。Client 暂时每 10 秒发送包含当前真实来源健康和年龄的完整快照；不会改变 Devices 的 30 秒采集周期，也不会用心跳伪造成功观测。代价是 LAN/Serial 定期传输完整快照；后续应实现逐来源续租并重新评估流量和功耗。LAN 模拟器回归与 Resync race 测试通过，Serial 真机暂停超过 15 秒后用户确认曲目仍显示。
- Serial → LAN 旧故障记录：断开串口后 Wi-Fi 恢复，但重邀时 `internal free=17471 largest=7680`，动态创建 worker 失败；本轮修复和复测结果见上。此前完整 Client transport race 测试通过。
- Client 发现去重：同一轮重复的未配对响应只计一次配对丢失观察，候选结果按设备去重并限制为 32 项；未经认证的发现仍不会删除配对凭据。新增 UDP 回归测试及 race 测试通过。
- 固件 LAN 主机测试新增旧 session 快照隔离检查：拒绝旧 session 后，同序号当前 session 快照仍可应用，测试通过。此项仅覆盖快照路径。
- 固件主机测试 34 项均通过：其中 API server 测试需本机回环监听，沙箱内被拒绝后在允许监听的环境单独补跑通过。
- 初次实板 LAN：真实 Client 于 10:15 进入 `connected`；当时 headless Agent 未启用 GitHub、Codex 和音乐，三项显示为空。重启后约 33 秒重新打开会话并于 10:17:13 进入 `connected`。后续启用数据源及固件改动的结果见上。

更新时间：2026-11-02 19:50（UTC+8）。项目目标与验收以 [固件能力计划](FIRMWARE_CAPABILITIES_PLAN.md) 和 [Companion 协议](ESP32_COMPANION_PROTOCOL_V1.md) 为准。本文把“已实现”和“已在真机验证”分开记录；Serial 功耗长测与 72 小时稳定性仍未完成，Windows Client 也尚未验收。

## 当前状态

LAN Companion 的 MQTT 会话、完整快照接收、第二页真实数据绑定、第一页代理所有权与掉线回退已完成，并在真实开发板 + 真实 Client Agent 上验证到 `connected`。LAN 会话的内部 RAM 占用已改为 PSRAM 大对象，90 秒无回执过期与 Agent 恢复后的重连已在真机验证；Client 天气代理的 gzip 响应处理已修复，真机视图恢复 `weather=1 aqi=1`。Serial 模式内部 RAM 已完成多轮（2–3 轮）实测：无音乐稳态约 `34.6 KB free`（`largest` 24.6–26.6 KB），显示 CJK 音乐后约 `21.3 KB free / 7,680 B largest`，会话内最低约 `11.0 KB free`；重复轮次间无累积碎片，但音乐路径的 `largest` 仍低于 8,192 B 的临时编码缓冲，LAN 连接态余量仍待补测（见下文“内部 RAM 与网络余量（2026-11-02 Serial 实测）”）。逐来源续租已替换“每 10 秒整快照”的临时方案，并在实板验证心跳时间在快照 91 秒后仍保持 `time=1`；LAN 认证会话现在会立即结束启动动画等待。双端撤销（Client→板端 `host_revoked`、板端→Client `pairing_revoked`）已实现并完成真机双向验证；真实 BOOT 5 秒长按、板端配对弹窗 KEY 确认、撤销后的重新配对，以及无活动会话撤销感知，均已在 2026-11-02 真机验收；撤销后 Wi-Fi 保持连接，天气配置保留并可下发给新 Client。Serial F3 的 HELLO/AUTH、AES-256-GCM Config、ConfigAck、Snapshot/Heartbeat/ACK 与真实 Client `ConnectSerial` 已互通并完成真机稳定运行验证；Serial PAIR 未配对流程已完成：板端发送 PairRequest、显示短码、KEY 确认/BOOT 取消、NVS 保存后直接 AUTH，并完成真机配对、撤销与已配对重连验证；Serial F4 的 Wi-Fi/AP/DNS 真关闭、退出恢复与 Serial 状态图标也已完成真机验证。

2026-11-02 晚已在 Windows 上完成 ESP-IDF 环境配置、生产固件构建和 `COM5` 烧录；音乐/Codex 反复消失的重复 lease 缺陷与 `desktop-ipc` 静默误判已修复并有 Client 单元/race 回归；时间优先级已改为可信 RTC > NTP > Client。

**仍未完成：** Windows Client 的 P0 验收、未配对 Serial 弹窗的 BOOT 取消（真实 KEY 确认已完成）、LAN 异常路径验收、真实 GitHub 数据验收、Serial 模式功耗和 72 小时稳定性、修复后的音乐/Codex 长时间实板复核；同设备双路由还需端口改名和重新配对行刷新的完整 GUI 验收；LAN 连接态的 Wi-Fi/TLS 峰值余量与串口↔LAN 切换窗口的直接天气失败仍需在有局域网时补测（Serial 模式内部 RAM 已完成 2–3 轮实测）。普通 KEY 翻页、断线回退、Serial USB 图标、LAN/串口配对与撤销按键、无活动会话撤销感知、串口配对短码 GUI、LAN 配对后插入 USB 自动切串口及拔掉 USB 自动回 LAN 已目视或真机确认。OTA 不在本轮。

## 待开发与完善（2026-11-02）

1. **无活动会话撤销感知（已完成真机验收）**：板端撤销墓碑与 Client 认证删除逻辑已完成，主机固件测试、Client race 测试和固件构建/烧录通过。真机在“板端未配对”和“板端已与另一 Client 配对”两种状态各验证一次：原 Client 均在探测后删除本地凭据（`pairs=0`），新配对保持连接；详细日志见下文。仍需把该场景纳入后续长稳回归。
2. **串口配对界面与流程一致性（已完成）**：Client GUI 的串口连接已显示 6 位短码；串口和 LAN 的最终窗口都只显示短码与「取消等待」，不再要求 Client 二次确认，最终确认由 Monitor 上的 KEY 完成（LAN 仍在开始前要求显式选择设备和接受明文风险）。同时修复配对成功后串口行仍显示「探测并连接」、连接 RPC 跨整个会话导致误报失败提示、迟到握手/配对帧误杀已认证会话，以及串口列表包含蓝牙/虚拟端口的问题；详见下文“串口配对 UI 与流程一致性”。
3. **内部 RAM 与网络余量（Serial 已实测，LAN 待补）**：MQTT 工作任务常驻约 8 KB 内部 RAM，已避免串口切回 LAN 时因连续栈申请失败而断连，但不代表碎片消失。2026-11-02 在无局域网、仅 USB 串口的条件下完成 2–3 轮连接/超时/重连对照：无音乐稳态约 `34.6 KB free`（`largest` 24.6–26.6 KB），显示 CJK 音乐后约 `21.3 KB free / 7,680 B largest`，会话内最低约 `11.0 KB free`；重复轮次间无累积碎片。`largest=7,680 B` 仍低于 8,192 B 的临时编码缓冲，PSA/任务栈余量偏紧。LAN 连接的 Wi-Fi/TLS 峰值、串口↔LAN 切换窗口的直接天气 `ESP_ERR_HTTP_CONNECT` 仍须有局域网后补测。保留 16 KB 串口任务栈与 8 KB MQTT worker 栈，不以无证据缩栈作为优化。详见下文“内部 RAM 与网络余量（2026-11-02 Serial 实测）”。
4. **逐来源续租余项（Client 去重缺陷已修复）**：Host/模拟器与实板音乐/Codex 已覆盖暂停音乐、旧快照 seq、设备 stale/expire 与心跳时间续租；2026-11-02 晚又修复了“只发送前进 lease”导致的音乐/Codex 反复消失，以及 `desktop-ipc` 静默 15 秒误判 stale，并有单元测试与全量 race 回归。剩余：GitHub 和 Serial 的真实来源续租仍需在有真实来源的实板上补证，修复后的长时间音乐/Codex 稳定性也要在实板复核。
5. **同设备双路由自动连接（已实现，部分实机验证）**：Client 串口看护循环、发现主动探测、LAN 配对后 USB 自动切换、端口改名重连、重新启用优先串口、串口 > LAN 门控和 GUI 共享状态已完成；一次配对同时授权 LAN 与串口，GUI 不再显示单独的「启用局域网」。主机/前端测试、LAN→Serial 真机切换、拔掉 USB 后自动回 LAN 已通过。端口改名和重新配对行刷新仍待 GUI 验收。详见“同设备 LAN/串口共享配对与自动连接”。

## 用户要求与交接范围

- 按能力计划实现 LAN、Serial、三模式、电源与真实数据 UI；OTA 不在本轮。
- Client 配对时板端显示六位码弹窗，风格与现有界面一致，带弹出动画；完成或取消后消失。
- NAS 尚未部署 Server，联调先使用本机服务。音乐长标题以 `...` 结束，音乐字体使用动态加载的 `.bin`，包含英文和 CJK。
- 开发板在 macOS 上连接在 `/dev/cu.usbmodem1101`，在 Windows 上当前为 `COM5`（以设备管理器实际枚举为准）。用户允许直接开发与烧录；仍须保留能验证结论的证据，不以模拟器代替真机或电池测量。
- 每个阶段完成后测试；先完成 MQTT 会话和真实数据接收，再继续 Serial、模式/电源、UI 和稳定性。

## 工作区与风险

工作树仍有大量未提交改动和新增文件，含用户原有改动；先运行 `git status --short`、阅读 diff，不要重置、清理或覆盖。共享 UI 同时由固件与模拟器编译，新增源文件必须同时登记 `firmware/components/monitor_core/CMakeLists.txt` 和 `simulator/CMakeLists.txt`。

本机还有两类**不应提交**的本地材料：`firmware/components/lvgl/Kconfig` 的符号链接替代改动（仅为在这台 Windows 构建机上绕过链接问题）以及 `.logextract/`、用户提供的日志 zip。提交前按文件名逐个暂存，不要使用 `git add -A`。

当前 `firmware/sdkconfig` 未启用 NVS 加密和 Flash 加密。配对凭据存于独立 NVS namespace，但**没有加密保护**；不要称其为硬件保护存储，也不要自动烧录 eFuse 或更改安全启动配置。日志、交接文档和截图不得记录 token、serial key、配对 ticket 或其他秘密。

## 本次完成与证据（2026-11-01）

1. **构建登记补齐**：`monitor_lan_session.c`、`monitor_mqtt_wire.c`、`monitor_proto_strict.c`、`monitor_companion_view.c` 已加入 monitor_core 与模拟器；`monitor_companion_config.c`、`monitor_companion_mqtt.c` 已加入固件组件；`test_lan_session.c`、`test_mqtt_wire.c`、`test_companion_view.c` 已登记 host.cmake。此前这些文件不在任何构建中。
2. **配对发现 → MQTT 启动已接通**：已配对 `monitor_connect_offer` 校验通过后，调用 `monitor_companion_mqtt_start` 连接 Client Broker；启动/订阅/Open/Session/Config/ConfigAck/Snapshot/Heartbeat/ACK 全链路在真机完成。Client 日志依次出现 `syncing_config → waiting_snapshot → connected`；板端日志出现 `authenticated MQTT transport connected`。
3. **关键联调发现（必须保留）**：Client 会把板端 Config 中的 Devices URL 与 GUI 的精确授权比较；未授权时以 `invalid_config` 拒绝，板端表现为 `session ended: lan_receive`。本机联调使用板端已保存的 `http://192.168.110.2:18081`，并以 `--companion-local-devices-url http://192.168.110.2:18081` 精确授权后连接成功。**不能默认放行任意私网地址。**
4. **第二页真实数据**：`monitor_companion_view.c` 将 Snapshot 转为共享 UI 数据（GitHub 32 列对齐、Codex 配额/任务、音乐位置插值、proxy weather/aqi/time/devices、独立过期）。`monitor_activity_apply` 原位更新页面，不重建对象；CJK 字体切换同步行高。生产固件第二页启动时为空占位，只由已应用快照填充，不再显示演示数值。
5. **第一页代理所有权与回退**：认证会话建立后暂停直接 weather/Devices 生产者，Proxy 数据接管第一页；板载电池、温湿度始终本地。会话断开时立即恢复生产者并请求刷新。真机日志：`direct producer resumed` 后约 1 秒出现直接 `monitor_devices: summary received`；连接期间没有直接 Devices 请求。
6. **掉线回退已验证**：停止 Client Agent 后板端在同一毫秒记录 `session ended: read`、`companion view overlay=0`、两个 `direct producer resumed`，无需等待 90 秒超时（TCP 关闭优先结束会话）。重新启动 Agent 后可再次进入 `connected`。
7. **第二页入口限制**：standalone 忽略翻页事件，日志 `page switch ignored: companion session inactive`；会话断开且第二页可见时自动切回第一页。**该按键行为尚未做物理按键目视验收**。
8. **诊断日志（保留）**：`companion_mqtt: session ended: <dial|connect|subscribe/start|read|publish_parse|topic|lan_receive|puback|pingresp|unexpected_packet|config_*|expired|stop|goodbye|revoked>`；`rejected down/<topic>`；`companion view overlay=... devices=...`；`companion poll gen=... seen=... int_free=... int_largest=... psram_free=...`；`companion_serial: serial heap close/after close: ...`（Goodbye 前经 USB/UART0 输出，无秘密）；`heap worker start/end/freed`；Wi-Fi `heap suspend/resume`；`pairing record revoked by host`；Client `device_message_rejected`、`device_session_state`、`device_pairing_revoke`、`device_pairing_revoked`、`proxy_fetch_failed`（host/path/status，不打印 key/token）。均不打印秘密。

## 双端撤销（2026-11-01 晚完成）

- **Client 撤销 → 板端**：`Backend.Revoke` 先删除本地凭据，再通过 session 运行循环串行发送 `down/goodbye`，`reason=host_revoked`（`closeReq` 避免与快照/心跳并发写 MQTT），随后取消会话。板端 `monitor_lan_receive` 校验 Control/Goodbye 并记录关闭原因；MQTT 任务以 `session ended: revoked` 结束，并调用 `monitor_pairing_store_revoke()` 删除配对记录；不删除 Wi-Fi/天气/Devices 配置。
- **板端撤销 → Client**：BOOT 长按路径改为 `monitor_companion_mqtt_revoke()`（设置 `s_stop+s_revoke`，不立即 shutdown socket）；MQTT 任务在退出前发布 `up/goodbye`，`reason=pairing_revoked`，再关闭连接。BOOT 处理仍随后删除本地记录。Client 校验 `up/goodbye` 的 session 后，在 `reason=pairing_revoked` 时记录 `device_pairing_revoked`、删除本地凭据并清理 recovery，不会自动重连。
- **防止撤销后复活**：`Backend.invite` 重新检查凭据库中的 DeviceID/PairingID；`Revoke` 删除凭据后最多关闭两个已登记会话，覆盖并发 recovery 的窄窗口。
- 新增共享协议函数：`monitor_lan_goodbye()`、`monitor_lan_close_reason()`；新增 API `monitor_companion_mqtt_revoke()`。

**真机证据**（真实板 + headless Client Agent + IPC）：

- Client→板端：IPC `POST /v1/monitors/revoke` 后，Agent 日志 `device_pairing_revoke`、`device_session_close`；板端 `session ended: revoked`、`pairing record revoked by host`、`MQTT session closed`；`companion.json` 的 `pairs` 归零；重新发现后设备为 `paired=false`；约 8 秒后板端出现新的直接 Devices 请求，证明直接生产者已恢复。
- 板端→Client：为避免物理 BOOT 长按，临时构建在会话建立后调用与 BOOT 路径相同的 `monitor_companion_mqtt_revoke()` + `monitor_pairing_store_revoke()`；Agent 日志 `device_pairing_revoked` 后会话关闭，`pairs` 归零；板端 NVS 分区哈希相对备份发生变化，证明配对记录已删除。随后已恢复生产固件、NVS 备份和 Client 凭据库并重新连接 `connected`；生产二进制不含临时触发代码。
- **人工验收（2026-11-02 完成）**：真实 BOOT 5 秒长按、板端六位码弹窗的 KEY 确认、撤销后重新配对均已在真机完成；撤销后 Wi-Fi 保持连接，天气配置保留并可下发给新 Client。Serial 未配对弹窗的 KEY 确认/BOOT 取消仍待验收。

> 真机撤销测试前备份了 NVS 分区与 Client 凭据库，测试后恢复并删除备份。NVS 未加密；此类备份属于敏感材料，不得留存、提交或上传。

### 无活动会话撤销感知（2026-11-02 实现）

- **问题**：板端在没有活动会话时撤销（物理 BOOT、串口撤销或作为新配对替换旧配对），原 Client 收不到 `up/goodbye(pairing_revoked)`，会继续保存旧凭据并反复尝试重连。
- **板端墓碑**：配对存储升为 v2（v1 blob 自动迁移）：`version/has_pair/pair/revocation_count/revocations[4]`。每次撤销或替换旧 pairing_id 时写入 `{host_id, pairing_id, discovery_key[32]}`；`discovery_key = HKDF-SHA256(raw(serial_key), SHA256(lp(host)||lp(device)||lp(pairing)), "ESP32MON-DISCOVERY-V1")`，与发现 MAC 同源。墓碑不含 token/serial_key 明文，按 pairing_id 去重，满 4 条时丢弃最旧。撤销仅清除当前配对，不碰 Wi-Fi/天气/Devices 配置。
- **认证响应**：发现任务收到带 `device_id/pairing_id/MAC` 的 `monitor_discover` 时，先尝试当前配对；失败后逐个加载墓碑，匹配 host/device/pairing 并用墓碑 key 验证请求 MAC。验证通过才回复 `monitor_announce(pairing_open=true, 原 pairing_id, MAC)`，响应 MAC 由同一 key 签名。此路径不创建配对候选，也不接受无认证请求；真正未配对的发现仍使用原未认证广播。
- **Client 处理**：`discoveryRevokes()` 只在 `device_id`、`pairing_id`、`pairing_open=true` 全部匹配且响应 MAC 用本机 `serial_key` 验证通过时调用 `revokeFromDiscovery()`：记录 `device_pairing_revoked_offline`，删除 `companion.json` 凭据、候选、recovery 与 pairing-lost 状态并停止会话。伪造、缺失或错误 MAC 一律不删除。Client 的 `maintain()` 会以 1–60 秒退避探测已配对设备，因此双方重新处于同一局域网后自动触发。
- **测试**：固件 host 34/34，含墓碑增删/去重/上限、v1 迁移、损坏 blob 安全、派生 key 与 keyed MAC 向量、撤销 announce 的 `pairing_open=true` 与篡改 key 失败；Client `go test -race ./...`、`go vet ./...` 通过，新增 `discovery_revoke_test.go` 覆盖伪造 MAC、非撤销响应、错误 pairing ID 和有效 MAC 删除凭据。
- **真机已验（烧录回归）**：新固件烧录后启动日志 `pair_discovery: pairing record: present, revocations=0`，44.6 秒出现 `authenticated LAN session starting`，随后 MQTT connected、视图 `overlay=1 proxy=1 weather=1 aqi=1 time=1`，说明已配对发现与 v2 读取无回归。
- **真机验收 A（板端保持未配对，2026-11-02）**：无活动会话时真实 BOOT 长按 5 秒，板端 `Companion pairing removed; device can pair again`；重启后 `pairing record: absent, revocations=1`，证明墓碑持久化。启动保留旧凭据的 Client 后，Agent 记录 `device_pairing_revoked_offline`，其 `companion.json` 的 `pairs` 从 1 变为 0，未发起新配对。
- **真机验收 B（板端已与另一 Client 配对，2026-11-02）**：恢复备份后再次 BOOT 长按撤销，板端生成 A 的墓碑；全新 B Client 完成发现、6 位码人工核对、板端 KEY 确认与 IPC 批准，B `connected`。停 B 并重启板端，日志 `pairing record: present, revocations=1`，即当前配对 B 与 A 墓碑并存。启动仍保存 A 凭据的 Client，Agent 再次记录 `device_pairing_revoked_offline`，A 的 `pairs` 变为 0；重启 B 后仍 `connected`，`pairs=1`，证明删除 A 不影响当前配对。
- **恢复**：测试后写回 NVS 备份，启动日志 `pairing record: present, revocations=0`；真实 Client 重新 `connected`，板端视图恢复 `weather=1 aqi=1`，真实凭据库保持 `pairs=1`。NVS 与凭据备份已删除，未提交或上传。

### Resync（LAN 余项）

- 板端在心跳的 `last_snapshot_seq` 或 `source_updates[].snapshot_seq` 与本会话已应用快照不一致时，不再直接断开，而是发送 `up/resync(missing_snapshot, last_snapshot_seq)`；同一会话最短 10 秒一次。心跳本身仍 ACK 并刷新应用回执，避免把“缺快照”误判为“链路失活”。
- Client 既有 `resync` 处理校验 reason/配置后重发完整快照；新增模拟器 `SendResync()` 与 `TestLANResyncRespondsWithSnapshot` 验证 Client 会返回完整快照。
- 固件主机测试覆盖首次 Resync、10 秒内不重复发送和心跳仍 ACK；真机只验证了带 Resync 代码的正常连接/重连，未在真机人为丢失快照。

### Serial F3 进度（帧/密钥层）

- `firmware/src/companion/monitor_serial_wire.c`：EMON 12 字节帧头、类型/seq/长度校验、CRC32 IEEE、magic 重扫、日志/噪声共存、半帧 2 秒丢弃后恢复、认证帧 HMAC、16336/2048 字节边界。
- `monitor_serial_derive_keys()`：按 `lp("ESP32MON-SERIAL-V1")||lp(host)||lp(device)||lp(pairing)||lp(session)||raw(host_nonce)||raw(device_nonce)` 构造 transcript，SHA256 盐、HKDF-SHA256 Extract/Expand（`frame`/`proxy-config`）、device/host proof。密钥文本通过既有 `monitor_companion_decode_nonce()` 解码。
- `firmware/components/firmware/monitor_serial_crypto.c`：复用配对模块的 PSA SHA256/HMAC，新增分三段（direction/header/payload）的 HMAC；AES-256-GCM（PSA AEAD）、Config nonce/AAD。
- `firmware/src/companion/monitor_serial_proto.c`：HELLO/AUTH/EncryptedConfig 的 protobuf 编解码（nanopb 未生成静态字段，使用有界手写编解码）；Envelope 解码复用 `monitor_proto_strict`。
- 主机测试 `firmware/tests/serial_wire_test.py`、`serial_proto_test.py`：用 Go `wire.Marshal` 独立生成的 Hello/Auth/EncryptedConfig 整帧向量逐字节比对 C 编码；覆盖 CRC/HMAC 篡改、方向反射、碎片/噪声、2 秒半帧恢复、尺寸边界、重复字段/截断拒绝。测试中的 SHA256/HMAC 是确定性桩，只验证 transcript/参数接线；PSA 原语本身未在该测试中执行。
- 主机测试总数 34/34 通过，新增 `test_serial_session.c` 覆盖 serial 明文 ACK/Goodbye、ConfigAck 后的快照/心跳、连接状态与重放拒绝。

### Serial F3 传输（真机互操作已验收）

- `monitor_serial_service.c`：USB Serial/JTAG 驱动与任务；HELLO request/response、AUTH challenge/device proof/host proof 校验、ACK、AES-256-GCM EncryptedConfig、ConfigAck、Snapshot/Heartbeat/ACK、1 秒板端心跳、3 秒失活超时、`host_revoked` 处理、BOOT 撤销时 `up/goodbye(pairing_revoked)`。Config 编解码缓冲已改到堆上（原 18KB 局部数组会超过任务栈），串口任务栈 16KB。
- 会话复用 `monitor_lan_session`：新增 serial 创建入口与 `monitor_lan_config_accepted()`；serial 的 ACK/Goodbye 直接发送内层 Envelope，不套 LAN Control。
- Snapshot 落到与 LAN 相同的 `monitor_companion_sink_*`，第二页/第一页代理行为一致；认证 Serial 优先：AUTH challenge 校验通过后先停止 LAN MQTT 并**等待 worker 退出**，再派生密钥和发送 Config。原 `monitor_companion_mqtt_stop()` 只发停止信号，HKDF/AES 会在 worker 释放 12KB 栈前运行并因内部 RAM 不足失败。
- 配对：已配对设备直接 AUTH；未配对时进入 `SERIAL_PAIRING`（见下节“Serial PAIR”）。
- 日志与帧共用 TX 互斥；**认证成功后 USB 口只承载二进制帧**，人类可读日志仅写 UART0。实测在认证后继续往 USB 写日志会在大帧（Config）期间造成主机侧丢字节/错位，配置帧 CRC/HMAC 失败；静默后配置、快照、心跳帧连续数十秒全部 CRC/HMAC 正确。
- 帧发送缓冲按实际长度从内部 RAM 分配，不再使用 16KB PSRAM 缓冲；避免外部内存缓存行为影响帧内容。
- **真机问题（已修复）**：关闭 ROM USB 控制台后，原来的 USB 控制台不再持有 `NO_LIGHT_SLEEP` 锁；系统进入 Light Sleep 后 USB Serial/JTAG 掉线，且主机侧无法重新枚举。`monitor_serial_start()` 现显式创建并持有 `ESP_PM_NO_LIGHT_SLEEP` 锁。
- **真机问题（已修复）**：串口 tick 使用循环开始时捕获的 `now`，而帧回调会用稍新的时钟写 `last_rx_ms`；`uint64 now - last_rx_ms` 下溢成巨大值，导致刚收到数据就误判 3 秒超时。超时/心跳比较均改为先判断 `now >= last_*` 再相减。
- **真机问题（已修复）**：Client 的串口连接原先绑定 HTTP 请求 context；GUI 请求取消或 curl 超时会拆掉已建立会话。IPC `/v1/monitors/serial/connect` 现使用 `context.WithoutCancel`，会话继续运行；重开端口时 `ResetInputBuffer` 并跳过上一会话遗留的陈旧帧，避免 `invalid_message`。
- **真机问题（已修复）**：GUI 串口连接把 `device_id` 误当作端口传入，已改为 `port.port`。
- **内存配置**：`CONFIG_SPIRAM_TRY_ALLOCATE_WIFI_LWIP=y`、`CONFIG_SPIRAM_MALLOC_ALWAYSINTERNAL=8192`。此前 Wi-Fi+MQTT 后内部 RAM 仅剩数百字节，PSA HKDF/AES-GCM 分配失败；调整后串口启动内部 RAM 约 218KB，认证前仍有约 18–33KB。
- **未完成**：Serial 模式功耗实测、72 小时长稳。

### Serial PAIR 未配对物理确认（2026-11-01 晚完成）

- **板端状态机**：`monitor_serial_service.c` 新增 `SERIAL_PAIRING`。未配对 HELLO 在 HELLO response 后发送 `PairRequest(begin)`（设备 ID、名称 `ESP32 Monitor`、本次 HELLO 的 device nonce）；收到 host 的 `PairResponse(pending)` 后调用 `monitor_pairing_begin()` 生成 6 位短码并通过 pairing service 显示弹窗；KEY 单按确认、BOOT 单按取消沿用现有按键路由（`monitor_pairing_service_action`），确认后发送 `PairRequest(confirm, device_confirmed=true)`。approved 后 `monitor_pairing_store_approved()` 写入 NVS、加载 `serial_key` 并将状态切回 `SERIAL_HELLO_SENT`，随后 Client 的 AUTH 直接完成。
- **超时与取消**：等待 host pending 5 秒、确认后等待 approved 5 秒；配对等待受 host 的 `expires_in_ms`（≤120 秒）约束。取消/过期发送无认证 Error 帧让 Client 立即结束配对等待，并隐藏弹窗、释放串口任务。配对弹窗只在收到 host pending 后出现，单独的串口探测（只读 HELLO）不会弹出或锁住设备。
- **Client 修复**：串口配对窗口创建时设置 `hostConfirmed=true`（物理连接+显式设备选择的同意已成立），否则 `pairWindow.approve()` 永远不会批准；新增 `TestSerialPhysicalPairing` 覆盖 begin→pending→confirm→approved→AUTH 全链路、短码一致性和 NVS 等价存储。
- **串口撤销修复（关键）**：Client 的 `host_revoked` Goodbye 在串口上原先发出后立即关端口，USB CDC 末包可能丢失，板端仍保留旧配对而 Client 已删除凭据。`session.run` 的关闭路径现在对 serial 在 Goodbye 后保留 500 ms（实测撤销调用到会话关闭 505 ms），板端完成 `pairing record revoked` 后再释放端口。
- **真机证据（`/dev/cu.usbmodem1101`，真实 Client Agent）**：
  - 21:02:46 未配对串口连接，`pairing_approved` → `device_session_open transport=serial` → 21:02:49 `connected`；
  - 21:09:58 串口会话中 Client 撤销，`device_pairing_revoke` → 505 ms 后 `device_session_close`，ping 板端立即恢复；随后对板端发 HELLO 得到 `pairing=<nil>`，证明板端 NVS 配对已清除；Client `companion.json` 的 `pairs` 为空；
  - 21:11:10 正式固件（无自动确认）未配对连接：Client `PairingStatus` 为 `pending` 且有 6 位短码，9 秒后仍无会话且日志无 `pairing_approved`，证明必须由板端物理确认；
  - 21:13:18 临时自动确认构建完成最终配对（仅用于测试，已移除），21:15:31 正式生产固件用已保存配对直接串口 `connected`，无配对弹窗，期间 ping 板端全部丢失（Wi-Fi 挂起）。
- **人工目视**：配对弹窗已在真机 LCD 出现（用户确认），随后被新一帧覆盖消失；真实 KEY 按下确认已在 2026-11-02 完成，BOOT 取消路径已由按键路由测试覆盖。Client GUI 现在显示同一 6 位短码（详见下文），板端提示 “Check code on Client” 已与实际流程一致。
- **测试构建清理**：临时 `MONITOR_SERIAL_PAIR_AUTOCONFIRM` / `MONITOR_SERIAL_TEST_REVOKE` 与调试抓包测试已全部删除；板上最终运行无自动确认的生产固件。

### 串口配对 UI 与流程一致性（2026-11-02 完成）

- **统一最终确认**：串口和 LAN 的最终弹窗都只显示 6 位短码与「取消等待」，不再要求 Client 二次确认；Client 在 LAN 开始前的显式设备选择和明文风险勾选仍保留，最终确认由 Monitor 上的 KEY 完成（`device_confirmed`）。`pairWindow` 在 LAN `BeginPairing` 和串口 attach 时设置 `hostConfirmed=true`。
- **取消**：新增 `POST /v1/monitors/pairing/cancel` 与 `Backend.CancelPairing()`。串口取消会取消等待上下文、立即释放端口和窗口；LAN 取消同时关闭配对监听，允许立即重试。修复“取消后 120 秒内重试得到 `session_conflict`”。
- **连接 RPC 语义**：`Application.ConnectMonitorSerial` 在后台运行会话，并在 `SerialConnected` 为真后立即返回；GUI 不再等到串口断开才收到成功。后续断开通过设备状态体现，不再误报“串口连接失败”弹窗。
- **迟到握手帧**：已认证串口会话忽略迟到的 HELLO/AUTH/PAIR 帧，避免配对批准后的首个会话被误判 `session_expired`（此前表现为配对成功但 Client 显示离线，重启后才恢复）。
- **发现列表状态**：串口行根据配对/在线状态显示「探测并连接」「连接」或「已连接」；配对成功后立即原位更新，不再继续显示「探测并连接」。串口路径写入本机凭据记录（`serial_port`，非秘密），Agent 重启后仍能恢复该端口的配对身份；连接旧配对时也会自动补写。发现结果右侧操作列固定宽度并右对齐，长端口名换行时按钮仍对齐。
- **串口过滤**：`ListSerialPorts` 只保留可能为 ESP32 的端口：Espressif `303A`、CP210x `10C4`、WCH `1A86`、FTDI `0403`，或无 USB 身份但名称属于 `usbmodem/usbserial/SLAB_USBtoUART/wchusbserial/ttyACM/ttyUSB` 的端口；蓝牙、调试控制台等虚拟端口不再列出。
- **测试与真机证据**：`go test -race ./...`、`go vet ./...`、前端构建与 13 项测试通过；新增 LAN 仅设备确认配对测试、串口迟到握手帧测试、串口候选过滤测试和取消窗口测试。电脑操控实测串口配对弹窗显示短码、取消后立即重试成功、配对成功后行状态更新；真机日志 12:48:41 `pairing_approved` → 12:48:43 `connected`，无需重启板端或 Client。

### 同设备 LAN/串口共享配对与自动连接（2026-11-02 用户要求）

- **用户现场**：standalone 下 Client 与开发板处于同一 Wi-Fi 且 USB 已连接；一次发现同时出现串口和 LAN 两条连接。点击 LAN 设备配对后，LAN 行按钮没有更新，再次点击报 `session closed`。重新发现后 LAN 显示已配对、串口显示已连接，Client 与开发板通信正常，但 Monitor 屏幕仍显示 Wi-Fi 图标。日志包（项目根目录 `esp32-monitor-logs-20261102-130829.zip`）时间线为 13:02:43 串口 `connected` → 13:05:59 撤销 → 13:06:31 LAN `pairing_opened` → 13:06:36 `pairing_approved` → 13:06:42 LAN `connected`，说明当时活动链路是 LAN；USB 没有自动接管，Wi-Fi 图标是 LAN 会话的真实状态，不是 UI 误报。
- **用户要求**：
  1. 同一设备与同一 Client 在 Wi-Fi 和串口之间共享配对状态。
  2. 发现设备时，对可能为 ESP32 的串口也主动发起探测。
  3. LAN 配对成功后插入 USB，自动切换到串口模式。
  4. 串口像 LAN 一样，换串口（端口名变化）也能自动连接。
  5. Client 运行期间，已配对 Monitor 出现在局域网或接入串口时自动连接。
  6. 同一设备连接优先级：串口 > LAN。
- **后续修订（2026-11-02）**：用户要求移除已配对设备上的「启用局域网」按钮。现在一次配对（无论 LAN 还是串口）同时授权两种传输，Client 直接使用同一 `Pairing` 记录自动连接；LAN 首次配对仍显示明文风险提示，但不再有第二种传输的额外启用步骤。
- **实现要点**：Client 新增串口看护循环，周期枚举可能为 ESP32 的端口；已保存 `serial_port` 的端口直接认证连接，未知/改名端口先发只读 HELLO 识别。识别为已配对设备后建立串口会话；串口认证成功后取消同设备 LAN 会话，板端在 AUTH challenge 时先停止 LAN MQTT，形成“串口 > LAN”。串口连接进行期间以 `serial_pending` 阻止 LAN recovery 邀请。LAN 配对批准后重置该设备的串口探测退避，使已插入的 USB 在约 2 秒内完成切换。停用设备会同时阻止新串口会话（包括握手完成前的竞态）。发现流程现在并行探测最多 2 个候选端口，跳过已占用/已探测端口，并使用 5–60 秒退避，避免干扰终端和其他应用。
- **安全边界（2026-11-02 用户后续要求）**：探测只发送 HELLO，不开始配对、不加锁；配对凭据仍由同一个 `Pairing` 记录（`device_token`/`serial_key`/`pairing_id`）供两种传输共用。一次配对（LAN 或串口）即同时授权两种传输，Client 不再要求串口配对的设备在 GUI 中额外「启用局域网」，LAN 配对也不再要求额外启用串口。LAN 仍是无 TLS 的明文传输；LAN 首次配对时仍显示明文风险勾选作为提示，但串口配对不会重复阻断。不得把该行为描述为 LAN 已加密。
- **UI**：LAN 配对批准后立即把同一 `device_id` 的所有发现行标记为已配对并刷新设备列表；串口行在会话建立/连接中时隐藏操作按钮。已配对设备卡和总览设备卡显示当前传输（串口/局域网）。已移除「启用局域网」按钮，两种传输配对后直接自动可用。
- **测试（2026-11-02）**：新增 `serial_watch_test.go` 覆盖：已保存端口自动连接、端口改名后先 HELLO 探测再连接、串口 pending 时拒绝 LAN invite、重新启用时优先已知串口、探测到的身份优先于过期端口映射。Client `go test -race ./...`（transport 约 112 s）与 `go vet ./...` 通过；前端类型检查、构建和 13/13 DOM 测试通过。
- **真机与 GUI 证据（2026-11-02 13:25–14:06，`/dev/cu.usbmodem1101`）**：
  - 旧 Client 在 13:06:42 以 LAN 进入 `connected`；新 Client 启动后 13:25:04 关闭 LAN 会话，13:25:08 `serial_autoconnect`，13:25:09 串口 `connected`。期间 ping 板端全部丢失，证明板端已停止 Wi-Fi 并进入 Serial 模式；GUI 总览和 ESP32 页显示 `— · 串口 · 已连接`，发现结果中串口行为「已连接」、LAN 行为「串口已连接」。
  - 停用再启用时先出现 LAN 会话，串口认证后立即关闭 LAN（13:26:47 LAN open → 13:26:48 LAN close + serial open）；加入“重新启用先启动已知串口”后，13:28:34 串口关闭到 13:28:40 自动恢复期间没有新的 LAN 会话，13:36 重启也直接由串口接管。
  - 13:43 最终构建的 IPC 发现同时返回两条路由，`device_id` 均为 `67930f18…`：LAN 行 `paired=true, connected, online`（`identity_verified=false`），串口行 `/dev/cu.usbmodem1101` `paired=true, connected, online, identity_verified=true`；同一次发现中 LAN 与串口共享同一配对状态，活动传输为串口。
  - 真实拔插：14:01:44 串口会话关闭且 `/dev/cu.usbmodem*` 消失，14:02:17 LAN 会话自动建立；重新启动 Client 后也直接以 LAN `connected`。反向切换同样不需要任何「启用局域网/启用串口」操作，证明一次配对直接覆盖两种传输。
  - **未实机覆盖**：USB 端口名变化后的重新识别（单测已覆盖 HELLO 探测路径）、以及重新配对后 LAN 行即时更新（为避免再次消耗/重建配对凭据，本轮未重复撤销重配）。USB 图标与此前 F4 验收一致，真实拔插时未截取 LCD 画面。

### Serial F4 模式/电源与状态图标（2026-11-01 晚完成）

- `monitor_wifi_suspend()` / `monitor_wifi_resume()`：Serial 会话运行后真正调用 `StopStation()` 与 `StopConfigAp()`（含 AP、DNS、配置 WebServer），暂停 Wi-Fi 回调、超时定时器和按键配置模式；有凭据时恢复 Station，无凭据时恢复配置 AP。退出 Serial 后时钟/天气/Devices 直连生产者随 `Connected` 事件恢复。若串口在 Wi-Fi 启动前完成认证，`monitor_wifi_start()` 会延迟电台启动。
- **时序修正（关键）**：最初在 AUTH 成功后、Config 发送前停止 Wi-Fi。启动日志占满 USB TX 环形缓冲时，Config 大帧会出现写入超时或主机解到损坏帧（CRC 失败、`session ended: config`）。现在 Config 仍在 Wi-Fi 开启时发送，收到合法 ConfigAck 并发出首个心跳后才挂起 Wi-Fi；AUTH 后先静默 USB 日志并调用 `usb_serial_jtag_wait_tx_done(2s)` 排空积压，`tx_bytes` 在 3 秒总期限内重试部分/超时写，避免把未送达的大帧报告为成功。
- **Client 修复**：串口启动阶段在收到 Config 前不再按 3 秒运行看门狗判死，改用 15 秒会话窗口；任何合法串口帧都刷新应用回执；首帧等待 12 秒；类型为 HELLO 但内容不是 hello response 的帧立即拒绝，陈旧心跳/Goodbye/旧 nonce 响应仍被跳过。
- **Serial 状态图标**：共享 UI 新增 `MONITOR_WIFI_SERIAL`，串口会话活动时在 Wi-Fi 图标位置显示 `LV_SYMBOL_USB`（无断线斜线），日期行保留正常日期；`monitor_misans_14.c` 由 `generate_misans.py` 重新生成并加入 FontAwesome `0xf287`。新增确定性模拟器场景 `serial-status`，已渲染检查；真机 LCD 显示已由用户确认。
- **真机时序自测**：临时开机探针测得 `monitor_wifi_suspend()` 36 ms、`resume` 4 ms，无重启；诊断代码已全部移除。
- **真机最终验证（生产固件）**：20:47:46 串口连接，T+3/6/9 秒均为 `connected` 且期间 ping 192.168.110.169 全部丢失；调用方 14 秒超时后会话仍保持 `connected`（`context.WithoutCancel`）；关闭串口后 6 秒内 ping 恢复；重新使能后 6 秒内 LAN 回到 `connected`；串口探针返回真实设备名 `ESP32 Monitor`，确认生产固件不含诊断名称。
- **未完成**：串口功耗必须在 USB 断开、电池接口上多分钟对比；72 小时长稳与异常恢复仍未执行。

### LAN 内部 RAM 耗尽与 90 秒过期重连（2026-11-01 21:40–22:05，已解决）

- **根因**：每个 LAN 会话在 internal RAM 分配两个 `Envelope`（各 6128 B，共 12.2 KB）和一次性 `monitor_companion_mqtt_snapshot_t`（约 7.9 KB，长期驻留），再加 8 KB MQTT worker 栈。会话结束后大块交替分配/释放，internal 最大连续块降到 1.4 KB，下一次会话无法创建 worker：`worker task create failed: internal free=2295 largest=1408` → `LAN session unavailable: ESP_ERR_NO_MEM`。此前的 Serial→LAN 切换放大了碎片化。
- **修复**：`monitor_lan_session.c` 的会话双重 Envelope 与 presence 临时 Envelope 改用 `heap_caps_calloc(MALLOC_CAP_SPIRAM)`（主机测试回退 `calloc`）；`monitor_companion_mqtt.c` 的 sink 快照同样放 PSRAM。worker 栈保持 internal 8 KB：worker 会执行 NVS 撤销（写 flash），PSRAM 栈在 cache 关闭时不可用。
- **修复后真机堆**：会话建立前 `int_free=32163 largest=19456`；worker start `int_free=23371 largest=11264`（修复前 15463/7680）；连接期稳定在约 21.3 KB / 11.3 KB，会话结束 `worker freed` 后 23.6 KB，恢复生产者后 41.7 KB。未再出现 `ESP_ERR_NO_MEM`。
- **90 秒过期真机验证**：21:55:17 冻结 Client Agent，板端于 141.076 s 记录 `session ended: expired`；`heap worker freed` + `direct producer resumed`（weather/devices）。21:57:10 恢复 Agent，21:57:22 新会话 `syncing_config → waiting_snapshot → connected`，视图回到 `overlay=1 proxy=1`。另一次 Agent 重启也在同一构建上完成重连。
- **注意**：串口会话同样受益于 PSRAM Envelope；serial 快照/心跳仍由 USB CDC 承载，不受该改动影响。

### Client 天气代理 gzip 响应（2026-11-01 22:00–22:04，已解决）

- **现象**：Client 连接后板端视图持续 `overlay=1 proxy=1 devices=4/2 weather=0 aqi=0`，第一页无天气；板端自身 HTTP 抓取正常。
- **配置来源（确认无需新增）**：板端通过 LAN `up/proxy-config`（Config）把 NVS 中的 QWeather host/key、location（`ip` 或 `coordinates`）与 Devices 配置上传给 Client；Client 用该 Config 启动 proxy worker。Client 此前已接受该 Config（`syncing_config → waiting_snapshot`），问题不在上传或校验。
- **根因**：`client/internal/transport/proxy.go` 的 `fetch` 固定发送 `Accept-Encoding: identity`。QWeather 仍返回 `Content-Encoding: gzip`；Go 在调用方手动设置该头时不会自动解压，Client 以 `invalid_upstream` 丢弃响应。诊断日志：`proxy_fetch_failed ... content_encoding=gzip`。板端自己实现了 gzip 解码，所以板端天气不受影响。
- **修复**：`fetch` 不再固定 `Accept-Encoding`，由 Go Transport 协商 gzip 并透明解压；保留非 identity/gzip 编码的拒绝。新增 `TestProxyFetchForcedGzip`（上游无条件返回 gzip）作为回归测试。
- **真机验证（最终构建）**：Client 会话 `connected` 后板端视图 `companion view overlay=1 proxy=1 devices=4/2 weather=1 aqi=1 time=1 ...`，第一页显示 Client 代理的天气与 AQI。

### 逐来源续租与启动接管（2026-11-02，已实现并实板验证）

- **固件视图**：`monitor_companion_view_build` 增加 heartbeat 与快照/心跳两个 elapsed。`leased_age()` 只接受 `snapshot_seq == 当前快照 seq`、path 匹配、`age_ms >= 0` 且 `observed_at/last_success_at > 0` 的 lease；有效年龄取 `min(snapshot age + snapshot elapsed, lease age + heartbeat elapsed)`，旧 lease 不会让数据“变新”，零时间戳等非法 lease 被忽略。
- **路径与时钟**：github、codex.quota、codex.tasks、music、proxy.weather/aqi/devices 按真实观测续租；时钟由心跳 `time` 单独续租，不再依赖完整快照。代理总体不再用聚合 age 一票否决，各来源独立过期，单个来源失效只影响自身卡片；`proxy_owned` 只要求当前会话持有 proxy data。
- **Client**：删除每 10 秒强制完整快照，保留语义变化时立即快照；Serial 心跳不再清空 `source_updates`。语义比较覆盖 GitHub、Codex quota/tasks 与第一页 proxy（weather/aqi/devices/time offset）；仅 `age_ms`、`observed_at`、`last_success_at`、proxy time 的 `utc_unix_ms`、音乐 `position_observed_at` 等元数据变化不发送快照。`TestLANRefreshesUnchangedSnapshot` 改为 `TestLANKeepsHeartbeatsWithoutSnapshotRefresh`（13 秒内不得有整快照，心跳仍需继续），新增 `TestSourceLeasesCarryOnlyObservedHealthySources`、`TestSemanticIgnoresTimePassageAndObservationMetadata` 与 `TestSemanticDetectsContentChanges`（逐项验证 GitHub/Codex/代理内容变化仍会推送）。心跳按协议继续发送；初版只携带新观测推进的 lease，不重复完整数据。**该去重在 2026-11-02 晚被修正**：健康但无新观测的来源会因此失去 lease、回退到快照年龄并在 15 秒后过期，现改为每次心跳重复发送全部健康来源（age 按发送时重算）。`desktop-ipc` 的 15 秒静默失效也一并移除，详见“2026-11-02 接续记录”和下方补丁记录。
- **启动接管**：`main.c` 的启动动画等待在 `monitor_companion_mqtt_running()` 时结束。此前 LAN 在 32.6 s 认证后仍要等到 123.9 s 的启动超时才应用 Companion；修复后 33.6 s 即结束等待。
- **主机/模拟器/回归**：固件 34/34；模拟器 34/34；Client 全量 `go test -race ./...` 通过（transport 104.8 s）、`go vet ./...` 通过。新增视图测试覆盖 lease 匹配 seq、零时间戳拒绝、lease 年龄随时间增长、设备 stale 与心跳时间续租。
- **实板（2026-11-02 11:24–11:26 UTC+8，生产固件 + 真实 headless Agent）**：32.568 s 认证会话开始，33.596 s 启动等待结束；36.667 s `overlay=1 proxy=1 time=1`，37.694 s `weather=1 aqi=1`。首个快照约 37 s，之后 67.385/98.105/128.824 s 三次心跳视图均为 `time=1`；时钟 TTL 60 s，128.8 s 距快照约 91 s 仍有效，证明来自心跳时间续租而非整快照刷新，全程无 `session ended`。
- **实板补证（11:27–11:29，`--music --codex`）**：板端 216.9–279.4 s 持续 `music=1`（62.5 s，远超 15 s TTL），期间曲目 `not_playing`、位置与元数据稳定，视图约每 10 s 由心跳触发，证明音乐靠心跳 lease 续租；Codex `usage=2/71` 同时保持。GitHub 未登录启用，Devices URL 已清除，其他 proxy 子来源没有 15 秒级 TTL 可用于观测。
- **未覆盖**：GitHub 真实数据与 Serial 心跳 `source_updates` 尚未在有真实来源的实板上验证（Serial 仅主机测试覆盖）；不得据此宣称这两项已完成实板验收。
- **重复 lease 补丁（2026-11-02 晚）**：`session.heartbeat` 删除按 `LastSuccessAt` 去重的 `leased` 表，每次心跳携带 `leases(sections, lastSnapshot)` 全量健康来源，age 在发送时重新计算；固件仍只接受 `snapshot_seq` 匹配当前快照的 lease，因此重复发送不会延长寿命。`desktopipc` 移除 15 秒 `observed` 年龄失效，只在 `!connected` 或线程从未观测时置为 unknown，并在连接且健康时每个 Snapshot 刷新 `LastSuccessAt`。测试：`TestHeartbeatRepeatsUnchangedSourceLeases`（两次心跳 LastSuccessAt 不变、AgeMS 1s→3s，仍携带 music lease）、`TestDesktopQuietConnectedStreamKeepsTaskFresh`（20 秒静默保持 Running 与新鲜 `LastSuccessAt`，断开后转 Unknown/Degraded）；Client 全量 `go test -race ./...`、`go vet ./...` 与前端 `npm test` 通过。

### 内部 RAM 与网络余量（2026-11-02 Serial 实测）

**条件与限制**：开发板通过 `/dev/cu.usbmodem1101` 连接电脑，现场无可用局域网；本轮只覆盖 Serial 会话，不覆盖 Wi-Fi 已连接时的 MQTT/TLS 峰值，也不覆盖串口→LAN 切换窗口。Client 使用真实 headless Agent 与已保存配对，板上运行生产构建。测量包含常驻 16 KB 串口任务栈和 8 KB MQTT worker 栈，未缩小任何已验证栈。

**会话堆汇总（固件诊断，保留）**：`monitor_serial_service.c` 在认证会话结束、发送 Goodbye 帧之前输出一行：

```text
companion_serial: serial heap close: start free=… largest=… session_min_free=…
session_min_largest=… suspend free=… largest=… now free=… largest=… psram_free=…
```

- `start`：认证成功、发送 ACK/Config 之前的内部 RAM；
- `session_min_free`：IDF `heap_caps_monitor_local_minimum_free_size_*` 统计的会话内最低空闲；监控启动失败时输出 `0`，不可当作真实余量；
- `session_min_largest`：认证、Config 缓冲分配、Config 发送完成和每次收到数据帧时采集的最小最大连续块；采样点有限，可能漏掉更短的瞬时低谷；
- `suspend`：ConfigAck 后、调用 `monitor_wifi_suspend()` 之前；
- `now`：结束前、销毁 LAN 会话结构之前；
- `psram_free`：结束时 PSRAM 空闲。
- 该行以文本写入 USB（并同步写 UART0）后才发送二进制 Goodbye；Client 解码器按噪声跳过，已用正式 Client 验证 Goodbye 后自动重连。`serial heap after close` 在恢复 Wi-Fi 后输出，但 Client 通常已关闭读取，只作尽力而为的记录。

实现入口：`firmware/components/firmware/monitor_serial_service.c` 的 `serial_ctx_t` 堆字段、`heap_sample()`、`serial_usb_line()` 与 `end_session()`；认证时启动 IDF 本地最低空闲监控，在 Config 缓冲分配、Config 发送完成和每次收到数据帧时采样最大连续块。串口帧格式、16 KB 串口任务栈和 8 KB MQTT worker 栈均未改变。

**测量方法**：每组配置先复位开发板；Agent 自动建立串口会话后运行约 15–20 秒，然后 `SIGSTOP` Agent 4 秒模拟板端 3 秒失活超时（Agent 仍持有端口但不读不写），再 `SIGCONT`；板端在 Goodbye 前输出汇总，Client 的临时噪声日志采集该行后立即还原，未留在生产代码。每组重复 2–3 轮（仅 Companion 2 轮，其余 3 轮）。

**结果（内部 RAM，字节）**：

| 场景 | 轮次 | start free / largest | session min free / largest | suspend free / largest | now free / largest | PSRAM free |
|---|---|---|---|---|---|---|
| 仅 Companion | 1（开机后首会话） | 45,071 / 31,744 | 26,511 / 24,576 | 43,283 / 31,744 | 35,279 / 26,624 | 5,607,536 |
| 仅 Companion | 2 | 34,603 / 26,624 | 24,427 / 16,384 | 34,583 / 26,624 | 35,267 / 26,624 | 5,607,536 |
| Companion + Codex | 1（开机后首会话） | 45,071 / 31,744 | 26,463 / 24,576 | 41,527 / 24,576 | 35,231 / 24,576 | 5,607,536 |
| Companion + Codex | 2 | 34,555 / 24,576 | 24,379 / 14,848 | 34,535 / 24,576 | 35,219 / 24,576 | 5,607,536 |
| Companion + Codex | 3 | 34,551 / 24,576 | 24,375 / 14,848 | 34,527 / 24,576 | 35,219 / 24,576 | 5,607,536 |
| Companion + Codex + 音乐 | 1（开机后首会话） | 45,959 / 31,744 | 15,167 / 7,680 | 45,491 / 31,744 | 19,987 / 7,680 | 4,645,988 |
| Companion + Codex + 音乐 | 2 | 21,275 / 7,680 | 11,003 / 7,680 | 21,255 / 7,680 | 21,923 / 7,680 | 4,645,988 |
| Companion + Codex + 音乐 | 3 | 21,255 / 7,680 | 11,079 / 7,680 | 21,235 / 7,680 | 21,923 / 7,680 | 4,645,988 |

**结论**：

1. **首次会话不代表稳态**：开机后首会话 `start` 约 45–46 KB，是因为 Wi-Fi/直连生产者和 CJK 字体尚未在会话中完成初始化。首会话结束后，无音乐稳态约 `34.6 KB`（`largest` 24.6–26.6 KB），有音乐约 `21.3 KB / 7,680 B`。
2. **首会话后的约 8–10 KB 内部下降来自网络/直连生产者恢复，不是 Codex**：仅 Companion 与 Companion+Codex 的第 2 轮几乎相同（34,603 vs 34,555 B）。`monitor_wifi_suspend()` 在无 AP 状态只回收约 690 B，说明该下降不是可随挂起释放的 Wi-Fi 瞬时缓冲；LAN 连接后的行为仍未知。
3. **CJK 音乐显示使内部余量明显变紧**：音乐路径使稳态 `start` 再降约 13.3 KB，`largest` 从 24.6–26.6 KB 降到 7,680 B；PSRAM 同时减少 961,548 B，与 929 KB 字体文件基本对应。内部下降更可能来自 LVGL 字形缓存/UI 状态，字体数据本身在 PSRAM；本轮未逐项拆分。
4. **会话瞬时约 10.2–10.3 KB**：无音乐 `start→min` 约 10.2 KB，有音乐约 10.3 KB，与 Config 加密缓冲（约 9.4 KB）和帧发送缓冲一致。音乐场景 `session_min_largest=7,680 B` 低于 8,192 B 的 `send_envelope` 临时编码缓冲；`malloc` 可能回退到 PSRAM，但 PSA HKDF/AES-GCM 和 MQTT worker 栈仍依赖内部 RAM，因此不能把 7,680 B 当作安全余量。
5. **重复轮次间没有累积碎片**：无音乐第 2/3 轮 `start` 差 4 B，有音乐第 2/3 轮差 20 B。此前串口→LAN 的 `ESP_ERR_NO_MEM` 主因仍是会话大对象/连续块；当前 Serial 路径未复现。
6. **`monitor_lan_session_t` 仍内联 8 KB `data[8192]`**：结构总大小约 9–10 KB，当前分配阈值下推定由 PSRAM 承载；不能依赖该行为，若分配策略或结构大小变化，会直接压缩内部余量。

**尚未覆盖 / 下一步**：

- LAN 恢复后，用同一诊断在 Wi-Fi 已连接 + MQTT 会话 + 天气/TLS 请求期间重复 3 轮，记录连接态 `suspend` 和会话最低值；重点复查串口→LAN 切换窗口的直接天气 `ESP_ERR_HTTP_CONNECT`。
- 继续 72 小时长稳，覆盖反复串口插拔、Agent 重启、板端重启和音乐字体已加载/未加载两种状态。
- 在获得更多证据前不缩小 16 KB 串口任务栈或 8 KB MQTT worker 栈。若要提高音乐场景余量，优先考虑把 `send_envelope` 的 8,192 B 编码缓冲改为 PSRAM（最终 USB 帧仍用内部精确长度缓冲），再用 CRC/HMAC 和 3 轮堆对照验证；本轮未实施。
- 真实 GitHub/Serial 来源续租仍待有真实来源的实板补证；与本节的 RAM 数据分开记录。

### 验证记录

- 2026-11-02 晚（Windows 固件）：`. C:\Users\zy201\esp\esp-idf\export.ps1` 后 `idf.py -C firmware build` 成功，`idf.py -C firmware -p COM5 flash` 成功并完成哈希校验；启动日志确认 LVGL、启动动画与面板模式正常。两个时钟 Python 测试（`clock_sync_test.py`、`clock_client_time_test.py`）通过；Windows UCRT64 下完整主机 CMake 套件因 `sys/select.h` 缺失未运行，不能记作 34/34。现场无可用 AP，未验证 Wi-Fi/LAN。
- 2026-11-02 晚（Client）：`CGO_ENABLED=1 make test`（`go test -race ./...`）通过（transport 约 114.7 s），`make check`（`go vet ./...`）通过，前端 `npm test` 类型检查、构建与 13/13 DOM 测试通过。新增 `TestHeartbeatRepeatsUnchangedSourceLeases`、`TestDesktopQuietConnectedStreamKeepsTaskFresh`；串口交接提示、GitHub 加载状态和配对短码字体只经前端构建与测试，修复后的真实串口配对/Windows GUI 仍需实机复测。
- 2026-11-02 Serial 内部 RAM 实测：固件 `./firmware/tests/run.sh` → 34/34 通过；`idf.py -C firmware build` 与烧录校验通过；Client `go test -race -count=1 ./...` 全量通过（transport 111.6 s）、`go vet ./...` 通过。仅 Companion、Companion+Codex、Companion+Codex+音乐三组分别完成 2、3、3 轮串口连接/超时/重连，数据见“内部 RAM 与网络余量（2026-11-02 Serial 实测）”。清理后的正式 Client 在结束汇总行 + Goodbye 后自动重连并回到 `connected`，未出现解码错误；测量用临时噪声日志已还原。
- 2026-11-02 同设备双路由：Client 新增 `serial_watch_test.go` 五项用例，`go test -race ./...`（transport 112.1 s）与 `go vet ./...` 通过；前端 `npm test` 类型检查、构建与 13/13 通过。真机 13:25 旧 LAN 会话关闭后自动切串口并在 1.8 s 内 `connected`，ping 板端全部丢失；13:28 重新启用后串口自动恢复且未再建立 LAN 会话；14:01 拔掉 USB 后 14:02 自动回 LAN，反向切换同样无需显式启用另一种传输。GUI 总览/ESP32 页和发现结果均显示当前活动传输，已配对卡只剩「停用/解除配对」。

- 2026-11-02 逐来源续租轮次：固件 34/34、模拟器 34/34、Client 全量 `go test -race ./...` 与 `go vet ./...` 通过；实板证据见“逐来源续租与启动接管”。实板音乐在 15 秒 TTL 后继续显示 62.5 秒，确认真实来源 lease 生效。`TestSemantic*` 证明 GitHub/Codex/第一页仅元数据变化时不产生快照，内容变化时仍产生快照。
- 固件主机测试：`./firmware/tests/run.sh` → 34/34 通过（含 LAN session/Resync、MQTT framing、companion view、serial wire/proto/session，以及 Wi-Fi suspend/resume、Serial 优先按键与串口启动窗口用例）。本轮 LAN 内存改动与天气修复未改变主机测试数量。
- 模拟器：`ctest --test-dir simulator/build --output-on-failure` → 31/31 通过；新增确定场景 `companion-live`、`companion-update`，已目视检查 `activity-live` 风格输出与 CJK 更新。
- Client：`make test`（`go test -race ./...`）全量通过（transport 58.0 s）；`make check`（`go vet ./...`）通过。新增 `TestProxyFetchForcedGzip` 覆盖 QWeather 强制 gzip 响应；此前新增 `TestLANRevokeSendsGoodbye`、`TestLANPairingRevokedByDevice`、`TestLANResyncRespondsWithSnapshot`、`TestSerialPhysicalPairing` 仍通过。
- Serial PAIR 真机（2026-11-01 21:02–21:15）：真实 Client `ConnectSerial` 首次完成 `pairing_approved` → Serial `connected`；正式固件未配对连接在 9 秒内保持 `pending` 且有短码、无 `pairing_approved`，证明无物理确认不会配对；临时自动确认构建完成最终配对后，正式固件于 21:15:31 用已保存配对直接 `connected`，ping 板端丢失（Wi-Fi 挂起）。串口撤销于 21:09:58 在 505 ms 内结束会话、ping 恢复，随后板端 HELLO `pairing=<nil>`、Client `pairs` 为空。临时自动确认/撤销测试代码与抓包测试已删除。
- 前端：`cd client/frontend && npm test` → 类型检查、构建与 13/13 DOM 测试通过。
- Serial 真机互操作（2026-11-01 20:11–20:14，UTC+8，`/dev/cu.usbmodem1101`）：真实 Client Agent `ConnectSerial` 完成 HELLO/AUTH/EncryptedConfig/ConfigAck/Snapshot/Heartbeat/ACK，`connected` 后持续 45 秒；44 秒抓取 52 个板端心跳与 45 个 ACK，全部 CRC/HMAC 正确、无 Goodbye。调用方 HTTP 在 5 秒后取消不影响会话；`enabled=false` 关闭串口会话后 LAN 重新认证成功；再次串口连接（存在上一会话陈旧帧）于 20:13:45 再次 `connected`。此前误判“连接后立即断开”的根因是 `uint64` 超时下溢。
- 固件：`idf.py -C firmware build` 成功；`idf.py -C firmware -p /dev/cu.usbmodem1101 flash` 成功；临时验收构建与生产构建均已验证，最终板上运行的是生产固件。
- 真机链路：开发板 192.168.110.169，Mac/Client 192.168.110.2，Client Broker 18883，本机 Devices API 18081（4 台设备）。板端视图日志示例（修复后）：`companion view overlay=1 proxy=1 devices=4/2 weather=1 aqi=1 time=1 github=0 ...`；`devices=4/2` 表示 4 张卡、状态 READY。修复前同一路径为 `weather=0 aqi=0`，根因是 Client 拒绝 gzip 响应，不是板端未配置 QWeather。
- 真机撤销：Client→板端与板端→Client 均完成；恢复原配对后再次 `connected`。详细证据见“双端撤销”一节。
- 真机串口统计（本次验证）：`connected` 期间 44 秒内 52 个板端心跳、45 个 ACK，Config 帧 1 个，CRC/HMAC 失败 0；Hello/Auth/Config 全部为真实 USB CDC 往返，不是模拟器或桩。
- Serial F4 真机（2026-11-01 20:47–20:48，生产固件）：串口 `connected` 的 9 秒内 ping 板端全部丢失（Wi-Fi 确实停止）；调用方 14 秒超时后会话仍 `connected`；关闭串口后 6 秒内 ping 恢复；重新使能后 6 秒内 LAN `connected`；同轮抓取的 Config、心跳、ACK 帧全部 CRC 正确。临时 Wi-Fi 挂起探针测得 36 ms/4 ms，生产固件已无诊断代码。
- 模拟器新增 `serial-status` 场景：`./simulator/run.sh serial-status` 渲染成功，预览图 `simulator/output/latest/serial-status_normal.png` 中 Wi-Fi 位置显示 USB 图标；真机 LCD 目视验收仍待人工完成。

## 已完成（前序，仍有效）

1. **Devices v2**：固件、Client 代理和本地模拟器使用 `/v2/devices/summary`；30 秒 cadence 保持不变。
2. **首次 LAN 配对**：nanopb、发现/配对编解码、有界校验、验证码、HTTP 配对和板端本地记录已完成；真实 Client 曾完成双确认。
3. **板端撤销**：正常运行时单独长按 BOOT 5 秒撤销板端 Companion 配对，不删除 Wi-Fi/天气配置；现在会先在活动会话上发送 `up/goodbye(pairing_revoked)`，再删除本地记录。
4. **启动期间发现**：发现服务在初始页面后启动；配对请求可打断启动数据等待。
5. **字体和长标题**：`firmware/assets/fonts/music-cjk.bin` 动态加载；模拟器场景 `activity-long-music`、`activity-cjk-music` 已验证。
6. **局域网发现**：开发板可响应单播；定向广播不可靠，Client 提供广播、有界单播回退和手动 IPv4；macOS 已添加本地网络权限说明。

## 实现导航

- 协议：`client/internal/transport/wire/companion.proto`、`companion.options`；生成产物在 `firmware/src/companion/`，nanopb runtime 在 `firmware/third_party/nanopb/`。变更静态字段会改变 `Envelope` 大小；发现和配对解码已改为堆分配。
- LAN 会话：`firmware/src/companion/monitor_lan_session.c`、`monitor_mqtt_wire.c`、`monitor_proto_strict.c`；MQTT 任务在 `firmware/components/firmware/monitor_companion_mqtt.c`。
- 视图转换：`firmware/src/companion/monitor_companion_view.c`；页面原位更新 `firmware/app/ui/monitor_activity.c`；页面控制 `monitor_pages.c`。
- 配对：`firmware/src/companion/monitor_pairing_*`、`monitor_discovery*`；板端服务、HTTP、身份/凭据存储位于 `firmware/components/firmware/monitor_pairing_*`。应用入口和唯一显示提交者在 `firmware/main/main.c`，按键在 `monitor_wifi_manager.cpp`。
- 弹窗：`firmware/app/ui/monitor_pairing_ui.c`，场景 `pairing`；显示任务拥有 LVGL，后台只发布状态。
- Client：`client/internal/transport/discovery.go`、`pairing.go`、`mqtt.go`、`session.go`、`config.go`、`proxy.go`。Client HTTP 配对和 MQTT 是可信 LAN 明文协议；必须维持身份、来源、nonce、MAC、会话和重放检查。
- 字体：`firmware/app/ui/monitor_music_font.c` 和 `firmware/assets/fonts/generate_music_font.py`；不要改回生成 `.c` 字库。字源许可见 `client/frontend/public/fonts/LICENSE-fusion-pixel.txt`。

## Windows 验证与开发交接（下一阶段）

**当前状态**：仓库已有 Windows x64 的构建、打包、命名管道 IPC、凭据 ACL、GSMTC 音乐、LibreHardwareMonitor/WMI 采集、登录任务、托盘和安装脚本。2026-11-02 已在 Windows 上完成 ESP-IDF 环境配置、生产固件构建和 `COM5` 烧录（烧录哈希校验、启动日志正常），但 **Windows Client（Agent/GUI/串口/LAN）仍没有真机验收证据**。下面把“已在代码中存在”与“必须在 Windows 上验证”分开；在 P0 清单通过前，不要宣称 Windows 客户端已验收。

### 0. 迁移注意事项

- 新 Windows 主机是**新的 Client 身份**。板端当前配对属于 macOS Client；可以直接发起新配对（新配对会替换旧配对并写入墓碑），也可以先在旧 Client 或板端 BOOT 长按撤销。不要默认复制 `companion.json`；若确实要迁移凭据，必须按秘密材料保护，且不要提交或上传。
- 板端 Wi-Fi/天气/Devices 配置保留；如果 Windows 主机不在原局域网，需要重新配置板端 Wi-Fi（配置 AP 或 BOOT+KEY 手势），天气坐标等配置不会被清除。
- 本轮结束时板端运行最终生产固件，macOS 上没有残留 Agent。上一阶段代码检查点为 `ed8def4`；2026-11-02 晚的修复（重复 lease、`desktop-ipc` 静默、串口交接提示、GitHub 加载、时间优先级、文档）作为新提交本地保存，未推送。换机时以最新本地提交为准。
- 首次在 Windows 上配对前，先确认板端没有残留的活动 LAN 会话；必要时重启板端或旧 Client，避免 `session_conflict`。

**验证前提**：

- Windows 10/11 x64；可用的 USB 串口线；LAN 路径需要开发板与 Windows 主机在同一可达局域网。
- WebView2 Runtime；目标机安装 .NET Framework 4.7.2+（LHM 助手需要）；构建机另有 .NET SDK。
- 构建机需要 Go 1.26+（`client/go.mod`）、Node 22 LTS（Vite 7 要求 Node 20.19+/22.12+）、MSYS2 UCRT64 的 `windres`（或已有 `.syso`）、可选 Inno Setup 6。
- 测试和证据中不得出现 token、serial key、配对 ticket、Wi-Fi 密码或 NVS/凭据备份。

**ESP-IDF 环境（2026-11-02 已在本机验证）**：

```powershell
Set-ExecutionPolicy -Scope CurrentUser RemoteSigned
[Environment]::SetEnvironmentVariable("PYTHONUTF8", "1", "User")
. C:\Users\zy201\esp\esp-idf\export.ps1
idf.py --version
idf.py -C firmware build
```

- `export.ps1` 定义 PowerShell 函数，必须用点号加载；直接执行脚本或使用 `&` 调用不会把 `idf.py` 留在当前会话。
- 新开 PowerShell 后如果仍提示 `python` 不存在，检查用户 PATH 是否包含 Python 与 `Scripts`，并重新加载环境；`PYTHONUTF8=1` 用于避免中文路径/输出的编码问题。
- 烧录前先确认没有 Client/Agent 或其他串口工具占用 `COMx`；本机曾因 `monitor-agent`/`monitor-client` 占用 `COM5` 报 `PermissionError(13)`，停止这两个进程后烧录成功。
- `export.bat` 在本机因 LF 行尾无法跳转标签，Git Bash 的 `source export.sh` 会因路径转义失败；Windows 上用 PowerShell + `export.ps1`。
- `firmware/tests/run.sh` 的完整主机 CMake 套件在 UCRT64 MinGW 下因缺少 `sys/select.h` 无法构建；在 Windows 上至少运行不依赖该头的 Python 测试，并在可用环境补跑完整 34 项。

### 1. 构建与基线（P0）

```powershell
cd client
npm --prefix frontend ci
npm --prefix frontend test
go test ./...
go vet ./...
powershell -ExecutionPolicy Bypass -File .\scripts\build-windows-app.ps1 -Version 0.1.0
```

- `build-windows-app.ps1` 会构建前端、net472 LHM 助手、生成 `.syso` 资源、运行 `go test ./...`/`go vet ./...`、构建 `monitor-client.exe`/`monitor-agent.exe`，并生成绿色 zip；检测到 Inno Setup 时还会生成安装包。
- 产物在 `client\build\windows\ESP32-Monitor\`；zip 为 `client\build\windows\ESP32-Monitor-<version>-windows-amd64.zip`，安装包为 `...-setup.exe`。
- `go test -race ./...` 仅在 Windows 有 cgo/gcc（MSYS2 UCRT64）时可用；构建脚本使用普通 `go test ./...`，不要把“未跑 race”写成通过。
- 从 macOS 可做交叉编译冒烟（不能替代 Windows 运行/链接验证）：`GOOS=windows GOARCH=amd64 go build ./cmd/monitor-agent ./internal/...`。
- 验收：干净环境构建成功；测试通过；产物包含 LHM 助手与许可证；zip/安装包可解压/安装。

### 2. 启动、IPC 与权限（P0）

- 以普通用户启动 `monitor-client.exe`：应只弹一次 UAC 启动 `monitor-agent.exe`；GUI 保持普通权限，Agent 以管理员权限运行。
- 命名管道：默认 `\\.\pipe\ESP32-Monitor-<当前用户 SID>`；验证 DACL 仅当前用户、Agent/Client 双向 peer SID 校验、其他用户无法连接、伪造同名管道被拒绝。
- 如果登录任务已启动 Agent，GUI 应复用现有管道，不重复启动 Agent、不重复弹 UAC。
- 设置热更新：切换采集指标、上传目标、Companion 设置时 Agent 热应用或受控重启；GUI 不重建；响应小于 1 秒；进程列表只有一个 Agent。
- 托盘/后台：关闭窗口继续运行、状态栏图标、菜单退出先停 Agent、单实例、崩溃后恢复。
- 登录自启：开启后验证 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` 与计划任务 `ESP32-Monitor Elevated Agent`；注销/登录后自动运行；关闭开关后两项都清理。
- 凭据库：`%APPDATA%\ESP32-Monitor\credentials\companion` 的所有者/DACL 应为当前用户 + SYSTEM（Administrators owner 可接受）；用 `icacls` 记录；GUI 与提权 Agent 都能读写；不得出现 Everyone/Users 可读。
- 验收：一次 UAC、单 Agent、热更新不重启 GUI、退出干净、登录任务可创建/删除、凭据库 ACL 正确。

### 3. Serial Companion（P0，与当前固件工作直接相关）

- 设备管理器/PnP：开发板应显示为 USB Serial/JTAG（`VID_303A`、`PID_1001`）并分配 `COMx`。记录 `Get-CimInstance Win32_PnPEntity` 或 `Get-WmiObject Win32_SerialPort` 输出。
- 端口筛选：`likelyMonitorSerial` 在 Windows 上依赖 enumerator 返回的 VID/PID（COM 名不含 `usbmodem` 等标记）；确认只显示 ESP32/CP210x/CH34x/FTDI 候选，蓝牙和虚拟 COM 不显示。当前测试夹具多用 `/dev/cu.*`，Windows 需要补 `COMx` 夹具。
- 打开端口：`OpenSerialPort` 明确设置 DTR=false、RTS=false；验证打开串口不会复位开发板（看启动日志/运行时间），`\\.\COM10` 及 COM10 以上可用，端口被占用时返回 `serial_unavailable_or_busy`。
- 配对：物理 KEY/BOOT、6 位短码、LAN/串口共享配对、撤销与墓碑；不要删除 Wi-Fi/天气/Devices 配置。一次配对同时授权 LAN 与串口，不应再出现单独的“启用局域网”。
- 会话：HELLO/AUTH/EncryptedConfig/ConfigAck/Snapshot/Heartbeat/ACK；音乐（CJK）、Codex、USB 图标、KEY 翻页；认证后 Wi-Fi 真挂起（`ping <板端 IP>` 失败），退出后恢复（ping 恢复，LAN 可重连）。
- 双路由：LAN 已连接时插入 USB，应在约 2 秒内切到串口并关闭 LAN 会话；拔掉 USB 后自动回 LAN；串口连接期间 LAN recovery 不应建立新会话；`session_conflict` 不应出现。
- 端口改名/重插：`COM3`→`COM5` 或换 USB 口后，应先只读 HELLO 识别，再连接已配对设备；3 次插拔不累积失败或重复 Agent。
- 串口堆汇总：固件在 Goodbye 前输出 `companion_serial: serial heap close: ...`。Client 会把它当噪声丢弃；Windows 上可用 UART0 USB-TTL 抓取，或临时 Client 噪声日志，或保持 DTR/RTS 为低的串口捕获工具。**不要用会拉高 DTR/RTS 的工具直接打开 COM 口**，否则可能复位板端。
- 验收：3 轮连接/超时/重连；无 `ESP_ERR_NO_MEM`、无 `session_expired`；重连小于 5 秒；打开串口不复位；音乐场景 `session_min_largest` 仍为 7,680 B 左右且会话可维持。

### 4. LAN Companion（P0，需要局域网与 Windows 防火墙）

- 网络：Windows 主机与开发板在同一 Wi-Fi/LAN；板端保存的 Wi-Fi 可能仍指向旧网络，必要时重新走配置 AP 并保留天气坐标。
- 防火墙：Client Agent 是 MQTT broker（TCP 18883）监听方，开发板会主动连入；发现使用 UDP 18444。先不建规则测试 Windows 是否弹窗并允许 `Private` 网络，再按需手动添加（不要在 Public 配置文件放行，也不要静默修改防火墙）：

```powershell
New-NetFirewallRule -DisplayName "ESP32 Monitor discovery" -Direction Inbound -Protocol UDP -LocalPort 18444 -Action Allow -Profile Private -Program "<安装目录>\monitor-agent.exe"
New-NetFirewallRule -DisplayName "ESP32 Monitor MQTT" -Direction Inbound -Protocol TCP -LocalPort 18883 -Action Allow -Profile Private -Program "<安装目录>\monitor-agent.exe"
```

- 验证网络配置文件为 Private；Public 或被第三方防火墙拦截时，GUI 应给出可理解的发现失败原因，而不是静默空白。
- 多网卡/VPN：在 GUI 选择正确的 LAN 接口；验证 `--companion-ip`、绑定地址、子网广播 + 有界单播回退；VPN/虚拟网卡不应抢走发现或 MQTT。
- 配对/会话：发现、LAN 明文风险确认、短码、KEY 确认、`connected`、Snapshot/Heartbeat、天气/AQI/devices 代理、直连生产者暂停/恢复、Agent 重启后重连、板端/Client 双向撤销、无活动会话撤销感知。
- 串口↔LAN 切换：串口优先；切换期间不应出现直接天气 `ESP_ERR_HTTP_CONNECT` 之外的未处理错误；记录该窗口的 `companion poll` 堆值与失败原因。
- LAN 内存余量：本轮没有局域网，未测 Wi-Fi 连接 + MQTT + TLS 峰值；Windows 上应抓 `companion poll gen=... int_free=... int_largest=... psram_free=...`，重复 3 轮并记录连接态最低值。
- 验收：`connected` 后 `overlay=1 proxy=1 weather=1 aqi=1`；重启 Agent/板端可恢复；LAN 撤销后不再自动重连；防火墙只对 Private 生效。

### 5. Windows 平台集成（P1）

- **音乐 GSMTC**：`--music` 通过 `powershell.exe`（PowerShell 5.1/WinRT）读取 `GlobalSystemMediaTransportControls`，每 2 秒轮询。验证 Windows Media Player、Spotify、浏览器、Apple Music 等；播放/暂停/停止/无会话；CJK 标题/艺术家；`music_permission_denied`/`music_unsupported`/`music_read_failed`；执行策略、PowerShell 7 不存在、杀毒软件拦截。当前没有 GSMTC 自动化测试，建议增加解析/失败路径夹具。
- **Codex**：`--codex` 配额走 CLI/可执行文件发现；任务走 `desktop-ipc` 命名管道 `\\.\pipe\codex-ipc`，校验服务端进程 SID。验证 Codex Desktop 运行/未运行、管道版本不匹配、权限拒绝、多个任务/线程选择、真实 token 不落日志。
- **GitHub**：Windows Credential Manager 目标 `esp32-monitor.github/<account>`；设备流/浏览器登录、读取、写入、删除；锁定/不可用时的错误；不得枚举其他凭据。
- **代理**：读取 WinINET（`HKCU\...\Internet Settings`）的 HTTP/HTTPS/SOCKS/PAC/bypass；验证天气和 devices 代理走系统代理；不修改系统设置。
- **采集器**：LHM net472 助手、UAC/管理员、.NET Framework 4.7.2、CPU/GPU/磁盘温度与 SMART；WMI 不可用/权限不足的原因；`--collector-scan`；`MONITOR_LIVE_V2=1` 本地假 Server 上传；VPN/虚拟网卡与电池指标。
- **前端/WebView2**：高 DPI、CJK 字体、长音乐标题省略号、滚动、托盘、主题、窗口缩放；WebView2 缺失提示。
- **路径/编码**：`%APPDATA%`、含空格/中文的用户目录、长路径、Unicode 设备名；状态目录和日志路径不混淆。
- 验收：上述每项有成功与失败两种证据；不把“不可用”显示成 0 或伪造数据。

### 6. 打包、安装与卸载（P1）

- 绿色包：WebView2 缺失提示；SmartScreen/未签名提示；.NET Framework 4.7.2 缺失提示；LHM 助手路径；`--collector-scan`。
- Inno Setup：按用户安装到 `%LOCALAPPDATA%\Programs`、升级覆盖、开始菜单/桌面快捷方式、卸载；卸载前清理登录任务/HKCU Run/托盘，个人数据按提示保留或删除。
- 验证 `MONITOR_AGENT_BIN` 可覆盖 Agent 路径（测试/便携场景）；安装目录含空格/中文时仍能启动。
- 记录代码签名状态；当前未签名，不要把它描述为已签名发行版。

### 7. 自动化测试与证据清单

```powershell
cd client
go test ./...
go vet ./...
go test ./internal/ipc ./internal/guibridge ./internal/transport ./internal/agent -count=1 -v
go test ./internal/collector -run 'Windows|Live' -count=1 -v
$env:MONITOR_LIVE_V2="1"; go test ./internal/collector -run TestLiveWindowsV2Upload -count=1 -v
npm --prefix frontend test
```

- Windows 专属/条件测试：`store_permissions_windows`、`ipc/*_windows_test`、`guibridge/*_windows_test`、`desktopipc/desktop_windows_test`、`collector/*_windows_test`、`providers/codex/executable_windows_test`；`TestLiveWindowsV2Upload` 需要 `MONITOR_LIVE_V2=1` 和本机硬件。
- 证据：构建日志与产物哈希；UAC/进程列表；命名管道与 `icacls`；`Get-CimInstance Win32_PnPEntity` 的 COM/VID/PID；防火墙规则与 Private/Public 配置；板端 `connected`、`serial heap close`、`companion poll`；Windows 事件查看器/Defender 日志；2 小时以上串口/LAN 混合长稳（72 小时更佳）。
- 不要把串口原始二进制帧、token、serial key、配对 ticket 或 Wi-Fi 密码写进交接/截图。

### 8. Windows 开发待办（代码层面）

- 为 `COMx` 增加 Windows 串口枚举/筛选夹具，并覆盖 `\\.\COM10`、端口改名、端口占用；当前测试多使用 `/dev/cu.*`。
- 增加 Windows 串口打开测试：DTR/RTS=false、打开不复位、HELLO 探测；需要硬件时用显式 `-tags hardware` 或脚本隔离。
- 为 GSMTC 增加可注入的 PowerShell/JSON 夹具，覆盖 `music_unsupported`、`music_permission_denied`、`music_read_failed`、CJK 和缺失 `powershell.exe`。
- 为发现/防火墙增加“检测到监听器/发现被拦截”的可理解提示；不要在没有用户同意时自动改防火墙。
- 增加 Windows 串口噪声/堆汇总抓取方式（调试开关或 UART0 文档），避免为了看一行日志临时改 Client。
- 增加 `windows-latest` CI：`go test ./...`、`go vet`、前端 `npm ci && npm test`、`build-windows-app.ps1 -SkipTests`（安装 .NET/MSYS2 依赖）；上传 zip 与测试日志。
- 复核命名管道 DACL/peer 在“GUI 普通权限 + Agent 提权”组合、快速重启和多用户登录下的行为。
- 复核凭据库 ACL 在普通/提权创建、升级安装、用户改名后的行为；增加 `icacls` 证据。
- 复核 LHM 在干净 Windows 10（无 .NET 4.7.2）、无管理员权限、缺少助手文件时的 GUI 提示。
- 复核安装/升级/卸载对 HKCU Run、计划任务、托盘和 `%APPDATA%` 的清理。
- 复核 Codex Desktop 命名管道版本兼容和 `--codex` CLI 回退；复核系统代理 PAC/bypass。
- 复核 Windows 10 与 Windows 11 的 WebView2、托盘、UAC、GSMTC 差异；至少各跑一次 P0。

### Windows 退出标准

- **P0 通过**：干净构建；GUI/Agent 单实例与 UAC 正常；串口配对/会话/撤销/插拔 3 轮稳定；LAN 发现/配对/MQTT/天气/devices 稳定；串口优先与回 LAN 正常；设置热更新、登录任务、托盘、凭据 ACL 正常；无误报“已连接”。
- **P1 通过**：音乐/Codex/GitHub/代理/采集器/安装包各自成功与失败路径都有证据；2 小时混合长稳无崩溃/泄漏；日志无秘密。
- **仍不在本轮**：OTA、NVS/Flash 加密、板端 Serial 功耗实测与 72 小时稳定性；这些仍需独立硬件验收，不能用 Windows Client 测试替代。

## 尚未完成的验收与下一步

1. **物理 UI 验收**：普通 KEY 翻页、断线回第一页并禁用翻页、Serial USB 图标及 Codex/音乐显示已由用户确认；GitHub 真实数据仍待授权后验收。未配对 Serial 弹窗的真实 KEY 确认已完成，BOOT 取消仍需验收。
2. **撤销的物理验收与残留**：真实 BOOT 5 秒长按、板端六位码弹窗的 BOOT 确认、撤销后的重新配对；确认 Wi-Fi/天气/Devices 配置保留。无活动会话撤销的认证墓碑与 Client 自动删除已在“板端未配对”和“板端已与其他 Client 配对”两种真机状态验收通过；真实 BOOT 长按本身也已在两种状态各执行一次。
3. **LAN 余项**：Resync 已实现（见上）；90 秒无有效应用回执结束与 Agent 恢复重连已在本轮真机验证；仍需验证 Client 下发 Error 的完整处理、旧 session 隔离、重复发现与慢速消费；保留 30 秒 Devices cadence。LAN 连接态的 Wi-Fi/TLS 峰值余量需用同一 `serial heap`/`companion poll` 口径补测（Serial 侧已完成 2–3 轮，见“内部 RAM 与网络余量（2026-11-02 Serial 实测）”）。
4. **Serial（阶段 F3）**：HELLO/AUTH/Config/ConfigAck/Snapshot/Heartbeat/ACK 已在真机与真实 Client 互通并稳定运行；日志已从认证后的 USB 流移除；ConfigAck 后 Wi-Fi/AP/DNS 真关闭，退出后恢复；未配对 PAIR 已完成板端短码、KEY 确认/BOOT 取消、NVS 保存、AUTH 接续与串口撤销。Serial 内部 RAM 已完成 2–3 轮实测，音乐场景 `session_min_largest=7,680 B` 仍偏紧。**剩余**：真实按键的物理验收、串口断开后的 72 小时长稳与异常恢复。
5. **模式/电源（F4/F6）**：认证 Serial 优先、内部 RAM 加锁、Wi-Fi 真关闭与退出恢复已完成并真机验证；**仍未**做 Serial 模式功耗实测（必须 USB 断开、电池接口多分钟对比）以及 HPM/LPM 与异常恢复；LAN 继续允许 Light Sleep。USB 日志/主机测试不能替代功耗结论。
6. **稳定性（F6）**：与真实 Client 全模式 72 小时，覆盖重连、Resync、Goodbye、凭据失效、反复插拔和主机休眠；不得把 MQTT PUBACK/PINGRESP 当作应用存活证明。
7. **安全**：NVS 加密未启用；双端撤销清理与无会话撤销感知均已实现并完成真机验收。墓碑允许持有派生 discovery key（不可反推 serial_key），不得改为保存 token/serial_key。不要无条件删除既有用户配置，也不要自动改安全启动/eFuse。撤销测试的 NVS/凭据备份属于敏感材料，不得留存或上传。

## 验证入口

```sh
./firmware/tests/run.sh
./simulator/run.sh companion-live
./simulator/run.sh companion-update
ctest --test-dir simulator/build --output-on-failure
cd client && go test -race ./... && go vet ./...
cd client && go test -race ./internal/transport -run 'TestLAN(EndToEnd|RevokeSendsGoodbye|PairingRevokedByDevice|ResyncRespondsWithSnapshot)' -v
```

本机 Headless LAN 联调（需先运行 `python3 simulator/api_server.py --num 4 --port 18081`，且 URL 必须与板端保存值逐字一致）：

```sh
cd client
make build
./bin/monitor-agent --companion --companion-ip 192.168.110.2 \
  --companion-state-dir "$HOME/Library/Application Support/ESP32-Monitor/credentials/companion" \
  --companion-local-devices-url http://192.168.110.2:18081
```

固件构建和烧录（先确认设备和实际配置）：

```sh
source ~/esp/esp-idf/export.sh
idf.py -C firmware build
idf.py -C firmware -p /dev/cu.usbmodem1101 flash
```

Windows PowerShell（2026-11-02 已验证）：

```powershell
Set-ExecutionPolicy -Scope CurrentUser RemoteSigned
[Environment]::SetEnvironmentVariable("PYTHONUTF8", "1", "User")
. C:\Users\zy201\esp\esp-idf\export.ps1
idf.py -C firmware build
idf.py -C firmware -p COM5 flash
```

烧录前停止占用串口的 Client/Agent；Windows 上完整主机 CMake 测试仍受 `sys/select.h` 缺失限制。

串口日志建议用可重连读取器，USB CDC 在低功耗期间可能短暂重新枚举；不要用会反复复位板子的监控进程。独立 Unity 测试固件会替换生产程序，使用后须恢复生产固件。测试可能需要本地 TCP/UDP 监听；受限沙箱报 `operation not permitted` 时不能记作功能失败或通过。
