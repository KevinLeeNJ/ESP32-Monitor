# V1 计划

核对日期：2026-10-29。以当前工作区 Client 实现为基线，**V1 尚未达到整体发布条件**。~~删除线~~仅表示对应代码子项已实现，不代表真实三平台、生产 Server 或板端验收完成。剩余事项保留未划线；不再沿用旧“受管 Telegraf、GUI/IPC均未实现”的基线。

当前事实详见 [Client整体能力表](CLIENT_CAPABILITIES.md)；接口只在 [Server–Client v2](SERVER_CLIENT_PROTOCOL_V2.md)、[ESP32–上位机协议](ESP32_COMPANION_PROTOCOL_V1.md) 维护；固件实现与目标见 [固件计划能力表](FIRMWARE_CAPABILITIES_PLAN.md)。本计划不另立冲突的路由、字段或指标清单。

## 1. 版本范围与已确认方向

| 范围 | V1要求 |
| --- | --- |
| Client | Windows/macOS/Linux完整GUI与同一Go Agent；无界面Agent作为补充，不能代替Linux GUI |
| 采集 | 进程内Core(gopsutil)+平台Native；七类实际可用指标、能力/对象选择、预览与v2上报；不再要求安装Telegraf |
| 上位机 | GitHub贡献墙、Codex额度与可观察任务、系统播放；模块可分别启停，只读、不采任务正文或播放历史 |
| Server | Web Dashboard、首次初始化、认证、验证码注册、v2存储/查询/投影、健康与配置管理 |
| ESP32 | standalone/lan/serial三模式、数据接入、双页、断连回退、电源策略；已有演示页改为真实数据 |
| 部署 | Go+SQLite单实例，Docker/Compose正式方案，升级迁移、备份恢复、平台安装包 |
| 隐私 | Server不保存GitHub/Codex凭据及第二页原始内容；设备上传Token与Companion配对凭据独立 |

每台ESP32一次只用一个活动上位机/传输；不做多机任务合并、播放控制、任意采集插件市场。OTA、自动更新、Linux arm64 GUI等未冻结的扩展先保留决策，不自动增加为已交付。

## 2. 当前交付状态与下一步

| 模块 | 已实现子项 | 仍需完成 / 退出条件 |
| --- | --- | --- |
| Agent/采集 | ~~Core/Native、七类能力、事务配置、固定缓存、v2档案差异与队列~~；Windows Native 采集与平台化 v2 映射已完成本机扫描/单测（含 CPU 负载估算、LHM 温度、WDDM GPU 温度） | Linux Native缺口、Windows 跨硬件矩阵（电池/温度/多GPU）、真实权限/对象矩阵、生命周期恢复；风扇转速已移除；不再安排Telegraf托管 |
| GUI/IPC | ~~Wails宿主、Vue七页、绑定服务、Supervisor、Unix同UID认证IPC、Windows Named Pipe IPC、Windows构建脚本与绿色包~~ | 三平台真实WebView/首次使用、托盘点击/登录自启验收、安装器/签名、关窗/唤出/退出与崩溃恢复 |
| Server连接 | ~~Client验证码换Token、私有文件保存、v2上传；本地模拟器~~ | 正式Server注册/管理、SQLite版本化Profile/样本/幂等、Dashboard与摘要投影 |
| GitHub | ~~Device Flow、日历、缓存、应用私有凭据文件、GUI绑定~~ | 三平台授权/过期/跨进程复用和真实GUI验证、发布OAuth配置 |
| Codex | ~~额度读取、任务Adapter/实验desktop IPC、统一状态~~ | 账号关联、任务全状态/重连、Windows任务原生证据、用户明确启用与兼容矩阵 |
| 播放 | ~~统一playing/not_playing、MediaRemote/GSMTC/MPRIS代码与快照~~；Windows GSMTC 适配器健康/无源路径已本机冒烟 | 三平台本地来源变化、真实播放元数据/暂停/切歌、权限、系统升级/分发兼容与恢复 |
| Companion | ~~Protobuf wire v2、发现/双确认配对、MQTT、串口认证/加密、代理、模拟Monitor~~ | 物理串口、首次配对完整交互、真实固件、休眠/DHCP/故障恢复矩阵 |
| 启动与诊断 | ~~设置、Agent重启/清理、脱敏报告与页面~~；Windows关窗后台已本机验证 | 三平台真正登录自启、托盘点击/重新唤出完整验收、系统服务、诊断导出、升级/卸载 |
| 固件显示 | ~~共享第一页、activity演示页/切页、I1差分、HPM/LPM、刷新/调度基础~~ | 第二页真实数据与空/错/过期态、模式控制、接收器与电源闭环 |
| 正式Server | ~~v1摄取、SQLite、摘要、Token/TUI、探针~~ | 下节全部生产管理与v2工作；旧Server不能直接接当前Client |

