# Telegraf 客户端安装与配置

本指南说明如何在 Windows、macOS、Linux 或 NAS 上运行 Telegraf，采集主机指标并通过 HTTP POST 上传到 ESP32-Monitor Server。无需安装 InfluxDB；NAS 使用 Linux 模板。

[English](README.md)

按以下顺序完成部署：选择模板、准备服务端设备和上传 Token、安装 Telegraf、验证采集与上传，最后按需配置后台服务。首次部署建议以前台方式完成上传验证后再设置开机运行。

## 模板

| 系统 | 配置模板 | 默认磁盘路径 |
| --- | --- | --- |
| Windows | [Win/telegraf.conf](Win/telegraf.conf) | `C:` |
| macOS | [Mac/telegraf.conf](Mac/telegraf.conf) | `/` |
| Linux（含 NAS） | [Linux/telegraf.conf](Linux/telegraf.conf) | `/` |

模板采用 Telegraf **1.40.2** 配置格式，使用 Telegraf 提供的跨平台输入插件和 HTTP 输出插件，适用于 Windows、macOS 和 Linux。

## 准备服务端信息

先由 Server 管理员在交互式配置中注册设备，取得该设备专用的 `write_token`，并将设备加入 `panel.devices`。客户端不能自行注册设备；每台主机使用不同的上传 Token，不使用面板的 `read_token`。

复制对应模板到部署位置，修改以下内容：

| 配置项 | 如何填写 |
| --- | --- |
| `outputs.http.url` | 服务端 Base URL 加 `/v1/ingest/telegraf`，例如 `https://monitor.kevinleenj.top:1443/v1/ingest/telegraf` |
| `outputs.http.headers.Authorization` | 保留 `Bearer ` 前缀，将 `<UPLOAD_TOKEN>` 替换为 Server 生成的 64 位十六进制上传 Token |
| `inputs.disk.mount_points` | 只填写要显示的一个实际挂载点，与服务端该设备的 `disk_mount_path` 完全一致 |

例如 NAS 要监控 `/vol1`，就在 Linux 模板中设置 `mount_points = ["/vol1"]`，服务端对应设备设置 `disk_mount_path: "/vol1"`。Windows 默认是 `C:`，不自行添加斜杠；以 `--test` 输出的磁盘 `path` 标签为准，服务端 YAML 可写 `disk_mount_path: 'C:'`。

设备身份由上传 Token 决定。修改主机名不会创建新设备；卡片名称、排序、磁盘选择在服务端配置。客户端与服务端应开启系统时间同步。

## 安装 Telegraf

