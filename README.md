# ESP32-S3 Monitor

ESP32-S3 monitor with a 400 × 300 reflective LCD (RLCD), an LVGL interface, and a desktop simulator that renders the same UI code as the firmware.

[简体中文](README.zh_CN.md)

## Features

- Shared LVGL v9.4 UI for the ESP32-S3 firmware and deterministic desktop rendering.
- 1-bit RLCD output with a documented pixel layout and polarity.
- Wi-Fi provisioning through a captive portal and a configuration page on the local network.
- Optional weather and air-quality data from QWeather.
- Simulator scenes that export panel-ready frames and machine-readable metadata.

## Quick start: desktop simulator

From the repository root, build the simulator, run its tests, and render the default `monitor` scene:

```sh
./simulator/run.sh
```

The primary preview is `simulator/output/latest/normal.png`. List available scenes or render a named scene with:

```sh
./simulator/run.sh --list-scenes
./simulator/run.sh dashboard
```

Each render includes a PNG reconstructed from the packed RLCD frame, raw RGB565 data, the packed 1bpp frame, a SHA-256 digest, and JSON metadata. See [simulator/README.md](simulator/README.md) for the scene workflow, CLI options, artifact formats, and tests.

## Build and flash the firmware

Install ESP-IDF for ESP32-S3 and open the repository in VS Code with the Espressif ESP-IDF extension, or use the command line:

```sh
source ~/esp/esp-idf/export.sh
idf.py -C firmware set-target esp32s3
idf.py -C firmware build
idf.py -C firmware -p PORT flash monitor
```

Replace `PORT` with the board's serial port. The firmware build is separate from the simulator build and writes to `firmware/build/`. Use this firmware workflow to build and flash the board; the simulator is the desktop rendering workflow.

## Display and board

| Property | Value |
|---|---|
| Panel | 400 × 300 reflective LCD, 1 bit per pixel |
| LVGL render format | RGB565 |
| Packed polarity | `1` = white, `0` = black |
| UI source | `firmware/app/ui/` |
| RGB565-to-RLCD conversion | `firmware/src/display/monitor_pack.c` |

The GPIO mapping follows `firmware/Examples/09_LVGL_V9_Test`:

| Signal | GPIO |
|---|---:|
| MOSI | 12 |
| SCK | 11 |
| DC | 5 |
| CS | 40 |
| RST | 41 |
| TE | 6 |

## Wi-Fi and weather setup

On first boot, or when no Wi-Fi credentials are saved, the device starts the `ESP32-Monitor-XXXX` provisioning access point. Connect to it and open `http://192.168.4.1/`. To enter provisioning mode later, hold BOOT (GPIO0) and KEY (GPIO18) for about three seconds.

After connecting to Wi-Fi, the device also serves the configuration page on the local network. Find its IP address in the serial log or the router's DHCP client list, then open `http://<device-ip>/`.

The portal's Advanced tab accepts optional latitude and longitude, plus a QWeather API host and key. If coordinates are blank, the firmware estimates location through `ipwho.is`. Weather and AQI data refresh after Wi-Fi connects and every 15 minutes. See [client/README.md](client/README.md) for Telegraf host monitoring and [server/README.md](server/README.md) for the NAS service.

## Repository layout

| Path | Contents |
|---|---|
| `firmware/app/ui/` | Shared LVGL screens |
| `firmware/components/firmware/` | ESP32-S3 drivers and services |
| `firmware/components/esp_wifi_connect/` | Wi-Fi provisioning component and portal |
| `firmware/src/display/` | Shared rendering and RLCD conversion |
| `simulator/` | Desktop renderer, scenes, CLI, and tests |
| `client/` | Telegraf configuration templates and setup guide |
| `server/` | Go service, configuration, and development guide |
| `third_party/lvgl/` | Pinned LVGL submodule (v9.4.0) |

## Related guides

- [Desktop simulator](simulator/README.md)
- [Telegraf client setup](client/README.md)
- [Monitor Server](server/README.md)
- [Wi-Fi component](firmware/components/esp_wifi_connect/README.md)
- [Weather icon assets](firmware/assets/weather/README.md)
- [Metric icon assets](firmware/assets/metrics/README.md)
