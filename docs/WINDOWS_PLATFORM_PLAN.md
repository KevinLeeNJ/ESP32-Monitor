# Windows 平台实施计划

核对日期：2026-10-29。以当前工作区 Client 源码为基线；本文只描述 Windows 平台待实现/待验收范围，不改变 [Client 整体能力表](CLIENT_CAPABILITIES.md) 与 [V1 计划](V1_RELEASE_PLAN.md) 的既有口径。存在测试用例不等于已在本机执行，也不等于真实安装包、真实硬件或干净系统验收完成。

## 1. 目标与边界

Windows 与 macOS/Linux 使用同一套 Go Agent、Wails GUI 和共享业务代码；Windows 只补齐平台相关实现，不复制一套业务逻辑。

本次范围：

- Windows GUI ↔ Agent 的本地认证 IPC。
- Agent 子进程在 Windows 上的启动、重启与退出。
- Windows Native 采集与 v2 语义映射。
- Windows 构建、打包与安装/绿色包验证。
- 已有 Windows 播放、凭据、登录自启代码的真实验证。
- Codex 额度读取与 Desktop 任务观察的 Windows 接入验证。

不在本次范围：

- 正式代码签名、公证、自动更新、系统服务化。
- 生产 Server、ESP32 实机与三平台联合验收。
- 用交叉编译结果替代 Windows 原生运行证据。

## 2. 当前现状与缺口

