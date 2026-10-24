# 固件回归测试

参考 [ESP-IDF ESP32-S3 单元测试指南](https://docs.espressif.com/projects/esp-idf/zh_CN/stable/esp32s3/api-guides/unit-tests.html)，使用 Unity 的断言、`TEST_CASE` 和模块标签。此目录集中管理测试，模拟器只提供共享 UI/渲染代码和 CLI。生产固件不会链接测试代码。

## 主机快速回归

需要 C/C++ 编译器、CMake、Python 3、Node.js、zlib 开发文件，以及初始化好的 `third_party/lvgl` 子模块。cJSON 优先复用固件已下载的版本；没有时由模拟器构建下载 v1.7.19。Unity 复用 LVGL 子模块自带的真实框架。

在仓库根目录执行：

```bash
./firmware/tests/run.sh
./firmware/tests/run.sh -R 'monitor_rtc_tests|monitor_contract_tests'
./firmware/tests/run.sh -N
```

构建产物默认放在 `firmware/tests/build/host`，可以通过 `MONITOR_TEST_BUILD_DIR` 指定其他目录。`run.sh` 会构建后执行 CTest，失败返回非零；单项测试有超时限制。Python 测试临时编译的二进制和测试数据在运行后自动清理。

`./simulator/run.sh` 和 `ctest --test-dir simulator/build --output-on-failure` 继续运行同一套测试注册，不保留两份用例。`test_api_server.py` 会在 `127.0.0.1` 上绑定系统分配的临时端口，需运行环境允许本地监听；权限错误不能当成功能回归，也不能跳过后宣称全通过。

## ESP32-S3 Unity 工程

这是独立测试应用，通过 `main` 测试组件显式注册源文件，依赖 `unity` 和固定版本的 `espressif/cjson`，使用 `WHOLE_ARCHIVE` 保留所有 `TEST_CASE`。不依赖生产工程预先下载 cJSON，也不会启动生产应用或访问真实 Wi-Fi、传感器、RTC 和屏幕。

在仓库根目录执行（`-B` 使用绝对路径，避免 IDF 将其解析到当前工作目录）：

```bash
source ~/esp/esp-idf/export.sh
idf.py -C firmware/tests -B "$PWD/firmware/tests/build/esp32s3" build
idf.py -C firmware/tests -B "$PWD/firmware/tests/build/esp32s3" -p /dev/cu.usbmodem1101 flash monitor
```

串口菜单输入 `*` 运行全部 10 个用例，也可以输入 `[devices]`、`[pack]`、`[schedule]`、`[display]`、`[animation]` 按组运行。`test_contracts.c` 在主机和板端使用同一份测试源码、真实生产打包/解析模块及内联调度函数；主机通过 `RUN_TEST` 执行，板端通过 `TEST_CASE` 注册。

烧录测试应用会替换设备当前应用和分区表。测试完成后，可用 `idf.py -C firmware -p /dev/cu.usbmodem1101 flash monitor` 恢复生产固件。构建成功仅说明可编译，不能当作板端测试已执行。

## 覆盖范围

| 测试 | 场景 |
| --- | --- |
| `test_contracts.c`（10 个 Unity 用例） | 调度首次同步、同槽去重、30 秒/分钟/15 分钟/跨天/前后校时；微秒取整；全部 64 种刷新原因组合；不同 tick 频率、动画超期跳帧和大整数；I1 非法区域、stride、空指针、写边界、逐像素和反色；URL/token 长度和端口边界；JSON 逐字节截断、4096 字节上限、嵌套深度、转义、0～4 台设备、百分比 0/100/null、重复字段和类型错误、失败保留旧快照 |
| `test_rtc.c`（8 个 Unity 用例，主机） | 编译完整生产 RTC 模块，SDK 边界替身；无总线、内存/加设备/锁/I²C 故障；初始化失败重试、控制位保留；闰年、月末、非法 BCD、24 小时制、OS/STOP；2024～2099 UTC 往返；写失败恢复和释放锁 |
| `sensors_test.py` | 电池曲线端点、单调性、ADC/校准失败；SHTC3 CRC 已知向量、负温/中值/满量程、湿度边界、重复值；唤醒/测量/读取/CRC 失败、休眠清理和旧值保留 |
| `display_diff_test.py` | 无变化时的 TE 等待、所有 X tile、首尾 page、局部矩形 staging、全帧回退、窗口/SPI/完成等待失败；失败不更新缓存、下次重试、缓冲区哨兵 |
| `unit_tests.c`、`animation_tests.c` | 原模拟器测试：打包布局及区域组合、LVGL I1 混合/局部重绘、场景与 PNG/SHA、设备数据/UI 替换、动画完整时间线与局部变化等 |
| `wifi_session_test.py`、`wifi_buttons_test.py`、`station_scan_test.py` | 首次启动无凭据开 AP/有凭据连 STA、重复启动、初始化/两处定时器/任务创建失败清理与重试；AP/LAN 会话、旧超时/保存与超时同时到达、重复断线不延长截止期限、保存响应排空、30 秒停止后不自动重启；100 次按压/抖动/保持/松开、每引脚屏蔽；共享扫描结果、空结果/扫描 SDK 错误保留旧列表、已存频道回退、断线所有权、退出清理 |
| `devices_transport_test.py` | 连接复用、服务端关闭、301/400/401/403/404/429/500/503、内容类型/编码、实际 body 越界、未完成流、JSON/读取失败；直接读取生产 `REQUEST_US` 验证共享 4 秒期限；锁/内存/客户端/定时器/请求头失败及恢复 |
| `devices_lifecycle_test.py` | 执行真实 `devices_task`：无配置/坏配置、离线、HTTPS 等待有效时间、HTTP 无时钟回退、固定周期轮询无忙等、首次/重复数据、旧值保留、90 秒精确过期、鉴权失败、API 恢复、请求期间断线/换配置、移除配置、过期定时器与请求竞态 |
| `wifi_retry_test.py` | 执行真实 Station 事件回调：每轮最多 5 次直接重连调用、断线通知去重、10/20/40/80/160/300 秒扫描退避和封顶、自定义封顶、候选 AP 切换、成功后重置、停止时先注销事件再删除定时器与关闭驱动 |
| `weather_behavior_test.py` | 天气任务的 Wi-Fi/时钟/通知门控、启动失败重试及幂等；host/key 未配置不访问定位服务；NVS 失败、经纬度边界/非数/尾随垃圾、IP 定位与缓存；首次/重复/部分数据、错误 JSON/业务码、非法 AQI 与整数溢出、分配失败、保留旧值及恢复 |
| `weather_transport_test.py` | 真实 HTTP 收集/清理代码：4 秒 SDK 超时配置、不同分片长度、容量边界与越界、HTTP/鉴权/限流/服务端错误、空响应；gzip 正常/损坏流/解码容量不足、两处分配失败与清理。ROM tinfl 边界使用主机 zlib 适配，不等于在主机验证 ESP ROM 解压器 |
| `firmware_animation_test.py`、`lvgl_adapter_test.py`、`firmware_watchdog_test.py` | 原行为测试按当前 native LVGL 和 HPM/LPM 实现更新：单帧提交、错误传播、模式切换恢复、优先级、TE 丢失与恢复、分钟边界、tick 回绕、看门狗 |
| `config_page_test.cjs` | 原配置网页行为测试 |
| `test_api_server.py` | 原 API 服务的 4 个测试，现在接入 CTest：0～4 台、鉴权、路由、方法、随机指标边界及 CLI 参数 |
| `e2e_check.cmake` | 原 CLI 端到端：场景注册、非法场景、PNG/packed/RGB565/元数据/哈希、动画帧数与重复运行一致性 |

主机行为测试的 SDK 替身用于控制时间、错误和资源生命周期。部分旧 Python 测试提取生产函数后编译，内部函数重命名时需要同步测试入口；提取失败必须报错，不能跳过。RTC 测试直接包含完整生产 `.c`，避免仅覆盖复制出的实现。

## 失败排查与边界

1. 先判断环境/测试入口问题：缺依赖、编译器/SDK 差异、沙箱权限、迁移路径、提取的函数是否仍存在。
2. 对照接口契约、芯片字段掩码、协议或已知向量核对测试预期；不能为了通过测试把正确行为改错。
3. 确认实现缺陷后，保留最小复现并做最小修复；先重跑相关测试，再跑完整回归；修改生产 C/C++ 时还要构建生产固件。
4. 失败用例不能静默跳过、放宽断言或更换预期快照来掩盖回归。

这套测试覆盖上述确定性逻辑和已知故障路径，不代表穷尽硬件场景。真实 I²C/SPI/DMA/TE 电气时序、屏幕残影、Light Sleep 功耗、真实网络/TLS、ESP ROM gzip 解码器、gzip 可选头与尾部校验、外部接口变化及长期并发稳定性仍需板端或集成验证。所有主机用例通过也不能推导出真实硬件和服务端的全部组合都正确。

## 重试策略的验收边界

按本次用户选择，保留 API 的固定周期轮询，不新增指数退避或连续失败熔断。主机 CTest 共注册 20 个套件；其中循环执行的矩阵/边界场景不按单个 CTest 项目计数。

- Wi-Fi：单个候选 AP 的失败事件最多直接调用 5 次 `esp_wifi_connect`，之后进入扫描；扫描间隔指数翻倍并封顶。应用层还有独立的 30 秒总连接窗口，重复断线通知不能滑动该期限。到期停止 Station 并进入离线；迟到断线通知不能改变离线状态。底层退避封顶测试与上层 30 秒停止测试分别验证各自契约，不能把 300 秒扫描上限当成应用会持续重试 300 秒。
- Devices：每个逻辑请求最多两次传输尝试，仅复用的旧连接发生可重试传输失败时补一次，始终共享原始 4 秒期限。鉴权/协议/业务响应失败不在同一请求中立即重试；后续仍按 30 秒周期轮询，包括 401/403。测试明确验证这一现状，不声称已具备累计失败停止条件。
- 天气：只在 Wi-Fi 可用、时间有效且收到刷新通知时请求；正常时钟调度仍为 15 分钟。HTTP 失败不在函数内无限循环重试，后续刷新可以恢复；未新增累计失败停止条件。

本次新增用例先复现再修复的缺陷：Wi-Fi 启动失败的定时器泄漏、重复断线事件延长截止期限和离线后的迟到断线事件改变状态；天气无配置时仍请求定位、重复启动创建多份任务、经纬度字符串/范围校验不足、错误业务码或非法天气/AQI 数值污染缓存。测试侧同时修正了原来写死 8 秒的 HTTP 期限，以及旧测试桩残留连接队列导致的场景间串扰。
