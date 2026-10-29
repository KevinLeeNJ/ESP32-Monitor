# ESP32 与上位机通信协议

核对日期：2026-10-28。业务协议 `protocol_version=1`；二进制线编码 `wire_version=2`。本文依据当前 Client 的 [transport](../client/internal/transport/backend.go) 实现整理。**Client 与模拟 Monitor 已有实现；真实 ESP32 Companion 接收器、配置同步、模式管理和第二页数据接入仍待实现。** 固件计划见 [固件计划能力表](FIRMWARE_CAPABILITIES_PLAN.md)。

## 1. 编码、版本与边界

所有 UDP、HTTP 配对、MQTT/LWT、串口业务载荷均使用 Protobuf `Envelope`；字段号、required/optional 和 oneof 的唯一机器契约是 [companion.proto](../client/internal/transport/wire/companion.proto)。不要按 Go DTO 的 JSON tag 编写网络接收器。Client 本地 IPC/vault、上游天气及 Server 摘要仍各用原有 JSON，不受本协议的编码迁移影响。

- `Envelope.wire_version` 必须为 2，且恰有一个 payload；内部业务消息版本仍为 1。串口帧头版本同为 `0x02`；不自动回退 JSON 或旧帧。
- 缺必填、重复单值、冲突 oneof、错误 wire type、截断、非法 UTF-8、超限拒收；未知字段可忽略。optional 缺席表示未知，存在的 0/false 是有效值。
- 身份类 `host_id/device_id/pairing_id/session_id/request_id/config_epoch` 使用小写 UUID 文本；序号非零 uint32、会话内各方向递增，不回绕。重放不续应用看门狗。
- `sent_at/observed_at/last_success_at/resets_at` 为 Unix 秒；`utc_unix_ms` 为 Unix 毫秒；`age_ms`、TTL、超时和剩余有效期为毫秒。寿命判断采用本地单调时钟，不因墙钟回拨复活旧数据。
- 随机 nonce、pairing ticket、device token、serial key 是 32 字节随机数的无 padding Base64URL 文本。它们不是 Server 的 64 位 hex 上传 Token。
- 只向已配对的 Monitor 发送只读快照。没有远程音乐控制、任意 URL 代理或 LAN 管理 API；发现/配对/撤销由本机 GUI/Agent 管理。

## 2. 拓扑与资源上限

| 项目 | 当前约定 |
| --- | --- |
| UDP | Monitor 在 `18444/udp` 接收发现；Client 从所选 IPv4 网卡发起广播或单播 |
| 首次 HTTP | Client 临时监听所选网卡，默认 `18080/tcp`；仅开放配对窗口 |
| MQTT | Client 内置 Broker，默认 `18883/tcp`，Monitor 主动连接 |
| HTTP 编码 | 请求/响应 `application/x-protobuf`，无压缩或 identity；`Cache-Control: no-store` |
| UDP 载荷 | 最多 1024 字节 |
| HTTP 配对 body / Config 明文 | 最多 4096 字节；HTTP header 也受 4096 字节限制 |
| MQTT 应用载荷 | 最多 16384 字节，Broker 最大 packet 18432 字节 |
| 配对记录 / 活跃会话 | 最多 8 / 4；候选最多 32 |
| 会话入队 / MQTT in-flight | 有界入队 4，in-flight 1，禁止无界积压 |
| 文本 | 设备名/固件版本等按实现限制 UTF-8 字节；歌曲标题 160、歌手 96、来源 64 |

修改网卡或 DHCP 地址时保留 Host/配对身份，结束旧 LAN session，以新地址重新发认证邀请；不复用旧 session。候选通常 10 秒过期；一次人工发现 5 秒，操作间隔至少 5 秒。

## 3. 发现、配对与凭据

发现方向是 **Client 查找 Monitor，再邀请 Monitor 主动连接 Client**。不让固件凭历史 IP 自动恢复旧 session。