| 能力 | 状态 | 当前事实 | 源码依据 |
| --- | --- | --- | --- |
| Windows IPC | 已实现/已验证 | 每用户 Named Pipe `\\.\pipe\ESP32-Monitor-<SID>`，SDDL 仅授权当前用户；服务端/客户端分别校验对端进程 Token SID，拒绝伪造成本机管道；重复监听返回 `ErrAlreadyRunning`；Agent 管道端到端 Status/Quit 通过 | [transport_windows.go](../client/internal/ipc/transport_windows.go)、[server.go](../client/internal/ipc/server.go)、[client.go](../client/internal/ipc/client.go)、[ipcpath_windows.go](../client/internal/guibridge/ipcpath_windows.go) |
| Agent 生命周期 | 已实现/已验证 | 解析 `monitor-agent.exe`；IPC Quit 现在取消整个 `agent.Run`（不再只取消 worker）；Windows 先走 IPC Quit，5 秒未退出再 Kill；Supervisor 启动→Status→Stop→进程退出的原生实测通过 | [supervisor.go](../client/internal/guibridge/supervisor.go)、[process_windows.go](../client/internal/guibridge/process_windows.go)、[run.go](../client/internal/agent/run.go) |
| Native 采集 | 已实现/已验证 | Native source 列表按平台拆分；Windows 已接入 `windows_cpu/gpu/disk/disk_usage/network/public_ip/battery/system`，WMI 查询受 context 约束，不可用返回原因；CPU 负载按队列长度+占用估算，CPU 温度走包内 LHM 0.9.4 helper，GPU 温度走 WDDM D3DKMT；本机真实扫描已读到 CPU/GPU 占用与 GPU 温度、双 NVMe 健康与用量、WLAN/有线、系统版本；提权 Agent 下 `windows_cpu` 含 `temperature_celsius`，非提权扫描报告 `lhm_permission_required`；电池按硬件缺失报告；风扇转速已全平台移除 | [native_windows.go](../client/internal/collector/native_windows.go)、[windows_wmi_windows.go](../client/internal/collector/windows_wmi_windows.go)、[windows_lhm_windows.go](../client/internal/collector/windows_lhm_windows.go)、[windows_gpu_sensors_windows.go](../client/internal/collector/windows_gpu_sensors_windows.go)、[windows_load_windows.go](../client/internal/collector/windows_load_windows.go)、[windows_system_windows.go](../client/internal/collector/windows_system_windows.go)、[platform_sources_windows.go](../client/internal/collector/platform_sources_windows.go) |
| v2 语义映射 | 已实现/已验证 | `nativeSourceID(role)` 按平台解析 CPU/内存/GPU/磁盘/网络/电池/系统 source；Inventory、对象归属、公共 IP 排除项不再硬编码 `darwin_*`；Windows 本机扫描生成 OS/产品/CPU/16 核/GPU/2 物理盘/2 物理网卡的对象档案，必选指标可用，缺失项带 `metric_unavailable`；`cpu.load1/5/15` 在 Windows 绑定 `windows_cpu`，其他平台仍绑定 Core `system` | [v2.go](../client/internal/collector/v2.go)、[v2_inventory.go](../client/internal/collector/v2_inventory.go)、[platform_sources_darwin.go](../client/internal/collector/platform_sources_darwin.go)、[platform_sources_windows.go](../client/internal/collector/platform_sources_windows.go) |
| 播放（GSMTC） | 已实现/部分验收 | PowerShell + WinRT 只读适配器已存在；本机脚本冒烟返回 `healthy` 且无当前媒体源；真实播放/暂停/切歌、元数据与退出恢复待实测 | [platform_windows.go](../client/internal/nowplaying/adapters/platform_windows.go)、[gsmtc.ps1](../client/internal/nowplaying/adapters/scripts/gsmtc.ps1) |
| GitHub 凭据 | 已实现/已验证 | 使用 Windows Credential Manager，禁止枚举；本机 Save/Load/Delete 往返与删除后不可读已通过 | [credentials_windows.go](../client/internal/platform/credentials_windows.go) |
| 登录自启 | 已实现/待验收 | 写 `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`，以 `--login-item` 后台启动，并配置最高权限登录任务；GUI 开关尚未在本机实测 | [loginitem_windows.go](../client/internal/guibridge/loginitem_windows.go) |
| 后台托盘 | 已实现/部分验收 | 关窗后台在 `WindowClosing` hook 中取消关闭并隐藏窗口，进程与 Agent 保持运行；Windows 托盘使用彩色应用图标并支持左键唤出/右键菜单；托盘图标点击与菜单已实现，待人工点击验收 | [main.go](../client/cmd/monitor-client/main.go) |
| 文件锁/权限 | 已实现/已验证 | 使用 `LockFileEx`；私有文件权限检查在 Windows 跳过 POSIX mode；Companion vault 接受提权令牌创建的 Administrators owner 并把 DACL 收紧为当前用户+SYSTEM，修复提权 Agent 重启后 `credential_directory_owner_mismatch` | [lock_windows.go](../client/internal/collector/lock_windows.go)、[api.go](../client/internal/collector/api.go)、[store_permissions_windows.go](../client/internal/transport/store_permissions_windows.go) |
| Codex 额度 | 已实现/已验证 | 解析 `%LOCALAPPDATA%\OpenAI\Codex\bin\<hash>\codex.exe` 并复用 app-server 只读链路；已在本机登录后读到 5h/7d 窗口 | [executable.go](../client/internal/providers/codex/executable.go)、[provider.go](../client/internal/providers/codex/provider.go) |
| Codex 任务 | 已实现/运行态已验证 | Windows 通过 `\\.\pipe\codex-ipc` Named Pipe 复用 v11 观察器；默认读取 `session_index.jsonl` 自动跟踪 `updated_at` 最新的本地任务（只解码 id/updated_at），`--codex-thread` 可显式覆盖；fake pipe 测试、真实管道连接/SID 校验通过，并在真实运行任务上读到 `running`、`active=1`；完成态可用 turn `durationMs` 还原实际完成时间，完成发生在连接前 60 秒内也会上报 `completed`，更早的历史完成按 `idle` 处理；等待输入/审批与实时完成/中断转换待验收 | [platform_windows.go](../client/internal/taskmonitor/adapters/platform_windows.go)、[desktop_socket_windows.go](../client/internal/taskmonitor/adapters/desktopipc/desktop_socket_windows.go)、[desktop_discovery.go](../client/internal/taskmonitor/adapters/desktopipc/desktop_discovery.go) |
| Windows 打包 | 已实现/待安装器验收 | `scripts/build-windows-app.ps1` 生成前端资源、`monitor-client.exe`、`monitor-agent.exe`、LHM helper 与绿色 zip；exe 内嵌 `app-icon.ico`（含多尺寸图标组）与版本信息；GUI 用 `-H windowsgui`、Agent 保留控制台以支持 `--collector-scan`；启动前检测 WebView2 运行时；包内附 README、版本与第三方声明。当前机器未安装 Inno Setup，`.iss` 已提供但安装器未编译；产物未签名 | [build-windows-app.ps1](../client/scripts/build-windows-app.ps1)、[packaging/windows](../client/packaging/windows)、[resources](../client/packaging/windows/resources) |