优先依赖：冻结当前Client v2契约 → 正式Server对齐并打通最小链路 → GUI/跨平台能力补齐 → 固件协议/模式 → 三平台与板端联合验收 → 发布。允许各模块独立推进，但不能用模拟端结果提前标记集成完成。

## 3. Server实施范围

### 3.1 对齐当前Client

- 实现6位数字码、10分钟、单次兑换的注册流程，按v2协议下发设备Token，支持重新发码、令牌轮换与删除撤销。废弃旧计划中另一套5分钟配对码/`clients/enroll`作为当前Client默认注册路线。
- 实现Profile GET/PUT/PATCH、ETag条件更新、单调revision、SQLite原子提交与恢复；支持当前全部语义ID/对象，不把任意平台标签写入协议。
- 样本严格校验、批次幂等、时间窗口、乱序latest、历史关联原revision、停用投影立即隐藏、旧补传不续在线；每项按协议验收。
- 解决物理盘新口径与既有挂载点DISK投影的迁移；为代表磁盘建立明确选择。保留ESP32既有四卡/4096字节摘要结构。
- 既有外部Telegraf v1入口可继续兼容；同一设备的v1/v2写入模式必须明确，迁移时防止两种latest混合。当前Client不加静默v1降级。
- 周期5–60秒与在线阈值联动，不能将60秒采样和45秒离线阈值直接组合；客户端“探针成功”和“样本成功落盘”分别显示。

~~Client侧修复`drive.used`队列判空，增加仅开磁盘信息组的实际HTTP上传断言，覆盖0字节、大整数及SMART去重~~；当前已通过本机HTTP回归，正式Server v2与原生硬件端到端仍待验收，具体范围见 [Client能力表](CLIENT_CAPABILITIES.md)。

### 3.2 Web管理、配置与健康

| 能力 | 实施要求 |
| --- | --- |
| 首次初始化 | 一次性初始化凭据、并发初始化保护、管理员与恢复手段；不凭公网首次访问抢占管理权 |
| 管理认证 | 独立管理员会话、HttpOnly/Secure/SameSite Cookie、CSRF/Origin校验、登录限流；数据Bearer不能取得管理权限 |
| Dashboard | 设备/对象/指标及新鲜度、在线原因、来源健康、最后成功时间和配置应用状态；未知不伪装为健康 |
| 配置版本 | 并发提交条件、desired/applied revision、失败补偿或回滚；保存成功与实际生效分开 |
| 生效边界 | 名称/排序/选择/TTL/限流等可热更新；清理/备份走有界后台任务；监听地址/数据库路径等明确需受控重启 |
| 健康/审计 | 只上传有限健康摘要，权限隔离；区分Server自测、Client上报、ESP32 ACK、人工视觉证据 |
| 查询与保留 | 最新值/短期历史、能力感知展示、基数/体积限制、明确过期清理；不因新档案重解释旧记录 |

管理会话建议空闲30分钟/绝对12小时超时；登录、初始化、注册码、未认证洪泛、读取和修改分别限流，缓存有上限。仅信任配置的反代来源，不能接受任意X-Forwarded-For。日志脱敏Token/Cookie/验证码/上游秘密，审计保留操作者、版本、结果，具体参数在实现/压测时冻结。

SQLite迁移需版本管理、失败保全与可恢复备份；单实例WAL，数据库放本地盘，不让多个Server写同一文件。当前YAML中已有Token迁移不得无提示失效；新管理凭据以不可逆摘要保存，不在查询接口展示秘密。

## 4. Client与固件剩余里程碑

| 阶段 | 交付物 | 完成门槛 |
| --- | --- | --- |
| M0 支持矩阵 | OS/架构、WebView依赖、Codex/播放权限、USB/日志/供电、功耗基线 | 每个必选平台/来源有真实证据或明确缺口；不把交叉构建当原生验证 |
| M1 契约对齐 | ~~当前v2 DTO/编码、Companion protobuf schema、GUI服务/Unix IPC~~；生产Server与板端对齐 | 字段/版本/来源/时效无冲突，固定向量与错误矩阵通过 |
| M2 Server | 上节v2+管理/迁移/备份 | 当前Client注册→Profile→样本→SQLite→Dashboard/摘要闭环；故障后可恢复 |
| M3 Client产品化 | ~~GUI/采集与注册代码~~；Windows IPC、Windows Native矩阵、自启、关窗/托盘、诊断 | 三平台干净安装和真实操作；Windows 跨硬件电池/温度/多GPU矩阵；权限失败/崩溃/重新绑定均可定位 |
| M4 上位机 | ~~提供者/传输/代理与模拟端~~；账号关联、任务/播放全状态、物理链路 | LAN/串口配对、恢复/撤销、休眠唤醒、重连/版本不兼容实测 |
| M5 固件 | 模式管理、协议接收、真实第二页、数据/电源切换 | 模拟器+板端日志+屏幕观察+电池接口功耗证据齐备 |
| M6 健康与运维 | 故障定位、审计、备份、迁移/升级/卸载、诊断 | 断网/拔线/磁盘故障等不误报完成或在线 |
| M7 发布候选 | 三平台包、amd64/arm64镜像、签名/校验和/SBOM、72小时记录 | 发布门槛逐项通过，支持范围/限制明确，固定正式版本 |

