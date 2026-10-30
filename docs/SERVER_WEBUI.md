# Server V1 与 WebUI

Server 的产品版本是 V1；与 Client 的通信协议版本是 v2，二者独立。Server 以 Client 实际上传协议为准，提供 v2 指标协议、Cookie 管理面和 SQLite WAL 持久化。旧 `/v1/ingest/telegraf`、`/v1/devices/summary` 均返回 404。

## 启动

```sh
cd server
make build
./bin/monitor-server configure --config ./config/monitor.yaml
./bin/monitor-server serve --config ./config/monitor.yaml
```

也可以直接 `serve`：配置不存在时自动创建 `0600` 配置和随机只读 Token。新配置默认监听 `0.0.0.0:8080`，允许本机和局域网通过 IP 地址直接访问；已有配置不会自动覆盖。公网部署前将 `public_base_url` 改成自己的 HTTPS 域名；`configure` 可编辑监听地址、SQLite 路径、公网地址，查看或轮换只读 Token。设备在 WebUI 注册。

根页面 `/` 提供设备注册、指标查看和改密页。视觉复用 Client 黑白灰像素样式、FusionPixel/Unscii 字体及 Pixelarticons，全部嵌入 Go 二进制。CPU 型号/核心数、内存总量、磁盘型号/容量显示在对应指标卡片；磁盘按 Profile 对象 ID 关联型号和观测，有型号时用型号作标题。缺少名称时提示未上传，不虚构 Client 没有上传的硬件信息。网卡、GPU、电池和核心同样通过 `inventory.objects` 的 ID 对照表优先显示名称/型号，编号仅作辅助。旧版 Client 没有目录时保留介质类型等降级显示。

## HTTP 后端与 HTTPS 反代

```yaml
listen: "127.0.0.1:8080"
public_base_url: "https://monitor.example.com"
database: "./data/monitor.db"
read_token: "<configure 生成的 64 位小写十六进制随机值>"
history_retention_hours: 24
```

在已有 Nginx HTTPS 虚拟主机中配置：

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $http_host;
    client_max_body_size 256k;
}
```

Server 根据配置的公网地址与保留的 Host 判断外部 HTTPS，校验 Origin 并设置 Secure Cookie；不直接信任 `X-Forwarded-Proto/For`。反代必须保留原始 Host，从域名根路径转发，不加路径前缀。仅将反代 HTTPS 端口暴露公网，认证响应不缓存。

本机和局域网可通过 `http://<服务器局域网IP>:8080` 直接访问；`public_base_url` 用于指定公网反向代理域名。默认允许本机或私网来源使用回环、私网及链路本地 IP 字面地址，其他域名仍须与 `public_base_url` 一致；不信任转发头。Docker 桥接模式下可发布 `8080:8080`，通过宿主机局域网 IP 访问，容器内通常只能看到桥接网络地址。HTTP 不加密验证码、令牌、密码和指标，建议使用 HTTPS。未来容器化时需持久化配置和 SQLite 目录；stderr 输出自然进入 Docker 日志。

## 密码与 Cookie

