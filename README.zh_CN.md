# ESP32-Monitor

ESP32-S3 400×300反射式LCD监视器，包含桌面Client、NAS Server及复用固件UI的桌面模拟器。

主文档仅维护以下五份；事实以当前Client实现为准，计划与原生/实机验收分开标注：

| 文档 | 内容 |
| --- | --- |
| [Server与Client通信协议（v2）](docs/SERVER_CLIENT_PROTOCOL_V2.md) | 注册、档案、指标上报和生产Server待实现部分 |
| [ESP32与上位机通信协议](docs/ESP32_COMPANION_PROTOCOL_V1.md) | 业务V1、Protobuf线编码v2、LAN与串口 |
| [Client整体能力表](docs/CLIENT_CAPABILITIES.md) | 承接原METRICS.md全部七类要求，含GUI/采集/提供者/平台限制和开发入口 |
| [V1计划](docs/V1_RELEASE_PLAN.md) | 剩余工作、阶段、部署与发布门槛 |
| [ESP32固件计划能力表](docs/FIRMWARE_CAPABILITIES_PLAN.md) | 现有基础、三模式目标、实机验收和构建/测试入口 |

**当前Client指标上传只支持v2，正式Server仍为v1；升级前通过本地sim-server联调，不能宣称完整链路已可用。**

旧文档与历史证据按原路径保存在 [.bak/docs-cleanup-2026-10-28](.bak/docs-cleanup-2026-10-28)，[manifest.json](.bak/docs-cleanup-2026-10-28/manifest.json)记录原路径与SHA-256，未删除原内容。备份不再作为当前规范。许可证、素材来源、依赖组件说明、补丁说明和AGENTS.md保留原位；两个README只作导航。

[English entry](README.md)

## Linux Client 使用准备

以下以已安装桌面环境的 Debian 13 为例，使用一条命令安装 AppImage 所需的系统工具和服务：

```sh
sudo apt install libgtk-4-1 libwebkitgtk-6.0-4 xdg-utils libglib2.0-bin udisks2 upower bubblewrap xdg-dbus-proxy
```

Fedora 用户需安装界面运行库及对应的系统工具和服务：

```sh
sudo dnf install gtk4 webkitgtk6.0 xdg-utils udisks2 upower bubblewrap xdg-dbus-proxy
```

如果需要采集 GPU 占用率和温度，另行安装支持 `--snapshot` 的 `nvtop`：

```sh
sudo apt install nvtop
```

其他发行版请安装对应名称的软件包。GitHub 登录使用桌面环境中已解锁的 Secret Service 兼容密钥环；Codex 额度功能需要已登录的 Codex CLI 或受支持路径下的 Codex Desktop。指标可用性取决于硬件、驱动和权限。磁盘容量只累计已挂载分区/整盘与 RAID 成员分区，未挂载且不属于阵列的分区不计入；RAID 卷单独上报，卷用量按成员容量分摊到物理盘，详见 [Client整体能力表](docs/CLIENT_CAPABILITIES.md)。

### 运行 AppImage

赋予下载的 AppImage 执行权限后运行：

```sh
chmod +x ESP32-Monitor-x86_64.AppImage
./ESP32-Monitor-x86_64.AppImage
```

AppImage 包含 Client、Agent 和前端资源；GTK 4、WebKitGTK 6.0 及其依赖由系统提供，需先按上述命令安装。程序使用系统默认的 Wayland 或 X11 后端。本次发行包在 Debian 13（glibc 2.41）构建，旧版系统及其他 GTK / WebKitGTK 版本的兼容性未保证；CPU 架构须与 AppImage 一致。Fedora 45 已由用户验证系统库实验包可运行。

首次从 AppImage 启动时，Client 会在 `~/.local/share` 下写入桌面项和图标，供 KDE 等 Wayland 合成器显示窗口图标；设置 `ESP32_MONITOR_SKIP_DESKTOP_INTEGRATION=1` 可跳过。删除 `~/.local/share/applications/esp32-monitor.desktop` 和 `~/.local/share/icons/hicolor/256x256/apps/esp32-monitor.png` 可撤销。