## 3. P0：Windows IPC 与进程生命周期

### 3.1 认证 IPC 通道

Windows 使用每用户 Named Pipe，Unix 保持现有 Unix socket 行为，IPC HTTP 路由与 DTO 不变。

| 项目 | 要求 |
| --- | --- |
| 管道名 | `\\.\pipe\ESP32-Monitor-<SID>`；SID 取当前进程 Token User，避免同名不同域用户共享 |
| 管道 ACL | SDDL `D:P(A;;GA;;;<SID>)`，只允许当前用户访问 |
| 服务端校验 | 每个连接用 `GetNamedPipeClientProcessId` 取客户端 PID，打开进程 Token，比较 Token User SID 与当前进程一致；不匹配返回 401 |
| 客户端校验 | 连接后用 `GetNamedPipeServerProcessId` 校验服务端 SID，防止连接到同名伪造管道 |
| 连接模型 | 保持每请求一条连接、`DisableKeepAlives`；HTTP 超时与 body 上限继续生效 |
| 浏览器隔离 | 继续拒绝带 `Origin` 或 `Sec-Fetch-Site` 的请求 |
| 默认路径 | Windows 返回管道名；macOS/Linux 继续返回 `os.UserConfigDir()/ESP32-Monitor/agent-ipc.sock` |
| 单实例 | 同一用户重复监听返回 `ErrAlreadyRunning`；不做“删除旧 socket 文件”的 Unix 逻辑 |

实现建议：优先使用 `github.com/Microsoft/go-winio` 的 `ListenPipe`/`DialPipe` 处理 Named Pipe 与 deadline；peer 校验使用 `golang.org/x/sys/windows`。若引入依赖，必须在 `go.mod` 固定版本并在 Windows 测试中覆盖 ACL 与 SID 校验。

### 3.2 Agent 启停与路径

| 项目 | 要求 |
| --- | --- |
| Agent 解析 | 依次尝试 `MONITOR_AGENT_BIN`、GUI 同目录 `monitor-agent.exe`、`bin/monitor-agent.exe`；不把 `.exe` 拼错 |
| 优雅退出 | GUI 先通过 IPC `/v1/app/quit` 请求退出；Agent 的 Shutdown 必须让 `agent.Run` 返回，而不只是取消 worker |
| 超时回退 | 宽限期（建议 5 秒）内未退出则 `Process.Kill`；重启场景需等待旧进程退出后再等待新 IPC 可用 |
| 信号 | Unix 保留 SIGTERM 处理；Windows 不依赖 SIGTERM |
| 崩溃恢复 | 既有 backoff 与 Supervisor 重启逻辑保持不变 |
| 设置变更重启（Windows） | GUI 调用 `/v1/app/restart`，已提权 Agent 用同一提权令牌拉起替换进程（`MONITOR_AGENT_RESTART=1`、`MONITOR_AGENT_RESTART_FROM=<旧 PID>`）后退出；替换进程先等旧 PID 退出，再以 200ms 间隔重试命名管道监听，避免采集/Companion 状态双开。GUI 保存只等待重启确认，后续保存等待新 PID 就绪 |
| 登录自启 | GUI 只写 HKCU Run，并通过 `/v1/app/login-task` 请求提权 Agent 创建/删除登录任务；任务用 Task Scheduler XML 创建以避开 `schtasks /TR` 的 261 字符限制，全程无 UAC |

