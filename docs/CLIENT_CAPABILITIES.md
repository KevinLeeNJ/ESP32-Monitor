# Client 整体能力表

核对日期：2026-10-28；依据当前工作区源码，包含尚未提交的实现。不把旧文档、历史测试或交叉构建当作当前三平台验收。发布工作见 [V1 计划](V1_RELEASE_PLAN.md)。

状态口径：**已实现**表示可定位代码；**部分实现**表示链路/平台/产品入口有缺口；**待实现**表示尚无完整交付。存在测试用例不等于本次已执行，也不等于真实系统、Server 或板端验收。

## 1. 整体架构与运行边界

```text
Wails GUI + Vue 页面
  → 受限 Go services → 用户认证 IPC → monitor-agent
      ├─ Core(gopsutil) + Native Collector → HTTPS v2 → Server
      ├─ GitHub / Codex quota / 可选任务观察 / 系统播放
      └─ Companion → LAN MQTT 或串口 → ESP32
```

| 能力 | 状态 | 当前事实 / 剩余边界 | 源码依据 |
| --- | --- | --- | --- |
| GUI 宿主与前端 | 已实现 | Wails v3 宿主、Vue 3/TypeScript/Vite 页面、打包静态资源；三平台安装/真实 WebView 验收未齐 | [main.go](../client/cmd/monitor-client/main.go)、[App.vue](../client/frontend/src/App.vue) |
| GUI/Agent 分进程 | 已实现 | Supervisor 启动/停止/重启 Agent，业务服务走 IPC；后台行为受设置控制 | [supervisor.go](../client/internal/guibridge/supervisor.go) |
| 用户认证 IPC | 部分实现 | Unix socket、同 UID 对端校验、拒绝浏览器 Origin；macOS/Linux 有 peer API，其他平台不具备等效实现 | [ipc.go](../client/internal/ipc/ipc.go)、[server.go](../client/internal/ipc/server.go) |
| Windows IPC | 待实现 | 尚无 Windows Named Pipe 通道；现有 Unix 实现不能当作 Windows GUI 完成 | [peercred_other.go](../client/internal/ipc/peercred_other.go) |
| 独立 Agent | 已实现 | 一次 JSON、watch JSONL、Collector/Companion/IPC 后台入口；不强制使用 GUI | [run.go](../client/internal/agent/run.go) |
| 后台运行与完整退出 | 部分实现 | 关窗后台、Supervisor/OnShutdown 清理、可选状态栏图标与重新唤出；系统会话切换仍需产品验收 | [GUI 入口](../client/cmd/monitor-client/main.go)、[icons.go](../client/internal/guibridge/icons.go) |
| 登录自启 | 已实现 | 通用开关；macOS 写 LaunchAgent、Windows 写 HKCU Run、Linux 写 XDG autostart；以 `--login-item` 后台启动且不显示窗口 | [settings.go](../client/internal/guibridge/settings.go)、[loginitem_darwin.go](../client/internal/guibridge/loginitem_darwin.go) |
| 系统采集服务 | 待实现 | 用户会话音乐/账号与系统服务隔离、跨模式所有权未产品化 | [lifecycle](../client/internal/platform/lifecycle/doc.go) |
| macOS 应用打包 | 部分实现 | `.app` 构建脚本含 GUI、Agent、播放 dylib 与本地 ad-hoc 签名；正式签名/公证/升级/卸载未完成 | [build-darwin-app.sh](../client/scripts/build-darwin-app.sh) |
| Windows/Linux 分发 | 待验收 | 有跨平台业务代码，缺完整安装包和依赖/权限/原生 GUI 验证 | [packaging](../client/packaging) |

默认 GUI 开启 Collector，Companion、Codex、音乐默认关闭；GitHub 在完成 Device Flow 登录后启用，GUI 内置本项目公开的 OAuth App Client ID（无需 Client Secret）。模块独立，采集不强制登录第三方账号，Companion 不强制注册指标 Server。当前 Collector 已是 **进程内 Core/Native**，不再管理 Telegraf 子进程。

## 2. GUI 页面覆盖