1. Client 发 `Discovery(type=monitor_discover)`，包含版本、request_id、host_id、nonce。首次未配对消息不含 MAC；只接受处于配对窗口的 Monitor 响应。
2. Monitor 返回 `monitor_announce`，回显请求标识/nonce/host_id，增加 device_id、response_nonce、device_name、firmware_version、pairing_open。已配对响应还带 pairing_id 和 MAC。
3. Client 只向用户选中的设备发送 `monitor_connect_offer`。首次邀请带 HTTP host_port；已配对邀请带 mqtt_port、全新 session_id 和 MAC。
4. 首次配对必须确认可信 LAN 风险：HTTP/MQTT 无 TLS，局域网可见秘密和内容。临时 HTTP 只允许所选设备 IP，窗口 120 秒。窗口外关闭监听。
5. ESP32 发 `PairRequest(step=begin)`，Client 返回 `201 PairResponse(state=pending)`。双方显示同一 6 位核对码并人工确认；`status` 最快每秒一次（202），设备 `confirm` 带 device_confirmed，Client 本地确认通过后才 approved。
6. approved 响应下发独立 `device_token` 和 `serial_key`，同时保存 pairing_id；拒绝、超时、源地址不匹配均不能取得凭据。

临时路由为 `GET /companion/v1/capabilities`、`POST /companion/v1/pair`。PairResponse 含 host_id、host_name、host_nonce、pairing_ticket、expires_in_ms；后续 status/confirm 必须带正确 pairing_id/ticket。begin 限每 IP 5 次/分钟、全局 10 次/分钟。

核对码计算：SHA256(`"ESP32MON-PAIR-V1" || raw(device_nonce) || raw(host_nonce) || UTF8(device_id+host_id+pairing_id)`)，取摘要前 4 字节大端整数模 1000000，补零至 6 位。此码用于用户核对，不代替持久化高熵凭据。

已配对发现 MAC 的跨语言定义见 [DiscoveryMAC](../client/internal/transport/discovery.go)：

- `lp(s) = uint16_le(UTF8 字节长度) || UTF8(s)`；缺席的 optional 字段为空串，端口为十进制文本，bool 为 `1/0`。
- salt = SHA256(`lp(host_id)||lp(device_id)||lp(pairing_id)`)。
- 密钥 = HKDF-SHA256(raw(serial_key), salt, info=`ESP32MON-DISCOVERY-V1`, 32字节)。
- HMAC-SHA256 输入按顺序对下列每项 lp：域 `ESP32MON-DISCOVERY-V1`、type、protocol_version、request_id、host_id、device_id、pairing_id、nonce、response_nonce、host_port、mqtt_port、session_id、device_name、firmware_version、pairing_open；输出小写 hex。

Client 将配对记录保存到专用 `companion.json` 凭据库，公开设备状态不带秘密；Unix 目录/文件 `0700/0600`，Windows 有专门 DACL 实现。ESP32 的受限 NVS 存储与撤销清理待固件实现。串口首次配对沿用同一双确认流程，但须用户确认物理连接，不能向所有串口自动发送凭据。

## 4. LAN MQTT 会话

使用 MQTT 3.1.1（ProtocolLevel=4）、CleanSession=true、KeepAlive=60 秒、无 TLS。ClientID 固定 `emon-<device_id>`，username 为 `<device_id>:<session_id>`，password 为配对 `device_token`。只允许与当前有效邀请匹配的设备；同一会话重复接管拒绝。

主题前缀 `P = emon/v1/<host_id>/<device_id>/<session_id>`；所有消息 retain=false，只用 QoS 0/1：

| 方向 | 主题后缀 | QoS | 载荷 |
| --- | --- | --- | --- |
| Monitor → Client | `up/open` | 1 | Control.data 内嵌 Envelope(Open) |
| Monitor → Client | `up/proxy-config` | 1 | Control.data 内嵌 Envelope(Config) |
| Monitor → Client | `up/ack` | 0 | Control.data 内嵌 Envelope(Ack) |
| Monitor → Client | `up/resync` / `up/goodbye` / `up/error` | 1 | Control.data 内嵌相应 Envelope |
| Monitor → Client | `up/presence` | 1 | Envelope(Presence)，LWT 同型 |
| Client → Monitor | `down/session` / `down/config-ack` / `down/goodbye` / `down/error` | 1 | Envelope(Control)，data 内嵌具体 Envelope |
| Client → Monitor | `down/snapshot` | 1 | Envelope(Snapshot) |
| Client → Monitor | `down/heartbeat` | 0 | Envelope(Heartbeat) |

