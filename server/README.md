# Monitor Server V1

按 `../docs/DEVICES_DATA_SOURCE_V1_PLAN.md` 实现 NAS 服务端。所有服务端源码、依赖锁文件、示例和开发说明位于本目录。使用 Go 标准库 HTTP / JSON，配置菜单使用 Charm Huh v2（Bubble Tea / Lip Gloss），SQLite 使用 `modernc.org/sqlite`，YAML 使用 `go.yaml.in/yaml/v3`，限流使用 `golang.org/x/time/rate`，交互终端检测使用 `golang.org/x/term`，配置编辑锁使用 `gofrs/flock`。

已完成模块 UT、真实 HTTP + SQLite API 集成、macOS Telegraf 回环、NAS 原生构建与 Linux 回环，以及本机 Telegraf 通过公网 HTTPS 连接 NAS 的端到端验收。测试报告与验收证据已清理；集成测试必需的脱敏采集输入保留在 `tests/integration/testdata/`。ESP32 Devices 接入与设备联调尚未完成。

## 本地开发与 UT

需要 Go 1.26 或更新版本；依赖版本锁定在 `go.mod` / `go.sum`。

```sh
cd server
go mod download
make test-ut
make test-race
make test-api
make vet
make build
```

普通 UT 不监听网络、不访问 NAS、不启动 Telegraf。HTTP 模块以 `httptest.NewRecorder` 和替身仓储测试单个 handler；SQLite 仓储 UT 使用 `t.TempDir()` 的独立文件与固定时间。覆盖率文件在本地忽略，不进入仓库。

配置菜单仍通过 `bin/monitor-server configure --config /实际路径/monitor.yaml` 启动。方向键选择、Enter 确认、Tab / Shift+Tab 在输入字段间切换，Esc 返回当前菜单。在主菜单按 Esc 或 Ctrl+C 请求退出；存在未保存修改时需确认放弃。删除、轮换 Token、保存的确认按钮默认选择“返回”。主菜单与设备列表隐藏 Token，显式选择查看才显示；新生成的 Token 在保存成功后输出供复制。配置修改保存后仍需手动重启 Server。

输入校验错误会原地显示，可修正后继续。菜单自动适应终端窗口，`/` 可筛选选项；`TERM=dumb` 时 Huh 自动降级为编号选择模式。普通单元测试使用可替换的提示接口，验证配置事务、Token 操作、排序、取消、容量与并发保护；另外提供真实 PTY 终端测试，覆盖按键导航、输入修正、窗口缩放、保存、删除和退出后的终端恢复。

在 macOS / Linux 上安装仅供终端测试的 Python 依赖后运行（不连接 NAS，不启动 HTTP 服务，不保留配置、Token 或测试报告）：

```sh
python3 -m venv /tmp/monitor-tui-venv
/tmp/monitor-tui-venv/bin/python -m pip install -r tests/e2e/requirements-tui.txt
PATH="/tmp/monitor-tui-venv/bin:$PATH" make test-tui
```

