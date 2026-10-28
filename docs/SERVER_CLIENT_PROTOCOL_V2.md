# Server 与 Client 通信协议（v2）

核对日期：2026-10-28。以当前工作区 Client 实现为准；完整字段类型见 [uploadv2/types.go](../client/internal/collector/uploadv2/types.go)，指标映射见 [v2.go](../client/internal/collector/v2.go)，上传状态机见 [v2_upload.go](../client/internal/collector/v2_upload.go)。

**当前 Client 只支持 v2 指标上传。正式 `server/` 尚未实现 v2，不能直接与当前 Client 注册、上传；`client/cmd/sim-server` 用于本地联调。** 本文把 Client 已执行的行为与正式 Server 必须补齐的要求分开标明。ESP32 读取 Server 的既有摘要接口见第 9 节，ESP32 与 Client 的通信另见 [Companion 协议](ESP32_COMPANION_PROTOCOL_V1.md)。

## 1. 边界、地址与认证

- Client 主动通过 HTTPS 访问 Server，无需开放 Client 的公网端口。仅数字回环地址允许 HTTP；不跳过证书校验，不跟随上传重定向。
- 上传 URL 必须为 `https://<host>/v2/collector/samples`，不能带 userinfo、query、fragment；当前 uploader 不支持部署到路径前缀下。Profile 使用同源 `/v2/collector/profile`。
- Profile、Patch、SampleBatch 使用 UTF-8 JSON、`Content-Type: application/json`；业务请求携带 `Authorization: Bearer <device token>`。设备由 Token 绑定，正文不重复传 `device_id`。
- 注册码、设备上传 Token、ESP32 只读 Token、管理员会话、Companion 配对密钥属于不同凭据。GitHub/Codex 凭据、播放内容和任务正文不进入此指标协议。
- 当前请求超时为 8 秒，响应体与上传正文上限均为 256 KiB。

## 2. 注册与令牌

GUI 在「Server 连接」页输入基地址及 6 位验证码，由 Go 宿主调用注册接口。成功后保存设备身份、上传地址，并将 Token 写入应用私有 `server-token` 文件。WebView 只得到设备身份，不得到 Token。实现见 [registration.go](../client/internal/guibridge/registration.go)。

| 方法与路径 | 请求 / 权限 | 成功响应 |
| --- | --- | --- |
| `POST /api/devices` | 管理面；`{"name":"My Mac"}`，名称 1–64 字符 | `201`，设备信息、`code`、`code_expires_at` |
| `GET /api/devices` | 管理面 | `200`，设备列表，不返回上传 Token |
| `POST /api/devices/{id}/code` | 管理面 | `200`，重新生成验证码 |
| `DELETE /api/devices/{id}` | 管理面 | `204`，删除设备并使其 Token 失效 |
| `POST /v2/collector/register` | 无 Bearer；`{"code":"123456"}` | `200 {"device_id":"…","name":"My Mac","token":"<64 lowercase hex>"}` |

当前模拟器验证码有效期 10 分钟、单次使用，重新生成作废旧码；成功重新注册会轮换设备 Token。错误为 `404 invalid_code`、`410 code_expired`；GUI 对正式旧 Server 缺少注册路由显示不支持。模拟器数据仅在内存，重启后需重新注册。

正式 Server 待实现：管理员认证、CSRF、验证码兑换限流、审计和持久化。模拟器的无认证管理网页仅供回环联调，不能作为生产管理面。

Unix 下应用私有目录为 `0700`、Token 文件为 `0600`；Agent 每次请求重新读取文件并校验，不要求用户手工复制 Token。默认路径由 `os.UserConfigDir()` 下的 `ESP32-Monitor` 目录确定。Windows 原生权限与完整 IPC 仍待验收。

## 3. 本机配置与上传选择

本机配置不是远端报文。当前结构如下；本机 `selections` 使用采集源/measurement/field ID，远端 `upload.optional` 使用稳定语义 ID：

```json
{
  "interval_seconds": 10,
  "selections": [],
  "upload": {"enabled": true, "interval_seconds": 10, "optional": []}
}
```