设备只可订阅自身 `P/down/#` 或精确合法 down 主题，只可发布自身 up 主题。LWT 必须为 `P/up/presence`、QoS1、不 retain，Presence.state=`offline`、reason=`transport_lost`，身份/session 必须一致；在线 Presence 为 online/connected。

入场顺序：认证 CONNECT → 订阅完整 down 集合 → 发 open → 收 Session(config_required=true) → 上传 Config → 收 applied ConfigAck → 完整 Snapshot 被应用并 ACK → 至少间隔 1 秒后的后续应用 ACK → connected。MQTT PUBACK/PINGRESP 或 Presence 不能代替业务快照确认。

Control 带 protocol_version、host/device/session、seq、sent_at、可选 request_id、data。request_id 对配置/open 事务必须一致；同 ID 同内容可重发响应，内容不同拒绝。事务缓存最多 64 项。

稳态完整快照按语义变化推送，最短间隔 2 秒。空闲心跳 30 秒；music section 健康或 tasks section 健康时为 10 秒（不单按 playing/running 判断）。初始入场等候阶段可用 1 秒心跳。90 秒没有有效应用回执结束 LAN 会话，传输关闭可提前结束。收到 `Resync`（missing_snapshot/source_mismatch/parse_failed）重发完整快照，同会话最短 10 秒一次。

## 5. 配置所有权与代理

**配置由 ESP32 持有并发给 Client。** Client 的代理 worker 只在活动会话内持有天气/Devices 配置，不写入通用 GUI 设置、缓存或诊断；断开/替换配置取消旧 worker。不要反向设计为 Client 向固件下发账号秘密。

Config 带 device/session/request_id、`config_epoch` UUID、非零 `config_revision`，以及完整三组配置：

| 组 | 字段与约束 |
| --- | --- |
| weather | enabled、location_mode（coordinates/ip）；启用时 qweather_host 为合法 DNS、qweather_key 非空且最多127字节；坐标模式含 latitude/longitude，IP 模式不带坐标 |
| devices | enabled；启用时 devices_api_url、devices_token；URL 最多192字节，禁止 userinfo/query/fragment，Token 最多128字节且字符受限 |
| time | enabled 必须 true，utc_offset_minutes 在 −720～840 |

关闭的 weather/devices 组不带其可选秘密/坐标字段。新 epoch 可重新开始 revision；同 epoch 旧 revision 或同 revision 异内容为 config_conflict。先完整校验，再建立候选 worker 并替换，失败保留原配置；applied ACK 回显 epoch/revision 与 weather_configured/devices_configured。ACK 只证明配置应用，不证明上游请求已成功。

天气/AQI 启动后及每 15 分钟获取；devices 每 30 秒获取（**不能为省电降低该频率**）。Devices URL 是基地址，Client 追加 `/v1/devices/summary`，只用只读 Token。IP 定位通过 ipwho.is；请求有超时、体积限制与重定向限制。生产代理校验 DNS 解析结果，私网地址需要精确授权；测试注入例外不等于 GUI 已完成授权流程。

时间快照来自本机 system_clock，当前 `trusted=false`，不能宣称 Client 已验证上位机时钟准确。板载电池和温湿度始终由固件本地采集，不被上位机指标覆盖。

## 6. 快照、数据源状态与新鲜度

Snapshot 包含版本、host/device/session、seq、sent_at、config_epoch/revision、完整 `sections`：github、codex、music、proxy。每个 section 为 health 元数据与可选 data；完整类型以 proto 为准。Heartbeat 带 last_snapshot_seq、配置版本、可选时间和 source_updates，不是部分数据快照。

