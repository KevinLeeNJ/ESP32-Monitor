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
sudo apt install util-linux xdg-utils libglib2.0-bin udisks2 upower bubblewrap xdg-dbus-proxy
```

如果需要采集 GPU 占用率和温度，另行安装支持 `--snapshot` 的 `nvtop`：

```sh
sudo apt install nvtop
```

其他发行版请安装对应名称的软件包。GitHub 登录使用桌面环境中已解锁的 Secret Service 兼容密钥环；Codex 额度功能需要已登录的 Codex CLI 或受支持路径下的 Codex Desktop。指标可用性取决于硬件、驱动和权限。磁盘只显示包含可归属已挂载文件系统的物理盘；容量和用量都只统计已挂载文件系统。

### 运行 AppImage

赋予下载的 AppImage 执行权限后运行：

```sh
chmod +x ESP32-Monitor-x86_64.AppImage
./ESP32-Monitor-x86_64.AppImage
```

AppImage 包含 Client、Agent、前端资源以及 GTK 4 和 WebKitGTK 6.0 运行库；图形驱动、桌面服务及上面列出的命令仍由系统提供。支持 Ubuntu 24.04+ 与 Debian 13+（x86_64）；本次在 Debian 13（glibc 2.41）构建并测试，最低运行环境为 glibc 2.39 和 GLIBCXX 3.4.32，Fedora 40+ 也满足该要求但未测试。Ubuntu 22.04、Debian 12 及更早版本不受支持。可用 `ldd --version` 查看系统 glibc 版本；CPU 架构也必须与 AppImage 一致。

首次从 AppImage 启动时，Client 会在 `~/.local/share` 下写入桌面项和图标，供 KDE 等 Wayland 合成器显示窗口图标；设置 `ESP32_MONITOR_SKIP_DESKTOP_INTEGRATION=1` 可跳过。删除 `~/.local/share/applications/esp32-monitor.desktop` 和 `~/.local/share/icons/hicolor/256x256/apps/esp32-monitor.png` 可撤销。

构建和验证使用的版本如下；这些是已使用的版本，并非对更低版本兼容性的承诺：

| 依赖 | 版本 | 用途 |
| --- | --- | --- |
| GTK | 4.18.6（内置） | 桌面界面 |
| WebKitGTK | 2.54.0，API 6.0（内置） | 界面渲染 |
| GLib | 2.84.4（界面运行库内置） | GTK 基础库；系统 `gsettings` 仍需 `libglib2.0-bin` |
| util-linux | 2.41.5 | `lsblk`，要求支持 `--json`、`--bytes` 和 `FSUSED` 列 |
| UDisks2 / UPower | 2.10.1 / 1.90.9 | 磁盘 SMART 缓存 / 电源信息 |
| xdg-utils | 1.2.1 | `xdg-open` 打开链接 |
| bubblewrap / xdg-dbus-proxy | 0.12.0 / 0.1.6 | WebKit 沙箱 |
| nvtop（可选） | 3.2.0 | GPU 信息，要求支持 `--snapshot` |

若本机无法通过 FUSE 挂载，可运行 `APPIMAGE_EXTRACT_AND_RUN=1 ./ESP32-Monitor-x86_64.AppImage`。

## 许可证

本项目采用 MIT 许可证，见 [LICENSE](LICENSE)。第三方组件与内置素材的许可证见 [client/packaging/THIRD_PARTY_NOTICES.txt](client/packaging/THIRD_PARTY_NOTICES.txt)。