### 3.3 P0 验收

- Windows 上 GUI 能启动 Agent，`Status`/`Collector*`/`Companion*` 路由全部可达。
- 不同用户或伪造管道无法通过 peer 校验。
- 关闭窗口/退出应用后 `monitor-agent.exe` 不残留；重启 GUI 不出现双 Agent 或锁冲突。
- Unix IPC 既有测试继续通过；Windows 新增 Named Pipe 测试通过。

本机原生证据（2026-10-29）：`go test ./internal/ipc/ ./internal/guibridge/ ./internal/agent/` 通过；Named Pipe 上完成 `Dial → Status → Quit`，第二实例监听返回 `ErrAlreadyRunning`；以真实 `monitor-agent.exe` 验证 Supervisor 启动、状态读取、退出后管道消失且进程结束。Unix 行为由平台分支保留，仍需在 macOS/Linux 原生环境复跑；不同 Windows 用户/伪造管道的拒绝需用第二个账户或独立测试进程补证。

## 4. P1：Windows Native 采集

### 4.1 平台源与 v2 映射重构

Native source 列表已按平台构建：Darwin 保持原列表，Windows 使用 `windows_*`，其他平台为空（`platform_sources_*.go`）。`v2Bindings` 由 `nativeSourceID(role)` 生成，Inventory 构建、对象归属和公共 IP 排除项统一走平台 helper，不再硬编码 `darwin_*`。现有 collector 测试改为按平台 Native source 生成 fixture；无 Native 实现的平台跳过依赖 Native 的用例。

Windows Core `diskio` 计数器按卷符（`C:`、`D:`）统计，无法与物理盘对象可靠对应，因此 Windows 不把 `diskio` 映射为 v2 `drive` 对象，`drive.read_rate`/`drive.write_rate` 在 Windows 明确不可用；不让卷对象和物理盘对象混用同一个 v2 drive ID。v2 网卡对象在 Windows 只包含 Native 已确认介质的物理适配器，Loopback/Tailscale/vEthernet/蓝牙不进入对象列表。

### 4.2 Windows 指标源