| 页面 | 已有入口 | 尚需验收 / 缺口 |
| --- | --- | --- |
| 总览 | Agent、采集、上传、连接状态 | 真实故障下显示和修复闭环 |
| 指标 | 能力、分组选择、对象、5–60秒周期、预览/上传配置 | 各平台真实来源、缺失值和多设备场景 |
| Server 连接 | 基地址、健康测试、验证码注册、设备身份 | 正式 Server v2 尚缺；健康探针成功不代表可上传 |
| 上位机 | GitHub Device Flow 登录（内置公开 Client ID）、Codex/音乐状态与设置 | 真实 GitHub 账号授权与贡献墙加载需验收；Codex 任务不等于只打开额度开关 |
| ESP32 | 发现、配对、设备开关、串口列举/探测/连接 | 真实固件、物理串口、网络切换恢复 |
| 启动与后台 | 模块开关、关窗后台、登录自启、状态栏图标、退出 | 三平台登录项/托盘的真实系统验收 |
| 诊断 | 脱敏报告、队列/错误/时间、重启 Agent | 日志包导出、权限和复杂故障恢复 |

页面源码集中于 [views](../client/frontend/src/views)，绑定方法集中于 [services.go](../client/internal/guibridge/services.go)。`frontend/preview` 为示例数据原型，不作为真实后端或原生 GUI 的验收证据。

## 3. 指标能力矩阵

Core 只使用 gopsutil 可提供的能力；Native 补平台专有字段。可用性由真实扫描决定，硬件/权限/API 缺失明确报告原因，不能补 0。七类口径以当前 [collectors.go](../client/internal/collector/collectors.go)、[v2Bindings](../client/internal/collector/v2.go) 为准。

| 类别 | 当前目标字段 | macOS 实现 | Windows/Linux 边界 |
| --- | --- | --- | --- |
| CPU | 型号、物理/逻辑/可辨别P/E核数、总体/逐核占用、1/5/15分钟负载、平均温度、风扇 | Core占用/负载；sysctl型号/核数；SMC温度/风扇按硬件可用 | Core依对应API；Native扩展为stub，无统一温度/风扇保证 |
| 内存 | 总量/已用/比例、App/Wired/压缩、Swap总量/已用、压力 | Core内存/Swap；vm_stat细分与可用压力级别 | Core依平台支持；不补macOS专有明细 |
| GPU | 占用、平均温度、可确认风扇 | IOKit PerformanceStatistics；SMC温度；无法归属风扇为system | GPU Native尚无实现；不以没有GPU或读数失败显示0% |
| 磁盘 | 型号、物理总量/已用、读写B/s、SMART健康与部分细项 | diskutil档案/SMART；可可靠归属的APFS容器汇总；Core计数器速率 | Core读写依平台；物理盘已用量未实现，Native SMART未实现 |
| 网络 | Wi-Fi/有线类型、上传/下载B/s、本地IPv4、公网IPv4 | Core速率；Native接口介质/连接/IP；显式选择公网查询 | Core速率依平台；Native介质/地址/公网IP未实现 |
| 电池 | 电量百分比、充电状态 | 系统电源信息，两项成对 | Native为stub；不能承诺电池采集 |
| 系统 | Uptime、OS类型/名称/版本/构建、硬件产品名 | Core Uptime；Native OS与产品映射 | Core系统信息依平台；产品名称Native尚缺 |

macOS 温度/风扇需要 CGO/SMC，关闭 CGO 时不可用；传感器映射与产品名称映射不保证所有机型。HID 备选温度路径、完整 NVMe/ATA SMART 明细未实现。源码/来源分别见 [native_darwin.go](../client/internal/collector/native_darwin.go)、[darwin_extra.go](../client/internal/collector/darwin_extra.go)、[第三方许可证](../client/docs/third_party/monitor-LICENSE.txt)。

当前不再采集挂载点容量/文件系统用量、进程/连接统计；不把旧 Telegraf 任意插件列表或 CPU 最高频率列为当前 v2 支持字段。`volume.*` 为历史兼容类型，配置会清除。

macOS 物理磁盘用量只在分区均能唯一归属单盘 APFS 容器时给出；共享容器只计一次，混合分区/跨盘容器/查询失败省略。固定拓扑启动缓存，硬件变动需重启 Agent。公网 IP 当前选择启用后通过 `https://api.mac-stats.com/ip` 查询，缓存5分钟，保留真实观测时间；扫描不触发此请求。

### 3.1 原 METRICS.md 要求逐项承接

本节完整承接 [原 METRICS.md](../.bak/docs-cleanup-2026-10-28/client/docs/METRICS.md) 的七类能力要求；原文归档后不再作为当前规范。表内“已实现”仍取决于硬件/系统实际可获取，平台差异见上表；“字段存在”不等于三平台均可采集或正式 Server 已接收。