周期范围为 5–60 秒，默认 10 秒。`upload.interval_seconds` 归一化后也作为采集周期。`upload.enabled=false` 停止样本上传；`optional=[]` 仍上传三项必选值，以及可获取的内存总量和已用量。

选择对象：`{"metric":"drive.read_rate","objects":[3]}`；SMART 细项另带 `detail`。对象字段必须列非零 uint16 ID，空数组不代表所有对象；非对象字段不得带对象。逐核 CPU 例外：开启 `core.usage` 时覆盖全部可获取逻辑核，不支持只上传部分核。必选项不写入 optional；未知、重复、不可用的选择会报错。

Client 按能力展开同组字段：CPU 型号/核数、负载、内存构成、Swap、磁盘信息、磁盘读写、网络连接、网络速率、IP、电池、系统信息。只加入本机可获取的字段；电池电量和充电状态成对。网络速率/IP 依赖同一网卡连接组，GPU 温度依赖同一 GPU 占用率，磁盘读写/SMART 依赖同一磁盘信息组。选择范围最多 256 项，展开后的 `(metric, object, detail)` 最多 1024 项。

当前廉价本地预览会采集未选上传的指标；**上传开关不等于本机完全停止读取**。SMART 与公网 IP 单独按选择启用；公网 IP 能力扫描不发起外部查询。旧 `volume.*` 选择在配置恢复/应用时移除，不推断成物理磁盘选择。分组和依赖以 [v2_config.go](../client/internal/collector/v2_config.go) 为准。

## 4. 设备档案 Profile

```json
{"version":2,"interval_seconds":10,"enabled":[],"inventory":{"memory_total":"17179869184"}}
```

| 字段 | 类型与含义 |
| --- | --- |
| `version` | uint8，固定 2 |
| `interval_seconds` | uint16，5–60 |
| `enabled` | 完整可选上传选择数组；元素为 `metric`、可选 `objects`、可选 `detail` |
| `inventory` | 已选固定信息，以及动态指标所需的最小对象目录 |

Inventory 字段全部可省略：

| 路径 | 内容 |
| --- | --- |
| `os` | `family`（darwin/windows/linux）、`name`、`version`，可选 `build` |
| `product` | 产品名称字符串 |
| `cpu` | 可选 `model`、`physical_cores`、`logical_cores`、`performance_cores`、`efficiency_cores`；核数为 uint16 |
| `memory_total` | uint64 十进制字符串；可获取时隐式同步，无需 enabled 声明 |
| `cores[]` | `id`、逻辑 CPU 序号 `index`、可选 `kind`（performance/efficiency/general） |
| `gpus[]` / `batteries[]` | `id` |
| `drives[]` | `id`，可选 `model`、`total`（uint64 字符串）、`smart[]` |
| `drives[].smart[]` | `id`、`key`、`unit`、`scale10`、`source`；实际值 = 整数文本 × 10^scale10 |
| `interfaces[]` | `id`，可选 `medium`（wifi/ethernet/other） |
| `fans[]` | `id`、`scope`（cpu/gpu/system），可选所属 `gpu` ID |

`volumes` 类型仅为历史状态解码兼容保留，当前 Client 不生成卷档案或卷样本。风扇无法确认归属时标为 system，不猜测 CPU/GPU 归属。

对象 ID 在本机状态目录持久化、非零、不复用；当前按采集源对象键关联（如设备名/接口名），不能承诺任意设备重命名后仍能识别为同一硬件。ID 耗尽时报错；不要用另一设备身份共享状态目录。原始序列号、MAC、接口名等本机对象标签不作为远端通用 tags 上传。

固定信息每次 Manager/Agent 启动读取一次；重扫、预览和配置切换复用启动缓存。OS 升级、容量/拓扑或硬件变动需重启 Agent 更新。已确认档案和 revision 缓存在私有 `collector-v2.json`，同内容重启不重复上传。缓存绑定完整上传地址；切换 Server 不可直接混用旧状态目录。

## 5. 样本、语义 ID 与单位

```json
{"r":12,"id":"0123456789abcdef0123456789abcdef","s":[{"t":1793059200,"c":1250,"m":6200,"u":86400,"mem":{"u":"10651518976"}}]}
```

