# ESP32 固件计划能力表

核对日期：2026-10-28。当前实现以固件源码为准；新增上位机行为以当前 Client 的 [ESP32通信协议](ESP32_COMPANION_PROTOCOL_V1.md) 为对齐目标。**已有第二页UI/翻页代码，但目前由 `monitor_activity_demo()` 提供演示数据；LAN/Serial Companion固件、真实数据绑定和三模式管理仍待实现。**

“已有基础”表示有代码入口，不表示本次已构建/烧录或完成硬件验收；~~删除线~~只标出已实现子项。主机测试、模拟器、编译、串口日志、视觉观察和电池测量分别提供不同证据。

## 1. 当前基础与计划能力

| 范围 | 当前实现 | V1剩余目标 | 依据 |
| --- | --- | --- | --- |
| 启动/调度 | ~~NVS、RTC/网络时钟、天气、Devices、传感器、Wi-Fi启动~~ | 纳入模式管理，避免直连与代理同时写同一数据 | [main.c](../firmware/main/main.c) |
| 第一页 | ~~时钟/日期、天气/AQI、板载环境/电池、设备卡片~~ | 代理/直连切换时保留准确缺失/过期语义 | [monitor_ui.c](../firmware/app/ui/monitor_ui.c) |
| 第二页与切页 | ~~共享activity页、页面切换与动画、演示数据~~ | 接入真实GitHub/Codex/音乐、健康/空态/过期；仅有效Companion模式开放 | [monitor_activity.c](../firmware/app/ui/monitor_activity.c)、[monitor_pages.c](../firmware/app/ui/monitor_pages.c) |
| 渲染与面板 | ~~共享LVGL I1、1bpp打包、差分/dirty提交、TE/HPM/LPM基础~~ | 各模式刷新档位、异常恢复、长文本和动态内容实机观察 | [monitor_lvgl.c](../firmware/components/firmware/monitor_lvgl.c)、[display_port.c](../firmware/components/firmware/display_port.c) |
| Wi-Fi配网 | ~~无凭据AP/网页、LAN配置页面、组合按键与会话控制~~ | 与模式退出/恢复一致，Serial进入配网先退出该模式 | [monitor_wifi_manager.cpp](../firmware/components/firmware/monitor_wifi_manager.cpp) |
| 时间 | ~~RTC、SNTP与现有分钟显示~~ | 上位机时间来源、trusted/时效、Serial秒显示与缓冲/布局调整 | [monitor_clock.c](../firmware/components/firmware/monitor_clock.c) |
| 天气/AQI | ~~现有固件HTTPS获取路径~~ | standalone直连，LAN/Serial代理，版本/旧请求隔离 | [monitor_weather.c](../firmware/components/firmware/monitor_weather.c) |
| Server设备 | ~~30秒请求、90秒短期保留、四卡摘要解析~~ | 保持cadence；接入代理快照、过期清除/空列表语义 | [monitor_devices.c](../firmware/components/firmware/monitor_devices.c) |
| 板载传感器 | ~~本地温湿度、电池ADC读取~~ | 三模式持续使用本机值，不被电脑电池覆盖，保留读取失败语义 | [monitor_sensors.c](../firmware/components/firmware/monitor_sensors.c) |
| 自动睡眠 | ~~主程序PM自动Light Sleep、启动锁、事件驱动刷新~~ | 模式锁生命周期、Wi-Fi/屏幕策略、回退恢复及实测 | [main.c](../firmware/main/main.c)、[sdkconfig.defaults](../firmware/sdkconfig.defaults) |
| Companion接收 | 待实现 | Protobuf Envelope wire2、UDP/配对/MQTT、串口帧/认证/配置加密 | [Client schema](../client/internal/transport/wire/companion.proto) |
| 统一模式管理 | 待实现 | 身份/session/配置、连接与回退状态机、生产者所有权 | [模式契约](ESP32_COMPANION_PROTOCOL_V1.md) |
| 凭据/NVS迁移 | 已有网络/天气/Devices配置 | 配对密钥受限存储/撤销、新schema、保留既有用户配置 | [esp_wifi_connect](../firmware/components/esp_wifi_connect) |
| 发布升级 | 当前串口开发烧录路径 | 版本/配置保留与失败恢复；OTA是否纳入V1未冻结 | [V1计划](V1_RELEASE_PLAN.md) |