| 原能力要求 | 当前 Client 能力与限制 | v2 语义 ID |
| --- | --- | --- |
| CPU型号 | 已实现，固定信息；macOS有Native补充 | `cpu.model` |
| 核心数、大小核数 | 已实现物理/逻辑核数及可辨别的P/E核数；无法辨别则省略P/E | `cpu.physical_cores`、`cpu.logical_cores`、`cpu.performance_cores`、`cpu.efficiency_cores` |
| CPU整体占用率 | 已实现，启用远端上报时必选 | `cpu.usage` |
| CPU Per Core占用率 | 已实现；当前按全部可获取逻辑核启停，不支持只上传部分核 | `core.usage` |
| 平均负载1/5/15分钟 | 已实现Core读取/分组上传，OS不提供时省略 | `cpu.load1`、`cpu.load5`、`cpu.load15` |
| CPU平均温度 | macOS SMC按可用传感器分组平均，依机型与CGO | `cpu.temperature` |
| CPU风扇转速 | 有风扇转速读取；当前macOS不能确认CPU专属归属，标system，专属归属仍缺 | `fan.rpm` + Inventory `fans[].scope` |
| 内存总量 | 已实现；可获取时随内存组隐式同步固定档案 | `memory.total` |
| 内存已用 | 已实现；可获取时随内存百分比隐式上传 | `memory.used` |
| 内存百分比 | 已实现，远端必选，沿用平台used/total口径 | `memory.usage` |
| App内存 | macOS Native已实现 | `memory.app` |
| 联动/连线内存Wired | macOS Native已实现，统一称Wired | `memory.wired` |
| 压缩内存 | macOS Native已实现 | `memory.compressed` |
| Swap总量/已用 | Core已实现，作为动态值成组 | `memory.swap_total`、`memory.swap_used` |
| 内存压力（macOS） | Native有读取路径，不能保证每台系统可获取 | `memory.pressure` |
| GPU占用率（有GPU时） | macOS IOKit已实现；Windows/Linux Native未实现 | `gpu.usage` |
| GPU平均温度 | macOS SMC有实现，依传感器/CGO；上传依赖同GPU占用率 | `gpu.temperature` |
| GPU风扇转速 | 有通用风扇读取；当前macOS无可靠GPU归属，专属能力未完成 | `fan.rpm` + Inventory `fans[].scope/gpu` |
| 磁盘型号 | macOS物理盘固定信息已实现；按盘选择 | `drive.model` |
| 磁盘总容量 | 物理盘总量固定档案；按平台实际可获取情况提供 | `drive.total` |
| 磁盘已占用容量 | macOS可归属APFS容器采集/编码/独立上传已有；Windows/Linux未实现；不等于挂载点用量 | `drive.used` |
| 磁盘正写入速度 | Core累计计数器差分为B/s，无有效基线时省略 | `drive.write_rate` |
| 磁盘正读取速度 | 同上，与写速率按组选择 | `drive.read_rate` |
| SMART健康状态 | macOS diskutil提供可读状态；不支持/权限不足不伪造健康 | `drive.smart_health` |
| SMART可获得细项 | macOS部分可读细项，显式选detail；完整NVMe/ATA明细未完成 | `drive.smart_attribute` |
| 网络连接类型Wi-Fi/LAN | macOS接口介质/在线状态已实现，按网卡列举，不猜唯一主链路 | `interface.medium`、`interface.up` |
| 网络上传速度 | Core按网卡计数器差分B/s；上传依赖同网卡连接组 | `interface.upload_rate` |
| 网络下载速度 | 同上，与上传速率成组选择 | `interface.download_rate` |
| 本地IP（仅IPv4） | macOS Native支持多地址及确认无地址的空数组 | `interface.ipv4` |
| 公网IP（如能获取，仅IPv4） | macOS显式选择后外部查询，5分钟缓存，失败省略 | `network.public_ipv4` |
| 电池容量百分比（有电池时） | macOS系统电源读取；当前不是容量Wh/mAh或电池健康度 | `battery.level` |
| 电池充电状态 | macOS已实现，与电量成对可获取/上传；未充电false保留 | `battery.charging` |
| Uptime运行时长 | Core已实现，远端必选 | `system.uptime` |
| 系统OS类型+版本 | 固定档案已有；macOS补名称/代号/构建，其他系统依Core可用值 | `system.os` |
| 硬件产品名称（FastFetch Host口径） | macOS产品映射已有；不是主机网络名；其他平台Native尚缺 | `system.product` |

不一致的处理：保留“有硬件/可获取才提供”的约束；按当前实现明确逻辑核范围、物理盘/APFS口径、系统风扇归属及平台缺口。旧文档的指标愿望不自动变成完成状态，协议中不存在的值不新增为虚假能力。