依赖 [Huh 官方配置 API](https://github.com/charmbracelet/huh) 和 [pyte 终端模拟器](https://pyte.readthedocs.io/en/latest/tutorial.html)；pyte 仅用于测试，不进入 Server 二进制。

`make test-api` 显式启用 `integration` build tag，通过真实回环临时端口调用生产 HTTP Server 与 SQLite；配置、随机 Token 和数据库均位于临时目录。测试共享正式 Server 参数，使用独立 SQL 连接核对持久化状态，并覆盖事务回滚、重复/乱序、离线/TTL、并发限流、重建服务与凭据变更。完整回归可运行 `go test -race -tags=integration -count=1 ./...`，静态检查用 `go vet -tags=integration ./...`。

```text
cmd/monitor-server/  命令入口、进程生命周期、每小时清理
internal/config/    YAML schema、严格校验、随机 Token、0600 原子保存与冲突检测
internal/cli/       首次配置、设备维护、展示排序、查看与轮换 Token
internal/metrics/   Telegraf 批量 JSON 校验、host 剔除、tag 规范化
internal/storage/   schema 版本、WAL/外键/忙等待、事务、latest/history、清理
internal/panel/     在线过滤、固定字段映射、新鲜度与显示格式
internal/api/       HTTP Server 参数、权限隔离、限流、大小限制、summary、探针
tests/integration/  真实回环 HTTP + SQLite 的 API 模拟测试及脱敏协议夹具
tests/e2e/          安装版 Telegraf 与 Server 进程端到端测试、真实采集证据
```

## 配置与运行

以下命令是本地部署和人工操作入口。API 集成模拟运行临时 HTTP Server，不启动此处的长期服务进程，也不连接真实 Agent。

```sh
./bin/monitor-server configure --config ./config/monitor.yaml
./bin/monitor-server check-config --config ./config/monitor.yaml
./bin/monitor-server serve --config ./config/monitor.yaml
```

`configure` 要求 stdin/stdout 为交互终端。首次设置监听地址、SQLite 路径、对外 Base URL 并逐台登记客户端；配置确认保存后显示新生成的面板与上传 Token。再次运行提供基础配置、增删改客户端、展示排序、查看和轮换 Token 菜单。设备 ID 稳定，编辑名称/挂载点/排序不会轮换 Token。取消或输入中断不保存；配置被其他编辑器修改时拒绝覆盖。菜单中的查看操作读取暂存配置；Token 在保存并重启服务后生效。

Token 为密码学随机源生成的 32 字节、64 位小写十六进制。只读与各上传 Token 必须不同。配置只接受 `0600` 的普通文件；写入使用同目录临时文件、同步、原子替换与编辑锁。保留的 `.lock` 文件是锁载体，不含凭据；不要在另一个向导运行时删除它。人工编辑配置也须保持 `0600`，不得向仓库或发布包提交实际配置。脱敏示例见 `config/monitor.example.yaml`；其中占位 Token 故意不能通过校验。

SQLite 路径相对于 **服务进程工作目录**；NAS 部署建议使用绝对路径。服务启动会创建不存在的数据目录和数据库文件；数据库为 `0600`，父目录新建权限为 `0700`。在 NAS 本地磁盘存储，避免 NFS/SMB。配置调整后先 `check-config` 再受控重启；轮换旧 Token 在新进程中失效，删除客户端后其旧 Token 返回 401。

向导只展示需复制的 Token，不输出 Telegraf 配置片段。管理员参照计划的 Telegraf 模板自行设置上传 URL、header 和挂载点。面板 Base URL 与只读 Token 复制到 ESP32 的 Devices 配置区。

## HTTP 契约

| 接口 | 权限 | 成功 |
| --- | --- | --- |
| `POST /v1/ingest/telegraf` | 对应 Agent 的 Bearer 上传 Token | 事务提交后 204，无响应体 |
| `GET /v1/devices/summary` | 面板 Bearer 只读 Token | 200，完整在线设备快照，最多 4096 字节 |
| `GET /healthz` | 无 | 200，进程存活 |
| `GET /readyz` | 无 | SQLite 可读写时 200，否则 503 |

上传只接受 `application/json`、identity 编码与原生 `{"metrics":[...]}` 批量对象，Unix 秒时间戳。最多 256 KiB / 100 条 metric；每条最多 16 个 tag / 64 个 field，名字和 tag 值最多 128 UTF-8 字节。只保留 cpu/mem/disk/system 的数值 field，其他 measurement/非数值 field 跳过；无可用数值返回 422。结构、时间窗口、非有限数值任一错误拒绝整批。样本窗口固定最近 24h 到未来 30s。每设备最多 256 个 measurement+tags 组合，包括已保留的 latest。`host` 不参与身份或组合；其余 tag 按键排序保存。

每 Agent 使用独立 token bucket：每秒补充 1 次额度（每分钟平均 60 次），最大突发 10 次。已认证的非法上传也消耗额度。上传 Token 读取面板、只读 Token 上传均返回 403；缺失/未知 Token 返回 401。超限 413、格式 415、非法 JSON 400、内容 422、限流 429、仓储失败 503。错误体仅返回短错误码。不存在的路由 404，方法错误 405。

数据通过一条 SQLite 连接串行操作，WAL、外键及 5s busy timeout。历史幂等键为设备/measurement/规范 tags/field/采样时间；重复键保留首次值，乱序旧值只补 history，不能覆盖 latest。设备 `last_seen_at` 与数据同事务提交。未知/已删除配置的 Agent 不可上传，历史设备记录保留到既定清理策略。每小时按采样时间分批清理 history，保留 latest。schema 使用 `PRAGMA user_version=1`；未知版本拒绝启动。备份需用 SQLite 在线备份工具或停服一致性备份，不能仅复制运行中的主 db 文件。

面板最多四台，按 `panel.devices` 顺序输出在线设备；在线阈值默认 45s，指标 TTL 默认 45s。CPU 使用 `cpu.usage_active` + `cpu=cpu-total`，MEM 使用 `mem.used_percent`，DISK 使用 `disk.used_percent` + 配置的 path。tag 选择必须唯一；不能用其中一个候选过期来绕过歧义。缺失/过期/歧义返回 `--` / null。数值文本取原值四舍五入的整数百分比，进度限制为 0–100；超过 8 字节的文本返回 `OVR` / null。uptime 必须是非负整数秒，按 `12d 4h` / `3h 2m` / `2m 4s` / `12s` 格式化，最长 8 字符。名称必须为 1–8 个可打印 ASCII 字节。summary 不压缩，返回 `Cache-Control: no-store`，离线设备直接省略。读取失败返回 503，不伪造空列表。

进程接收 SIGINT/SIGTERM 后停止接收请求并等待最多 10s 退出。内置 HTTP 的 read-header/read/write/idle 超时为 5/10/10/60s，单 handler 上下文为 8s，header 上限 16 KiB。服务默认只监听 `127.0.0.1:8080`，HTTPS 在现有 NAS 反向代理终止；反代还应限制上传体大小。日志输出结构化操作结果、设备 id、状态、接受字段数、拒绝批次数、耗时、SQLite/WAL 文件大小和清理行数；不记录 Token 或原始请求。

## NAS 二进制与后续验证

```sh
make build-linux-amd64
shasum -a 256 bin/monitor-server-linux-amd64
```

使用 `CGO_ENABLED=0` 与纯 Go SQLite，运行时不需要 NAS 安装 Go、SQLite 动态库或容器。已使用 Go 1.27.3 完成 NAS 原生编译、Linux 运行和 NPM 公网 HTTPS 链路验收；服务管理器重启保活仍未专项验证。服务以非 root 账户前台运行，不预设 NAS 一定使用 systemd。本机 NAS 实际部署在 `/home/admin/.local/opt/esp32-monitor/`，包含 `config/`、`bin/` 与运行后自动创建的 `data/`；监听 `0.0.0.0:18080`，NPM 通过 NAS 局域网 IP 转发。

macOS 的真实 Telegraf 本地回环已通过，包括上传、503/429 缓冲重试、SQLite/summary 数值核对、真实 Server 进程重启、45 秒离线移除和 Agent 恢复。安装 Telegraf 后运行 `make test-telegraf` 可复现，默认将脱敏证据保存到临时目录，详情见回环记录。真实采集夹具也已纳入 `make test-api` 的兼容性回归。

NAS 回环及远端 Mac 客户端 HTTPS 联调也已通过：76 个真实数值 field 与 NAS SQLite 一致，公网与 NAS 本机 summary 一致，Token 隔离、重复提交、离线和恢复均通过。公网测试使用 Go 1.27.3 临时构建的 Telegraf，未替换系统安装版本。后续接入 ESP32 Devices 并完成设备端 HTTPS 与 UI 联调；服务管理器保活和长期运行另行验证。

## 官方资料

- [Telegraf JSON serializer 批量对象和时间单位](https://github.com/influxdata/telegraf/blob/master/plugins/serializers/json/README.md)
- [Telegraf HTTP output](https://github.com/influxdata/telegraf/blob/master/plugins/outputs/http/README.md)
- [CPU](https://github.com/influxdata/telegraf/blob/master/plugins/inputs/cpu/README.md)、[MEM](https://github.com/influxdata/telegraf/blob/master/plugins/inputs/mem/README.md)、[DISK](https://github.com/influxdata/telegraf/blob/master/plugins/inputs/disk/README.md)、[system](https://github.com/influxdata/telegraf/blob/master/plugins/inputs/system/README.md)
- [modernc SQLite 驱动和 DSN PRAGMA](https://pkg.go.dev/modernc.org/sqlite)
- [YAML Decoder.KnownFields](https://pkg.go.dev/go.yaml.in/yaml/v3#Decoder.KnownFields)
- [Go HTTP Server](https://pkg.go.dev/net/http#Server)、[Token bucket](https://pkg.go.dev/golang.org/x/time/rate)、[交互终端检测](https://pkg.go.dev/golang.org/x/term)、[文件锁](https://pkg.go.dev/github.com/gofrs/flock)