## 2. 三种模式的目标能力

以下是待接入固件的目标，不能因Client模拟测试通过划线为完成。

| 项目 | standalone | lan | serial |
| --- | --- | --- | --- |
| 页面 | 第一页，隐藏Companion第二页入口 | 两页 | 两页 |
| 网络 | 原Wi-Fi关联/配网 | 原Wi-Fi + Monitor主动连Client MQTT | 稳态关闭Wi-Fi/AP/DNS/网络轮询 |
| 时间 | RTC + 网络校时，分钟级显示 | 上位机 + RTC，默认分钟级 | 上位机 + RTC，支持秒与局部动画 |
| 天气/AQI、Devices | 固件既有直连生产者 | 上位机代理 | 上位机代理 |
| 本地环境/电池 | 本机传感器 | 本机传感器 | 本机传感器 |
| Light Sleep | 自动策略 | 继续允许，不因会话常持禁止睡眠锁 | 明确持有禁止自动Light Sleep锁 |
| 屏幕 | 按变化更新，空闲LPM/动画HPM | 同类策略，成本单独实测 | 常驻HPM，无变化不重复写整帧 |
| 失联 | 保持本地RTC/传感器与网络状态提示 | 90秒无有效应用证明回退 | 3秒无有效应用证明回退；流断开可提前 |

LAN与standalone共用低功耗关联方向（LOW_POWER、WIFI_PS_MAX_MODEM、listen_interval=10），具体有效配置须检查实际构建/运行；不能由defaults推断实机已经生效。**Devices API仍每30秒一次**，不以节能为由降低频率。低功耗Wi-Fi必须保留LAN配置页及时响应。

## 3. 模式管理、数据所有权与恢复

1. 每次启动先standalone，让本地时钟/传感器/直连生产者可用；历史IP、保存模式、USB供电不作为进入Companion的条件。持久化配对身份与用户启用意图，运行态记录effective_mode、connection_state、fallback_reason、active_host_id、session、epoch/revision。
2. LAN通过新认证邀请进入MQTT、订阅/open、完整配置、完整快照和应用心跳确认；Serial通过HELLO、必要配对、双向AUTH、配置/快照、两次心跳再入场。业务协议V1和线编码v2分别检查。
3. 一次只激活一个传输；认证完成的Serial优先，不允许未认证串口候选踢掉LAN。退出后至少稳定10秒才再次自动入场，失败退出始终立即执行。
4. 切换前冻结旧session写入、取消旧生产者/网络请求；旧响应不得覆盖新模式。网络/串口任务只发布有界快照和事件，唯一显示任务操作LVGL/屏幕。
5. 回退依次取消session、切回第一页/隐藏第二页、从Serial恢复Wi-Fi与直连生产者、完成最后显示后转LPM、释放睡眠锁。保留RTC和板载传感器；恢复Wi-Fi不是已联网。
6. 单一GitHub/Codex/音乐故障只降级对应卡片，不能触发整机回退。重复/过期/旧session帧不能续看门狗；MQTT PINGRESP/PUBACK不能代替应用层证明。
7. 配网组合长按维持BOOT GPIO0 + KEY GPIO18约3秒语义；Serial显式配网先退出Serial，不同时声称Wi-Fi关闭与AP开启。现有演示页按键行为需按模式限制，不作为此目标已经完成的依据。

## 4. UI与数据落地清单

| 能力 | 实施/验收要求 |
| --- | --- |
| 共享UI | 所有新页面/状态在firmware/app/ui实现，模拟器使用同一源码；新增源登记monitor_core CMake |
| GitHub | 连续日期/levels的贡献墙、未知/授权失效/过期；不把演示格子当真实贡献 |
| Codex额度 | 多窗口截断提示，已用/剩余口径准确，未知显示缺值；不能由额度推断任务状态 |
| Codex任务 | idle/running/waiting_input/waiting_approval/completed/failed/cancelled/unknown；终态过期和所选任务范围 |
| 播放 | 仅playing/not_playing，健康暂停保留元数据，退出清current；长中文、来源、位置插值与错误独立 |
| 代理第一页 | 配置epoch/revision匹配；各来源独立age/TTL，snapshot/续租不伪造成功；devices错误与成功空列表不同 |
| 时间 | HH:MM:SS需至少9字节（含终止符）；当前time_text[8]不能直接复用；检查格式接口/区域布局 |
| 刷新 | 无变化不刷；可见页动画/HPM与隐藏页处理分开；唯一提交者、TE超时/异常后恢复 |
| 布局与档位 | 400×300、1bpp；Serial先以16fps局部动画为目标，32fps仅探索，记录p50/p95/传输量/TE超时 |
| UI诚实性 | unknown/unsupported/permission/error/stale分清；真实0/false不被当缺失；无有效来源不显示默认演示数值 |