| Source | 目标字段 | 实现来源 | 可用性说明 |
| --- | --- | --- | --- |
| `windows_cpu` | 型号、物理/逻辑核、1/5/15 分钟负载、温度 | gopsutil CPU Info/Counts；`Win32_PerfFormattedData_PerfOS_System.ProcessorQueueLength` + `..._Processor._Total.PercentProcessorTime` 估算并按 60/300/900 秒 EWMA 平滑；包内 LHM 0.9.4 `net472` helper 读取 Package/Tctl/Tdie 温度（Windows 兼容路径仍保留 `root\LibreHardwareMonitor` WMI） | 负载为估算值，不是操作系统 load average；温度需 Agent 以管理员权限运行并安装 .NET Framework 4.7.2+，否则按 `lhm_permission_required`/`dotnet_framework_472_required` 报告；查询失败不重复卡住采集 |
| `windows_gpu` | 占用、温度 | `Win32_VideoController` + GPU Engine 性能计数器（按 LUID 汇总同引擎进程，取最忙引擎）；D3DKMT 枚举提供 LUID→PNP 设备映射；温度经 WDDM `D3DKMTQueryAdapterInfo(KMTQAITYPE_ADAPTERPERFDATA)` 读取驱动上报值 | 计数器被禁用或无 GPU 时不伪造 0；占用/温度仅在该卡与 D3DKMT 适配器唯一匹配时归属，重复同型号或多卡歧义时省略；需 WDDM 2.4+ 与驱动支持，软件适配器跳过 |
| `windows_disk` | 型号、物理总量、已用量、温度、SMART 健康 | `MSFT_PhysicalDisk` 物理盘清单/健康；包内 LHM 0.9.4 `net472` helper 提供温度与健康回退 | SMART 只探测健康状态，不读取细项；温度缺少 .NET Framework 4.7.2+ 或未提权时前端明确提示，不伪造数据 |
| `windows_disk_usage` | 物理盘已用 | `MSFT_Partition` 卷符归属 + `Win32_LogicalDisk` 已用空间汇总 | 仅当卷符唯一归属单盘时累加；无盘符分区、动态盘/存储空间跨盘场景不猜测 |
| `windows_network` | 接口类型、在线、IPv4 | `Win32_NetworkAdapter`（GUID/连接名/设备名映射）+ `net.Interfaces` | 只列 PCI/USB 物理适配器；介质按名称/连接名识别，未知物理适配器记 `other`；不猜唯一主链路 |
| `windows_public_ip` | 公网 IPv4 | 显式选择后 HTTPS 查询，缓存 5 分钟 | 扫描不触发外部请求；失败省略并保留真实观测时间 |
| `windows_battery` | 电量、充电状态 | `GetSystemPowerStatus` | 台机无电池时整体不可用，两项必须成对 |
| `windows_system` | OS 名称/版本/构建、产品名 | 注册表 `CurrentVersion`（含 UBR/DisplayVersion）+ BIOS `SystemManufacturer/ProductName` | 构建号 ≥22000 时把注册表 `Windows 10` 修正为 Windows 11；产品名为空或占位值时省略，不使用主机名 |

约束：

- 不可用返回原因（`unavailable` / `partial_data` / `permission_required`），不能补 0、不能把查询失败显示为健康。
- 固定信息只在启动时缓存；采样值按采集周期读取；磁盘健康 5 分钟缓存，分区拓扑 5 分钟缓存。
- 所有 WMI 调用受 context/超时约束，不能让一次采集卡住 Agent；注册表与电源状态为本地快速读取。
- 不新增协议字段；Windows 能力仍映射到现有 v2 语义 ID。

### 4.3 P1 验收

- `monitor-agent --collector-scan` 在 Windows 输出真实能力，缺失来源附原因。
- 单元测试覆盖：WMI/注册表字段映射、空值/权限失败、物理盘归属、电量成对、公网 IP 校验与缓存。
- v2 上传在 Windows 上包含必选指标；可选指标按实际可用性出现，不出现伪造值。
- Darwin 既有测试不回归。

2026-10-29 本机原生证据：`go test -count=1 ./internal/collector/` 通过（含 `TestWindowsNativeLiveScan` 与全部映射/温度/负载单测），实时扫描读到 Ryzen 7 7700（8/16 核，负载已采样）、RTX 4070 SUPER（占用 2%，D3DKMT 温度多次采样 47.5–59.8°C）、2×NVMe 健康与已用量、WLAN/有线网卡、Windows 11 Pro 25H2（26200.9711，MAXSUN MS-eSport B650ITX）；电池在本机不存在。提权 Agent 通过包内 LHM 0.9.4 helper 已使 `windows_cpu` 温度可用（`temperature_celsius`），同一机器非提权 `--collector-scan` 报告 `lhm_permission_required`。`monitor-agent --collector-scan` 输出对应来源与字段：`windows_cpu` 含 `load1/5/15`、`windows_gpu` 含 `temperature_celsius/usage_percent`，`windows_disk` 含温度与 SMART 健康状态，其余来源可用性一致。另用本地假 Server 跑通真实 Core/Native→v2 上传（未连接正式 Server）：样本必选 `c/m/u` 齐全，包含 CPU 负载 `l1/l5/l15=1335/1291/1284`、GPU `u=200,t=506`（2.00%、50.6°C）、Swap、两块物理盘的已用容量与 SMART 健康、WLAN/有线在线状态。仍需在带电池的笔记本、AMD/Intel 显卡与多 GPU 机器上复跑；本机已确认 `drive.read_rate/write_rate` 因 Windows 卷级计数器无法映射物理盘而不可用，风扇转速已移除。