磁盘已用量独立上传已修复：[enqueueV2Locked](../client/internal/collector/v2_upload.go) 判空时包含Used，只有SMART/已用量/读写速率均缺失才省略该磁盘。新增 [HTTP回归测试](../client/internal/collector/v2_disk_upload_test.go) 验证仅上传已用量、0字节、精确大整数，以及SMART观测去重后仍保留已用量；重复SMART且没有其他值的空对象仍会被省略。验证范围为Client与本机测试HTTP接收端，正式Server v2与真实平台/硬件验收边界不变。

## 4. 采集配置、预览与上传

| 能力 | 状态 | 当前行为 |
| --- | --- | --- |
| 能力发现/重扫 | 已实现 | 报告来源、字段、对象、可用/部分/权限/不支持及原因；固定信息不随重扫重新读取 |
| 事务应用 | 已实现 | 候选校验/采样/持久化后切换；失败不停止旧循环；新配置清空旧上传队列 |
| 本机/远端选择 | 已实现 | 本机 selections 与 upload.optional 分离；廉价预览可能采集未上传项；SMART/公网IP选择启用 |
| v2最小上传 | 已实现 | 总体CPU、内存百分比、Uptime必选；可获取时隐式带内存总量与已用量 |
| 组/对象约束 | 已实现 | 按组补齐可用字段、对象依赖；逐核CPU全部核；电池成对；SMART细项显式选择 |
| 固定信息与Profile | 已实现 | 启动一次缓存、持久化对象ID、首次PUT、后续差异PATCH、revision对账 |
| 上传队列 | 已实现 | 内存FIFO，5分钟/1000轮/256KiB上限；丢最旧计数、退避、鉴权暂停 |
| 无损离线恢复 | 未实现 | 队列不持久化；退出可能丢样本；重启恢复配置不等于补回旧样本 |
| v1上传 | 不支持 | 当前 uploader只接受`/v2/collector/samples`，旧Server需升级 |
| 正式Server端到端 | 待实现/联调 | 模拟器支持注册/Profile/Sample；生产Server尚无v2存储/投影 |
| 系统时间修复 | 待实现 | 422仅报通用样本/时钟错误，无精确诊断、授权同步、UAC及恢复上传闭环 |

字段/编码/重试细节统一由 [Server–Client v2协议](SERVER_CLIENT_PROTOCOL_V2.md) 维护。不能因本地保存成功宣称远端字段立即移除；需等待 Server 确认 Profile。

## 5. GitHub、Codex 与播放

| 提供者 | 当前实现 | 证据与限制 |
| --- | --- | --- |
| GitHub | Device Flow默认浏览器授权、轮询/取消/过期、账号确认、贡献日历、限流/缓存/账号隔离 | [auth](../client/internal/auth/github)、[provider](../client/internal/providers/github)；实际授权和三平台复用需各自验收 |
| GitHub凭据 | 应用私有文件、原子写入、路径/符号链接校验；Unix0700/0600 | [credential_file.go](../client/internal/platform/credential_file.go)；不恢复Keychain依赖，不打包Client Secret |
| Codex额度 | 通过本机App Server只读读取账号额度、多bucket/窗口、未知值处理 | [codex](../client/internal/providers/codex)；不启动推理，不把额度读取成功等同桌面任务可观察 |
| Codex任务 | 独立Adapter、显式启用desktop IPC观察、所选任务状态/活动数；最多指定128个ID | [taskmonitor](../client/internal/taskmonitor)；内部IPC是实验接入，非稳定分发保证；macOS/Linux实现、Windows不支持 |
| 系统播放 | 统一只读快照、playing/not_playing、元数据/来源/位置；取消和异常降级 | [adapters](../client/internal/nowplaying/adapters)；仅覆盖向系统播放接口暴露的应用 |
| macOS播放 | MediaRemote + dylib桥接，isPlaying为播放权威状态 | 依私有API/系统版本及桥接库；真实分发兼容仍需验证 |
| Windows播放 | GSMTC，PowerShell适配器 | 原生会话/权限/安装环境待验收 |
| Linux播放 | MPRIS，Python/DBus适配器，优先播放中的源 | 原生桌面/DBus依赖与来源选择待验收 |

暂停时保留可用元数据；无当前源时 current 缺席；provider错误单独显示，不伪装为正常暂停。Codex quota账号、运行中Desktop账号及任务覆盖范围必须核对，不自动推断一致。系统手动代理/环境代理实现见 [proxy](../client/internal/platform/proxy)；PAC/WPAD、企业认证代理及平台特有行为仍有缺口，不能静默绕开直连。