`r` 是已确认 Profile revision，正整数且不大于 2^53−1；`id` 是随机 128 位数的 32 位小写十六进制文本；`s` 为样本数组。正常一轮一批，当前 Client 不合并积压批次。示例时间为固定夹具，实际上传必须使用当前采集时间。

| 语义 ID | 远端路径 | 编码 |
| --- | --- | --- |
| `cpu.usage` / `memory.usage` | Sample `c` / `m` | 必选 uint16，0–10000，1250 = 12.50% |
| `system.uptime` | `u` | 必选 uint32，秒 |
| — | `t` | 必选 int64，Unix 秒，同轮共用 |
| `cpu.model` | Inventory `cpu.model` | 固定型号 |
| `cpu.physical_cores` / `cpu.logical_cores` | Inventory `cpu.physical_cores/logical_cores` | uint16，物理/逻辑核数 |
| `cpu.performance_cores` / `cpu.efficiency_cores` | Inventory `cpu.performance_cores/efficiency_cores` | uint16，可辨别时的P/E核数 |
| `core.usage` | `cores[{i,u}]` | `i` 对象 ID，`u` 百分比 ×100 |
| `cpu.load1` / `cpu.load5` / `cpu.load15` | `cpu.l1/l5/l15` | uint32，负载 ×1000 |
| `cpu.temperature` | `cpu.t` | int16，摄氏度 ×10 |
| `memory.total` | Inventory `memory_total` | 隐式固定信息，uint64 字符串 |
| `memory.used` | `mem.u` | 隐式可选动态值，uint64 字符串 |
| `memory.app` / `memory.wired` / `memory.compressed` | `mem.a/w/c` | 字节，uint64 字符串 |
| `memory.swap_total` / `memory.swap_used` | `mem.st/su` | 字节，uint64 字符串 |
| `memory.pressure` | `mem.p` | uint8，0 normal / 1 warning / 2 critical |
| `gpu.usage` / `gpu.temperature` | `gpu[{i,u,t}]` | 对象 ID、百分比 ×100、摄氏度 ×10 |
| `drive.model` / `drive.total` | Inventory `drives[].model/total` | 固定信息 |
| `drive.used` | `disk[].u` | 物理磁盘已用字节，uint64 字符串 |
| `drive.read_rate` / `drive.write_rate` | `disk[].r/w` | B/s，uint64 字符串；对象键 `i` |
| `drive.smart_health` | `disk[].sm.{t,h}` | 实际读取 Unix 秒、健康 bool；false 表示已知故障 |
| `drive.smart_attribute` | `disk[].sm.v` | 细项 ID 字符串 → 十进制整数文本，单位由档案声明 |
| `interface.medium` | Inventory `interfaces[].medium` | 固定介质类型 |
| `interface.up` | `net[].up` | bool；对象键 `i` |
| `interface.upload_rate` / `interface.download_rate` | `net[].tx/rx` | B/s，uint64 字符串 |
| `interface.ipv4` | `net[].ip` | IPv4 字符串数组，`[]` 表示确认无地址 |
| `network.public_ipv4` | `ip.{t,v}` | 实际查询 Unix 秒、有效公网 IPv4 |
| `battery.level` / `battery.charging` | `bat[{i,l,c}]` | 百分比 ×100、bool，两值同时存在且非 null |
| `fan.rpm` | `fan[{i,r}]` | uint32 RPM |
| `system.os` / `system.product` | Inventory `os/product` | 固定信息 |

当前 SMART 可选择细项由 Client 的 `smartFields` 定义，不能把类型中可容纳的 128 个细项理解为当前已实现 128 项采集。

值缺失时省略可选字段，不能发送 null 或虚构 0。真实 `0`、`false`、空 IP 列表必须保留。三项必选任一不可获取时整轮不生成上传样本。空组、只有 ID 的对象不发送。百分比/温度/负载编码前取整并检查范围；字节、B/s、SMART 使用精确文本避免浮点精度丢失。

