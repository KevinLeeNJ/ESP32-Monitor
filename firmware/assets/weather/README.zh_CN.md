# 天气图标

天气图标基于 Erik Flowers 的开源项目 [Weather Icons](https://github.com/erikflowers/weather-icons)，遵循 SIL Open Font License 1.1 许可证。原始 SVG 文件保留在本目录；固件和模拟器使用的单色字形遮罩嵌入在 `firmware/app/ui/weather_assets.c` 中。

[English](README.md)

界面使用嵌入在 `firmware/app/ui/weather_zh_font.c` 中的 12 px Noto Sans SC 子集显示中文天气标签。该字体子集只包含下表所需的字符。

支持的界面天气类型按枚举顺序排列，也用于固件演示轮播：

| 天气类型 | 标签 | 图标类别 |
|---|---|---|
| 晴 | 晴 | day sunny |
| 少云 | 少云 | day cloudy |
| 多云 | 多云 | cloudy |
| 阴天 | 阴天 | cloudy |
| 小雨 | 小雨 | rain |
| 中雨 | 中雨 | rain |
| 大雨 | 大雨 | rain |
| 雷阵雨 | 雷阵雨 | thunderstorm |
| 小雪 | 小雪 | snow |
| 中雪 | 中雪 | snow |
| 大雪 | 大雪 | snow |
| 雨夹雪 | 雨夹雪 | hail/sleet |
| 冰雹 | 冰雹 | hail |
| 雾 | 雾 | fog |
| 霾 | 霾 | fog/haze |
| 沙尘暴 | 沙尘暴 | dust |