| 分区 | data 结构与裁剪 |
| --- | --- |
| github | login、start_date、days（1–371）、连续日期的 levels（字符0–4）、可选 today_count/total_count；不传逐日完整 GraphQL 对象 |
| codex.quota | windows（最多4个）、truncated；每窗口 bucket_id、可选 window_minutes/used_percent/resets_at；百分比为已用量 |
| codex.tasks | adapter、scope、state、可选 active_count/selected_task/last_completed_at；任务 ID 用 SHA256 前16字节 hex 脱敏，不传任务正文/标题 |
| music | adapter、coverage、可选 current；current 为 playing/not_playing，以及可选 title/artist/source/duration/position/position_observed_at |
| proxy | config_epoch/revision、time、weather、aqi、devices 子分区，各有独立健康/时效 |

task state：unknown、idle、running、waiting_input、waiting_approval、completed、failed、cancelled。终态的短期展示不能永久当实时状态；桌面任务适配器在终态超过60秒后改为 `idle` 上报，若上游仍报终态，投影在60秒后降为 `unknown` 并移除 `selected_task`。`last_completed_at` 仅在选中的任务处于 `completed` 时携带。

播放业务只区分 playing/not_playing。健康暂停允许保留歌曲信息；播放器退出/无当前项时 current 缺席。Provider 错误应显示健康异常，不能伪装为正常 not_playing。播放位置可按观测点在本地插值，不为进度每秒重传整个快照。

health.state：disabled、starting、healthy、degraded、auth_required、permission_denied、unsupported、error；含 observed_at、last_success_at、error_code、source_version、age_ms、stale_after_ms、expire_after_ms。超 stale 可降为 degraded；超 expire 移除数据，不能把心跳当成上游刷新。

| 来源 | stale / expire |
| --- | --- |
| GitHub | 24小时 / 7天 |
| Codex quota | 180秒 / 300秒 |
| Codex tasks、music | 15秒 / 15秒 |
| weather、AQI | 30分钟 / 2小时 |
| devices | 45秒 / 90秒 |
| time | LAN 45秒、Serial 15秒 / 60秒 |

source_updates 只在来源成功观测推进时续租，包含 path、snapshot_seq、真实观测/成功时间和 age；仅匹配已有完整快照时才能更新来源寿命。当前串口心跳不带此续租数组。任一来源不可用只影响自身卡片，不触发整个设备回退；传输/session 不可用才触发模式回退。

## 7. 串口帧、认证与配置加密

串口必须由用户选端口并主动 Probe/Connect。USB 插入或供电不是完成握手；帧解码须能与启动日志/噪声共存。物理串口和 ESP32 互操作尚待实测，不能把内存字节流测试当作硬件验收。

| 偏移 | 长度 | 内容 |
| --- | --- | --- |
| 0 | 4 | ASCII `EMON` |
| 4 | 1 | 帧版本 `0x02` |
| 5 | 1 | 消息类型 |
| 6 | 2 | payload_len，小端，含认证 tag |
| 8 | 4 | seq，小端，非零 |
| 12 | N | Protobuf Envelope；认证后尾随32字节 HMAC |
| 12+N | 4 | CRC32 IEEE，小端；覆盖从版本字节到 payload 末尾，不含 magic/CRC |

总帧 `16+N` 字节，`N≤16368`；认证后业务 Envelope 最多16336字节；认证前载荷最多2048字节。坏版本/类型/长度/CRC 从 magic+1 重扫；半帧超过2秒丢弃候选，保留后续帧恢复能力。

类型：`01 HELLO`、`02 AUTH`、`03 PAIR`、`10 CONFIG`、`11 CONFIG_ACK`、`20 SNAPSHOT`、`21 HEARTBEAT`、`22 ACK`、`7e GOODBYE`、`7f ERROR`（均为十六进制）。每个帧类型与 Envelope payload 必须匹配。

握手：Host HELLO(request) → Monitor HELLO(response、身份、nonce、supported_versions、max_payload_bytes=16368) → 必要时首次 PAIR → Host AUTH(challenge，新 session) → Monitor AUTH(response,device proof) → Host AUTH(result,host proof,accepted) → Monitor 用新帧密钥 ACK → Config 同步。HELLO/AUTH 阶段未认证帧不等于可信业务；串口只有认证成功才可接管旧 LAN 会话。

串口密码学定义见 [serial_codec.go](../client/internal/transport/serial_codec.go)：