速率依赖两个有效计数器采样，首次无基线、计数器倒退或间隔过长时省略；成对读写/上下行按有效性处理。内存百分比沿用平台 used/total 口径，App/Wired/压缩之和不保证等于 used。磁盘已用量当前仅 macOS 可可靠归属的单物理盘 APFS 容器汇总；共享容器不重复计数，跨盘/混合分区/无法归属时省略，不能报 0。不再采集挂载点容量。

Client入队时仅在SMART、Used、读速率和写速率均缺失时省略磁盘对象；只有`disk[].u`也会上传，真实0字节仍保留。SMART缓存去重不会清掉同轮已用量，相关HTTP回归测试见 [v2_disk_upload_test.go](../client/internal/collector/v2_disk_upload_test.go)。

SMART 和公网 IPv4 默认缓存 5 分钟，携带真实观测时间，同一观测只入队一次；重试原样保留。不用每轮采集时间伪装缓存刷新。

## 6. Profile 同步、条件更新与样本确认

| 方法与路径 | Client 请求 | 预期成功响应 |
| --- | --- | --- |
| `GET /v2/collector/profile` | Bearer | `200` 完整 Profile + `ETag: "12"`；缺失 `404` |
| `PUT /v2/collector/profile` | 首建完整 Profile，`If-None-Match: *` | `200 {"r":12}`，ETag 必须同为 `"12"` |
| `PATCH /v2/collector/profile` | 操作数组，`If-Match: "12"` | `200 {"r":13}` + `ETag: "13"` |
| `POST /v2/collector/samples` | SampleBatch | `204`，整批确认 |

当前 Client 的 GET 不发送条件头；不依赖 304，也不依赖后续全量 PUT 覆盖档案。Patch 为 `add/replace/remove` 操作，路径是受限 JSON Pointer：仅 `/enabled`、`/interval_seconds`、已定义 inventory 字段；不整体替换 inventory 或已有 CPU/OS 组。对象数组按已确认下标修改，新增用 `/-`、删除倒序；叶子字段只发变化值。

```json
[{"op":"replace","path":"/interval_seconds","value":15}]
```

当前执行顺序：

1. Client 校验候选配置/能力/对象，先持久化本机配置，切换采集循环，清空旧配置队列。失败时保留旧循环。
2. 后台先对账 Profile。已确认缓存与目标相同直接复用 revision；否则 GET，对不存在档案用 PUT，对变化档案用 PATCH。正文 revision 与 ETag 必须一致。
3. Profile 确认后才给样本批次赋 revision、发送样本；没有 Profile 确认不发该配置样本。
4. 网络失败/条件冲突等待退避，下一次对账重算；不能将本机配置保存成功显示成远端已生效。
5. Sample 返回 `409 profile_required/profile_outdated` 时清除已确认档案缓存，对账后用新 ID 重新提交该样本。普通重试保持 ID、正文与时间戳不变。
6. 停用上传时排队同步空 enabled / inventory 档案；离线时远端旧投影不能立即清除。仅档案同步成功不更新 `last_upload_at`。

状态机没有把内存中的样本队列持久化；Agent 退出可能丢失待发样本。崩溃后可从保存的配置/档案重新对账，不能宣称有持久化 outbox 或无损补传。

## 7. 错误、队列与正式 Server 要求

| 结果 | 当前 Client 行为 |
| --- | --- |
| 2xx | 弹出队列；有样本时更新最近上传时间 |
| 401/403 | `upload_auth_required`，暂停，需修复凭据/重新应用 |
| 网络失败、429、5xx | 指数退避并加抖动；429 遵守 Retry-After，最长采用 24 小时 |
| Profile 412 | 按可重试失败处理，下次 GET 重算差异 |
| Sample 409 档案缺失/过期 | 重新对账 Profile，生成新批次 ID |
| Sample 422 | `upload_invalid_sample_or_clock`，丢弃该批，不自动改系统时间 |
| 其他不可重试状态 | `upload_rejected_<status>`，丢弃并计数 |

内存 FIFO 队列同时受 5 分钟、1000 轮样本、256 KiB 序列化样本总量限制，超过丢最旧并计数。当前 422 仅给出通用错误，尚无完整的时钟诊断/授权校时闭环。

