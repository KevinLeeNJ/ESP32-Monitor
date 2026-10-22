# Weather Icons

Weather symbols are based on Erik Flowers' open-source [Weather Icons](https://github.com/erikflowers/weather-icons) project, licensed under the SIL Open Font License 1.1. The original SVG files are kept in this directory; the firmware and simulator use monochrome glyph masks embedded in `firmware/app/ui/weather_assets.c`.

[简体中文](README.zh_CN.md)

The UI displays Chinese condition labels using an embedded 12 px Noto Sans SC subset in `firmware/app/ui/weather_zh_font.c`. The subset contains only the characters required for the labels below.

Supported UI conditions (in enum order, also used by the firmware demo sweep):

| Condition | Label | Symbol family |
|---|---|---|
| Clear | 晴 | day sunny |
| Few clouds | 少云 | day cloudy |
| Scattered clouds | 多云 | cloudy |
| Overcast | 阴天 | cloudy |
| Light rain | 小雨 | rain |
| Moderate rain | 中雨 | rain |
| Heavy rain | 大雨 | rain |
| Thunderstorm | 雷阵雨 | thunderstorm |
| Light snow | 小雪 | snow |
| Moderate snow | 中雪 | snow |
| Heavy snow | 大雪 | snow |
| Sleet | 雨夹雪 | hail/sleet |
| Hail | 冰雹 | hail |
| Fog | 雾 | fog |
| Haze | 霾 | fog/haze |
| Dust storm | 沙尘暴 | dust |