从 [InfluxData 官方安装文档](https://docs.influxdata.com/telegraf/v1/install/) 或 [官方下载页](https://www.influxdata.com/downloads/) 选择对应操作系统与 CPU 架构，核对下载包的官方 SHA-256。模板不包含可执行文件。

### Windows

下载官方 Windows ZIP 并解压，将 `telegraf.exe` 放入 `C:\Program Files\Telegraf`，将本目录的 `Win/telegraf.conf` 复制到同一目录后填写配置。下面的命令使用 PowerShell；安装服务时以管理员身份运行。

```powershell
& 'C:\Program Files\Telegraf\telegraf.exe' version
& 'C:\Program Files\Telegraf\telegraf.exe' --config 'C:\Program Files\Telegraf\telegraf.conf' --test
```

配置含明文 Token，在文件安全属性中仅允许管理员和实际运行 Telegraf 的账户读取。前台测试和服务运行应使用同一份配置。

### macOS

已安装 Homebrew 时：

```sh
brew install telegraf
telegraf version
mkdir -p "$HOME/.config/esp32-monitor"
chmod 700 "$HOME/.config/esp32-monitor"
cp client/Mac/telegraf.conf "$HOME/.config/esp32-monitor/telegraf.conf"
chmod 600 "$HOME/.config/esp32-monitor/telegraf.conf"
```

复制命令在项目根目录执行，随后编辑复制出的配置。也可下载官方 macOS 二进制放到用户自己的工具目录，后续命令替换为其绝对路径。

### Linux（含 NAS）

可以按照官方文档安装发行包；也可下载官方 Linux 压缩包，解压后找到其中的 `telegraf` 可执行文件。以下使用独立二进制方式，无需 root，也不会自动安装系统服务：

```sh
mkdir -p "$HOME/.local/bin" "$HOME/.config/esp32-monitor"
chmod 700 "$HOME/.config/esp32-monitor"
# 将下方源路径替换为解压后找到的实际文件。
install -m 755 /实际解压路径/telegraf "$HOME/.local/bin/telegraf"
cp client/Linux/telegraf.conf "$HOME/.config/esp32-monitor/telegraf.conf"
chmod 600 "$HOME/.config/esp32-monitor/telegraf.conf"
"$HOME/.local/bin/telegraf" version
```

复制模板时在项目根目录执行，或者改用模板的绝对路径；随后编辑部署配置。运行账户需要能读取选定挂载点的容量信息。

## 检查采集并验证上传

### 仅检查采集

macOS：

```sh
telegraf --config "$HOME/.config/esp32-monitor/telegraf.conf" --test
```

Linux：

```sh
"$HOME/.local/bin/telegraf" --config "$HOME/.config/esp32-monitor/telegraf.conf" --test
```

Windows 使用前述 PowerShell `--test` 命令。应检查以下 measurement、field 和 tag；`--test` 输出通常是行协议，这不改变 HTTP 输出的 JSON 格式。

| 用途 | 必需的数据 |
| --- | --- |
| `CPU` | `cpu.usage_active`，标签 `cpu=cpu-total` |
| `MEM` | `mem.used_percent` |
| `DISK` | `disk.used_percent`，标签 `path` 等于服务端配置的磁盘路径 |
| 运行时间 | `system.uptime`，整数秒 |

`--test` 不执行输出插件，不能证明上传成功。CPU 是差分采样，首次采样可能不完整，需在持续运行后确认。

### 持续运行，验证实际上传

macOS：

```sh
telegraf --config "$HOME/.config/esp32-monitor/telegraf.conf"
```

Linux：

```sh
"$HOME/.local/bin/telegraf" --config "$HOME/.config/esp32-monitor/telegraf.conf"
```

Windows：

```powershell
& 'C:\Program Files\Telegraf\telegraf.exe' --config 'C:\Program Files\Telegraf\telegraf.conf'
```

运行至少 30 秒后，由管理员检查 Server 的 `ingest` 日志：对应 `device_id` 返回 `status=204` 且 `accepted_fields>0`，再用面板只读 Token 请求 `GET /v1/devices/summary`，确认该设备及 CPU、MEM、DISK 和运行时间出现。仅 `/readyz` 返回成功不能证明数据已上传。前台运行时按 Ctrl+C 退出。

默认每 10 秒采集和刷新上传，HTTP 请求使用秒级时间戳的批量 JSON，不压缩。模板保留这四类插件产生的数值字段，Server 固定选择上述三个百分比字段显示。停止上报超过服务端配置的在线超时（默认 45 秒）后，下一次 summary 请求将不再包含该设备。

## 可选：配置后台运行

先完成前台上传验证，再按实际需要配置后台运行。已经运行 Telegraf 的主机应先检查现有配置和服务，避免同一设备运行两个上传进程。

### Windows 服务

管理员 PowerShell，注册时使用配置文件的绝对路径：

```powershell
& 'C:\Program Files\Telegraf\telegraf.exe' --config 'C:\Program Files\Telegraf\telegraf.conf' service install
& 'C:\Program Files\Telegraf\telegraf.exe' service start
& 'C:\Program Files\Telegraf\telegraf.exe' service status
```

修改配置后停止再启动；移除服务：

```powershell
& 'C:\Program Files\Telegraf\telegraf.exe' service stop
& 'C:\Program Files\Telegraf\telegraf.exe' service uninstall
```

启动错误查看 Windows 事件查看器的“Windows 日志 → 应用程序”；运行日志输出到模板默认的 stderr，若需落盘可设置 `[agent]` 的 `logfile`，并为运行账户授予该日志目录的写权限。

### macOS / Linux / NAS

在现有服务管理器中创建任务，启动命令使用 **Telegraf 二进制绝对路径 + `--config` + 配置文件绝对路径**。例如将 Linux 启动命令中的 `$HOME` 展开为实际路径：

```text
/home/your-user/.local/bin/telegraf --config /home/your-user/.config/esp32-monitor/telegraf.conf
```

配置运行账户、退出后重启策略及按需开机启动。macOS 可用 launchd 或 Homebrew 服务；Homebrew 服务默认读取其前缀下的 `etc/telegraf.conf`，如使用它，应先将已填写的配置部署到 `$(brew --prefix)/etc/telegraf.conf`，核对权限，再执行 `brew services start telegraf`。Linux 发行包服务通常读取 `/etc/telegraf/telegraf.conf` 并可能额外加载 `telegraf.d`；检查启动参数，避免混入默认 InfluxDB 输出或其他采集配置。

NAS 使用 Linux 模板和其自身的服务管理器。Telegraf 只需主动连接 Server，无需设置反向代理或入站端口。如果 NAS 与 Server 在同一主机，可将 NAS 自身的输出 URL 改为 `http://127.0.0.1:18080/v1/ingest/telegraf`（端口按实际配置）；其他机器使用公网 HTTPS URL。

默认日志写 stderr，可由服务管理器收集。systemd 部署可使用 `journalctl -u 实际服务名 -f` 查看；其他服务管理器使用其日志界面。修改配置后重启对应服务，再检查 Server 入库和 summary。

## 常见问题

| 现象 | 检查项 |
| --- | --- |
| HTTP 401 | Token 是否完整、是否属于该 Server；不要保留尖括号或复制额外空白 |
| HTTP 403 | 是否误用了面板只读 Token |
| HTTP 404 / 502 | URL 是否包含正确端口和 `/v1/ingest/telegraf`；反代是否能连接 Server |
| TLS 校验失败 | 域名、证书链、本机时间和根证书；保留 `insecure_skip_verify = false` |
| HTTP 422 | 系统时间是否偏差；是否改坏原生 JSON、时间单位或插件结构 |
| CPU / MEM / DISK 显示 `--` | 必需字段是否存在和新鲜；CPU 是否已完成差分采样；磁盘 `path` 是否逐字一致 |
| 上传成功但没有卡片 | Token 对应设备是否加入 `panel.devices`；是否已超过在线超时 |
| 网络中断 | 缓冲只在内存中，重启进程会丢失；HTTP 429 和 5xx 会重试，模板列出的其他错误状态不重试，应修正配置 |

将真实配置保存在仓库外，不覆盖提交用的占位模板。分享日志时检查是否包含密钥、主机信息或磁盘标识。

## 官方参考

- [安装与平台说明](https://docs.influxdata.com/telegraf/v1/install/)
- [Windows 服务（v1.40.2）](https://github.com/influxdata/telegraf/blob/v1.40.2/docs/WINDOWS_SERVICE.md)
- [HTTP 输出](https://github.com/influxdata/telegraf/blob/v1.40.2/plugins/outputs/http/README.md) 与 [JSON 序列化](https://github.com/influxdata/telegraf/blob/v1.40.2/plugins/serializers/json/README.md)
- [CPU](https://github.com/influxdata/telegraf/blob/v1.40.2/plugins/inputs/cpu/README.md)、[内存](https://github.com/influxdata/telegraf/blob/v1.40.2/plugins/inputs/mem/README.md)、[磁盘](https://github.com/influxdata/telegraf/blob/v1.40.2/plugins/inputs/disk/README.md)、[系统运行时间](https://github.com/influxdata/telegraf/blob/v1.40.2/plugins/inputs/system/README.md)