以下为正式 Server 的实现/验收要求，**不是当前生产 Server 已具备的能力**：

- Profile、revision、batch 去重及样本原子持久化到 SQLite；revision 设备内递增，无语义变化不增版。PATCH 最多 2048 操作，重建后的 Profile 也限 256 KiB；每类对象最多 256、每盘 SMART 最多 128、展开选择最多 1024。
- 批次 1–30 轮，先校验全部再提交。验证已选字段、对象、细项与范围，隐式允许 memory.total/used；拒绝未知字段、重复 key/ID、非法 null/整数/IP，不部分接收。
- 以 `(device_id,batch_id)` 保存摘要及成功结果至少 24 小时。同 ID 同正文返回 204，不重复写历史/刷新在线；同 ID 不同正文为 `409 batch_id_conflict`。去重应先于 active revision 检查。
- 样本时间窗口 `server_now−86400 ≤ t ≤ server_now+30`；未来/过旧分别报告 `timestamp_in_future/timestamp_too_old` 并提供 Server 时间。慢观测不晚于样本时间，有效窗口 15 分钟。
- 旧 revision 返回 409；未启用字段返回 422。最新值只由更新采样时间替换；相同时间冲突需明确拒绝。历史记录按原 revision 解释。
- 停用字段/删除对象在 Profile 提交事务中立即退出有效投影；仍启用值保留原时间和时效。新样本只有足够新（最近 `max(3×interval,30秒)`）才续在线，旧补传、重复批次、档案读写不续在线。
- 管理认证、限流、审计、备份与重启恢复须单独验收；不要将模拟器行为等同完整严格校验或生产持久化。

## 8. 本地联调与验证入口

从仓库根目录启动模拟 Server：

```sh
cd client
go run ./cmd/sim-server -listen 127.0.0.1:18080
```

浏览器打开模拟器根页面，添加设备取码，再在 Client「Server 连接」页填写 `http://127.0.0.1:18080` 与验证码。示例基地址无路径前缀。`-token` 固定凭据模式用于测试，不提供注册设备管理。

相关测试：[v2_test.go](../client/internal/collector/v2_test.go)、[registration_test.go](../client/internal/guibridge/registration_test.go)、[simserver 测试](../client/internal/transport/simserver/server_test.go)、[注册码测试](../client/internal/transport/simserver/register_test.go)。端到端回归可运行 `cd client && make test-collector-e2e`，使用 `cmd/sim-server` 验证注册、Profile 创建/PATCH、样本上传、重启复用档案且不访问 v1 入口。测试覆盖边界不等于真实 Server/三平台 GUI 验收。

## 9. 既有 Server 与 ESP32 摘要兼容边界

正式 Server 当前路由见 [api.go](../server/internal/api/api.go)：

| 路径 | 当前实现 |
| --- | --- |
| `POST /v1/ingest/telegraf` | 既有外部 Telegraf JSON 上报，独立写 Token；当前 Client 已不支持此上传入口 |
| `GET /v1/devices/summary` | ESP32/Companion 使用只读 Token 获取摘要 |
| `GET /healthz` / `GET /readyz` | 存活 / SQLite 就绪探针 |

摘要使用 JSON，最多四台设备、4096 字节；`device_count` 与 `devices` 长度一致，每台为 `name`、`uptime`、`metric_count:3`、按 CPU/MEM/DISK 排列的 `metrics`。每项含 `name`、`value`、可空 `percent`。名称/显示文本最多 8 字节；缺值为 `--`/null，离线设备省略。默认 online timeout 与 metric TTL 均为 45 秒。

投影先过滤时效，再选择采样时间最新的匹配序列；并列最新仍有歧义则显示缺失。不能沿用旧文档“先判重复再检查时效”的规则。实现见 [panel.go](../server/internal/panel/panel.go)。

正式 v2 必须补齐到该摘要的投影并保持其精简结构；磁盘已改成物理盘口径，不能继续静默用旧挂载点配置映射，需新增明确选择和迁移。未实现前，不承诺当前 Client → 正式 Server → ESP32 全链路可用。