## 5. P2：Windows 构建与打包

| 交付物 | 要求 |
| --- | --- |
| `scripts/build-windows-app.ps1` | 构建前端、同步 `internal/guiassets/assets`、生成 `monitor-client.exe` 与 `monitor-agent.exe`、执行 `go test`/`go vet` |
| `packaging/windows/` | exe 图标/版本信息、绿色包目录结构、安装/卸载脚本或 Inno Setup 定义 |
| 绿色包 | 解压即用；GUI 与 Agent 同目录；包含许可证与 WebView2 运行要求说明 |
| WebView2 | 启动前检测运行时；缺失时给出明确提示，不在无提示情况下闪退 |
| 登录自启/托盘 | 使用既有 HKCU Run 与 Wails 托盘；退出时清理自启项仅在用户关闭开关时执行 |
| 安装包 | 有 Inno Setup 时生成安装器；无 `ISCC` 时先交付绿色 zip，并记录未完成项 |
| 签名 | 本次不承诺正式签名；未签名产物必须标注 SmartScreen 风险 |

构建入口建议：

```powershell
cd client
.\scripts\build-windows-app.ps1
```

输出建议：`client/build/windows/ESP32-Monitor/` 与 `client/build/windows/ESP32-Monitor-<version>-windows-amd64.zip`。

### 5.1 P2 验收

- 干净目录解压后，`monitor-agent.exe --collector-scan` 可运行。
- 启动 `monitor-client.exe`，GUI 正常加载前端资源并连上 Agent。
- 关窗后台、重新唤出、退出、重启 Agent、登录自启开关逐项实测并留有证据。
- 安装/卸载（若提供安装器）不残留 Agent 进程、自启项或锁文件。

2026-10-29 本机原生证据：`scripts/build-windows-app.ps1` 通过（前端构建、net472 LHM helper、`go test ./...`、`go vet ./...`、windres 资源生成；设置测试已隔离 `APPDATA`/`HOME`，不再覆盖真实用户配置），产出 `client/build/windows/ESP32-Monitor/` 与 `ESP32-Monitor-0.1.0-windows-amd64.zip`；`monitor-client.exe` 为 Windows GUI 子系统且内嵌 `ESP32 Monitor Client 0.1.0` 版本信息与 `app-icon.ico`，`monitor-agent.exe` 保持 CUI 以支持 `--collector-scan`；解压目录直接运行 `monitor-agent.exe --collector-scan` 退出码 0。启动打包 GUI 后复用/启动提权 Agent，IPC `Status` 读到 Collector/Companion/Music/Codex 均启用；`windows_cpu`、`windows_gpu`、`windows_disk` 在本机均为 `available`，CPU 源包含 `temperature_celsius`，确认 LHM helper 在提权会话可用。关闭主窗口后进程仍存活且窗口隐藏，重新显示窗口成功；Windows Credential Manager Save/Load/Delete 往返通过；模拟 ESP32 已完成发现/配对并上报快照。设置变更实测由 Agent 自重启完成：IPC 确认 45ms、新 PID 就绪 1.06s，旧进程为父进程且无 UAC；登录自启计划任务经 IPC + Task Scheduler XML 创建/删除（0.14s）；v2 对象显示名改用 GPU/磁盘型号（本机 `NVIDIA GeForce RTX 4070 SUPER`、`ZHITAI Ti600 1TB`），不再显示 PNP 路径。仍未完成：托盘图标人工点击/菜单、真实播放中的进度/暂停/切歌复测、登录后实际自启与 `--login-item` 行为、Inno Setup 安装器（本机无 ISCC）与代码签名。绿色包未签名，首次运行会触发 SmartScreen。

