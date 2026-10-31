# AGENTS.md

Guidance for coding agents working in this repository. Commands below assume the repository root unless a working directory is shown.

## Project overview

ESP32-Monitor comprises ESP32-S3 firmware for a 400×300 reflective LCD, a deterministic desktop simulator sharing the firmware UI, a desktop Client, and a NAS Server. The target has 16 MB flash and octal PSRAM; the UI uses LVGL v9.4.0.

- `firmware/`: ESP-IDF application, hardware drivers, shared display and UI code, and host and device tests.
- `simulator/`: desktop renderer, scene registry, and preview API. It builds the shared firmware UI, not a separate implementation.
- `client/`: Go Agent and Wails desktop application, Vue/TypeScript frontend, platform integrations and local test servers.
- `server/`: Go server, SQLite storage, embedded WebUI and CLI.
- `docs/`: current protocol, capabilities and release documents. Historical material in `.bak/` is not a current specification.

See `docs/CLIENT_CAPABILITIES.md`, `docs/FIRMWARE_CAPABILITIES_PLAN.md`, `docs/SERVER_CLIENT_PROTOCOL_V2.md`, `docs/ESP32_COMPANION_PROTOCOL_V1.md`, and `docs/SERVER_WEBUI.md` for details and the distinction between implemented and planned behavior. Server product V1 uses the Client/Server v2 protocol; the production server implements v2. The Client's `sim-server` is for local integration. ESP32/host companion communication is a separate protocol. Check the relevant code and protocol documents when changing an integration; other documents may lag implementation.

## Build and test

Initialize the LVGL submodule (`git submodule update --init --recursive`) before building the simulator. The simulator may also fetch cJSON v1.7.19 if the firmware's managed copy is unavailable.

### Simulator and firmware

```sh
./simulator/run.sh --list-scenes
./simulator/run.sh                    # build, test, render monitor
./simulator/run.sh activity           # render a registered scene
./simulator/run.sh animation-minute --animate
ctest --test-dir simulator/build --output-on-failure
./firmware/tests/run.sh               # host firmware tests
```

The simulator writes PNG previews, packed frames, raw data, digests and metadata under `simulator/output/latest/` by default. The default preview is `normal.png`; other scenes use `<scene>_normal.png`. `SIMULATOR_BUILD_DIR` and `SIMULATOR_OUTPUT_DIR` override the default paths.

For firmware, configure an ESP-IDF environment first:

```sh
source ~/esp/esp-idf/export.sh
idf.py -C firmware build
idf.py -C firmware/tests -B "$PWD/firmware/tests/build/esp32s3" build
```

Select the actual port before flashing or monitoring. The separate device test application replaces the production application when flashed; restore production firmware after running it. Host and simulator tests do not establish hardware behavior.

### Client

The Client is a multi-platform project. When fixing an issue on one platform, do not affect behavior on other platforms.

```sh
cd client
make build                            # Agent
make test                             # go test -race ./...
make check                            # go vet ./...
make test-collector-e2e
go run ./cmd/sim-server -listen 127.0.0.1:18080
```

For the frontend: `cd client/frontend && npm ci && npm test` (includes type checks, build and DOM tests). `make build-app-darwin` builds the macOS app; `client/scripts/build-windows-app.ps1` builds the Windows app. Platform builds and native WebView or hardware behavior require appropriate platform validation.

### Server

```sh
cd server
make build
make test-ut
make test-race
make vet
make test-api                         # integration-tagged HTTP tests
make test-v2                          # real Client Agent and Server loopback
```

`make test-tui` additionally requires the dependencies in `server/tests/e2e/requirements-tui.txt`. See `docs/SERVER_WEBUI.md` for server configuration and deployment.

## Shared UI and display contract

- `firmware/app/ui/` contains shared LVGL screens; `firmware/src/display/` contains shared rendering, damage tracking, device data and packing; public contracts are in `firmware/include/`.
- `firmware/components/monitor_core/CMakeLists.txt` and `simulator/CMakeLists.txt` explicitly list shared source files. Update both when adding shared sources. `third_party/lv_conf.h` is shared; do not create divergent LVGL configurations.
- Display output is 400×300, 1 bpp, 15,000 packed bytes, with bit 1 white and bit 0 black. Current shared rendering primarily uses LVGL I1; RGB565 conversion and export remain available. Preserve packing polarity and layout.
- Register deterministic preview scenes in `simulator/src/scene_registry.c`. Use fixed fixture data rather than wall-clock time or randomness, render the affected scenes, and inspect the output. Integrate production screens through the firmware application rather than implementing simulator-only UI.
- Changes to shared code affect both independent builds; run relevant simulator/host tests and the firmware build when the ESP-IDF environment is available.

## Firmware boundaries and configuration

`firmware/main/main.c` is the application entry point. `firmware/components/firmware/` contains display, clock, RTC, sensors, weather, device transport, power and Wi-Fi integration. `firmware/components/esp_wifi_connect/` implements provisioning; `firmware/components/lvgl/` adapts LVGL to ESP-IDF. `firmware/sdkconfig.defaults` specifies target defaults and `firmware/partitions.csv` defines the partition layout. Inspect the generated `sdkconfig` as well as defaults when diagnosing a build.

On first boot without saved Wi-Fi credentials the device starts its provisioning AP; BOOT (GPIO0) and KEY (GPIO18) provide a reconfiguration gesture. The configuration page also supports weather settings. See the implementation and firmware capability document for current behavior before changing provisioning or networking.


**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

---

**These guidelines are working if:** fewer unnecessary changes in diffs, fewer rewrites due to overcomplication, and clarifying questions come before implementation rather than after mistakes.