1. transcript T = `lp("ESP32MON-SERIAL-V1") || lp(host_id) || lp(device_id) || lp(pairing_id) || lp(session_id) || raw(host_nonce) || raw(device_nonce)`。
2. Device/Host proof = HMAC-SHA256(raw(serial_key), ASCII(`device`或`host`) || T)，小写 hex。salt=SHA256(T)，HKDF-SHA256 的 info 分别为 `frame`、`proxy-config`，各派生32字节密钥。
3. 帧 HMAC = HMAC-SHA256(frame_key, direction_byte || 完整12字节帧头 || Envelope字节)；direction=1 为 Host→Monitor，2 为 Monitor→Host。CRC 验证后仍须验证 HMAC。
4. CONFIG 使用 Envelope(EncryptedConfig)：algorithm=`aes-256-gcm`、nonce/ciphertext/tag 为 bytes。加密明文是 Envelope(Config)，≤4096字节。
5. nonce 为12字节，前8字节零、后4字节是发送帧 seq 的大端值；AAD=`lp("ESP32MON-CONFIG-V1")||lp(session_id)||lp(request_id)`；tag16字节。解密后内外 request/session 必须一致。每会话新 nonce/密钥，序号不能回绕复用。

Serial 每1秒双向心跳，3秒无有效应用证明结束。入场要求配置 applied、完整快照确认以及两次有效设备心跳；后续数据重放不续寿命。拔线/流关闭结束 worker、会话及内存代理，真实固件恢复步骤见下一节。

## 8. 固件模式契约与待实现部分

| 模式 | 页面/网络 | 数据与电源要求 |
| --- | --- | --- |
| standalone | 第一页；本机 Wi-Fi | 现有 RTC/传感器与天气/Devices 直连，自动 Light Sleep、空闲 LPM |
| lan | 两页；Wi-Fi + MQTT | 第一页天气/Devices/时间由上位机代理；保留省电关联和自动 Light Sleep，按需 HPM |
| serial | 两页；正常运行关闭 Wi-Fi/AP/DNS | 代理同上；禁止自动 Light Sleep、常驻 HPM，可显示秒与局部动画 |

每次启动先 standalone，不凭历史模式或插线自动进入 Companion。保存配对身份和显式启用意图；运行态维护 effective_mode、connection_state、fallback_reason、active_host_id、session、配置版本。一次只接一个活动上位机/传输，认证 Serial 优先。

入场需同一身份/session、配置与快照和应用存活证明齐备。回退：冻结旧写入 → 取消旧 session → 第一页/隐藏 Companion 入口 → 从 Serial 恢复 Wi-Fi 和直连生产者 → 完成显示后转 LPM、释放禁止睡眠锁。RTC/本地传感器不重置，Wi-Fi 恢复不等于联网成功。回退后至少稳定10秒再自动入场；数据源个别故障不触发回退。

上述为固件后续实现要求；当前 `main.c` 创建的是 activity 演示页，不具备完整模式管理。LAN 与 standalone 共用低功耗策略不意味着功耗相同；USB 断开后的电池接口测量、长时运行、LAN配置响应与真实屏幕观感须单独验收。

## 9. 错误与验证入口

公共 Error 为 protocol_version、可选 request_id 与 `{code,message,retryable,retry_after_ms?}`；不回显秘密。常见 code：invalid_message、unsupported_version、unauthorized、pairing_closed/expired、session_conflict/expired、config_not_ready/conflict、invalid_config、payload_too_large、rate_limited、capacity_exceeded。HTTP 使用相应 4xx/5xx；重试策略不能绕过配对人工确认。

可执行证据入口：[protocol_test.go](../client/internal/transport/protocol_test.go)、[LAN 模拟链路](../client/internal/transport/lan_e2e_test.go)、[Serial 模拟链路](../client/internal/transport/serial_e2e_test.go)、[事务/ACL](../client/internal/transport/transactions_test.go)、[Protobuf 与独立 Python 向量](../client/internal/transport/wire/protobuf_test.go)。这些证明 Client/模拟端的相应行为，不证明板端实现、功耗或跨平台分发。