## 6. ESP32 上位机能力

| 能力 | 状态 | 边界 |
| --- | --- | --- |
| LAN发现、选设备、临时HTTP配对 | 已实现 | 可信LAN确认、双方核对、记录凭据；实机交互未完成 |
| 内置MQTT与只读快照 | 已实现 | 主题ACL、会话认证、配置事务、快照/心跳/ACK/resync、Protobuf v2 |
| 串口列举/探测/配对/连接 | 已实现 | EMON帧、CRC/HMAC/HKDF、Config AES-GCM；物理串口仍待验证 |
| 配置代理 | 已实现 | ESP32配置天气/Devices/时间，Client会话内存代理；不把上游秘密落入普通设置/诊断 |
| 地址恢复/传输接管 | 部分实现 | 有重新绑定、认证邀请、串口认证优先代码/测试；休眠、DHCP、复杂断线矩阵待验收 |
| 真固件双页三模式 | 待实现 | 模拟Monitor通过不能证明固件已接入或电源切换有效 |

完整报文/安全/时效契约仅维护于 [ESP32与上位机协议](ESP32_COMPANION_PROTOCOL_V1.md)。Server–Client v2 与 Companion wire v2 是两套独立协议。

## 7. 设置、诊断与本地接口

GUI 设置保存在 `os.UserConfigDir()/ESP32-Monitor/gui-settings.json`，记录设备身份、Server地址、开关和偏好。上传Token路径是宿主内部配置，不要求用户手填；凭据正文不进入WebView返回值。采集状态目录有所有权锁，默认 `collector/`；Companion凭据在 `credentials/companion/`。

正式 GUI 调用链：WebView → Go受限服务 → IPC → Application；状态/采集/提供者/Monitor方法见 [services.go](../client/internal/guibridge/services.go)，验证码注册在宿主完成。诊断报告只提供健康、错误码、版本、队列、时间和计数，省略贡献日历/任务标题/媒体内容与凭据，见 [report.go](../client/internal/diagnostics/report.go)。当前页面能读取报告，完整导出包尚待实现。

独立调试可显式开 `--collector-api 127.0.0.1:0`，要求临时Bearer并拒绝浏览器Origin；提供 capabilities/rescan/status/config 端点。它不是正式 GUI 到 Agent 的通用浏览器通道，不能把描述文件或上传Token交给网页。

## 8. 开发、运行与验证入口

仓库根目录构建 Agent、运行能力扫描（输出依当前系统而变）：

```sh
cd client
make build
./bin/monitor-agent --collector-scan
```

macOS 应用构建入口为 `cd client && make build-app-darwin`，生成 `client/build/ESP32 Monitor.app`；脚本构建并嵌入前端、Agent与播放桥接库，执行本地签名。该入口不等于已有公证安装包。纯Agent可通过 `--collector` 及私有状态目录运行；真实上传需先得到v2 Server签发的Token，GUI优先走验证码注册。

可选模块参数：GitHub使用 `--github-login --github-client-id <public-id>` 或 `--github-user <login>`；额度用 `--codex`，任务需额外 `--codex-task-adapter auto --codex-thread <id>`；播放用 `--music`；LAN上位机用 `--companion --companion-ip <local-ip>`。完整参数以 [Agent入口](../client/internal/agent/run.go) 为准。

| 检查 | 从对应目录执行 | 证明范围 |
| --- | --- | --- |
| Client测试/静态检查 | `go test -race ./...`、`go vet ./...` | 测试覆盖内的行为，不替代原生系统验收 |
| 前端 | `cd frontend` 后 `npm ci`、`npm test` | 类型、构建与DOM测试，不替代真实WebView操作 |
| Collector回环 | `make test-collector-e2e` | 当前脚本覆盖的Agent/HTTP/SQLite路径；需检查脚本与v2迁移是否一致 |
| Companion | `go test ./internal/transport/...` | 模拟LAN/字节流/编码/认证，无实机功耗结论 |
| 模拟Server | `go run ./cmd/sim-server -listen 127.0.0.1:18080` | 注册与v2联调，无生产持久化/管理安全保证 |

清理前的旧验收文件保存在 [.bak历史证据](../.bak/docs-cleanup-2026-10-28/client/docs/evidence)；其中 Telegraf 与早期 Core 结果只代表对应版本，不能为当前 Native/v2/GUI 完成背书。本次整理的验证结果由交付说明单独报告。