构建和验证使用的版本如下；这些是已使用的版本，并非对更低版本兼容性的承诺：

| 依赖 | 版本 | 用途 |
| --- | --- | --- |
| GTK | 4.18.6（构建时） | 桌面界面 |
| WebKitGTK | 2.54.0，API 6.0（构建时） | 界面渲染 |
| GLib | 2.84.4（构建时） | GTK 基础库；系统 `gsettings` 仍需 `libglib2.0-bin` |
| UDisks2 / UPower | 2.10.1 / 1.90.9 | 磁盘 SMART 缓存 / 电源信息 |
| xdg-utils | 1.2.1 | `xdg-open` 打开链接 |
| bubblewrap / xdg-dbus-proxy | 0.12.0 / 0.1.6 | WebKit 沙箱 |
| nvtop（可选） | 3.2.0 | GPU 信息，要求支持 `--snapshot` |

若本机无法通过 FUSE 挂载，可运行 `APPIMAGE_EXTRACT_AND_RUN=1 ./ESP32-Monitor-x86_64.AppImage`。

## 无头模式（仅 Agent）

服务器或 NAS 可只运行 Agent：进程内采集并上传 v2，不需要 GUI、WebView 或桌面依赖。必须使用普通用户运行，禁止 `sudo`/root；无需也不应获得块设备写权限或 SMART 刷新权限。

构建 Agent：

```sh
cd client
make build        # 输出 bin/monitor-agent

# 也可在其他机器交叉编译（按目标架构调整 GOARCH）：
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o monitor-agent ./cmd/monitor-agent
```

首次注册：

1. 在 Server 管理 WebUI 添加设备，复制 6 位验证码（10 分钟内有效、仅可使用一次）。
2. 用验证码换取设备 Token：

```sh
curl -sS -X POST https://server.example/v2/collector/register \
  -H 'Content-Type: application/json' -d '{"code":"123456"}'
```

3. 将返回的 `token` 写入私有文件；Agent 每次上传前都会重新读取并校验：

```sh
mkdir -p ~/.config/esp32-monitor
umask 077
printf '%s\n' '<token>' > ~/.config/esp32-monitor/collector-token
```

创建配置文件（`upload.optional` 初始为空；CPU、内存和运行时长三项必选值始终上传）：

```json
{"interval_seconds": 10, "selections": [], "upload": {"enabled": true, "interval_seconds": 10, "optional": []}}
```

需要可选指标时，在 `upload.optional` 中列出对象 ID，例如 `{"metric":"drive.read_rate","objects":[5]}`。运行

```sh
monitor-agent --collector --collector-state-dir ~/.local/state/esp32-monitor/collector --collector-scan
```

可输出可用指标与对象 ID；ID 与机器相关，不能从其他机器复制。

启动 Agent：

```sh
monitor-agent --collector \
  --collector-state-dir ~/.local/state/esp32-monitor/collector \
  --collector-upload-url https://server.example/v2/collector/samples \
  --collector-upload-token-file ~/.config/esp32-monitor/collector-token \
  --collector-config ~/.config/esp32-monitor/collector.json
```

Agent 会创建并校验 `0700` 状态目录，Token 文件须为 `0600`。`--collector-config` 仅在尚无持久化配置的首次启动时应用。可选依赖用于补充指标：`udisks2`（SMART 缓存）、支持 `--snapshot` 的 `nvtop`（GPU）、`upower`（电池）；未安装时相应指标省略，不影响上传。磁盘采集只读 `/sys`、`/proc`、`statfs` 与 UDisks2 缓存属性，不会唤醒休眠磁盘。完整的指标、RAID 与 Linux 限制见 [Client整体能力表](docs/CLIENT_CAPABILITIES.md)。

## 许可证

本项目采用 MIT 许可证，见 [LICENSE](LICENSE)。第三方组件与内置素材的许可证见 [client/packaging/THIRD_PARTY_NOTICES.txt](client/packaging/THIRD_PARTY_NOTICES.txt)。
