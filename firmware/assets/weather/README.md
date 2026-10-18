# Weather Icons

Weather symbols are derived from the open-source [Weather Icons](https://github.com/erikflowers/weather-icons) project by Erik Flowers (SIL Open Font License 1.1). The user-facing weather label is Chinese and is rendered with an embedded 12 px Noto Sans SC subset (`firmware/app/ui/weather_zh_font.c`), containing only the characters needed for these labels.

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

The SVG source files are retained in this directory. Their monochrome glyph masks are embedded in `firmware/app/ui/weather_assets.c` for firmware and simulator use.
