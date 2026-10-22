# References

This index collects upstream documentation and project references used by the firmware, simulator, Telegraf client, and NAS service.

## Display and firmware

- [LVGL 9.4 display setup](https://lvgl.io/docs/open/9.4/details/main-modules/display/setup)
- [LVGL 9.4 display API](https://lvgl.io/docs/open/9.4/API/display/lv_display_h)
- [ESP-IDF SPI LCD example](https://github.com/espressif/esp-idf/tree/master/examples/peripherals/lcd/spi_lcd_touch)
- [Waveshare ESP32-S3-RLCD-4.2 documentation](https://docs.waveshare.net/ESP32-S3-RLCD-4.2)
- Waveshare panel sequence and GPIO reference example: `firmware/Examples/09_LVGL_V9_Test`
- [ESP-WiFi-Connect upstream usage reference](https://github.com/78/xiaozhi-esp32)

## Telegraf client and ingestion

- [Telegraf installation and platform guide](https://docs.influxdata.com/telegraf/v1/install/)
- [Telegraf downloads](https://www.influxdata.com/downloads/)
- [Telegraf Windows service guide (v1.40.2)](https://github.com/influxdata/telegraf/blob/v1.40.2/docs/WINDOWS_SERVICE.md)
- [HTTP output plugin (v1.40.2)](https://github.com/influxdata/telegraf/blob/v1.40.2/plugins/outputs/http/README.md)
- [JSON serializer (v1.40.2)](https://github.com/influxdata/telegraf/blob/v1.40.2/plugins/serializers/json/README.md)
- Input plugins: [CPU](https://github.com/influxdata/telegraf/blob/v1.40.2/plugins/inputs/cpu/README.md), [memory](https://github.com/influxdata/telegraf/blob/v1.40.2/plugins/inputs/mem/README.md), [disk](https://github.com/influxdata/telegraf/blob/v1.40.2/plugins/inputs/disk/README.md), and [system](https://github.com/influxdata/telegraf/blob/v1.40.2/plugins/inputs/system/README.md)

## NAS service implementation

- [modernc SQLite driver and DSN PRAGMA](https://pkg.go.dev/modernc.org/sqlite)
- [YAML `Decoder.KnownFields`](https://pkg.go.dev/go.yaml.in/yaml/v3#Decoder.KnownFields)
- [Go HTTP Server](https://pkg.go.dev/net/http#Server)
- [Go token bucket rate limiter](https://pkg.go.dev/golang.org/x/time/rate)
- [Go terminal detection](https://pkg.go.dev/golang.org/x/term)
- [Go file lock library](https://pkg.go.dev/github.com/gofrs/flock)
- [Charm Huh terminal form library](https://github.com/charmbracelet/huh)
- [pyte terminal emulator](https://pyte.readthedocs.io/en/latest/tutorial.html)

## UI assets

- [Lucide Icons](https://github.com/lucide-icons/lucide) — ISC-licensed metric icon sources.
- [Weather Icons](https://github.com/erikflowers/weather-icons) — weather symbol source, SIL Open Font License 1.1.
