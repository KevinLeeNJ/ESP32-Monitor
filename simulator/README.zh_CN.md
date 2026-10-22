# 桌面 UI 模拟器

本目录提供 ESP32-S3 RLCD 项目的桌面显示模拟器。它在电脑上渲染固件使用的 LVGL 界面，并生成像素精确的 400 × 300 PNG，无需连接开发板。

[English](README.md)

模拟器面向 UI 开发：新界面注册为场景后，可通过命令行选择渲染。它运行真实的共用 UI 代码，不是静态样稿，也不使用 SDL。

## 工作流程

```text
已注册的 LVGL 场景
        |
        v
LVGL 9.4 RGB565 渲染
        |
        v
firmware/src/display/monitor_pack.c（RLCD 1bpp，1=白色 / 0=黑色）
        |
        +--> <scene>_normal.png       根据打包帧生成的预览图
        +--> <scene>_normal.packed    面板实际使用的字节
        +--> <scene>_normal.rgb565    LVGL 原始输出
        +--> <scene>_normal.sha256    打包帧摘要
        +--> <scene>_normal.json      场景及产物元数据
```

PNG 是从打包帧重建的，而不是直接复制 LVGL RGB565 缓冲区。因此，面板极性、位布局和打包逻辑的问题会体现在预览图中。

## 快速开始

在仓库根目录运行：

```bash
./simulator/run.sh
```

默认场景为 `monitor`，主预览图位于：

```text
simulator/output/latest/normal.png
```

脚本也会生成 `inverted` 诊断帧。渲染其他已注册场景：

```bash
./simulator/run.sh dashboard
```

查看可用场景：

```bash
./simulator/run.sh --list-scenes
```

## 添加 LVGL 界面

1. 将实际界面代码放在 `firmware/app/ui/`，不要放在 `simulator/`。
2. 提供如下形式的工厂函数：`lv_obj_t *my_screen_create(lv_obj_t *parent)`。
3. 在 `simulator/src/scene_registry.c` 注册工厂函数。
4. 运行 `./simulator/run.sh my_screen`。
5. 检查 `simulator/output/latest/my_screen_normal.png`。
6. 集成时在固件中运行同一界面。

需要 inverted 诊断模式的场景还应提供接受 `monitor_frame_variant_t` 的工厂函数。当前 monitor 场景使用 `monitor_ui_create_variant()`；仅渲染 normal 状态的场景可以忽略此参数。

场景注册表保持小而明确。不要复制一份仅供模拟器使用的界面；模拟器应渲染最终部署到固件中的同一份 UI 源码。

## 命令行

```bash
simulator/build/monitor_simulator --list-scenes
simulator/build/monitor_simulator --scene monitor --variant normal \
  --output simulator/output/latest
```

| 选项 | 说明 |
|---|---|
| `--scene NAME` | 选择已注册的 LVGL 场景，默认 `monitor` |
| `--variant normal\|inverted` | 渲染 normal 或 inverted 诊断状态 |
| `--output DIR` | 输出目录；不存在的子目录会自动创建 |
| `--list-scenes` | 输出场景名称和说明 |
| `--help` | 显示帮助 |

## UI 开发流程

1. 阅读 `firmware/app/ui/` 中的相关界面和 `firmware/include/` 中的头文件。
2. 修改共用 UI，不要创建模拟器专用副本。
3. 运行 `./simulator/run.sh <scene>`。
4. 检查场景的 `*_normal.png`。
5. 查看对应的 `.json` 和 `.sha256`，读取机器可用的结果信息。
6. 若单元测试或端到端测试失败，或预览图未生成，则本次变更未通过模拟器检查。

用于视觉回归的场景应保持确定性。避免使用实时文本、随机值和不受控动画。对于依赖时间的内容，应提供固定测试状态或确定性场景工厂。

## 输出约定

默认 `monitor` 场景生成：

| 文件 | 说明 |
|---|---|
| `normal.png` | 根据打包数据重建的 400 × 300 预览图 |
| `normal.rgb565` | 240,000 字节的 LVGL RGB565 原始帧 |
| `normal.packed` | 15,000 字节的 RLCD 1bpp 帧 |
| `normal.sha256` | `normal.packed` 的 SHA-256 |
| `normal.json` | 场景、尺寸、格式、极性和摘要 |
| `inverted.*` | inverted 诊断状态对应的产物 |

其他场景的文件名为 `<scene>_normal.*` 和 `<scene>_inverted.*`。

Waveshare RLCD 参考驱动使用 `ColorWhite = 0xff`、`ColorBlack = 0`，因此打包约定为 **bit 1 = 白色，bit 0 = 黑色**。位布局和极性由单元测试覆盖，且固件与模拟器必须共用该约定。

## 测试

`./simulator/run.sh` 会构建模拟器并运行两层测试：

- `monitor_unit_tests`：SHA-256 向量、RGB565/1bpp 往返转换、面板角点映射与极性、LVGL 确定性渲染及 PNG 生成。
- `monitor_simulator_e2e`：使用实际模拟器 CLI 渲染两种状态，校验文件大小、PNG 签名和 400 × 300 尺寸、打包帧摘要、场景元数据、场景列表及未知场景拒绝行为。

构建后单独运行测试：

```bash
ctest --test-dir simulator/build --output-on-failure
```

## 主要源码位置

| 路径 | 作用 |
|---|---|
| `simulator/src/main.c` | 场景 CLI 和产物写入 |
| `simulator/src/scene_registry.c` | LVGL 场景注册表 |
| `simulator/tests/` | 单元与端到端检查 |
| `firmware/app/ui/monitor_ui.c` | 模拟器和固件共用的 LVGL UI |
| `firmware/src/display/monitor_render.c` | 可复用的离屏 RGB565 渲染器 |
| `firmware/src/display/monitor_pack.c` | 共用的 RLCD 1bpp 转换 |
| `third_party/lvgl/` | 固定版本的 LVGL v9.4.0 子模块 |
| `third_party/lv_conf.h` | 共用 LVGL 配置 |