Windows时间问题单独闭环：识别Server未来时间拒收 → 显示Server/本机偏差 → 用户明确同意系统设置修改 → 呈现UAC/同步失败 → 以真实成功上传确认恢复。当前通用422错误不满足此要求。

固件三模式、电源及回退的全部细项集中到 [固件计划能力表](FIRMWARE_CAPABILITIES_PLAN.md)，不在此重复报文。已有演示页不计为真实第二页完成；现有30秒Devices API cadence和局域网配置页响应必须保留。

## 5. 正式部署与运维

- 交付固定版本/digest的linux/amd64、linux/arm64镜像和Compose。Server内嵌静态资源，无运行时Node/CDN；宿主机指标由宿主Client采集，不能用容器内部数值冒充整机。
- 宿主反代拓扑只发布loopback端口；容器反代拓扑用专用网络且Server不发布宿主端口。容器内监听可达地址，HTTPS由受控反代终止；管理面默认内网/VPN或访问策略保护。
- 非root、只读根目录、drop capabilities、no-new-privileges、不挂Docker socket或宿主根目录；数据库卷0700/0600、只给明确UID写入。提供空卷/bind mount/rootless权限验证。
- 明确CPU/内存/PID、日志轮转、请求大小/超时及优雅退出预算；探针使用可用的应用入口，不能假定精简镜像带curl。健康失败的处理策略单独说明。
- 升级前SQLite一致性备份和配置导出；在线用一致性备份机制或停服复制完整状态，不单拷活动WAL主文件。演练空主机/空卷恢复和迁移失败回退，旧二进制不能直接读取已升级schema。
- 正式分发需要Windows签名策略、macOS签名/公证、Linux打包/依赖与卸载方案；当前macOS ad-hoc脚本不代表正式分发完成。

## 6. 验证门槛与开发入口

| 范围 | 发布前必须覆盖 |
| --- | --- |
| Client | 字段/对象/依赖、必选缺失、0/false/空列表、配置失败保旧、队列上限/鉴权、Profile冲突/丢ACK/重启恢复、隐私隔离 |
| 三平台GUI | 干净安装、首次流程、真实绑定/权限、Server注册/上传、关窗继续运行/重新唤出/退出、自启启停、升级/卸载 |
| 真实采集 | GUI选择→Agent采样→正式Server落盘/投影→Dashboard/ESP32；各平台原生权限与不支持值 |
| 提供者 | GitHub授权/过期/限流，Codex账号关联/任务完整转换，播放来源变化/暂停/退出与私有API兼容 |
| 通信 | 错版本/超限/重放/乱序/慢消费、配对撤销、IP变化、网络切换、拔线、睡眠唤醒、物理串口/真实固件 |
| Server安全/恢复 | 初始化竞争、登录/CSRF、撤销/轮换、限流、配置冲突/热更新、数据库故障、备份迁移恢复 |
| 固件与屏幕 | 共享UI、中文/长文本/空态/过期、双页/三模式、传输失联回退，实际刷新质量和功耗 |
| 容器/稳定性 | 两架构、非root/只读/卷权限、两种反代、SIGTERM、72小时无崩溃与持续内存增长 |

目标预算（仍需实测）：首次有效数据10分钟内；配置热更新通常2秒内；LAN本机变化到屏幕可见≤8秒、Serial≤2秒（不含上游刷新）；LAN失联≤90秒、Serial≤3秒回退；串口局部动画先以16fps为目标。32fps仅探索，不承诺60fps。模式1相同条件下空闲平均功耗回归目标不超过基线10%，模式2另测，不据Wi-Fi配置声称节能。

现有开发入口：Client构建/测试见 [Client能力表](CLIENT_CAPABILITIES.md)；固件/模拟器见 [固件能力表](FIRMWARE_CAPABILITIES_PLAN.md)。既有Server从`server/`执行 `make build`、`make test-ut`、`make test-api`、`make test-race`、`make vet`；运行入口：

```sh
./bin/monitor-server configure --config ./config/monitor.yaml
./bin/monitor-server check-config --config ./config/monitor.yaml
./bin/monitor-server serve --config ./config/monitor.yaml
```

配置示例位于 [monitor.example.yaml](../server/config/monitor.example.yaml)，占位Token不能直接运行；实际配置必须私有，不提交。当前TUI修改需受控重启。TUI更改需业务测试及真实PTY（含小终端）；`make test-tui`所需Python依赖见 [requirements-tui.txt](../server/tests/e2e/requirements-tui.txt)。

待冻结决策：具体OS版本/架构和Linux桌面、Codex实验接口可接受范围、签名/分发、固件升级方式（OTA是否入V1）、手动/自动更新、系统服务边界。没有相应实现与证据前不设定正式发布日期。