## 6. P3：已有 Windows 代码的真实验证

| 能力 | 验证内容 |
| --- | --- |
| GSMTC 播放 | 播放/暂停/切歌/退出播放器；`playing`/`not_playing`、元数据、进度；PowerShell 退出与超时 |
| GitHub 凭据 | Credential Manager 保存、读取、删除；GUI 不返回秘密；跨 Agent 重启复用 |
| 登录自启 | 注册表值写入/删除；`--login-item` 不显示主窗口；状态栏图标可唤出 |
| 托盘 | Windows 图标显示、菜单打开/退出；无图标资源缺失 |
| 诊断 | 导出报告不含 Token、播放内容、任务标题、贡献日历 |

2026-10-29 本机 P3 证据：GSMTC PowerShell 脚本读到真实媒体会话（`成全`/林宥嘉），`SourceAppUserModelId` 经 Start 菜单映射为友好名称 `Folia`；播放进度按 `TimelineProperties.LastUpdatedTime` 外推，GUI 基于 `position_observed_at` 每秒插值。真实播放/暂停/切歌与进度推进仍需在播放音频时复测。Credential Manager 使用独立测试账号完成 Save→Load（内容一致）→Delete→不可读往返。关闭主窗口后 `monitor-client.exe` 存活、窗口隐藏，重新显示成功；托盘图标资源由彩色应用图标生成，人工左键/右键菜单尚未点击验收。登录自启与 `--login-item` 仍需在用户会话中实际开关并检查 HKCU Run/计划任务/重启后行为。

Codex Desktop 任务观察已通过内部 Named Pipe `\\.\pipe\codex-ipc` 接入：默认从 `session_index.jsonl` 选择 `updated_at` 最新的任务并只读 follow 该线程，连接期间每 2 秒复查索引、出现更新任务时自动切换；`--codex-thread` 仍可显式覆盖。观察器不处理审批、不启动或恢复 turn，并校验管道服务端与当前进程同用户；索引只解码 id/updated_at，不保留任务名称或内容。fake pipe 单测覆盖自动选择、注册、follow、快照与断连；真实管道连接、SID 校验及真实运行任务观察均在本机通过，观察到 `running` 状态与 `active=1`。完成态由 turn 的 `durationMs` 还原实际完成时间：连接建立时若完成发生在 60 秒内，即使未观察到 running→completed 转换也上报 `completed`，并按实际完成时间计算 60 秒保留期；更早的历史完成仍按 `idle` 处理，避免把旧完成当成新事件。该路径有单测覆盖。仍需补齐等待输入/审批、实时完成/中断转换的 Windows 原生证据；不得用“额度读取成功”冒充任务可观察。

## 7. 测试与证据

| 检查 | 命令/方式 | 证明范围 |
| --- | --- | --- |
| Go 单测/竞态 | `go test -race ./...` | 包内行为 |
| 静态检查 | `go vet ./...` | 编译期/常见错误 |
| IPC 集成 | Windows Named Pipe 测试 + 现有 Unix 测试 | 认证、单实例、路由、Quit |
| 采集能力 | `monitor-agent --collector-scan` | 本机真实 Native 可用性 |
| 前端 | `cd frontend && npm ci && npm test` | 类型、构建、DOM 测试 |
| 打包 | 解压绿色包并启动 GUI/Agent | 产物完整性 |
| 手工验收 | 关窗/唤出/退出/自启/播放/凭据 | 真实 Windows 会话行为 |

证据至少包含：命令与退出码、关键日志、能力扫描 JSON、GUI 截图、进程/注册表状态。不得用交叉编译或模拟端结果替代 Windows 原生证据。

## 8. 风险与未决项