- 无用户名。首次没有密码时生成随机 128 位临时密码，向 stderr 打印一次；SQLite 仅存随机 salt 和 Argon2id 哈希（64 MiB、3 次、4 并行度、32 字节输出）。
- 首次登录后，后端只允许状态、改密和退出；修改临时密码才能管理设备。新密码至少 12 字符、最多 256 UTF-8 字节，不含控制字符，不能与旧密码相同；普通改密也验证当前密码。
- 参考 [SnowLuma 认证流程](https://github.com/SnowLuma/SnowLuma/blob/main/packages/core/src/webui/auth.ts)：上次未完成首次改密时，重启重新生成临时密码并撤销旧会话；改密完成后重启不再打印密码。已存凭据损坏则启动失败，保留数据库。
- Cookie 为随机 256 位会话，SQLite 只存其 SHA-256 验证值；最长 7 天、闲置 24 小时后失效，最多 16 个会话。刷新和 Server 重启保留有效登录；退出撤销当前会话，改密撤销全部旧会话并轮换当前 Cookie。
- Cookie 带 HttpOnly、SameSite=Strict、Path=/。公网使用 Secure 与 `__Host-monitor_session`，直接数字回环 HTTP 使用独立 `monitor_session`。前端仅在内存保存 CSRF Token，不把凭据存入 localStorage/sessionStorage。
- 所有管理写请求同时校验 Origin 和会话绑定的 `X-CSRF-Token`；登录也校验 Origin。Host 白名单阻止 DNS rebinding；CSP、禁止嵌入/嗅探/缓存和公网 HSTS 加固浏览器响应。
- 登录/改密：全局突发 10 次，之后每 6 秒一次；来源突发 5 次，之后每 12 秒一次。注册码兑换：全局及来源突发 5 次，每 12 秒恢复一次。429 带 Retry-After。同一反代后的用户共享来源限流，不信任任意 X-Forwarded-For。
- 目前无在线忘记密码入口；需恢复受控的一致性备份，勿删除整个数据库重置密码。

认证参考 [OWASP 会话管理](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)、[密码存储](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html) 和 [Go Argon2](https://pkg.go.dev/golang.org/x/crypto/argon2)。实现测试不等同于第三方渗透测试。

## 注册、指标与存储

1. WebUI 添加名称，生成有效 10 分钟、单次使用的六位验证码；Client 的“Server 连接”页填写基地址与验证码，兑换专属上传 Token。
2. Client 首次连接时先同步包含 `inventory.objects` 的 Profile，即使尚未启用样本上传或没有采样配置，也上传全部启动时可识别对象的 `{id,kind,name,model?}`。该目录随固定信息同步、名称变化使用 PATCH，样本不重复携带名称；关闭样本时的唯一当前档案任务在长时间离线后仍会退避重试；停用样本时保留目录。动态指标仍遵守用户选择。重新发码使旧码失效；新码兑换成功时才轮换原上传 Token。设备 Token 不在 WebUI 列表和指标接口返回，数据库仅保存其验证哈希。
3. 指标每 5 秒顺序轮询，隐藏标签页暂停；最近 100 轮图表从最近 100 批中读取。缺失/过期观测显示 `—`，真实 0、false 和空地址数组保留。固定信息与动态值按对象 ID 合并到对应卡片；未选择动态指标的对象不显示；已选择动态指标的对象即使暂无有效观测，仍显示名称/型号和占位。
4. 面板最多 4 台设备，名称为 1–8 个非空格可打印 ASCII 字符。DISK 显式选择 Profile 的物理磁盘 ID，根据该盘总容量/已用量投影，不沿用挂载点推断。
5. 删除设备同时删除档案、历史与凭据，WebUI 有确认对话框。

单批 256 KiB、1–30 轮；PATCH 最多 2048 操作，重建档案也限 256 KiB。拒绝未知字段、重复 key/对象 ID、null、非法整数/IP。兼容 Client 用 replace 补入缺失的 inventory 标量，以及逐字段删除 CPU/OS 后移除空组的行为。

Profile/revision、原始批次、去重哈希和有效值在 SQLite WAL 中原子提交。相同批次 ID/相同原始正文重试返回 204，不重复写入或续在线，且先于 active revision 校验；同 ID 不同正文为 `409 batch_id_conflict`，同字段同时间戳不同值为 `409 sample_time_conflict`。任一轮失败整批回滚；停用字段立即退出最新投影，历史仍按原 revision 解释。

在线和普通观测时效取 `max(3×interval_seconds,30 秒)`，SMART/公网 IP 使用真实观测时间、最多 15 分钟有效期。旧补传、去重和档案操作不续在线。历史/幂等验证至少保留 24 小时，配置更长时沿用更长时间；历史页面遵守配置期限。每分钟分 1000 行批次清理并释放连接，过期会话同时清理，审计保留 30 天。

旧 YAML agents、panel.devices、online_timeout_seconds、metric_ttl_seconds 保留解码用于迁移，v2 设备身份和时效不使用这些字段。旧设备设置启动时警告；configure 保存时清除。旧 SQLite 表保留，v2 独立建表并升级 schema 到 2。正常重启保留 Profile 和对象对照表；如果已认证设备的 Profile 缺失，样本接口返回 409 `profile_required`，Client 重新发送完整 Profile（含静态信息与对照表）后重试样本。若设备凭据也已丢失，则需重新注册。Client 的档案确认缓存同时绑定 Server 地址与凭据，重新注册不会复用旧设备的确认。Windows 复用 WMI GPU 型号、网卡连接名和适配器名称；macOS 复用 GPU 型号和接口名。不上传硬件序列号、MAC 或原始 PNP 标识。Server 不自动采集本机指标；NAS 本机需要运行并注册 Client Agent。

## 路由

| 接口 | 权限 / 内容 |
| --- | --- |
| `POST /api/auth/login` | `{password}`；设置 Cookie，返回 `must_change_password,csrf_token` |
| `GET /api/auth/state` | Cookie；会话状态与 CSRF Token |
| `POST /api/auth/password` | Cookie + CSRF + Origin；`{current_password,new_password}` |
| `POST /api/auth/logout` | Cookie + CSRF + Origin；撤销会话 |
| `GET/POST /api/devices` | 管理员；列表 / `{name}` 创建并取码 |
| `POST /api/devices/{id}/code` / `DELETE /api/devices/{id}` | 管理员；重新发码 / 删除 |
| `GET /api/devices/{id}/profile` | 管理员；档案与 revision |
| `GET /api/devices/{id}/metrics` | 管理员；当前有效值、观测时间与档案 |
| `GET /api/devices/{id}/history` | 管理员；最多 100 个历史批次及原 revision |
| `PUT /api/devices/{id}/panel` | 管理员；`{enabled,name,disk_id}` |
| `POST /v2/collector/register` | `{code}`；返回 `{device_id,name,token}` |
| `GET/PUT/PATCH /v2/collector/profile` | 设备 Bearer；Client 条件更新、revision/ETag |
| `POST /v2/collector/samples` | 设备 Bearer；整批成功 204 |
| `GET /v2/devices/summary` | 独立只读 Bearer；最多 4 台在线设备、CPU/MEM/DISK |
| `GET /healthz` / `GET /readyz` | 存活 / SQLite 可读并能获得写锁 |

管理 API 时间字段为 Unix 秒。上传细节见 [通信协议](SERVER_CLIENT_PROTOCOL_V2.md)。

**现有固件 monitor_devices.c 与 Client Companion 的 proxy.go 仍读取 `/v1/devices/summary`，本次没有修改这些读取方。** 接通摘要链路需要另行迁移到 `/v2/devices/summary`；当前真实联调验证的是 Client Agent 到正式 Server 的 v2 注册、档案、采样链路。

## 备份与验收

运行中的 SQLite 使用在线备份，不能只复制主 `.db` 文件：

```sh
sqlite3 ./data/monitor.db ".backup '/absolute/backup/monitor.db'"
```

配置另行备份；目录及文件按敏感数据管理。恢复前停止 Server，保留原目录用于回滚，再恢复一致的数据库与配置。恢复旧备份可能恢复当时的凭据和会话。

```sh
cd server
make test-ut
make test-race
make vet
make test-api
make test-v2       # 真正 Client Agent + Server，临时状态和回环 HTTP
make test-tui      # 安装 tests/e2e/requirements-tui.txt 后运行
make build-linux-amd64
# 本机安装 Playwright / Chrome：
NODE_PATH=/path/to/node_modules node tests/e2e/webui_browser.cjs bin/monitor-server ../artifacts/server-webui
```

测试覆盖认证/CSRF/限流/会话恢复、反代 Cookie 属性、注册与撤销、原子回滚、并发幂等、Client PATCH/停用/重启、保留期，以及桌面与 390px 浏览器布局。浏览器截图用合成指标夹具；Agent 联调使用真实本机采集。尚未验收真实公网证书/Nginx 部署、第三方渗透或固件摘要全链路。
