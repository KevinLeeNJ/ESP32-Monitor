# Client 图标设计

以「4:3 屏幕 + 阶梯形 M」组成统一标识。屏幕呼应 400 × 300 RLCD，M 代表 Monitor，也呈现监测信号的起伏。沿用 Client 的黑白像素语言，去掉文字、芯片引脚和装饰。

- 应用图标：暖白 `#F3F3ED` 底，炭黑 `#252525` 图形，圆角底板与透明外边距。屏幕轮廓采用切角。
- 状态栏图标：22 × 22 透明画布，纯黑图形；可见范围为 18 × 14。单独按像素网格绘制，避免直接缩小应用图标。

## 文件

| 文件 | 用途 |
| --- | --- |
| `app-icon.svg` | 可编辑的应用图标矢量源 |
| `app-icon.png` | 1024 × 1024 RGBA 主图 |
| `app-icon-{16,32,64,128,256,512}.png` | 常用尺寸 |
| `app-icon.icns` | macOS 应用图标 |
| `app-icon.ico` | Windows 多尺寸图标 |
| `menu-bar.svg` | 可编辑的状态栏矢量源 |
| `menu-bar.png` | 22 × 22，1× |
| `menu-bar@2x.png` | 44 × 44，2× |
| `menu-bar@3x.png` | 66 × 66，3× |
| `preview.png` | 应用图标及浅色、深色状态栏预览 |

## 使用约定

macOS 状态栏使用 template image，由系统根据背景决定显示颜色。PNG 保持黑色 RGB 和透明背景，透明的屏幕内部也参与镂空。预览中的白色版本用于演示系统着色效果。

`app-icon.icns` 由 `scripts/build-darwin-app.sh` 打包进 App 的 `Contents/Resources`；`menu-bar@2x.png` 由 Client 内嵌，用于可选的 macOS 菜单栏图标（template image）。

## 再生成

在 macOS 上安装 Pillow 后执行 `python3 client/assets/icons/generate.py`。预览文字使用系统 Arial 字体；图标本体由几何图形组成，不依赖字体。脚本同时导出矢量与位图，状态栏位图不经过缩放抗锯齿。