- **WMI 权限与可用性**：温度、SMART、GPU 计数器在不同整机上差异很大；必须允许整体不可用。CPU/磁盘温度依赖包内 LHM helper 和 .NET Framework 4.7.2+，Agent 需管理员权限；SMART 健康状态优先 `MSFT_PhysicalDisk`，LHM 仅作回退；非提权或运行时缺失按 `lhm_permission_required`/`dotnet_framework_472_required` 报告，不伪造数据。
- **物理盘已用量归属**：动态磁盘、跨盘卷、无盘符分区可能导致无法唯一归属；宁可省略。当前实现只累加卷符唯一映射到单盘的分区，存储空间/动态盘未专项建模。
- **Windows 磁盘速率与 SMART**：Core `diskio` 是卷级计数器，无法映射物理盘，故 `drive.read_rate/write_rate` 不可用；SMART 全平台只探测健康状态，`MSFT_PhysicalDisk` 提供主要状态、LHM helper 作回退，温度由 helper 按可用性提供，驱动器不支持或未提权时省略；不读取 SMART 细项。
- **GPU 温度**：使用 WDDM 驱动性能数据而非 NVML/ADXL/IGCL，NVIDIA/AMD/Intel 统一走内核显示驱动；驱动需支持 WDDM 2.4+，部分虚拟/旧驱动不返回传感器。温度到 WMI 显卡的归属按 PNP 前缀/型号唯一匹配，多卡或同型号重复时省略。
- **多 GPU 归属**：GPU Engine 以 LUID 标识适配器；Windows 通过 D3DKMT 枚举拿到 LUID、PNP 设备 ID 与温度，再按 PNP 前缀/型号唯一映射到 WMI 显卡，因此多 GPU（含核显+独显）在映射唯一时可分别上报占用与温度。重复同型号、虚拟适配器或映射失败时省略，不用同一 LUID 猜多张卡。
- **电池跨硬件矩阵**：本机为无电池台式机，配对与缺失路径有单测，但真实笔记本仍需补原生证据。
- **WebView2**：干净机器可能未安装；安装包策略需冻结（引导安装或明确前置依赖）。
- **安装器工具链**：当前机器未检测到 Inno Setup/NSIS；绿色 zip 已交付，`packaging/windows/ESP32-Monitor.iss` 已提供但未编译，需在装有 ISCC 的环境补安装器验收。
- **代码签名**：未签名 exe 会触发 SmartScreen；不属于本次完成条件，但发布前必须解决。
- **Codex Desktop**：`\\.\pipe\codex-ipc` 是内部协议（流版本 v11），随桌面版本变化；未知版本会显式降级为不观察，但仍需随桌面升级持续回归，真实任务广播证据待补。
- **时间偏差修复**：Server 未来时间拒收、本机时间同步与 UAC 流程仍是独立缺口，不因 Windows IPC 完成而关闭。

## 9. 实施顺序

1. P0：Named Pipe IPC + peer 校验 + Supervisor/Agent 生命周期 + Windows IPC 测试。[已完成并本机验证]
2. P1：平台 source/v2 映射重构 + Windows Native 采集 + 能力扫描与单测。[已完成并本机验证；跨硬件矩阵待补]
3. P2：Windows 构建脚本 + 绿色包/安装包 + 启动验证。[已完成绿色包、图标/版本资源、WebView2 检测；安装器因无 ISCC 未编译，签名未做]
4. P3：GSMTC、凭据、自启、托盘的真实验收。[部分完成：凭据往返、GSMTC 适配器健康、关窗后台、提权 LHM 采集已在本机验证；托盘点击、登录自启、真实播放元数据待人工验收]

每阶段完成后再更新 [Client 能力表](CLIENT_CAPABILITIES.md) 与 [V1 计划](V1_RELEASE_PLAN.md) 中对应状态；未取得 Windows 原生证据前不得标记为已交付。
