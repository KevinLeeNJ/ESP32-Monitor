# AGENTS.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

ESP32-S3 Monitor with a 400 x 300 reflective LCD (1bpp RLCD) display. Includes a deterministic desktop simulator for UI development without hardware. Uses LVGL v9.4.0 for UI rendering, with shared code between firmware and simulator.

Target hardware: ESP32-S3 with 16MB flash, SPIRAM (octal mode, 80MHz), RLCD panel on SPI with GPIO mappings from `firmware/Examples/09_LVGL_V9_Test`.

## Build Commands

### Simulator (desktop)

```bash
# Build, test, and render default monitor UI to simulator/output/latest/normal.png
./simulator/run.sh

# Render a specific scene
./simulator/run.sh dashboard

# List available scenes
./simulator/run.sh --list-scenes

# Run tests only (after build)
ctest --test-dir simulator/build --output-on-failure
```

The simulator produces PNG previews, packed RLCD frames, RGB565 raw output, SHA-256 digests, and JSON metadata for each scene.

### Firmware (ESP32-S3)

VS Code with Espressif ESP-IDF extension (recommended):
- **ESP-IDF: Build your Project**
- **ESP-IDF: Flash your Project**
- **ESP-IDF: Build, Flash and Monitor**
- Select port with **ESP-IDF: Select Port**

Command-line (requires ESP-IDF environment):
```bash
source ~/esp/esp-idf/export.sh
idf.py -C firmware set-target esp32s3
idf.py -C firmware build
idf.py -C firmware -p /dev/cu.usbmodem1101 flash
idf.py -C firmware -p /dev/cu.usbmodem1101 monitor
```

Firmware build output goes to `firmware/build/`, separate from simulator build.

## Architecture

### Code Sharing Strategy

- **Shared UI**: `firmware/app/ui/` contains LVGL screens used by both simulator and firmware
- **Shared display logic**: `firmware/src/display/` has RGB565 rendering and 1bpp RLCD packing
- **Shared headers**: `firmware/include/` defines display contracts
- **Firmware-specific**: `firmware/components/firmware/` has hardware drivers (SPI display, sensors, clock, weather, Wi-Fi manager)
- **Simulator-specific**: `simulator/src/` has CLI, scene registry, and test harness
- **ESP-IDF components**: `firmware/components/` directory with `monitor_core` (builds shared code for ESP-IDF), `lvgl` (LVGL adapter), `esp_wifi_connect` (Wi-Fi provisioning with captive portal)
- **Main application**: `firmware/main/main.c` is the ESP32-S3 entry point

The `monitor_core` component builds the shared display and UI code into ESP-IDF by listing `firmware/src/display/*.c` and `firmware/app/ui/*.c` files with appropriate include paths. The simulator builds the same source files with a desktop CMake project.

### Display Contract

- Panel: 400 x 300 pixels, 1 bit per pixel (RLCD)
- LVGL renders to RGB565 (400 x 300 x 2 bytes = 240,000 bytes)
- `monitor_pack.c` converts RGB565 → 1bpp packed format (15,000 bytes)
- Packed polarity: bit `1` = white, bit `0` = black (matches Waveshare `ColorWhite = 0xff`)
- PNG output is reconstructed from packed bytes to verify packing logic

### Component Boundaries

- `firmware/components/firmware/`: Hardware-specific drivers
  - `display_port.c/h`: SPI RLCD driver for ESP32-S3
  - `monitor_clock.c/h`: SNTP time sync and text formatting
  - `monitor_sensors.c/h`: I2C environmental sensors and battery ADC
  - `monitor_weather.c/h`: QWeather API client (weather condition, AQI)
  - `monitor_wifi_manager.cpp/h`: Wi-Fi state integration with `esp_wifi_connect`

- `firmware/components/esp_wifi_connect/`: Captive portal Wi-Fi provisioning (modified from `78/esp-wifi-connect`)
  - Creates AP `ESP32-Monitor-XXXX` on first boot or when no credentials saved
  - Runs captive DNS and web server at `http://192.168.4.1/`
  - Stores credentials in NVS `wifi` namespace
  - Advanced tab stores lat/lon, QWeather API host/key in NVS `monitor_loc` namespace
  - Triggered by BOOT (GPIO0) + KEY (GPIO18) held for 3 seconds
  - After Wi-Fi connects, serves same page on LAN IP (no AP or captive DNS)

- `firmware/components/monitor_core/`: Builds shared code for ESP-IDF (UI, rendering, packing)

- `firmware/components/lvgl/`: LVGL v9.4.0 adapter for ESP-IDF

## UI Development Workflow

1. Implement screen under `firmware/app/ui/` (shared code)
2. Expose factory: `lv_obj_t *my_screen_create(lv_obj_t *parent)`
3. Register factory in `simulator/src/scene_registry.c`
4. Run `./simulator/run.sh my_screen`
5. Inspect `simulator/output/latest/my_screen_normal.png`
6. Verify tests pass (unit + e2e)
7. Integrate into firmware `firmware/main/main.c` when ready

Do not create simulator-only UI implementations. The simulator must render the exact UI source that ships in firmware.

## Configuration

- `firmware/sdkconfig.defaults`: Target ESP32-S3, 16MB flash, octal SPIRAM, custom partition table, 1000Hz FreeRTOS tick
- `firmware/partitions.csv`: NVS (24KB), PHY init (4KB), factory app (8MB)
- `third_party/lv_conf.h`: Shared LVGL configuration for simulator and firmware
- `third_party/lvgl/`: LVGL v9.4.0 pinned submodule

## Testing

Simulator tests cover:
- SHA-256 implementation
- RGB565 ↔ 1bpp round-trip conversions
- Panel bit layout and polarity
- Deterministic LVGL rendering
- Scene registration and metadata
- PNG generation and validation
- End-to-end CLI invocation with artifact checks

## Wi-Fi Provisioning

First boot or no saved credentials → open AP `ESP32-Monitor-XXXX` → captive portal at `http://192.168.4.1/`

Trigger reconfiguration: Hold BOOT (GPIO0) + KEY (GPIO18) for 3 seconds.

After connection, same configuration page available on LAN at device IP (check serial log or router DHCP list).

Advanced tab: optional lat/lon, QWeather API host/key for weather/AQI data. Blank coordinates → firmware uses IP geolocation from `ipwho.is`. Weather refreshes on Wi-Fi connect and every 15 minutes.

## Common Pitfalls

- Simulator and firmware build separately; changes to shared code require rebuilding both
- LVGL configuration is shared via `third_party/lv_conf.h`; do not create divergent configs
- Panel polarity is tested; do not invert the 1bpp packing without updating tests
- Scene factories must be deterministic for regression tests; avoid wall-clock time or random data in test scenes
- `monitor_core` component lists shared files explicitly; add new `firmware/app/ui/*.c` or `firmware/src/display/*.c` files to its CMakeLists.txt