显示输出为400×300的15000字节1bpp帧；bit1白/bit0黑，不能改反。当前共享渲染主要走LVGL I1，RGB565导出/转换属于兼容与模拟器工具，不应继续沿用“固件必先全屏RGB565”的旧描述。

## 5. 实施顺序与验收证据

| 阶段 | 工作 | 完成依据 |
| --- | --- | --- |
| F0硬件/资源 | USB通道、启动日志、DTR/RTS复位、RAM/PSRAM、PM/屏幕基线 | 对应板子/固件版本/供电方式记录；明确接口是否共用 |
| F1解析与存储 | 有界Protobuf解析、帧/校验/认证、NVS迁移/撤销 | 与Client固定向量互通、错误原子拒收、旧配置保留 |
| F2 LAN | 发现/双确认配对/MQTT/配置/ACK与完整快照 | Client GUI→真板；地址变化、重连、撤销、过期和慢消费 |
| F3 Serial | 端口握手/认证/Config加密、噪声重同步、心跳/拔线 | 物理串口；日志干扰、拆粘包/超限/重放、首次配对 |
| F4模式/电源 | 数据源切换、Wi-Fi真关闭/恢复、PM锁与HPM/LPM | 状态日志+锁状态+真实功耗；不能只打印模式名 |
| F5 UI | 真实第二页、缺失/过期/错误、秒显示/动画 | 确定性模拟器场景+真实屏幕视觉记录 |
| F6候选版 | 启动/掉电/网络/主机休眠/反复插拔，72小时 | 可重复记录，无持续增长/错误锁/半退出模式 |

功耗结论必须采用USB断开后的电池接口、多分钟相同内容/网络条件测量；USB供电日志、PM统计只能辅助解释。模式2额外通信耗电单独记预算，不承诺与standalone相同。电池冷启动、USB接入前后状态、Wi-Fi失联/配置页都要回归。

历史 [固件审查报告](../.bak/docs-cleanup-2026-10-28/docs/firmware-compliance-review.md) 仅作待复核线索。看门狗、显示抽象、时钟、配置保护、功耗及升级问题须按现源码逐项确认，不把旧报告已修复条目重新列为当前缺陷。相关出处保留在 [旧REFERENCES](../.bak/docs-cleanup-2026-10-28/docs/REFERENCES.md) 与素材来源文件中。

## 6. 构建、模拟器与测试入口

在仓库根目录：

```sh
./simulator/run.sh --list-scenes
./simulator/run.sh
ctest --test-dir simulator/build --output-on-failure
./firmware/tests/run.sh
```

模拟器产物位于 `simulator/output/latest/`，包含PNG、packed帧、raw、digest和metadata；按实际场景名定位输出。场景工厂必须确定性，不能用墙钟/随机数生成回归图。

固件开发构建（先初始化所需子模块/依赖）：

```sh
source ~/esp/esp-idf/export.sh
idf.py -C firmware build
```

真实板端再按实际端口执行 flash/monitor（常用 `/dev/cu.usbmodem1101`）。构建目录可能由工具配置指定，核对实际生成sdkconfig，不能只读defaults；build成功不等于已烧录/运行。

独立Unity测试工程：

```sh
idf.py -C firmware/tests -B "$PWD/firmware/tests/build/esp32s3" build
```

测试应用会替换当前生产应用/分区，板端执行应单独安排，随后恢复生产固件。主机用例和板端测试共享生产解析/调度/打包代码，但主机通过不等于实物电源/无线/显示验收。测试入口见 [host.cmake](../firmware/tests/host.cmake)、[test_contracts.c](../firmware/tests/test_contracts.c) 和 [test_main.c](../firmware/tests/main/test_main.c)。
